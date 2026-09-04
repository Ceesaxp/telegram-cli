package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/clipboard"
	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/keys"
	"github.com/Ceesaxp/telegram-cli/internal/notification"
	"github.com/Ceesaxp/telegram-cli/internal/render"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/attach"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/auth"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatview"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/contacts"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/dialog"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/help"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/hintbar"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/mediaview"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/palette"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/rail"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/reactionpicker"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/search"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/topbar"
	"github.com/Ceesaxp/telegram-cli/internal/ui/layout"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// paletteTopMargin is how far down the command palette sits, per
// docs/tui-2.0.md: "positioned about eight rows from the top". Anchored
// rather than centred so the chat it acts on stays readable behind it.
const paletteTopMargin = 8

type Model struct {
	auth      auth.Model
	chatList  chatlist.Model
	chatView  chatview.Model
	composer  composer.Model
	contacts  contacts.Model
	search    search.Model
	help      help.Model
	palette   palette.Model
	reactions reactionpicker.Model
	attach    attach.Model
	topBar    topbar.Model
	hintBar   hintbar.Model
	rail      rail.Model
	mediaView mediaview.Model

	// mediaTeardown is what the terminal must be told to forget once the
	// media overlay closes. Held rather than written: this Model has no
	// output of its own, so it rides out on the next frame and View drains
	// it. Empty for every protocol but kitty, whose images the terminal
	// owns and a text redraw does not erase.
	mediaTeardown string

	// noticeAt is when the hint bar's transient notice was raised, so the
	// chrome tick can give the row back after four seconds. Zero means
	// there is nothing to expire.
	noticeAt time.Time

	// pendingNotices are notifications waiting on the client to learn who
	// the chat is, and therefore whether it is muted. See notice.go.
	pendingNotices []pendingNotice

	// deviceCount is how many sessions are authorised on this account.
	// Zero means "not answered yet", which is always what zero means here:
	// every account has at least the session doing the asking.
	deviceCount int
	dialog      *dialog.Model

	screen     ScreenState
	focus      FocusPanel
	layout     layout.Layout
	tg         *telegram.Client
	store      *store.Store
	config     *config.Config
	roles      theme.Roles
	notifier   *notification.Notifier
	sound      *notification.SoundPlayer
	authorizer *telegram.TUIAuthorizer
	width      int
	height     int
	myUserId   int64

	// pasteInFlight is set while a clipboard paste command is running, so a
	// second Ctrl+V cannot start a racing paste.
	pasteInFlight bool

	// railOpen is whether the user wants the context rail. Whether it is
	// actually drawn is layout's decision — below 118 columns there is no
	// room for it and the preference is kept rather than overwritten, so
	// widening the terminal brings it back.
	railOpen bool

	// fatalError holds the reason the Telegram client died for good — a
	// telegram.ClientErrorMsg with Terminal set, or the authorizer
	// reporting telegram.AuthStateClosed. While it is set the whole UI is
	// replaced by an error panel.
	//
	// It has to be that loud. The failure it reports is silent by nature:
	// the run loop exits, no further updates arrive, and every panel keeps
	// rendering the last data it had. The app looks perfectly alive while
	// messages typed into it go nowhere. A notice in the composer would be
	// missed, and leaving the panels on screen would keep inviting input
	// that cannot be delivered.
	//
	// Never cleared: the run loop does not come back, so only restarting
	// the process recovers.
	fatalError string

	// keys holds the resolved (config-normalized, defaulted) key bindings
	// app.go itself dispatches on. See resolveKeys.
	keys resolvedKeys

	// pendingDeleteChatId/pendingDeleteMessageId capture the message
	// targeted by handleMessageAction's "delete" case for the duration of
	// the confirm dialog, since dialog.DialogResultMsg carries no payload
	// of its own to identify what was being confirmed.
	pendingDeleteChatId    int64
	pendingDeleteMessageId int64
}

// resolvedKeys holds the subset of config.KeyConfig that app.go dispatches
// on directly, after normalization, defaulting and collision refusal. See
// the doc comment on config.KeyConfig for the one rule they all follow.
type resolvedKeys struct {
	quit         string
	quitBrowsing string
	search       string
	globalSearch string
	contacts     string
	compose      string
	help         string
	nextFolder   string
	prevFolder   string
	nextChat     string
	prevChat     string
	nextUnread   string

	// The chat view's own bindings. app.go does not dispatch on these —
	// it normalizes and defaults them here and hands them to chatview via
	// SetKeys in New, which is what ends config.toml advertising fields
	// nothing reads.
	//
	// These are the values going IN. What comes back out of
	// chatview.ActiveKeys is what the panel actually accepted, and that
	// is what the help card must quote — see helpSections.
	reply         string
	editMessage   string
	deleteMessage string
	markRead      string
}

// resolveKeys turns config.toml's [keys] table into the bindings the
// dispatcher matches, under decision I-13's single rule: a value replaces
// the default, and a value that collides with something already bound is
// refused rather than allowed to shadow it.
//
// Resolution runs in the same three passes chatview.SetKeys uses, and for
// the same reason: explicit configuration has to outrank a built-in default
// regardless of field order.
//
//  1. Claim what the app hardcodes (keys.AppFixed).
//  2. Take every EXPLICITLY configured field, in field order, claiming as
//     it goes. A value already claimed is refused outright and the field
//     falls through to pass 3 as if it had not been set.
//  3. Every field without an accepted value takes its default, unless that
//     is claimed too — in which case the action is left with no key at all
//     rather than sharing one. The help card renders that as "(unbound)".
//
// The refusals are not silent: config.StartupWarnings reports them before
// the TUI takes the screen. Nothing here can happen with a default config,
// where no two fields and no fixed key share a spelling.
func resolveKeys(kc config.KeyConfig) resolvedKeys {
	claimed := map[string]bool{}
	for _, k := range keys.AppFixed {
		claimed[k] = true
	}

	// quit is settled first and separately: config refuses a bare
	// printable there outright, because quit is matched ahead of every
	// focus gate and a letter bound to it is a letter that cannot be typed
	// anywhere in the client (see config.ResolveQuitKey).
	quit, _ := config.ResolveQuitKey(kc.Quit)
	claimed[quit] = true

	// The fields, in two tiers. Within a tier, pass 2 (everything the user
	// set explicitly) runs before pass 3 (defaults), so an explicit value
	// outranks a default regardless of field order — order is only a
	// tie-break between two explicit settings, which is a situation the
	// user has already made ambiguous.
	//
	// The tiers themselves are not a preference, they are the dispatch
	// order: app-level matching runs before the focused panel sees the
	// event, so a chat-view binding pointed at an app-level one is not
	// ambiguous, it is dead. reply = "q" was accepted, advertised on the
	// help card as Reply, and quit the application when pressed. Resolving
	// the app's tier first is what makes that refusable here rather than
	// discovered by pressing it.
	out := resolvedKeys{quit: quit}
	appTier := []keyField{
		{kc.QuitBrowsing, "q", &out.quitBrowsing},
		{kc.Search, "/", &out.search},
		{kc.GlobalSearch, "ctrl+g", &out.globalSearch},
		{kc.Contacts, "c", &out.contacts},
		{kc.Compose, "i", &out.compose},
		{kc.Help, "?", &out.help},
		{kc.NextChat, "J", &out.nextChat},
		{kc.PrevChat, "K", &out.prevChat},
		{kc.NextUnread, "u", &out.nextUnread},
		{kc.NextFolder, "]", &out.nextFolder},
		{kc.PrevFolder, "[", &out.prevFolder},
	}
	// The chat view's own. Resolved here so config.toml, the help card and
	// the panel describe one keymap; chatview.SetKeys resolves them again
	// against the keys the panel itself owns, and ActiveKeys is what the
	// card must quote.
	chatViewTier := []keyField{
		{kc.Reply, "r", &out.reply},
		{kc.EditMessage, "e", &out.editMessage},
		{kc.DeleteMessage, "d", &out.deleteMessage},
		{kc.MarkRead, "m", &out.markRead},
	}

	resolveTier(claimed, appTier)
	resolveTier(claimed, chatViewTier)
	return out
}

// keyField is one row of the resolution table: what the file said, what the
// client falls back to, and where the answer goes.
type keyField struct {
	configured string
	def        string
	out        *string
}

// resolveTier runs decision I-13's two passes over one tier of fields,
// claiming as it goes.
//
// A field left empty is UNREACHABLE, not unbound-to-nothing-in-particular:
// keys.Press.Matches treats "" as never matching, so the action is inert
// rather than firing on somebody else's key, and the help card renders it as
// "(unbound)" so the user can see which binding to free.
func resolveTier(claimed map[string]bool, fields []keyField) {
	accepted := make([]bool, len(fields))
	for i, f := range fields {
		key := config.NormalizeKey(f.configured)
		if key == "" || claimed[key] {
			continue
		}
		claimed[key] = true
		*f.out = key
		accepted[i] = true
	}
	for i, f := range fields {
		if accepted[i] || claimed[f.def] {
			continue
		}
		claimed[f.def] = true
		*f.out = f.def
	}
}

func New(cfg *config.Config, tg *telegram.Client, s *store.Store, authorizer *telegram.TUIAuthorizer) Model {
	// Colour depth is resolved once, here, from the environment only —
	// never by querying the terminal, whose reply would arrive as
	// keystrokes. See theme.SupportsTrueColor.
	roles := theme.RolesFor(cfg.UI.Theme, theme.SupportsTrueColor())
	m := Model{
		auth:       auth.New(roles, authorizer),
		chatList:   chatlist.New(s, tg, roles),
		chatView:   chatview.New(s, tg, roles),
		composer:   composer.New(roles),
		contacts:   contacts.New(s, tg, roles),
		search:     search.New(s, tg, roles),
		help:       help.New(roles),
		palette:    palette.New(roles),
		attach:     attach.New(roles),
		reactions:  reactionpicker.New(roles),
		topBar:     topbar.New(roles),
		hintBar:    hintbar.New(roles),
		rail:       rail.New(roles),
		mediaView:  mediaview.New(roles),
		screen:     ScreenLoading,
		focus:      PanelChatList,
		tg:         tg,
		store:      s,
		config:     cfg,
		roles:      roles,
		notifier:   notification.NewNotifier(cfg.Notifications.Enabled, cfg.Notifications.ShowPreview, cfg.Notifications.Method),
		sound:      notification.NewSoundPlayer(cfg.Notifications.Sound),
		authorizer: authorizer,
		keys:       resolveKeys(cfg.Keys),
	}
	// Process-wide and set before the first render, like lipgloss's colour
	// profile: it describes the terminal this process is attached to, and
	// every panel that measures a string has to agree about it. A
	// declaration that only the top bar honoured would close the gap there
	// and leave the chat titles sheared.
	cell.SetEmojiMode(cell.ParseEmojiMode(config.ResolveEmojiWidth(cfg.UI.EmojiWidth)))

	m.chatView.ApplyMedia(cfg.Media)
	m.chatView.ApplyUI(cfg.UI)
	m.chatView.ApplyStorage(cfg.Storage)
	m.mediaView.ApplyMedia(cfg.Media)
	m.composer.SetEditingMode(composerEditingMode(cfg.UI.ComposeEditing))
	// Order matters: the chat view has to know what app.go has already
	// claimed BEFORE it resolves what the user configured, or it will
	// accept a binding that can never reach it. reply = "q" quitting the
	// application is what that looked like.
	m.chatView.SetReservedKeys(m.reservedKeys())
	// The chat view implements these itself, but it must not also decide
	// them: handing it the resolved values keeps the panel, the help card
	// and config.toml describing one keymap instead of three.
	m.chatView.SetKeys(chatview.Keys{
		Reply:    m.keys.reply,
		Edit:     m.keys.editMessage,
		Delete:   m.keys.deleteMessage,
		MarkRead: m.keys.markRead,
	})
	// Built once from the resolved bindings and the resolved editing mode:
	// neither changes after startup, so there is nothing to keep in sync
	// afterwards. Were bindings ever to become rebindable while running,
	// all four of these would have to be re-set with them.
	m.help.SetSections(m.helpSections())
	m.help.SetFooter(m.helpFooter())
	// The palette reads the same resolved bindings, so a rebound key shows
	// correctly in its right-hand column rather than advertising a default
	// the user replaced.
	m.palette.SetItems(m.paletteItems())
	m.rail.SetStore(s, tg)
	// The rail's default visibility is the user's preference; the backtick
	// toggles it from there. Decision 6: opening it is what starts the
	// fetching, so a config that leaves it off costs nothing at all.
	m.railOpen = cfg.UI.Rail
	m.composer.SetParseMarkdown(cfg.UI.ParseMarkdown)
	return m
}

// composerEditingMode maps the resolved ui.compose_editing setting onto the
// composer's editing mode. config owns the resolution (including the "auto"
// inference from $VISUAL/$EDITOR) so it can be tested without a composer;
// this is only the translation into the composer's own type.
func composerEditingMode(setting string) composer.EditingMode {
	if config.ResolveComposeEditing(setting) == config.ComposeEditingVi {
		return composer.ModeVi
	}
	return composer.ModeEmacs
}

var (
	// errNoChatOpen backs the submit guard: nothing can be sent before a
	// chat is selected.
	errNoChatOpen = errors.New("open a chat first")
	// errEditDroppedAttachment reports an attachment that reached an edit,
	// which can only carry text.
	errEditDroppedAttachment = errors.New("edits cannot carry an attachment — it was discarded")
)

// noticeEditAttach is shown when an attach action is refused during an edit.
const noticeEditAttach = "⚠ cannot attach while editing"

// Init starts the chrome tick. Without it the top bar's clock would show
// the time of the last window resize for the rest of the session, and a
// transient notice would own the hint bar until something replaced it.
func (m Model) Init() tea.Cmd { return chromeTick() }

// Update handles one message and then reconciles the frame with whatever it
// did.
//
// The reconciliation is here rather than at the end of the switch because
// the switch has sixty-five early returns, and a step that only runs when
// the code happens to fall out of the bottom is a step that runs for most
// messages and silently not for the ones that matter.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)

	updated, ok := next.(Model)
	if !ok {
		return next, cmd
	}

	// The composer changes height on its own — a reply bar, an attachment
	// chip, a notice, the expanded form — and the frame budgets its rows
	// from the layout rather than from the composer. One check, on the way
	// out of every update, because the alternative is every path that can
	// change the composer's shape remembering to say so, and one of them
	// forgetting.
	//
	// That is not hypothetical: it is why r opened a reply with no input
	// row. EnterReplyMode grew the composer to two rows, the layout still
	// budgeted one, and the frame drew the reply bar and cut off the line
	// you were meant to type into. It came back on the next resize, which
	// is why editing in $EDITOR and returning appeared to fix it.
	if updated.layoutStale() {
		updated.updateLayout()
	}
	return updated, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case tea.KeyPressMsg:
		// Bindings are matched through keyPress, not a bare msg.String():
		// see the type's doc comment for why String() alone misses
		// alt-modified keys on macOS.
		key := keys.NewPress(msg)

		// Quit
		// One chord, not three. ctrl+c was the third spelling of an action
		// that already had two, and the one most likely to be pressed by
		// accident — it is also the chord a terminal user reaches for to
		// abandon a command rather than to close an application.
		if key.Matches("ctrl+q", m.keys.quit) {
			return m, tea.Quit
		}

		// A dead client makes every other binding a lie: the panels behind
		// the error panel are not even visible, so acting on their keys
		// would only mutate state the user cannot see.
		if m.fatalError != "" {
			return m, nil
		}

		// Any keypress means at least one frame has been rendered since
		// the overlay closed, so the teardown has been emitted. Kitty
		// reads a second delete of the same id as a no-op, but carrying it
		// forever would prepend a sequence to every frame for the rest of
		// the session.
		m.mediaTeardown = ""

		if m.screen == ScreenMain {
			// The palette owns the keyboard outright while it is open,
			// ahead of every other overlay. It is a text surface: every
			// printable has to reach the query or a command whose name
			// contains that letter could never be typed. Nothing below
			// runs until it closes.
			// The reaction row owns the keyboard while it is open. It is
			// twelve choices and two ways out; anything that fell through
			// to the thread underneath would act on the message the row is
			// asking about.
			if m.reactions.IsVisible() {
				var cmd tea.Cmd
				m.reactions, cmd = m.reactions.Update(msg)
				return m, cmd
			}

			// The attach picker owns the keyboard on the same terms as
			// the palette, and ahead of it: it is a text surface whose
			// printables build a path, so a filename containing any
			// bound letter has to be typeable.
			if m.attach.IsVisible() {
				var action attach.Action
				m.attach, action = m.attach.Update(msg)
				switch action {
				case attach.ActionCancel:
					m.attach.Close()
				case attach.ActionAttach:
					if path, asPhoto, ok := m.attach.Chosen(); ok {
						m.attach.Close()
						return m.stageAttachment(path, asPhoto)
					}
				}
				return m, nil
			}

			if m.palette.IsVisible() {
				var action palette.Action
				m.palette, action = m.palette.Update(msg)
				switch action {
				case palette.ActionCancel:
					m.palette.Close()
				case palette.ActionRun:
					line := m.palette.Query()
					m.palette.Close()
					updated, cmd, notice := m.runCommandLine(line)
					m = updated
					if notice != "" {
						m.notify(notice)
					}
					return m, cmd
				}
				return m, nil
			}

			// The media overlay covers the frame, so it owns the
			// keyboard for the same reason the help card does: a key
			// that reached a panel behind it would take effect
			// invisibly. Three keys work — the way out, and the two
			// the overlay's own hint row advertises.
			if m.mediaView.IsVisible() {
				switch {
				// Esc, not q (decision I-8). q closed the overlay and,
				// one keystroke later, quit the application — the same
				// double-press that made the help card an exit.
				case key.Matches("esc"):
					m.mediaTeardown += m.mediaView.Close()
					return m, nil
				case key.Matches("s"):
					return m, m.chatView.DownloadCmd()
				case key.Matches("o"):
					return m, m.chatView.OpenExternallyCmd()
				}
				return m, nil
			}

			// The help overlay is a modal reference card. While it is up
			// it owns the keyboard: closing keys are handled here (the
			// component never closes itself), scrolling goes to the
			// component, and everything else is swallowed. Swallowing
			// rather than falling through matters — the panels are hidden
			// behind the overlay, so a key that changed one would take
			// effect invisibly.
			if m.help.IsVisible() {
				// Esc, or the help key itself. NOT q: it closed the card
				// and then quit, so "?qq" was an exit nobody meant to
				// type (decision I-8). q has one meaning.
				if key.Matches("esc", m.keys.help) {
					m.help.SetVisible(false)
					return m, nil
				}
				var cmd tea.Cmd
				m.help, cmd = m.help.Update(msg)
				return m, cmd
			}

			// A dialog is modal, and modal has to mean it for the
			// keyboard too. It owns every key from here down: esc
			// cancels, arrows (or tab) choose a button, enter
			// accepts, and a prompt's input takes the printables.
			// j / k are deliberately not button movement: the row is
			// horizontal, and a reflexive j after opening a delete
			// confirm must not arm Confirm.
			//
			// One break rather than a dialog check on each binding.
			// Gating them individually is how this was wrong three
			// separate ways at once — tab cycled panel focus behind the
			// modal (and tab is the FIRST key the dialog's own hint line
			// advertises, so the one advertised key did nothing), the
			// first esc moved focus and only a second one cancelled, and
			// alt+1/2/3 and f1-f3 each moved focus with the dialog still
			// up. Every one of those was a binding someone forgot to
			// gate, and there is no reason to believe the next one would
			// be remembered either.
			//
			// Breaking rather than returning hands the event to the
			// sub-model dispatch at the bottom of Update, where
			// blockedByDialog already routes input to the dialog alone —
			// the same yield chatview's find and chatlist's filter use.
			// ctrl+q / keys.quit are unaffected: they are
			// matched above this whole block, and a modal must never be
			// able to trap someone in the program.
			if m.dialog != nil {
				break
			}

			// In-chat search (chatview's ctrl+f) owns the keyboard while
			// its input line is open: esc closes the search rather than
			// moving focus, and printables are query text, not quick-type.
			// Break out of the key switch so the focused-panel dispatch
			// below hands the event to chatview untouched — the same yield
			// the composer performs for esc while composing.
			if m.focus == PanelChatView && m.chatView.SearchActive() {
				break
			}
			// The chat list's local filter is the same arrangement on the
			// other side: while its input line is open it owns the
			// keyboard, esc closes the filter rather than moving focus,
			// and printables are filter text. Yield to chatlist.
			if m.focus == PanelChatList && m.chatList.FilterActive() {
				break
			}
			// After the input closes, n/N cycle the surviving hits. They
			// are plain printables, so this must precede quick-type (and
			// chatview's own "n"-less default handling) to reach chatview.
			// Only the unmodified keys yield; alt+n and friends stay
			// available to app-level bindings.
			if m.focus == PanelChatView && m.chatView.HasSearchResults() &&
				key.Matches("n", "N") {
				break
			}

			// Tab / Shift+Tab cycle panels, from the composer as well as
			// the browsing panels — the help card advertises tab as global,
			// and a literal tab in a chat message is rare enough that
			// cycling is the more useful meaning. (Shift+Tab already
			// worked from the composer.) The search overlay keeps tab for
			// its own use.
			if key.Matches("tab") && m.focus != PanelSearch {
				switch m.focus {
				case PanelChatList:
					m.setFocus(PanelChatView)
				case PanelChatView:
					m.setFocus(PanelComposer)
				default:
					m.setFocus(PanelChatList)
				}
				return m, nil
			}
			if key.Matches("shift+tab") && m.focus != PanelSearch {
				switch m.focus {
				case PanelComposer:
					m.setFocus(PanelChatView)
				case PanelChatView:
					m.setFocus(PanelChatList)
				default:
					m.setFocus(PanelComposer)
				}
				return m, nil
			}

			// Escape: close overlay or go back
			if key.Matches("esc") {
				if m.search.IsVisible() {
					m.search.SetVisible(false)
					m.setFocus(PanelChatList)
					return m, nil
				}
				if m.contacts.IsVisible() {
					m.contacts.SetVisible(false)
					m.setFocus(PanelChatList)
					return m, nil
				}
				if m.focus == PanelComposer {
					// A composer in reply/edit mode, or holding a pending
					// attachment, clears that first — it handles the key
					// itself below instead of losing focus.
					if !m.composer.IsComposing() {
						m.setFocus(PanelChatView)
						return m, nil
					}
					break
				}
				if m.focus != PanelChatList {
					m.setFocus(PanelChatList)
					return m, nil
				}
			}

			// Panel focus has no keys of its own any more (decision I-1).
			// alt+1/2/3 and f1-f3 are gone: the alt spellings only reached the
			// app on a terminal configured to report Option as a modifier —
			// silently, undetectably not the case on most macOS terminals —
			// and the function keys existed only as their fallback. h, l, i,
			// Esc and Tab cover the three panels between them.

			noOverlay := m.dialog == nil && !m.search.IsVisible() && !m.contacts.IsVisible()

			// Colon opens the command palette from NORMAL and from VI —
			// that is vim's own muscle memory and it stays (decision
			// I-12). The mode resolver is the authority, so a focused
			// emacs composer (INSERT) types a colon as text while a vi
			// composer in its command state opens the palette. Consulting
			// the mode rather than the focus panel is what keeps that
			// distinction honest.
			if key.Matches(":") && noOverlay &&
				(m.Mode() == ModeNormal || m.Mode() == ModeVi) {
				m.palette.Open()
				return m, nil
			}

			// The context rail. From NORMAL only — unlike the colon,
			// which vi users expect from their command state as well. A
			// backtick is a character somebody may well want to type into
			// a message, and the composer owns it there.
			if key.Matches("`") && noOverlay && m.Mode() == ModeNormal {
				return m, m.toggleRail()
			}

			// browsing is the two panels that are neither a text surface
			// nor an overlay: the chat list and the chat view. Bare-letter
			// bindings are only safe there.
			browsing := noOverlay &&
				(m.focus == PanelChatList || m.focus == PanelChatView)

			// Folder cycling, from BOTH browsing panels (decision I-1).
			// It was alt+h / alt+l at app level and [ / ] inside the chat
			// list — one behaviour with two implementations, of which the
			// reachable-everywhere one was the one that mostly did not
			// work. [ and ] are the whole binding now, at app level, and
			// the chat list keeps only the arrows and the digits, which
			// are its own.
			if key.Matches(m.keys.prevFolder) && browsing {
				m.chatList.CycleFolder(-1)
				return m, m.chatList.FolderLoadCmd()
			}
			if key.Matches(m.keys.nextFolder) && browsing {
				m.chatList.CycleFolder(1)
				return m, m.chatList.FolderLoadCmd()
			}

			// J / K open the next and previous chat outright. That is the
			// point of them, and what separates them from the chat list's
			// own j / k, which move the cursor and open nothing (I-2):
			// this is "take me to the next conversation", not "look down
			// the list". They were alt+j / alt+k.
			if key.Matches(m.keys.nextChat) && browsing {
				if chatID, ok := m.chatList.SelectDelta(1); ok {
					return m, func() tea.Msg { return chatlist.ChatSelectedMsg{ChatId: chatID} }
				}
				return m, nil
			}
			if key.Matches(m.keys.prevChat) && browsing {
				if chatID, ok := m.chatList.SelectDelta(-1); ok {
					return m, func() tea.Msg { return chatlist.ChatSelectedMsg{ChatId: chatID} }
				}
				return m, nil
			}

			// u opens the next chat with unread messages: down from the
			// cursor within the active folder, wrapping once. The chat
			// list footer advertised this key for a whole release with
			// nothing bound to it, which is how decision I-6 came to
			// require every hint to be derived rather than written.
			//
			// With nothing unread it says so rather than moving. A key
			// that silently does nothing is a key people learn is broken.
			if key.Matches(m.keys.nextUnread) && browsing {
				chatID, ok := m.chatList.SelectNextUnread()
				if !ok {
					m.notify("no unread chats")
					return m, nil
				}
				return m, func() tea.Msg { return chatlist.ChatSelectedMsg{ChatId: chatID} }
			}

			// Bare h/l move between panels — the lazygit reading of
			// left/right, and the one a two-column layout invites: l from
			// the chat list enters the chat view, h from the chat view
			// goes back.
			//
			// l takes the CURSORED chat with it (decision I-2). The cursor
			// was decoupled from the open chat so that j would not load a
			// history per press, which stands; what did not stand was l and
			// i acting on the OPEN chat while the cursor sat somewhere else,
			// so that jjjl landed in the wrong conversation. Enter has
			// always opened what the cursor is on, and now means exactly
			// what l means.
			//
			// One step at a time, like the esc ladder: h at the left edge
			// and l at the right edge are no-ops, not wraps, and neither
			// ever reaches the composer — typing is entered deliberately
			// or not at all. The edge cases are still consumed, so no
			// panel underneath can give the key a second meaning.
			if key.Matches("l") && browsing {
				if m.focus == PanelChatList {
					if cmd, moved := m.openCursoredChat(); moved {
						// openChatAt, reached through the message, focuses
						// the chat view itself.
						return m, cmd
					}
					m.setFocus(PanelChatView)
				}
				return m, nil
			}
			if key.Matches("h") && browsing {
				if m.focus == PanelChatView {
					m.setFocus(PanelChatList)
				}
				return m, nil
			}

			// q quits from the browsing panels (config.KeyConfig.
			// QuitBrowsing, default "q") — lazygit's "q is the way out"
			// everywhere q cannot be text. The composer owns printables
			// and is unaffected; ctrl+q stays global; q still
			// closes the help overlay, which is handled above and returns
			// before reaching here.
			//
			// Work in the composer is not dropped on a single keystroke:
			// an unsent draft or a pending attachment routes through the
			// same confirm dialog a delete uses. See quitConfirming, which
			// :quit shares so the two cannot drift (decision I-5).
			if key.Matches(m.keys.quitBrowsing) && browsing {
				return m.quitConfirming()
			}

			// Search. Vi convention makes "/" mean "find in the buffer I am
			// looking at", and both browsing panels now honor it: from the
			// chat view "/" opens chatview's in-chat find, and from the
			// chat list it opens chatlist's local filter over the chats in
			// the active folder. Everywhere else "/" is the global message
			// search, and GlobalSearch (ctrl+g) reaches the global overlay
			// from any panel including the two that claim "/".
			//
			// OpenFind/OpenFilter are called directly rather than
			// re-emitting a synthetic ctrl+f key event. Re-emission
			// livelocks the command loop the moment a user configures
			// keys.search = "ctrl+f": the forwarded key re-matches this
			// very binding and is forwarded again, forever.
			//
			// All three are gated on noOverlay: an open overlay owns its
			// own text input, and without the gate "/" could never be
			// typed into the global search query or the contacts filter.
			if key.Matches(m.keys.search) && m.focus == PanelChatView && noOverlay {
				m.chatView.OpenFind()
				return m, nil
			}
			if key.Matches(m.keys.search) && m.focus == PanelChatList && noOverlay {
				m.chatList.OpenFilter()
				return m, nil
			}
			if key.Matches(m.keys.search, m.keys.globalSearch) &&
				m.focus != PanelComposer && noOverlay {
				m.search.SetVisible(true)
				m.setFocus(PanelSearch)
				return m, nil
			}

			// Help overlay. Not from the composer: the composer owns
			// printables, and "?" is a character someone will want to
			// type. Every other panel has no use for a bare "?".
			if key.Matches(m.keys.help) && m.focus != PanelComposer && noOverlay {
				m.help.SetVisible(true)
				return m, nil
			}

			// Contacts toggle, on a plain letter now (decision I-1). alt+c
			// and its f4 fallback are gone; ctrl+k was considered years ago
			// and rejected, because the composer's textarea binds it to
			// readline kill-to-end-of-line and app-level bindings are checked
			// before the focused panel sees the key.
			//
			// Gated on the browsing panels, plus the contacts overlay itself
			// so the key that opens it can close it. It no longer fires from
			// a focused composer: alt+c was not a character, and "c" is —
			// rule 1 says a printable belongs to whoever owns typing.
			if key.Matches(m.keys.contacts) && (browsing || m.focus == PanelContacts) &&
				m.dialog == nil && !m.search.IsVisible() {
				m.contacts.SetVisible(!m.contacts.IsVisible())
				if m.contacts.IsVisible() {
					m.setFocus(PanelContacts)
					return m, m.contacts.LoadContacts()
				}
				m.setFocus(PanelChatList)
				return m, nil
			}

			// Ctrl+V: attach the clipboard image to the open chat, from
			// whichever panel has focus.
			if key.Matches("ctrl+v") && m.dialog == nil && !m.search.IsVisible() &&
				!m.contacts.IsVisible() && m.chatList.ActiveChatId() != 0 {
				// An edit cannot carry an attachment — pasting one would be
				// silently dropped at submit time.
				if m.composer.IsEditing() {
					m.notify(noticeEditAttach)
					return m, nil
				}
				// One paste at a time: concurrent pastes race to set the
				// attachment and leak the losing spool file.
				if m.pasteInFlight {
					return m, nil
				}
				m.pasteInFlight = true
				m.setFocus(PanelComposer)
				m.notify("pasting from clipboard...")
				// Capture the active chat now — the paste runs async, and
				// the user may switch chats before it lands.
				return m, pasteFromClipboard(m.composer.ChatId())
			}

			// Compose (keys.compose, default i): move focus to the composer,
			// opening the cursored chat first when it is not already the open
			// one (decision I-2). Every key that leaves the chat list
			// rightward takes the cursor with it.
			//
			// Typing is always an explicit move. The app used to forward any
			// printable key straight to the composer, which made every
			// single-character binding a trade-off against being able to
			// start a message with that character, and made the reverse
			// mistake — a stray keystroke silently becoming message text —
			// just as easy.
			//
			// From the chat view the condition is the composer's own chat id:
			// it is what submit sends to. From the chat list it is "there is
			// a chat under the cursor", because opening it is the first half
			// of what the key does.
			if key.Matches(m.keys.compose) && browsing {
				if m.focus == PanelChatList {
					cmd, _ := m.openCursoredChat()
					// Nothing to send to at all — an empty or unloaded
					// list and no open chat — would only show the
					// composer's "open a chat first", so the key is
					// better left inert.
					if m.chatList.CursorChatId() == 0 && m.composer.ChatId() == 0 {
						return m, nil
					}
					m.setFocus(PanelComposer)
					return m, cmd
				}
				if m.composer.ChatId() == 0 {
					return m, nil
				}
				m.setFocus(PanelComposer)
				return m, nil
			}
		}

	case tea.MouseClickMsg:
		if m.screen == ScreenMain {
			// A click lands on whatever is drawn under the help card, which
			// the user cannot see. Dropped rather than routed: the card has
			// nothing clickable.
			if m.help.IsVisible() {
				return m, nil
			}
			return m.handleMouseClick(msg)
		}

	case tea.MouseWheelMsg:
		if m.screen == ScreenMain {
			// Dropped, not forwarded: scrolling the chat that is hidden
			// behind the card would be invisible, and help exposes no
			// scroll entry point of its own (its Update takes key events
			// only). Keyboard scrolling inside the card still works.
			if m.help.IsVisible() {
				return m, nil
			}
			return m.handleMouseWheel(msg)
		}

	case AuthStateChangedMsg:
		return m.handleAuthStateChanged(msg)

	case AuthErrorMsg:
		m.setScreen(ScreenAuth)
		m.auth.SetStep(auth.StepPhone)
		m.auth.SetError(msg.Err.Error())
		return m, nil

	case AuthenticatedMsg:
		m.setScreen(ScreenMain)
		m.myUserId = msg.UserId
		m.chatView.SetMyUserId(msg.UserId)
		m.chatList.SetMyUserID(msg.UserId)
		// We are authorized and the client works — show Connected
		// directly instead of relying on connection-state event timing.
		m.topBar.SetConnection(topBarConnState(telegram.ConnectionStateReady))
		m.setFocus(PanelChatList)
		m.updateLayout()
		// The device count is asked for once, here, and held. Sessions are
		// created and revoked by hand on the scale of days, so polling for
		// it would spend requests watching a number that does not move.
		return m, tea.Batch(m.chatList.Init(), m.deviceCountCmd())

	case chromeTickMsg:
		m.expireNotice(time.Time(msg))
		m.refreshChrome()
		return m, chromeTick()

	case deviceCountMsg:
		// Zero is the answer when the lookup failed, and zero drops the
		// cell — the same state as "not asked yet", which is what a
		// failure leaves the user in. No notice either: nobody asked for
		// this number, so failing to get it is not an event in their day.
		m.deviceCount = int(msg)
		// Refreshed here rather than left to the next tick: the answer is
		// worth up to a second of nobody noticing it appear, but not worth
		// the reader wondering whether it is coming.
		m.refreshChrome()
		return m, nil

	case telegram.ConnectionStateMsg:
		// The dot is the only standing statement about whether this client
		// is talking to Telegram, so every connection-state change has to
		// reach it. This used to be consumed by the status bar, which was
		// built but never drawn — so a reconnect was tracked and shown to
		// nobody. AuthenticatedMsg still sets Connected outright rather
		// than waiting for one of these, because the client demonstrably
		// works by then and the event timing is not guaranteed.
		m.topBar.SetConnection(topBarConnState(msg.State))
		return m, nil

	case telegram.ClientErrorMsg:
		// Terminal means the run loop has exited: nothing will arrive
		// again and nothing sent will leave. Anything else is a failure
		// the client survived, so it gets the same transient notice as any
		// other recoverable error.
		if msg.Terminal {
			return m.enterFatalError(clientErrorReason(msg.Err)), nil
		}
		m.notify(fmt.Sprintf("⚠ telegram: %v", msg.Err))

	case telegram.ClientWarningMsg:
		// A permanent but non-fatal degradation — the client works, just
		// with less capability. Worth telling the user once.
		m.notify(fmt.Sprintf("⚠ %s", msg.Text))

	case telegram.NewMessageMsg:
		// Never notify for our own messages — they arrive as updates too
		// when sent from another device.
		if !msg.Message.IsOutgoing && msg.Message.ChatID != m.chatList.ActiveChatId() {
			body := "New message received"
			if text, ok := msg.Message.Content.(*telegram.MessageText); ok {
				body = text.Text.Text
			}

			entry, ok := m.store.Chats.Get(msg.Message.ChatID)
			switch {
			case !ok || entry.Chat == nil || entry.Unresolved:
				// Nothing is known about this chat yet, including whether
				// it is muted. Deciding now means guessing, and the guess
				// that rings is wrong for exactly the chats a reader
				// silenced on purpose — so the notification waits for the
				// fetch the chat list is about to issue. See notice.go.
				cmds = append(cmds, m.holdNotice(msg.Message.ChatID, body))
			case entry.Chat.Muted:
				// The answer is known, and it is no.
			default:
				cmds = append(cmds, m.postNotice(entry.Chat.Title, body))
			}
		}

	case noticeGraceMsg:
		cmds = append(cmds, m.releaseAllNotices())

	case telegram.ChatUpdateMsg:
		// The answer to "is this chat muted" arrives here, for a chat a
		// notification may be waiting on. Read off the message rather than
		// the store: this switch runs ahead of the chat list that does the
		// storing, so the store still holds the unresolved stub.
		cmds = append(cmds, m.releaseNotices(msg.Chat))

	case openDiscussionMsg:
		// Straight to the post's copy in the linked group, the same way a
		// search result opens: the comments hang off it, and the top of
		// the group is not where the reader was going.
		cmds = append(cmds, m.openChatAt(msg.ChatId, msg.MessageId))

	case reactionpicker.ChosenMsg:
		cmds = append(cmds, m.sendReaction(msg))

	case reactionpicker.CancelledMsg:
		// Nothing to undo: the row never touched the message.

	case chatlist.ChatSelectedMsg:
		cmds = append(cmds, m.openChatAt(msg.ChatId, 0))

	case contacts.ContactSelectedMsg:
		m.contacts.SetVisible(false)
		cmds = append(cmds, m.openPrivateChat(msg.UserId))

	case search.SearchResultMsg:
		// Jump straight to the matched message rather than the bottom of
		// the chat.
		cmds = append(cmds, m.openChatAt(msg.ChatId, msg.MessageId))

	case composer.MessageSubmittedMsg:
		// Focus deliberately stays on the composer after a send. Chatting
		// is a run of messages, not one message, and bouncing focus back to
		// the chat view would make the second message cost another i/c.
		// Esc is how you leave (see the escape handler above), and the
		// panel border plus the composer help line make the mode visible.
		cmds = append(cmds, m.handleMessageSubmit(msg))

	case composer.PasteRequestedMsg:
		if !m.pasteInFlight {
			m.pasteInFlight = true
			cmds = append(cmds, pasteFromClipboard(m.composer.ChatId()))
		}

	case ClipboardPastedMsg:
		m.pasteInFlight = false
		// The active chat may have changed while the paste was in flight
		// (M-1): installing into whatever chat now happens to be open would
		// silently misattach the file, so discard it instead.
		if m.composer.ChatId() != msg.ChatId {
			clipboard.Remove(msg.Path)
			m.notify("⚠ paste discarded — chat changed")
			break
		}
		if m.composer.IsEditing() {
			// Edit mode was entered while the paste was running; the file
			// can never be sent, so drop it now.
			clipboard.Remove(msg.Path)
			m.notify(noticeEditAttach)
			break
		}
		m.replaceAttachment(msg.Path, msg.IsImage)
		m.setFocus(PanelComposer)

	case ClipboardPasteFailedMsg:
		m.pasteInFlight = false
		m.notify(fmt.Sprintf("⚠ %s", msg.Err))

	case composer.AttachmentDiscardedMsg:
		clipboard.Remove(msg.Path)

	case SendFailedMsg:
		// The composer was reset at submit time, so put the attachment back
		// rather than losing a pasted image to a transient send failure —
		// but only into the composer it came from, and only while nothing
		// newer holds the slot. Otherwise the newer file would be deleted
		// and the old one restored into the wrong chat.
		if msg.Attachment != "" {
			if m.composer.ChatId() == msg.ChatId && m.composer.Attachment() == "" && !m.composer.IsEditing() {
				m.replaceAttachment(msg.Attachment, msg.AsPhoto)
				m.notify(fmt.Sprintf("⚠ send failed — attachment restored: %v", msg.Err))
				break
			}
			clipboard.Remove(msg.Attachment)
		}
		m.notify(fmt.Sprintf("⚠ send failed: %v", msg.Err))

	case ErrorMsg:
		m.notify(fmt.Sprintf("⚠ %v", msg.Err))

	// A file dropped on the terminal arrives as a PASTE of its path, not as
	// keystrokes — the terminal is typing a command line for you, escaped
	// the way a shell would need it. Dragging a file is how people already
	// hand a path to a program, and it is the gesture Ctrl+T exists to
	// serve; see attach.UnquotePath for the three spellings in use.
	case tea.PasteMsg:
		if m.attach.IsVisible() {
			m.attach = m.attach.Paste(msg.Content)
			return m, nil
		}

		// With no picker open the same paste is ambiguous — it could be
		// the message somebody meant to send. attach.ResolvePath is
		// deliberately strict about that, and everything it refuses falls
		// through to the composer as ordinary text.
		//
		// Nothing may be over the frame. An overlay owns input while it is
		// up, and a paste that staged an attachment behind a confirm dialog
		// would change state the reader cannot see and move focus under a
		// modal that is still on screen.
		//
		// The composer's own chat rather than the list's selection: it is
		// the composer the file is staged on and the composer that sends
		// it, so it is the composer that has to have somewhere to send.
		if m.screen == ScreenMain && !m.keyboardOwnedByOverlay() &&
			m.composer.ChatId() != 0 && !m.composer.IsEditing() {
			if path, ok := attach.ResolvePath(msg.Content); ok {
				m.notify("attached " + filepath.Base(path))
				return m.stageAttachment(path, attach.IsImage(path))
			}
		}

	case composer.AttachRequestedMsg:
		if m.composer.IsEditing() {
			// An edit cannot carry media; do not let the dialog recreate
			// the state the Ctrl+V guard rejects.
			m.notify(noticeEditAttach)
			break
		}
		m.attach.Open(m.config.Storage.DownloadDir)

	case composer.ResizedMsg:
		// The composer's row count comes out of the thread's budget, so a
		// reply bar appearing or the expanded form opening is a layout
		// change, not just a redraw.
		m.updateLayout()
		return m, nil

	case chatview.OpenPhotoMsg:
		m.mediaView.SetSize(m.width, m.height)
		// Fed here as well as on the chrome tick: the overlay covers the
		// whole screen, and a row that only says how to leave from the
		// next tick onward is a trap for that long.
		m.mediaView.SetHints(m.hintsFor(SurfaceMedia))
		m.mediaView.Open(msg.Caption, "downloading…")
		return m, nil

	case chatview.OpenedPhotoMsg:
		// A download that lands after the overlay was dismissed is dropped
		// by the overlay itself — see mediaview.Model.Show. Checking it
		// again here would be a second mechanism for one rule.
		if msg.Err != nil {
			m.mediaView.Fail("could not download this photo: " + msg.Err.Error())
			return m, nil
		}
		m.mediaView.Show(msg.Path)
		return m, nil

	case chatview.YankMsg:
		switch {
		case msg.Err != nil:
			m.notify(fmt.Sprintf("⚠ copy failed: %v", msg.Err))
		case msg.Runes == 0:
			// A zero-rune yank is the nil Cmd case: the cursor was on a
			// message with no words in it. Saying nothing would leave the
			// press looking like it worked.
			m.notify("nothing to copy — this message has no text")
		default:
			m.notify(fmt.Sprintf("copied %d characters", msg.Runes))
		}
		return m, nil

	case chatview.MessageActionMsg:
		return m.handleMessageAction(msg)

	case dialog.DialogResultMsg:
		m.dialog = nil
		// Cleared unconditionally, regardless of which dialog this result
		// came from, so a stale target can never leak into some later
		// delete confirm.
		deleteChatID, deleteMsgID := m.pendingDeleteChatId, m.pendingDeleteMessageId
		m.pendingDeleteChatId = 0
		m.pendingDeleteMessageId = 0

		switch msg.ID {
		case "quit":
			// A cancelled confirm returns to exactly where the user was:
			// the dialog is already cleared above and no state was touched
			// on the way in.
			if msg.Confirmed {
				return m, tea.Quit
			}
		case "delete":
			// Three answers now, not two (decision I-7): the reach of the
			// delete is the user's choice, and Escape or Cancel is still
			// the one that does nothing. A server refusal of "for
			// everyone" — the message is too old, or this chat does not
			// permit it — comes back as an ErrorMsg and lands in the
			// notice row, which is the only place it could be reported
			// after the dialog has closed.
			revoke, ok := deleteRevokes(msg.Value)
			if ok {
				tg := m.tg
				cmds = append(cmds, func() tea.Msg {
					if err := tg.DeleteMessages(deleteChatID, []int64{deleteMsgID}, revoke); err != nil {
						return ErrorMsg{Err: err}
					}
					return nil
				})
			}
		}
	}

	// Dispatch to sub-models
	if m.screen == ScreenAuth {
		var cmd tea.Cmd
		m.auth, cmd = m.auth.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		var cmd tea.Cmd

		// Key events only go to the focused panel.
		// Non-key events (telegram updates, spinner ticks, etc.) go to all.
		_, isKey := msg.(tea.KeyPressMsg)
		_, isPaste := msg.(tea.PasteMsg)
		isInputEvent := isKey || isPaste
		// While a dialog is open it owns all key/paste input exclusively —
		// otherwise typing into the prompt also leaks into the panel behind it.
		dialogOpen := m.dialog != nil && m.dialog.IsVisible()

		blockedByDialog := dialogOpen && isInputEvent

		if !blockedByDialog && (!isInputEvent || m.focus == PanelChatList) {
			m.chatList, cmd = m.chatList.Update(msg)
			cmds = append(cmds, cmd)
		}
		if !blockedByDialog && (!isInputEvent || m.focus == PanelChatView) {
			m.chatView, cmd = m.chatView.Update(msg)
			cmds = append(cmds, cmd)
		}
		if !blockedByDialog && (!isInputEvent || m.focus == PanelComposer) {
			m.composer, cmd = m.composer.Update(msg)
			cmds = append(cmds, cmd)
		}
		if !blockedByDialog && (!isInputEvent || m.focus == PanelContacts) {
			m.contacts, cmd = m.contacts.Update(msg)
			cmds = append(cmds, cmd)
		}
		if !blockedByDialog && (!isInputEvent || m.focus == PanelSearch) {
			m.search, cmd = m.search.Update(msg)
			cmds = append(cmds, cmd)
		}
		// The rail takes no input of its own — it is a reading surface, not
		// a panel you focus — so it sees every non-input message and no
		// keys at all.
		if !isInputEvent {
			m.rail, cmd = m.rail.Update(msg)
			cmds = append(cmds, cmd)
		}

		if m.dialog != nil {
			var d dialog.Model
			d, cmd = m.dialog.Update(msg)
			m.dialog = &d
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleAuthStateChanged(msg AuthStateChangedMsg) (tea.Model, tea.Cmd) {
	switch telegram.AuthState(msg.State) {
	case telegram.AuthStateWaitPhone:
		m.setScreen(ScreenAuth)
		m.auth.SetStep(auth.StepPhone)
	case telegram.AuthStateWaitCode:
		m.setScreen(ScreenAuth)
		m.auth.SetStep(auth.StepCode)
	case telegram.AuthStateWaitPassword:
		m.setScreen(ScreenAuth)
		m.auth.SetStep(auth.StepPassword)
	case telegram.AuthStateReady:
		m.setScreen(ScreenAuth)
		m.auth.SetStep(auth.StepDone)
	case telegram.AuthStateClosed:
		// The authorizer only reports this once the client is gone, so it
		// is as terminal as a ClientErrorMsg and is surfaced the same way.
		// Without this case the state change was swallowed entirely and
		// the UI sat on whatever screen it happened to be showing.
		reason := "the Telegram session was closed"
		if msg.Hint != "" {
			reason = msg.Hint
		}
		return m.enterFatalError(reason), nil
	}
	return m, nil
}

// clientErrorReason renders a client failure for the error panel, guarding
// against a nil Err — Terminal is the meaningful field, and a caller that
// sets it without an error still has to produce something readable.
func clientErrorReason(err error) string {
	if err == nil {
		return "the Telegram client stopped unexpectedly"
	}
	return err.Error()
}

// setScreen switches the top-level screen, closing any overlay that only the
// main screen knows how to dismiss.
//
// The help card is the dangerous one: its close and scroll keys are handled
// inside Update's ScreenMain branch, so an auth transition arriving while it
// is open (a revoked session, an AuthErrorMsg) would strand an unclosable
// card over the login form. Clearing it here is the fix at the source; View
// also refuses to draw it off the main screen.
func (m *Model) setScreen(s ScreenState) {
	if s != ScreenMain {
		m.help.SetVisible(false)
	}
	m.screen = s
}

// enterFatalError puts the UI into the terminal-failure state described on
// Model.fatalError. The status bar is corrected on the way in so that
// nothing left on screen keeps claiming the app is connected.
func (m Model) enterFatalError(reason string) Model {
	if reason == "" {
		reason = "the Telegram client stopped unexpectedly"
	}
	// First failure wins: a run loop unwinding tends to report several
	// errors, and the first one is the cause rather than a consequence.
	if m.fatalError == "" {
		m.fatalError = reason
	}
	m.topBar.SetConnection(topBarConnState(telegram.ConnectionStateDisconnected))
	return m
}

// helpLine is the bottom bar under the status bar, ending in the given
// name for the focused panel.
//
// One line per panel, because one line for all of them was a line that
// lied in four of the five states: it advertised n/N (no-ops outside the
// chat view), called "/" find (it is the chat list's filter there), and
// offered Esc:back from the chat list, which is already as far back as
// Esc goes. With the search or contacts overlay up, nearly every entry on
// it was inert. A hint bar that names keys which do nothing is worse than
// no hint bar: it is read by exactly the user who does not yet know which
// ones are real.
//
// The delete confirm's answers. They travel on DialogResultMsg.Value, so
// they are constants rather than literals typed out at both ends.
const (
	deleteCancel      = "cancel"
	deleteForMe       = "me"
	deleteForEveryone = "everyone"
)

// deleteRevokes maps a delete answer onto Telegram's revoke flag, and
// reports whether anything should be deleted at all.
//
// An unrecognised answer deletes nothing. That is the safe direction for a
// destructive action: a value this does not know is a bug, and a bug here
// must not be the one that removes a message from everyone's history.
func deleteRevokes(answer string) (revoke, ok bool) {
	switch answer {
	case deleteForEveryone:
		return true, true
	case deleteForMe:
		return false, true
	default:
		return false, false
	}
}

// openCursoredChat opens the chat under the chat list's cursor, and reports
// whether it had to: the cursored chat being the open one already is the
// common case, and reloading it would cost a history fetch for nothing.
//
// It is what l, Enter and compose share (decision I-2). Called directly
// rather than by emitting a ChatSelectedMsg, because compose has to land
// focus on the composer AFTER the open — and openChatAt, which the message
// would reach eventually, focuses the chat view.
func (m *Model) openCursoredChat() (tea.Cmd, bool) {
	chatID, ok := m.chatList.OpenCursor()
	if !ok || chatID == m.chatView.ChatId() {
		return nil, false
	}
	return m.openChatAt(chatID, 0), true
}

// hasUnsentWork reports whether quitting now would lose something: a draft
// in the composer, or a file staged on it.
//
// One method, consulted by every way out that asks first, because two
// copies of this test is how :quit came to be the way out that did not
// (decision I-5).
func (m Model) hasUnsentWork() bool {
	return m.composer.HasDraft() || m.composer.Attachment() != ""
}

// quitConfirming quits, asking first when there is unsent work.
//
// An empty composer quits at once — a confirm on every quit is a prompt
// people learn to dismiss without reading, which is how the one that
// mattered gets dismissed too. ctrl+q is deliberately not routed through
// here: it is the documented way out of any state, including a broken one.
func (m Model) quitConfirming() (tea.Model, tea.Cmd) {
	if !m.hasUnsentWork() {
		return m, tea.Quit
	}
	d := dialog.NewConfirm(m.roles, "quit", "Quit",
		"Discard the message you are writing and quit?")
	m.dialog = &d
	return m, nil
}

// mouseInLeftPanel reports whether the point is over the left panel
// (chat list / contacts).
func (m Model) mouseInLeftPanel(x, y int) bool {
	if m.bodyRow(y) < 0 {
		return false
	}
	if m.layout.SinglePanel {
		return m.focus == PanelChatList || m.focus == PanelContacts
	}
	return x < m.layout.ChatListWidth
}

func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	x, y := msg.X, msg.Y

	// The top bar owns the folder tabs now, so a click on row 0 selects a
	// tab. Folder state still lives in the chat list — the top bar can say
	// WHICH tab was hit but not what that means.
	if m.layout.TopBar && y == 0 {
		if i := m.topBar.TabAt(x); i >= 0 && m.chatList.SelectFolderIndex(i) {
			m.refreshChrome()
			return m, m.chatList.FolderLoadCmd()
		}
		return m, nil
	}

	if m.mouseInLeftPanel(x, y) {
		// Panel-local coordinates. The frame is borderless, so a column is
		// the raw x and a row is y minus whatever chrome sits above it —
		// there is no longer a border cell to subtract, and subtracting one
		// anyway put every click a row and a column off.
		row, col := m.bodyRow(y), x
		if m.contacts.IsVisible() {
			m.setFocus(PanelContacts)
			if userID, ok := m.contacts.ClickAt(row); ok {
				return m, func() tea.Msg { return contacts.ContactSelectedMsg{UserId: userID} }
			}
			return m, nil
		}
		m.setFocus(PanelChatList)
		// ClickAtXY, not ClickAt: the column is what lets a click on the
		// folder tab bar hit-test which tab was pressed instead of being
		// swallowed as a click on a row that holds no chat.
		if chatID, ok := m.chatList.ClickAtXY(col, row); ok {
			return m, func() tea.Msg { return chatlist.ChatSelectedMsg{ChatId: chatID} }
		}
		return m, m.chatList.FolderLoadCmd()
	}

	switch row := m.bodyRow(y); {
	case row < 0:
		// Chrome row: the top and hint bars are not click targets.
	case row < m.layout.ThreadHeight:
		m.setFocus(PanelChatView)
		// And the cursor goes to the message under the pointer (decision
		// I-11). Focusing the panel alone left a mouse user with no way to
		// choose what r, y or + would act on — the cursor stayed wherever
		// the keyboard had put it, which on a live chat is the newest
		// message whatever they clicked.
		m.chatView.ClickAt(row)
	default:
		m.setFocus(PanelComposer)
	}
	return m, nil
}

func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	x, y := msg.X, msg.Y
	up := msg.Button == tea.MouseWheelUp

	if m.mouseInLeftPanel(x, y) {
		if m.contacts.IsVisible() {
			if up {
				m.contacts.ScrollBy(-1)
			} else {
				m.contacts.ScrollBy(1)
			}
			return m, nil
		}
		// The wheel moves the same cursor the keyboard does, so it asks for
		// the next page on the same terms — a mouse user would otherwise
		// reach the last loaded dialog and never get another.
		delta := 1
		if up {
			delta = -1
		}
		return m, m.chatList.ScrollBy(delta)
	}

	if row := m.bodyRow(y); row >= 0 && row < m.layout.ThreadHeight {
		if up {
			m.chatView.ScrollByLines(3)
		} else {
			m.chatView.ScrollByLines(-3)
		}
		// Wheel scrolling reveals the same off-screen media keyboard
		// scrolling does, so it has to kick off the same lazy loads.
		return m, m.chatView.LazyMediaCmd()
	}
	return m, nil
}

// stageAttachment puts a picked file on the composer and leaves the reader
// where the caption goes.
//
// Focus moves to the composer, which is what puts it in INSERT: SetFocused
// resets vi's submode on the unfocused-to-focused transition (divergence
// 36). Choosing a file is almost always followed by saying what it is, and
// closing back onto the chat list would make that two more keystrokes.
//
// It does NOT expand the composer, which the picker's spec asked for so that
// the staged chip would be visible while the caption is typed. The chip is
// visible in the inline form already — Rows grows by one for it and View
// draws it above the prompt — so expanding would spend eight rows of a
// twenty-four-row terminal to show something already on screen. See
// divergence 50.
func (m Model) stageAttachment(path string, asPhoto bool) (tea.Model, tea.Cmd) {
	m.replaceAttachment(path, asPhoto)
	m.setFocus(PanelComposer)
	return m, nil
}

// replaceAttachment hands a new pending attachment to the composer and
// deletes the spool file it displaces, so at most one spooled paste is alive
// at a time.
func (m *Model) replaceAttachment(path string, asPhoto bool) {
	previous := m.composer.SetAttachment(path, asPhoto)
	if previous != "" && previous != path {
		clipboard.Remove(previous)
	}
}

// pasteFromClipboard spools the system clipboard image to a temp file and
// hands it to the composer as a pending attachment. chatID is the chat that
// was active when the paste was requested; it rides along on the result so
// the caller can detect the chat having changed underneath the async paste.
func pasteFromClipboard(chatID int64) tea.Cmd {
	return func() tea.Msg {
		res, err := clipboard.Paste()
		if err != nil {
			return ClipboardPasteFailedMsg{Err: err}
		}
		return ClipboardPastedMsg{ChatId: chatID, Path: res.Path, IsImage: res.IsImage}
	}
}

// openChatAt is every way a chat gets opened: the list, a search result, and
// a contact by way of the list.
//
// One function because the four steps are not optional and were copied out
// twice. The header's buffer number rides along on switchComposerTo, which
// recomputes the layout and therefore refreshes the chrome — so it is
// correct on the frame the chat opens, not on the next tick. That is a
// chain rather than a statement, and TestOpeningAChatNumbersItImmediately
// is what holds it: the number was previously nobody's assertion.
//
// targetMsgID of 0 means the newest message, which is plain OpenChat.
func (m *Model) openChatAt(chatID int64, targetMsgID int64) tea.Cmd {
	title := ""
	if entry, ok := m.store.Chats.Get(chatID); ok && entry.Chat != nil {
		title = entry.Chat.Title
	}

	cmd := m.chatView.OpenChatAt(chatID, title, targetMsgID)
	m.switchComposerTo(chatID)
	m.setFocus(PanelChatView)

	return tea.Batch(cmd, m.openRailFor(chatID))
}

func (m *Model) openPrivateChat(userID int64) tea.Cmd {
	return func() tea.Msg {
		chat, err := m.tg.CreatePrivateChat(userID)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return chatlist.ChatSelectedMsg{ChatId: chat.ID}
	}
}

func (m Model) handleMessageSubmit(msg composer.MessageSubmittedMsg) tea.Cmd {
	if msg.ChatId == 0 {
		return func() tea.Msg { return ErrorMsg{Err: errNoChatOpen} }
	}
	if msg.EditMessageId != 0 {
		return func() tea.Msg {
			// Edits carry text only. Nothing upstream should let an
			// attachment reach here, but if one does, drop the file rather
			// than leak it — and say so instead of failing silently.
			dropped := msg.Attachment != ""
			if dropped {
				clipboard.Remove(msg.Attachment)
			}
			if _, err := m.tg.EditTextMessage(msg.ChatId, msg.EditMessageId, msg.Text); err != nil {
				return ErrorMsg{Err: err}
			}
			if dropped {
				return ErrorMsg{Err: errEditDroppedAttachment}
			}
			return nil
		}
	}
	if msg.Attachment != "" {
		return func() tea.Msg {
			var err error
			if msg.AsPhoto {
				_, err = m.tg.SendPhotoMessage(msg.ChatId, msg.Attachment, msg.Text, msg.ReplyToId)
			} else {
				_, err = m.tg.SendFileMessage(msg.ChatId, msg.Attachment, msg.Text, msg.ReplyToId)
			}
			if err != nil {
				// Keep the file: the composer is already reset, so the app
				// re-attaches it from this message.
				return SendFailedMsg{
					Err:        err,
					ChatId:     msg.ChatId,
					Attachment: msg.Attachment,
					AsPhoto:    msg.AsPhoto,
				}
			}
			// Drop the spool file once it is on its way to Telegram.
			clipboard.Remove(msg.Attachment)
			return nil
		}
	}
	return func() tea.Msg {
		if _, err := m.tg.SendTextMessage(msg.ChatId, msg.Text, msg.ReplyToId); err != nil {
			return ErrorMsg{Err: err}
		}
		return nil
	}
}

func (m Model) handleMessageAction(msg chatview.MessageActionMsg) (tea.Model, tea.Cmd) {
	switch msg.Action {
	case "reply":
		msgs := m.store.Messages.Get(msg.ChatId)
		preview := ""
		for _, message := range msgs {
			if message.ID != msg.MessageId {
				continue
			}
			// Who, then what. The name is the half that identifies the
			// message when the quote is cut short, which on a narrow pane
			// is most of the time.
			who := render.SenderName(message, m.store)
			if sender, ok := message.SenderID.(*telegram.MessageSenderUser); ok &&
				m.myUserId != 0 && sender.UserID == m.myUserId {
				who = "you"
			}
			body := "[media]"
			if text, ok := message.Content.(*telegram.MessageText); ok {
				body = text.Text.Text
			}
			preview = strings.TrimSpace(who + ": " + body)
			break
		}
		m.composer.EnterReplyMode(msg.MessageId, preview)
		m.setFocus(PanelComposer)
	case "edit":
		msgs := m.store.Messages.Get(msg.ChatId)
		for _, message := range msgs {
			if message.ID == msg.MessageId {
				if text, ok := message.Content.(*telegram.MessageText); ok {
					// An edit cannot carry media — the composer drops any
					// pending attachment and hands back its path.
					clipboard.Remove(m.composer.EnterEditMode(msg.MessageId, text.Text.Text))
					m.setFocus(PanelComposer)
				}
				break
			}
		}
	case "delete":
		// Captured here so DialogResultMsg — which carries the chosen
		// answer but not the target — knows what to delete.
		m.pendingDeleteChatId = msg.ChatId
		m.pendingDeleteMessageId = msg.MessageId
		// The question names the consequence and offers Telegram's real
		// choice (decision I-7). "Are you sure?" deleted for everyone and
		// said neither of those things: not what was being deleted, and
		// not that the reach of it was a decision at all.
		d := dialog.NewChoice(m.roles, "delete", "Delete Message",
			"Delete this message?", []dialog.Button{
				{Label: "Cancel", Accel: "n", Value: deleteCancel},
				{Label: "For me", Accel: "m", Value: deleteForMe, Affirmative: true},
				{Label: "For everyone", Accel: "e", Value: deleteForEveryone, Affirmative: true},
			})
		m.dialog = &d

	case "react":
		m.reactions.Open(msg.ChatId, msg.MessageId, m.myReactionOn(msg.ChatId, msg.MessageId))
		// After Open, so the "takes yours off" wording can read the
		// reaction this account already left.
		m.reactions.SetHints(m.hintsFor(SurfaceReactions))

	case "pin":
		return m, m.togglePin(msg.ChatId, msg.MessageId)

	case "thread":
		return m, m.openDiscussion(msg.ChatId, msg.MessageId)
	}
	return m, nil
}

// myReactionOn is the reaction this account has already left on a message,
// or "" — which is what lets the picker open on it and take it back off.
//
// Read off the message rather than remembered: Telegram sends the chosen
// flag with the tallies, and a client keeping its own copy of what it
// reacted with would disagree with the server the first time somebody
// reacted from their phone.
func (m Model) myReactionOn(chatID, messageID int64) string {
	for _, message := range m.store.Messages.Get(chatID) {
		if message.ID != messageID {
			continue
		}
		for _, reaction := range message.Reactions {
			if reaction.Chosen {
				return reaction.Emoji
			}
		}
		return ""
	}
	return ""
}

// openDiscussionMsg is where a channel post's comments turned out to live.
type openDiscussionMsg struct {
	ChatId    int64
	MessageId int64
}

// openDiscussion finds where a channel post's comments live and goes there.
//
// Two steps rather than one, because the answer is not local: a post's
// comments are in a group linked to the channel, as replies to a copy of the
// post whose message id this client has never seen. The lookup is the only
// thing that knows the translation, and it is a round trip — so the jump
// happens on the answer rather than on the keypress.
func (m Model) openDiscussion(chatID, messageID int64) tea.Cmd {
	tg := m.tg
	return func() tea.Msg {
		discussionChat, discussionMsg, err := tg.DiscussionMessage(chatID, messageID)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return openDiscussionMsg{ChatId: discussionChat, MessageId: discussionMsg}
	}
}

// messagePinned is whether a loaded message is one of the chat's pinned
// ones. A message nobody has loaded is not pinned as far as this client
// knows, and pinning something already pinned is what Telegram does with a
// second pin: nothing.
func (m Model) messagePinned(chatID, messageID int64) bool {
	for _, message := range m.store.Messages.Get(chatID) {
		if message.ID == messageID {
			return message.IsPinned
		}
	}
	return false
}

// sendReaction puts the chosen reaction on the message, or takes the
// existing one off when the picker reported an empty one.
//
// Nothing is written locally. The server answers with updateMessageReactions
// and that already routes into the refetch an edit takes, so the chips
// redraw from what Telegram says rather than from what this client hoped —
// which is what stops a reaction the server refused from sitting on screen
// as though it had worked.
func (m Model) sendReaction(msg reactionpicker.ChosenMsg) tea.Cmd {
	tg := m.tg
	return func() tea.Msg {
		if err := tg.SetReaction(msg.ChatId, msg.MessageId, msg.Emoji); err != nil {
			return ErrorMsg{Err: err}
		}
		return nil
	}
}

// togglePin pins the cursored message, or unpins it when it is already
// pinned.
//
// One key rather than two, because the message says which it is. A pin key
// that cannot tell has to be pressed and then checked, and the place to
// check is the rail — which may not even be open.
func (m Model) togglePin(chatID, messageID int64) tea.Cmd {
	pinned := m.messagePinned(chatID, messageID)
	tg := m.tg
	return func() tea.Msg {
		if err := tg.SetPinned(chatID, messageID, !pinned); err != nil {
			return ErrorMsg{Err: err}
		}
		if pinned {
			return chatview.MediaPlayMsg{Status: "info", Info: "unpinned"}
		}
		return chatview.MediaPlayMsg{Status: "info", Info: "pinned"}
	}
}

func (m *Model) setFocus(panel FocusPanel) {
	m.focus = panel
	m.chatList.SetFocused(panel == PanelChatList)
	m.chatView.SetFocused(panel == PanelChatView)
	m.composer.SetFocused(panel == PanelComposer)
	m.contacts.SetFocused(panel == PanelContacts)
}

// switchComposerTo points the composer at another chat, parking the draft it
// was holding and restoring that chat's own (decision 13).
//
// The three things that have to happen together: the composer swaps drafts,
// the chat list learns which chats now hold one, and the layout is recomputed
// because a restored reply bar or attachment chip changes how many rows the
// composer takes. Doing them in one place is what stops the list advertising
// a draft the composer has already sent, or the thread overlapping a composer
// that grew a row.
func (m *Model) switchComposerTo(chatID int64) {
	clipboard.Remove(m.composer.SetChatId(chatID))
	m.chatList.SetDraftChats(m.composer.DraftChats())
	m.updateLayout()
}

// openRailFor points the rail at a chat, but only when it is actually on
// screen.
//
// Decision 6: opening a chat costs no rail request. The primary history paint
// never competes with rail work, and a user who keeps the rail closed — or
// whose terminal is too narrow for it — never pays for it at all.
func (m *Model) openRailFor(chatID int64) tea.Cmd {
	if !m.railOpen || m.layout.RailWidth == 0 || chatID == 0 {
		return nil
	}
	return m.rail.Open(chatID)
}

// toggleRail is the backtick binding.
//
// The preference survives a terminal too narrow to honour it: layout decides
// whether the rail is drawn, this decides whether it is wanted. Overwriting
// the preference on a narrow terminal would mean a user who widened their
// window had to ask again for something they never turned off.
func (m *Model) toggleRail() tea.Cmd {
	m.railOpen = !m.railOpen
	m.updateLayout()
	if !m.railOpen {
		m.rail.Close()
		return nil
	}
	return m.openRailFor(m.chatView.ChatId())
}

// layoutStale reports whether the frame's row budget no longer matches what
// the current state would compute to.
//
// Against the COMPUTED layout rather than against the composer's own row
// count. A short terminal grants the composer fewer rows than it asks for —
// see layout.MaxContextRows — so comparing the ask to the grant finds a
// difference no relayout can close, and recomputes the whole frame on every
// message for the rest of the session. layout.Compute is pure and cheap,
// and comparing its answer converges by construction: after updateLayout
// the two agree, whatever the terminal was able to give.
//
// False before the first WindowSizeMsg, and without a branch for it: a
// layout computed from a zero terminal has a zero body to spend, so its
// composer height comes out zero — which is what an unsized model's layout
// already holds. The two agree, and nothing is relaid out.
//
// That mattered: an earlier version DID relayout there, and handed every
// panel a zero region — not a smaller frame but no frame, with every panel
// needing to be told its real size again. TestAnUnsizedFrameIsNeverStale
// holds it, because a guard here could not be told from no guard at all.
func (m Model) layoutStale() bool {
	want := layout.Compute(m.width, m.height, m.composer.Rows(), m.railOpen)
	return want.ComposerHeight != m.layout.ComposerHeight
}

func (m *Model) updateLayout() {
	l := layout.Compute(m.width, m.height, m.composer.Rows(), m.railOpen)
	m.layout = l
	m.auth.SetSize(m.width, m.height)
	// Borderless: panels get their whole region. The frame fits each line
	// to that width, so a panel that overshoots is clipped rather than
	// shearing the screen — see internal/ui/frame.
	m.chatList.SetSize(l.ChatListWidth, l.ChatListHeight)
	m.chatView.SetSize(l.ThreadWidth, l.ThreadHeight)
	m.composer.SetSize(l.ThreadWidth, l.ComposerHeight)
	m.contacts.SetSize(l.ChatListWidth, l.ChatListHeight)
	// The search and help overlays size their own boxes from the full
	// window dimensions.
	m.search.SetSize(m.width, m.height)
	m.help.SetSize(m.width, m.height)
	m.rail.SetSize(l.RailWidth, l.BodyHeight)
	// The media overlay is the whole terminal, not a region: it replaces
	// the frame rather than sitting inside it.
	m.mediaView.SetSize(m.width, m.height)
	m.refreshChrome()
}

func (m Model) View() tea.View {
	var content string

	switch m.screen {
	case ScreenAuth:
		content = m.auth.View()
	case ScreenLoading:
		blue := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
		cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

		tgLogo := cyan.Render(
			"⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣀⣤⣴⣾⣿⣿⣿⡄\n" +
				"⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⣠⣴⣶⣿⣿⡿⠿⠛⢙⣿⣿⠃\n" +
				"⠀⠀⠀⠀⠀⠀⠀⠀⢀⣀⣤⣶⣾⣿⣿⠿⠛⠋⠁⠀⠀⠀⣸⣿⣿⠀\n" +
				"⠀⠀⠀⠀⣀⣤⣴⣾⣿⣿⡿⠟⠛⠉⠀⠀⣠⣤⠞⠁⠀⠀⣿⣿⡇⠀\n" +
				"⠀⣴⣾⣿⣿⡿⠿⠛⠉⠀⠀⠀⢀⣠⣶⣿⠟⠁⠀⠀⠀⢸⣿⣿⠀⠀\n" +
				"⠸⣿⣿⣿⣧⣄⣀⠀⠀⣀⣴⣾⣿⣿⠟⠁⠀⠀⠀⠀⠀⣼⣿⡿⠀⠀\n" +
				"⠀⠈⠙⠻⠿⣿⣿⣿⣿⣿⣿⣿⠟⠁⠀⠀⠀⠀⠀⠀⢠⣿⣿⠇⠀⠀\n" +
				"⠀⠀⠀⠀⠀⠀⠘⣿⣿⣿⣿⡇⠀⣀⣄⡀⠀⠀⠀⠀⢸⣿⣿⠀⠀⠀\n" +
				"⠀⠀⠀⠀⠀⠀⠀⠸⣿⣿⣿⣠⣾⣿⣿⣿⣦⡀⠀⠀⣿⣿⡏⠀⠀⠀\n" +
				"⠀⠀⠀⠀⠀⠀⠀⠀⢿⣿⣿⣿⡿⠋⠈⠻⣿⣿⣦⣸⣿⣿⠁⠀⠀⠀\n" +
				"⠀⠀⠀⠀⠀⠀⠀⠀⠀⠙⠛⠁⠀⠀⠀⠀⠈⠻⣿⣿⣿⠏⠀⠀⠀⠀")

		title := blue.Render("  T E L E G R A M   C L I")
		sub := dim.Render("  Terminal Client for Telegram")
		author := dim.Render("  github.com/Ceesaxp/telegram-cli")

		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(1, 4).
			Render(tgLogo + "\n\n" + title + "\n\n" + sub + "\n" + author)

		spinner := blue.Render("  ⣾ Connecting to Telegram...")
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box+"\n\n"+spinner)
	case ScreenMain:
		content = m.renderMainScreen()
	}

	// A dead client replaces every screen, and outranks the overlays below
	// — a dialog or search box floating over a UI that cannot act is worse
	// than useless.
	if m.fatalError != "" {
		v := tea.NewView(m.renderFatalError())
		v.AltScreen = true
		return v
	}

	if m.dialog != nil && m.dialog.IsVisible() {
		content = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.dialog.View())
	}

	if m.search.IsVisible() {
		content = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.search.View())
	}

	// The media overlay is the whole screen: a photo shown inside the thread
	// column is acknowledged rather than shown, which is the point of having
	// an overlay at all. It draws its own full-size view instead of being
	// placed, so it can paint its surface edge to edge.
	if m.mediaView.IsVisible() && m.screen == ScreenMain {
		content = m.mediaView.View()
	}

	// Help is drawn last so it covers whatever else is open — it is only
	// ever opened when nothing else is, but a race would otherwise leave it
	// half-hidden behind a panel it is explaining.
	//
	// ScreenMain only. The close/scroll keys live in the ScreenMain branch
	// of Update, so drawing this card over the auth screen would make it
	// unclosable — and every keystroke aimed at dismissing it would land in
	// the phone or 2FA field behind it, typed blind. setScreen also clears
	// the overlay on any transition; this is the second lock.
	if m.help.IsVisible() && m.screen == ScreenMain {
		content = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.help.View())
	}

	// The palette is drawn last because it owns the keyboard last: while it
	// is open, Update returns before anything behind it sees a key, so
	// anything drawn over it would be inert decoration. It sits about eight
	// rows down rather than centred, so the chat it is acting on stays
	// visible underneath.
	if m.palette.IsVisible() && m.screen == ScreenMain {
		content = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Top,
			lipgloss.NewStyle().MarginTop(paletteTopMargin).Render(m.palette.View()))
	}

	// The picker sits where the palette sits, for the palette's reason: the
	// chat it is attaching to stays visible underneath.
	if m.attach.IsVisible() && m.screen == ScreenMain {
		content = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Top,
			lipgloss.NewStyle().MarginTop(paletteTopMargin).Render(m.attach.View()))
	}

	// The reaction row goes in the hint bar's row rather than over the
	// frame. It is one row and it is transient, which is what that row is
	// for — and the message it is asking about has to stay on screen, which
	// a centred card would cover.
	if m.reactions.IsVisible() && m.screen == ScreenMain {
		rows := strings.Split(content, "\n")
		if len(rows) > 0 {
			rows[len(rows)-1] = m.reactions.View()
			content = strings.Join(rows, "\n")
		}
	}

	// An overlay replaces the frame, and lipgloss.Place pads what is left
	// with PLAIN space — so the screen around a centred card showed the
	// terminal's own background, and the frame's surfaces went with it.
	// Filling here rather than at each Place: bg is what the app sits on,
	// and cell.Fill reopens it BEFORE each span's own sequences, so the
	// card's panel still wins inside the card.
	if m.overlayOpen() {
		content = cell.FillRows(m.roles.Bg, strings.Split(content, "\n"), m.width)
	}

	// Whatever the terminal is still holding from a closed overlay goes out
	// with this frame. It is a zero-width sequence, so prefixing it cannot
	// disturb the layout — and it has to travel this way rather than being
	// written directly, because a component writing to stdout beside the
	// renderer is how a frame gets torn in half.
	if m.mediaTeardown != "" {
		content = m.mediaTeardown + content
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	// Terminal focus/blur reporting (tea.FocusMsg/tea.BlurMsg) — chatview
	// gates read receipts on it. Support is terminal/multiplexer-dependent
	// (tmux needs `set -g focus-events on`); unsupported terminals simply
	// never send the messages.
	v.ReportFocus = true
	return v
}

// renderFatalError draws the panel that replaces the UI once the Telegram
// client has died. It says what happened, that it will not fix itself, and
// what to do about it — a bare error string would leave the user waiting
// for a reconnect that is never coming.
func (m Model) renderFatalError() string {
	errStyle := lipgloss.NewStyle().Foreground(m.roles.Red).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(m.roles.Fg)
	dimStyle := lipgloss.NewStyle().Foreground(m.roles.Dim)

	// The reason comes from the network layer and can be arbitrarily long;
	// wrapping keeps it inside the box instead of blowing the layout out.
	reason := lipgloss.NewStyle().Foreground(m.roles.Red).Width(56).Render(m.fatalError)

	body := errStyle.Render("Disconnected from Telegram") + "\n\n" +
		reason + "\n\n" +
		textStyle.Render("The connection has ended and will not recover on") + "\n" +
		textStyle.Render("its own. Restart teletui to reconnect.") + "\n\n" +
		dimStyle.Render("If this repeats, the session may have been ended") + "\n" +
		dimStyle.Render("from another device — you will be asked to sign in") + "\n" +
		dimStyle.Render("again on the next start.") + "\n\n" +
		dimStyle.Render("Run with TELETUI_DEBUG=/tmp/teletui.log for details.") + "\n\n" +
		textStyle.Render("Press Ctrl+Q to quit.")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.roles.Red).
		Padding(1, 4).
		Render(body)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// deviceCountMsg carries the answer to account.getAuthorizations, or zero
// when there was none.
//
// One field, not a count and an error: the two failure modes this has —
// "the RPC failed" and "we have not asked yet" — are the same fact as far
// as the top bar is concerned, and both draw nothing. Carrying an error
// beside the count would invite a branch that distinguishes them and then
// does the same thing on both sides.
type deviceCountMsg int

// deviceCountCmd asks how many sessions are authorised on this account.
//
// The number is worth a cell in the top bar for the reason Telegram gives it
// a whole screen: a count higher than the user expects is how an
// unauthorised login gets noticed. It replaces half of decision 7's
// placeholder pair; the other half, a transport version, is not here because
// there is nothing for it to vary with.
func (m Model) deviceCountCmd() tea.Cmd {
	tg := m.tg
	if tg == nil {
		return nil
	}
	return func() tea.Msg {
		// The error is dropped rather than branched on: DeviceCount reports
		// 0 when it cannot tell, and 0 is already the state that draws
		// nothing. A branch mapping failure to zero would be a second way
		// of saying what the zero return says.
		n, _ := tg.DeviceCount()
		return deviceCountMsg(n)
	}
}

// keyboardOwnedByOverlay reports whether something other than the panels is
// taking input.
//
// [overlayOpen] is about DRAWING; this is about whose a keystroke or a paste
// is, and the two lists differ. The reaction row draws into the hint bar's
// own row and the media overlay draws the whole screen, so neither is
// "placed" — but both own the keyboard while they are up, and a paste that
// reached the composer behind either of them would act on state the reader
// cannot see.
func (m Model) keyboardOwnedByOverlay() bool {
	return m.overlayOpen() ||
		m.contacts.IsVisible() ||
		(m.reactions.IsVisible() && m.screen == ScreenMain) ||
		(m.mediaView.IsVisible() && m.screen == ScreenMain)
}

// overlayOpen reports whether something is drawn over the frame rather than
// inside it. The five are the ones View places with lipgloss.Place.
func (m Model) overlayOpen() bool {
	return (m.dialog != nil && m.dialog.IsVisible()) ||
		m.search.IsVisible() ||
		(m.help.IsVisible() && m.screen == ScreenMain) ||
		(m.palette.IsVisible() && m.screen == ScreenMain) ||
		(m.attach.IsVisible() && m.screen == ScreenMain)
}
