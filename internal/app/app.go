package app

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/clipboard"
	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/keys"
	"github.com/imtaqin/telegram-cli/internal/notification"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/components/auth"
	"github.com/imtaqin/telegram-cli/internal/ui/components/chatlist"
	"github.com/imtaqin/telegram-cli/internal/ui/components/chatview"
	"github.com/imtaqin/telegram-cli/internal/ui/components/composer"
	"github.com/imtaqin/telegram-cli/internal/ui/components/contacts"
	"github.com/imtaqin/telegram-cli/internal/ui/components/dialog"
	"github.com/imtaqin/telegram-cli/internal/ui/components/groupinfo"
	"github.com/imtaqin/telegram-cli/internal/ui/components/help"
	"github.com/imtaqin/telegram-cli/internal/ui/components/palette"
	"github.com/imtaqin/telegram-cli/internal/ui/components/search"
	"github.com/imtaqin/telegram-cli/internal/ui/components/statusbar"
	"github.com/imtaqin/telegram-cli/internal/ui/layout"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
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
	groupInfo groupinfo.Model
	statusBar statusbar.Model
	dialog    *dialog.Model

	screen     ScreenState
	focus      FocusPanel
	layout     layout.Layout
	tg         *telegram.Client
	store      *store.Store
	config     *config.Config
	theme      *theme.Theme
	notifier   *notification.Notifier
	sound      *notification.SoundPlayer
	authorizer *telegram.TUIAuthorizer
	width      int
	height     int
	myUserId   int64

	// pasteInFlight is set while a clipboard paste command is running, so a
	// second Ctrl+V cannot start a racing paste.
	pasteInFlight bool

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
// on directly, after normalization and defaulting. See the doc comment on
// config.KeyConfig for which keys are wired here versus left inert.
type resolvedKeys struct {
	quit          string
	quitBrowsing  string
	focusChatList string
	focusChatView string
	focusComposer string
	search        string
	globalSearch  string
	contacts      string
	contactsAlt   string
	help          string
	nextFolder    string
	prevFolder    string
	nextChat      string
	prevChat      string

	// The chat view's own bindings. app.go does not dispatch on these —
	// it normalizes and defaults them here and hands them to chatview via
	// SetKeys in New, which is what ends config.toml advertising fields
	// nothing reads. The defaults are the keys chatview hardcoded before
	// it was made configurable.
	//
	// These are the values going IN. What comes back out of
	// chatview.ActiveKeys is what the panel actually accepted, and that
	// is what the help card must quote — see helpSections.
	reply         string
	editMessage   string
	deleteMessage string
	scrollUp      string
	scrollDown    string
	pageUp        string
	pageDown      string
}

// resolveKeys normalizes the configured key bindings app.go consults,
// falling back to the given hardcoded default whenever a field is empty
// (e.g. an older config.toml predating a field, or a zero-value Config in
// tests).
func resolveKeys(kc config.KeyConfig) resolvedKeys {
	resolve := func(configured, def string) string {
		if configured == "" {
			return def
		}
		return config.NormalizeKey(configured)
	}
	return resolvedKeys{
		quit:          resolve(kc.Quit, "ctrl+c"),
		quitBrowsing:  resolve(kc.QuitBrowsing, "q"),
		focusChatList: resolve(kc.FocusChatList, "f1"),
		focusChatView: resolve(kc.FocusChatView, "f2"),
		focusComposer: resolve(kc.FocusComposer, "f3"),
		search:        resolve(kc.Search, "/"),
		globalSearch:  resolve(kc.GlobalSearch, "ctrl+g"),
		contacts:      resolve(kc.Contacts, "alt+c"),
		contactsAlt:   resolve(kc.ContactsAlt, "f4"),
		help:          resolve(kc.Help, "?"),
		nextFolder:    resolve(kc.NextFolder, "alt+l"),
		prevFolder:    resolve(kc.PrevFolder, "alt+h"),
		nextChat:      resolve(kc.NextChat, "alt+j"),
		prevChat:      resolve(kc.PrevChat, "alt+k"),

		reply:         resolve(kc.Reply, "r"),
		editMessage:   resolve(kc.EditMessage, "e"),
		deleteMessage: resolve(kc.DeleteMessage, "d"),
		scrollUp:      resolve(kc.ScrollUp, "k"),
		scrollDown:    resolve(kc.ScrollDown, "j"),
		pageUp:        resolve(kc.PageUp, "pgup"),
		pageDown:      resolve(kc.PageDown, "pgdown"),
	}
}

func New(cfg *config.Config, tg *telegram.Client, s *store.Store, authorizer *telegram.TUIAuthorizer) Model {
	th := theme.ForName(cfg.UI.Theme)
	m := Model{
		auth:       auth.New(th, authorizer),
		chatList:   chatlist.New(s, tg, th),
		chatView:   chatview.New(s, tg, th),
		composer:   composer.New(th),
		contacts:   contacts.New(s, tg, th),
		search:     search.New(s, tg, th),
		help:       help.New(th),
		palette:    palette.New(th),
		groupInfo:  groupinfo.New(s, tg, th),
		statusBar:  statusbar.New(s, th),
		screen:     ScreenLoading,
		focus:      PanelChatList,
		tg:         tg,
		store:      s,
		config:     cfg,
		theme:      th,
		notifier:   notification.NewNotifier(cfg.Notifications.Enabled, cfg.Notifications.ShowPreview),
		sound:      notification.NewSoundPlayer(cfg.Notifications.Sound),
		authorizer: authorizer,
		keys:       resolveKeys(cfg.Keys),
	}
	m.chatView.ApplyMedia(cfg.Media)
	m.chatList.ApplyMedia(cfg.Media)
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
		Reply:      m.keys.reply,
		Edit:       m.keys.editMessage,
		Delete:     m.keys.deleteMessage,
		ScrollUp:   m.keys.scrollUp,
		ScrollDown: m.keys.scrollDown,
		PageUp:     m.keys.pageUp,
		PageDown:   m.keys.pageDown,
	})
	// Built once from the resolved bindings and the resolved editing mode:
	// neither changes after startup, so there is nothing to keep in sync
	// afterwards. Were bindings ever to become rebindable while running,
	// all four of these would have to be re-set with them.
	m.help.SetSections(m.helpSections())
	m.help.SetFooter(m.helpFooter())
	m.statusBar.SetHints(m.statusHints())
	// The palette reads the same resolved bindings, so a rebound key shows
	// correctly in its right-hand column rather than advertising a default
	// the user replaced.
	m.palette.SetItems(m.paletteItems())
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

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		if key.Matches("ctrl+c", "ctrl+q", m.keys.quit) {
			return m, tea.Quit
		}

		// A dead client makes every other binding a lie: the panels behind
		// the error panel are not even visible, so acting on their keys
		// would only mutate state the user cannot see.
		if m.fatalError != "" {
			return m, nil
		}

		if m.screen == ScreenMain {
			// The palette owns the keyboard outright while it is open,
			// ahead of every other overlay. It is a text surface: every
			// printable has to reach the query or a command whose name
			// contains that letter could never be typed. Nothing below
			// runs until it closes.
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
						m.composer.SetNotice(notice)
					}
					return m, cmd
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
				if key.Matches("esc", m.keys.help, "q") {
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
			// ctrl+c / ctrl+q / keys.quit are unaffected: they are
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

			// Alt+1/2/3 for panel focus, plus whatever F-key the config
			// assigns (config.KeyConfig.FocusXxx). The F-keys are the escape
			// hatch for terminals that cannot be made to report Option/Alt as
			// a modifier at all — see the macOS notes on config.KeyConfig.
			if key.Matches("alt+1", m.keys.focusChatList) {
				m.setFocus(PanelChatList)
				return m, nil
			}
			if key.Matches("alt+2", m.keys.focusChatView) {
				m.setFocus(PanelChatView)
				return m, nil
			}
			if key.Matches("alt+3", m.keys.focusComposer) {
				m.setFocus(PanelComposer)
				return m, nil
			}

			// Alt+j / Alt+k: next/prev chat (config.KeyConfig.NextChat/PrevChat,
			// default alt+j/alt+k) — gated like folder cycling below, since
			// both walk the same chat list.
			chatNav := m.dialog == nil && !m.search.IsVisible() && !m.contacts.IsVisible()
			if key.Matches(m.keys.nextChat) && chatNav {
				if chatID, ok := m.chatList.SelectDelta(1); ok {
					return m, func() tea.Msg { return chatlist.ChatSelectedMsg{ChatId: chatID} }
				}
				return m, nil
			}
			if key.Matches(m.keys.prevChat) && chatNav {
				if chatID, ok := m.chatList.SelectDelta(-1); ok {
					return m, func() tea.Msg { return chatlist.ChatSelectedMsg{ChatId: chatID} }
				}
				return m, nil
			}

			// Folder cycling (config.KeyConfig.NextFolder/PrevFolder,
			// default alt+l/alt+h) — only while no overlay or dialog is
			// stealing input.
			//
			// The alt-free spellings of folder cycling — the lazygit
			// [ and ], the left/right arrows, and the 1-9 jump — are
			// deliberately NOT handled here: chatlist binds them itself.
			// Claiming them in both places would mean two implementations
			// of one behavior, and app-level dispatch runs first, so the
			// chatlist copy would be dead code that still has to be kept
			// in step.
			noOverlay := m.dialog == nil && !m.search.IsVisible() && !m.contacts.IsVisible()

			// Colon opens the command palette, and only from NORMAL — the
			// interaction-mode resolver is the authority, so a focused
			// emacs composer (INSERT) types a colon as text while a vi
			// composer in its command state opens the palette. Consulting
			// the mode rather than the focus panel is what keeps that
			// distinction honest.
			if key.Matches(":") && noOverlay && m.Mode() == ModeNormal {
				m.palette.Open()
				return m, nil
			}

			if key.Matches(m.keys.prevFolder) && noOverlay {
				m.chatList.CycleFolder(-1)
				return m, m.chatList.FolderLoadCmd()
			}
			if key.Matches(m.keys.nextFolder) && noOverlay {
				m.chatList.CycleFolder(1)
				return m, m.chatList.FolderLoadCmd()
			}

			// browsing is the two panels that are neither a text surface
			// nor an overlay: the chat list and the chat view. Bare-letter
			// bindings are only safe there.
			browsing := noOverlay &&
				(m.focus == PanelChatList || m.focus == PanelChatView)

			// Bare h/l move between panels — the lazygit reading of
			// left/right, and the one a two-column layout invites: l from
			// the chat list enters the chat view, h from the chat view
			// goes back. They used to cycle the folder tabs; that role
			// now belongs entirely to [ / ], the arrows, the digits and
			// alt+h / alt+l, which lose nothing.
			//
			// One step at a time, like the esc ladder: h at the left edge
			// and l at the right edge are no-ops, not wraps, and neither
			// ever reaches the composer — typing is entered deliberately
			// or not at all. The edge cases are still consumed, so no
			// panel underneath can give the key a second meaning.
			if key.Matches("l") && browsing {
				if m.focus == PanelChatList {
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
			// and is unaffected; ctrl+c / ctrl+q stay global; q still
			// closes the help overlay, which is handled above and returns
			// before reaching here.
			//
			// Work in the composer is not dropped on a single keystroke:
			// an unsent draft or a pending attachment routes through the
			// same confirm dialog a delete uses. An empty composer quits
			// at once — a confirm on every quit is a prompt people learn
			// to dismiss without reading, which is how the one that
			// mattered gets dismissed too.
			if key.Matches(m.keys.quitBrowsing) && browsing {
				if m.composer.HasDraft() || m.composer.Attachment() != "" {
					d := dialog.NewConfirm(m.theme, "quit", "Quit",
						"Discard the message you are writing and quit?")
					m.dialog = &d
					return m, nil
				}
				return m, tea.Quit
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

			// Contacts toggle. ContactsAlt (f4) is the alt-free alternative
			// for terminals that cannot report Option/Alt. ctrl+k was
			// considered and rejected: the composer's textarea widget binds
			// it to readline kill-to-end-of-line, and app-level bindings are
			// checked before the focused panel sees the key.
			//
			// Gated on the dialog and the search overlay, but NOT on the
			// contacts overlay itself — this is a toggle, and gating it on
			// noOverlay would make the key that opens contacts unable to
			// close it. A modal dialog is the thing that must not be
			// bypassed: it is waiting for an answer, and swapping the panel
			// behind it out from under the user answers nothing. The search
			// overlay owns a text input for the same reason "/" is gated.
			//
			// It still fires from a focused composer, deliberately — alt+c
			// and f4 are not characters. See the composer exception list in
			// this file's keymap table.
			if key.Matches(m.keys.contacts, m.keys.contactsAlt) &&
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
					m.composer.SetNotice(noticeEditAttach)
					return m, nil
				}
				// One paste at a time: concurrent pastes race to set the
				// attachment and leak the losing spool file.
				if m.pasteInFlight {
					return m, nil
				}
				m.pasteInFlight = true
				m.setFocus(PanelComposer)
				m.composer.SetNotice("pasting from clipboard...")
				// Capture the active chat now — the paste runs async, and
				// the user may switch chats before it lands.
				return m, pasteFromClipboard(m.composer.ChatId())
			}

			// Compose: i (vi insert) or c (compose) from either browsing
			// panel moves focus to the composer.
			//
			// Typing is always an explicit move now. The app used to
			// forward any printable key straight to the composer, which
			// made every single-character binding a trade-off against
			// being able to start a message with that character, and made
			// the reverse mistake — a stray keystroke silently becoming
			// message text — just as easy.
			// Gated like every other bare-key binding: an open overlay or
			// dialog owns its own input, and with no chat selected the
			// composer has nothing to send to — it would only show its
			// "open a chat first" notice, so "c" is better left inert.
			//
			// The composer's own chat id is the condition, not the chat
			// list's: it is what submit actually sends to, and the two are
			// set from the same ChatSelectedMsg.
			if key.Matches("i", "c") && noOverlay &&
				m.composer.ChatId() != 0 &&
				(m.focus == PanelChatView || m.focus == PanelChatList) {
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
		m.statusBar.SetUserName(fmt.Sprintf("%s %s", msg.FirstName, msg.LastName))
		// We are authorized and the client works — show Connected
		// directly instead of relying on connection-state event timing.
		m.statusBar.SetConnected(true)
		m.setFocus(PanelChatList)
		m.updateLayout()
		return m, m.chatList.Init()

	case telegram.ClientErrorMsg:
		// Terminal means the run loop has exited: nothing will arrive
		// again and nothing sent will leave. Anything else is a failure
		// the client survived, so it gets the same transient notice as any
		// other recoverable error.
		if msg.Terminal {
			return m.enterFatalError(clientErrorReason(msg.Err)), nil
		}
		m.composer.SetNotice(fmt.Sprintf("⚠ telegram: %v", msg.Err))

	case telegram.ClientWarningMsg:
		// A permanent but non-fatal degradation — the client works, just
		// with less capability. Worth telling the user once.
		m.composer.SetNotice(fmt.Sprintf("⚠ %s", msg.Text))

	case telegram.NewMessageMsg:
		// Never notify for our own messages — they arrive as updates too
		// when sent from another device.
		if !msg.Message.IsOutgoing && msg.Message.ChatID != m.chatList.ActiveChatId() {
			entry, ok := m.store.Chats.Get(msg.Message.ChatID)
			title := "New Message"
			if ok && entry.Chat != nil {
				title = entry.Chat.Title
			}
			// Muted chats stay silent. A chat not yet in the store is an
			// unknown quantity, so this fails open and still notifies.
			muted := ok && entry.Chat != nil && entry.Chat.Muted
			if !muted {
				body := "New message received"
				if text, ok := msg.Message.Content.(*telegram.MessageText); ok {
					body = text.Text.Text
				}
				m.notifier.Notify(title, body)
				m.sound.Play()
			}
		}

	case chatlist.ChatSelectedMsg:
		entry, ok := m.store.Chats.Get(msg.ChatId)
		title := ""
		if ok && entry.Chat != nil {
			title = entry.Chat.Title
		}
		cmd := m.chatView.OpenChat(msg.ChatId, title)
		// Switching chats drops the draft, attachment included.
		clipboard.Remove(m.composer.SetChatId(msg.ChatId))
		m.statusBar.SetActiveChatId(msg.ChatId)
		m.setFocus(PanelChatView)
		cmds = append(cmds, cmd)

	case contacts.ContactSelectedMsg:
		m.contacts.SetVisible(false)
		cmds = append(cmds, m.openPrivateChat(msg.UserId))

	case search.SearchResultMsg:
		entry, ok := m.store.Chats.Get(msg.ChatId)
		title := ""
		if ok && entry.Chat != nil {
			title = entry.Chat.Title
		}
		// Jump straight to the matched message rather than the bottom of
		// the chat.
		cmd := m.chatView.OpenChatAt(msg.ChatId, title, msg.MessageId)
		clipboard.Remove(m.composer.SetChatId(msg.ChatId))
		m.setFocus(PanelChatView)
		cmds = append(cmds, cmd)

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
			m.composer.SetNotice("⚠ paste discarded — chat changed")
			break
		}
		if m.composer.IsEditing() {
			// Edit mode was entered while the paste was running; the file
			// can never be sent, so drop it now.
			clipboard.Remove(msg.Path)
			m.composer.SetNotice(noticeEditAttach)
			break
		}
		m.replaceAttachment(msg.Path, msg.IsImage)
		m.setFocus(PanelComposer)

	case ClipboardPasteFailedMsg:
		m.pasteInFlight = false
		m.composer.SetNotice(fmt.Sprintf("⚠ %s", msg.Err))

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
				m.composer.SetNotice(fmt.Sprintf("⚠ send failed — attachment restored: %v", msg.Err))
				break
			}
			clipboard.Remove(msg.Attachment)
		}
		m.composer.SetNotice(fmt.Sprintf("⚠ send failed: %v", msg.Err))

	case ErrorMsg:
		m.composer.SetNotice(fmt.Sprintf("⚠ %v", msg.Err))

	case composer.AttachRequestedMsg:
		if m.composer.IsEditing() {
			// An edit cannot carry media; do not let the dialog recreate
			// the state the Ctrl+V guard rejects.
			m.composer.SetNotice(noticeEditAttach)
			break
		}
		d := dialog.NewPrompt(m.theme, "attach-file", "Attach File", "Path to file:")
		m.dialog = &d

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
		case "attach-file":
			if msg.Confirmed && strings.TrimSpace(msg.Input) != "" {
				m.replaceAttachment(strings.TrimSpace(msg.Input), false)
			}
		case "quit":
			// A cancelled confirm returns to exactly where the user was:
			// the dialog is already cleared above and no state was touched
			// on the way in.
			if msg.Confirmed {
				return m, tea.Quit
			}
		case "delete":
			// A cancelled confirm does nothing.
			if msg.Confirmed {
				tg := m.tg
				cmds = append(cmds, func() tea.Msg {
					// revoke=true deletes for everyone, not just locally.
					// That's the more expected behavior for a delete
					// confirm; a for-me/for-everyone choice dialog is
					// future work.
					if err := tg.DeleteMessages(deleteChatID, []int64{deleteMsgID}, true); err != nil {
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
		if !blockedByDialog && (!isInputEvent || m.focus == PanelGroupInfo) {
			m.groupInfo, cmd = m.groupInfo.Update(msg)
			cmds = append(cmds, cmd)
		}

		// Status bar always gets all events (non-interactive).
		m.statusBar, cmd = m.statusBar.Update(msg)
		cmds = append(cmds, cmd)

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
	m.statusBar.SetConnected(false)
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
// Built from the resolved bindings, like helpSections, statusHints and
// helpFooter — the same rule applies here, and this is the line that is on
// screen at all times. It returns finished text rather than a format
// string so that a binding containing a "%" cannot be read as a verb.
func (m Model) helpLine(focusName string) string {
	k := m.keys
	var parts []string

	switch m.focus {
	case PanelComposer:
		// None of the navigation keys apply while typing; the composer's
		// own hint line carries the line-editing detail.
		parts = []string{
			"Enter:send", "Ctrl+J:newline", "Ctrl+O:editor",
			"Ctrl+T:attach", "Ctrl+V:paste", "Esc:cancel", "Tab:switch",
		}

	case PanelChatView:
		parts = []string{
			k.help + ":help", "Tab:switch", "Esc:back",
			"j/k:scroll", "h:chats",
			k.search + ":find", "n/N:match",
			"i or c:compose", k.quitBrowsing + ":quit",
		}

	case PanelSearch:
		// The overlay owns its text input; the panel keys behind it are
		// unreachable until it closes.
		parts = []string{"type to search", "j/k:move", "Enter:open", "Esc:close"}

	case PanelContacts:
		parts = []string{
			"j/k:move", "Enter:open chat",
			eitherKey(k.contacts, k.contactsAlt) + ":close", "Esc:close",
		}

	case PanelGroupInfo:
		parts = []string{"j/k:scroll", "Esc:back"}

	default: // PanelChatList
		// No Esc:back — the chat list is where back goes. No n/N — the
		// search hits belong to the chat view.
		parts = []string{
			k.help + ":help", "Tab:switch",
			"j/k:move", "l:messages",
			k.search + ":filter", "Enter:open",
			"i or c:compose", k.quitBrowsing + ":quit",
		}
	}

	return " " + strings.Join(append(parts, focusName), " │ ")
}

// eitherKey joins two spellings of one binding for the hint bar, dropping
// the second when it is absent or identical. Named for its prose ("alt+c
// or f4") to keep it distinct from the " / " joiner helpSections uses —
// the two render the same idea for different widths.
func eitherKey(a, b string) string {
	if b == "" || a == b {
		return a
	}
	if a == "" {
		return b
	}
	return a + " or " + b
}

// mouseInLeftPanel reports whether the point is over the left panel
// (chat list / contacts).
func (m Model) mouseInLeftPanel(x, y int) bool {
	if y >= m.layout.ChatListHeight {
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

	if m.mouseInLeftPanel(x, y) {
		// Row and column inside the panel border — the coordinate space
		// chatlist.ClickAtXY and contacts.ClickAt expect.
		row, col := y-1, x-1
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

	if y < m.layout.ChatViewHeight {
		m.setFocus(PanelChatView)
	} else if y < m.layout.ChatViewHeight+m.layout.ComposerHeight {
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
		if up {
			m.chatList.ScrollBy(-1)
		} else {
			m.chatList.ScrollBy(1)
		}
		return m, nil
	}

	if y < m.layout.ChatViewHeight {
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
			if message.ID == msg.MessageId {
				if text, ok := message.Content.(*telegram.MessageText); ok {
					preview = text.Text.Text
				} else {
					preview = "[Media]"
				}
				break
			}
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
		// Captured here so DialogResultMsg — which carries no payload of
		// its own — knows what to delete once the user confirms.
		m.pendingDeleteChatId = msg.ChatId
		m.pendingDeleteMessageId = msg.MessageId
		d := dialog.NewConfirm(m.theme, "delete", "Delete Message", "Are you sure?")
		m.dialog = &d
	}
	return m, nil
}

func (m *Model) setFocus(panel FocusPanel) {
	m.focus = panel
	m.chatList.SetFocused(panel == PanelChatList)
	m.chatView.SetFocused(panel == PanelChatView)
	m.composer.SetFocused(panel == PanelComposer)
	m.contacts.SetFocused(panel == PanelContacts)
	m.groupInfo.SetFocused(panel == PanelGroupInfo)
}

func (m *Model) updateLayout() {
	l := layout.Compute(m.width, m.height, m.config.UI.ChatListWidth)
	m.layout = l
	m.auth.SetSize(m.width, m.height)
	// Inner dimensions (subtract 2 for border)
	m.chatList.SetSize(l.ChatListWidth-2, l.ChatListHeight-2)
	m.chatView.SetSize(l.ChatViewWidth-2, l.ChatViewHeight-2)
	m.composer.SetSize(l.ComposerWidth-2, l.ComposerHeight-2)
	m.contacts.SetSize(l.ChatListWidth-2, l.ChatListHeight-2)
	// The search and help overlays size their own boxes from the full
	// window dimensions.
	m.search.SetSize(m.width, m.height)
	m.help.SetSize(m.width, m.height)
	m.groupInfo.SetSize(l.ChatListWidth-2, l.ChatListHeight-2)
	m.statusBar.SetSize(l.StatusBarWidth)
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
		author := dim.Render("  github.com/imtaqin/telegram-cli")

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
	errStyle := lipgloss.NewStyle().Foreground(m.theme.Error).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(m.theme.Text)
	dimStyle := lipgloss.NewStyle().Foreground(m.theme.TextMuted)

	// The reason comes from the network layer and can be arbitrarily long;
	// wrapping keeps it inside the box instead of blowing the layout out.
	reason := lipgloss.NewStyle().Foreground(m.theme.Error).Width(56).Render(m.fatalError)

	body := errStyle.Render("Disconnected from Telegram") + "\n\n" +
		reason + "\n\n" +
		textStyle.Render("The connection has ended and will not recover on") + "\n" +
		textStyle.Render("its own. Restart teletui to reconnect.") + "\n\n" +
		dimStyle.Render("If this repeats, the session may have been ended") + "\n" +
		dimStyle.Render("from another device — you will be asked to sign in") + "\n" +
		dimStyle.Render("again on the next start.") + "\n\n" +
		dimStyle.Render("Run with TELETUI_DEBUG=/tmp/teletui.log for details.") + "\n\n" +
		textStyle.Render("Press Ctrl+C to quit.")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Error).
		Padding(1, 4).
		Render(body)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderMainScreen() string {
	// Build left panel with rounded border
	var leftContent string
	if m.contacts.IsVisible() {
		// m.chatList.View() is intentionally skipped here: its dirty flag
		// just accumulates while contacts is shown and gets cleared the
		// next time the chat list actually renders. Self-healing, verified
		// harmless — do not "fix" by rendering it unseen just to clear it.
		leftContent = m.contacts.View()
	} else {
		leftContent = m.chatList.View()
	}

	leftStyle := m.theme.PanelNormal
	if m.focus == PanelChatList || m.focus == PanelContacts {
		leftStyle = m.theme.PanelFocused
	}
	leftPanel := leftStyle.
		Width(m.layout.ChatListWidth - 2).
		Height(m.layout.ChatListHeight - 2).
		Render(leftContent)

	// Build chat view with rounded border
	chatViewStyle := m.theme.PanelNormal
	if m.focus == PanelChatView {
		chatViewStyle = m.theme.PanelFocused
	}
	chatPanel := chatViewStyle.
		Width(m.layout.ChatViewWidth - 2).
		Height(m.layout.ChatViewHeight - 2).
		Render(m.chatView.View())

	// Build composer with rounded border
	composerStyle := m.theme.PanelNormal
	if m.focus == PanelComposer {
		composerStyle = m.theme.PanelFocused
	}
	composerPanel := composerStyle.
		Width(m.layout.ComposerWidth - 2).
		Height(m.layout.ComposerHeight - 2).
		Render(m.composer.View())

	// Right side = chat + composer stacked
	rightPanel := lipgloss.JoinVertical(lipgloss.Left, chatPanel, composerPanel)

	// Main area = left + right
	var mainArea string
	if m.layout.SinglePanel {
		switch m.focus {
		case PanelChatList, PanelContacts:
			mainArea = leftPanel
		default:
			mainArea = lipgloss.JoinVertical(lipgloss.Left, chatPanel, composerPanel)
		}
	} else {
		mainArea = lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
	}

	// Status bar
	statusBar := m.statusBar.View()

	// Keybind help line
	helpStyle := lipgloss.NewStyle().Foreground(m.theme.TextMuted)
	focusName := [...]string{"CHATS", "MESSAGES", "COMPOSE", "SEARCH", "CONTACTS", "INFO"}
	fi := int(m.focus)
	if fi >= len(focusName) {
		fi = 0
	}
	help := helpStyle.Render(m.helpLine(focusName[fi]))

	// Pad help to full width
	helpW := cell.MaxWidth(help)
	if helpW < m.width {
		help += strings.Repeat(" ", m.width-helpW)
	}

	return lipgloss.JoinVertical(lipgloss.Left, mainArea, statusBar, help)
}
