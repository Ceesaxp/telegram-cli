package app

import (
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/hintbar"
)

// Surface is the thing whose keymap is live right now: a panel, or the
// overlay that has taken the keyboard from it.
//
// It exists because the hint bar was keyed by MODE, and a mode is a much
// coarser question — "does the next printable key type or act?" — than "what
// can I press". Three surfaces share ModeNormal and agree on almost no keys
// between them, so the chat-view hint set showed in the chat list, under
// contacts, under a confirm dialog and in a vi composer: four places where
// it named keys that do nothing (decision I-6).
//
// Every hint the app draws is keyed by this, including the ones components
// paint themselves — the chat list footer, the dialog's own line, the media
// overlay's row. A hint that names an inert key is a defect, not a nit: it
// is how "u unread" sat in the chat list footer for a release with nothing
// bound to u.
type Surface int

const (
	// The browsing panels.
	SurfaceChatList Surface = iota
	SurfaceChatView

	// The composer, in each of its two keymaps. They are separate surfaces
	// rather than one, because vi's command state shares nothing with
	// insert: enter still sends, but i, o, dd and : are the keys worth the
	// row, and ctrl+j inserts nothing there at all.
	SurfaceComposerInsert
	SurfaceComposerVi

	// The overlays, in the order Update consults them.
	SurfaceReactions
	SurfaceAttach
	SurfaceForward
	SurfacePalette
	SurfaceMedia
	SurfaceHelp
	SurfaceDialog
	SurfaceSearch
	SurfaceContacts

	// The screens that are not the client: the auth form takes text, and
	// the loading screen takes nothing at all.
	SurfaceAuth
	SurfaceLoading
)

func (s Surface) String() string {
	switch s {
	case SurfaceChatView:
		return "chat view"
	case SurfaceComposerInsert:
		return "composer (insert)"
	case SurfaceComposerVi:
		return "composer (vi)"
	case SurfaceReactions:
		return "reactions"
	case SurfaceAttach:
		return "attach picker"
	case SurfaceForward:
		return "forward picker"
	case SurfacePalette:
		return "palette"
	case SurfaceMedia:
		return "media"
	case SurfaceHelp:
		return "help"
	case SurfaceDialog:
		return "dialog"
	case SurfaceSearch:
		return "search"
	case SurfaceContacts:
		return "contacts"
	case SurfaceAuth:
		return "auth"
	case SurfaceLoading:
		return "loading"
	default:
		return "chat list"
	}
}

// Mode is the badge's answer for this surface: will the next printable key
// be typed as text, acted on, or collected into a command?
//
// The mode is derived FROM the surface rather than beside it. Two
// derivations of one thing is what let the hint bar and the badge disagree
// about which keymap was live; there is one now, and this is the projection
// of it onto the coarser question the badge asks.
func (s Surface) Mode() InteractionMode {
	switch s {
	case SurfacePalette:
		return ModeCommand
	case SurfaceComposerVi:
		return ModeVi
	case SurfaceComposerInsert, SurfaceSearch, SurfaceAttach, SurfaceForward, SurfaceAuth:
		return ModeInsert
	default:
		return ModeNormal
	}
}

// surfaceInputs is the complete set of state [resolveSurface] reads.
// Gathering it into a struct keeps the rules a pure function of explicit
// inputs, which is what makes every combination testable without standing up
// a Model — and what makes it obvious, when a new overlay is added, that its
// place in the precedence order has to be decided rather than inherited by
// accident.
type surfaceInputs struct {
	screen ScreenState
	focus  FocusPanel

	// The overlays, one field each. They were three fields once — a text
	// overlay, a nav overlay and the palette — which was enough to answer
	// the badge's question and not nearly enough to answer "what can I
	// press": the attach picker and the global search collapsed into one
	// value, as did the help card, contacts and a confirm dialog.
	reactionsOpen bool
	attachOpen    bool
	forwardOpen   bool
	paletteOpen   bool
	mediaOpen     bool
	helpOpen      bool
	dialogOpen    bool
	searchOpen    bool
	contactsOpen  bool

	// composerViNormal is composer.IsViNormalMode(): vi editing is selected
	// and the editor has returned to its command state.
	composerViNormal bool
}

// resolveSurface maps the app's state onto the surface the user is looking
// at.
//
// The clauses are precedence-ordered, and the order is the same one Update
// dispatches in — an overlay owns the keyboard regardless of which panel is
// focused behind it, so overlays are consulted before focus. Getting that
// backwards would draw the composer's hints while a confirm dialog sat over
// it.
func resolveSurface(in surfaceInputs) Surface {
	switch {
	// The screens that are not the client come first: nothing behind them
	// exists yet.
	case in.screen == ScreenAuth:
		return SurfaceAuth
	case in.screen != ScreenMain:
		return SurfaceLoading

	// Overlays, in Update's own order.
	case in.reactionsOpen:
		return SurfaceReactions
	case in.attachOpen:
		return SurfaceAttach
	case in.forwardOpen:
		return SurfaceForward
	case in.paletteOpen:
		return SurfacePalette
	case in.mediaOpen:
		return SurfaceMedia
	case in.helpOpen:
		return SurfaceHelp
	case in.dialogOpen:
		return SurfaceDialog
	case in.searchOpen:
		return SurfaceSearch
	case in.contactsOpen:
		return SurfaceContacts

	// Focus, finally.
	case in.focus == PanelComposer:
		if in.composerViNormal {
			return SurfaceComposerVi
		}
		return SurfaceComposerInsert
	case in.focus == PanelChatView:
		return SurfaceChatView
	default:
		return SurfaceChatList
	}
}

// surface reports which keymap is live. It is the only place that knows
// which component answers which question, so a new overlay is wired up here
// and nowhere else.
func (m Model) surface() Surface {
	return resolveSurface(m.surfaceInputs())
}

// surfaceInputs gathers the resolver's inputs from the live model.
func (m Model) surfaceInputs() surfaceInputs {
	return surfaceInputs{
		screen: m.screen,
		focus:  m.focus,

		reactionsOpen: m.reactions.IsVisible(),
		attachOpen:    m.attach.IsVisible(),
		forwardOpen:   m.forward.IsVisible(),
		paletteOpen:   m.palette.IsVisible(),
		mediaOpen:     m.mediaView.IsVisible(),
		helpOpen:      m.help.IsVisible(),
		dialogOpen:    m.dialog != nil && m.dialog.IsVisible(),
		searchOpen:    m.search.IsVisible(),
		contactsOpen:  m.contacts.IsVisible(),

		composerViNormal: m.composer.IsViNormalMode(),
	}
}

// hintsFor is the one hint registry, keyed by surface (decision I-6).
//
// Every set is ORDERED and every bar keeps the longest prefix that fits, so
// the order here is a priority ranking rather than a cosmetic choice:
// whatever is last is what disappears first on a narrow terminal.
//
// Every key comes from the resolved bindings, never from a literal, which is
// what makes a rebound key show correctly and an unbound one disappear
// rather than be advertised. hint() drops a row whose key resolved to
// nothing for exactly that reason.
//
// The sets are docs/interaction-model.md's "Hints" table.
func (m Model) hintsFor(s Surface) []hintbar.Hint {
	k := m.keys
	cv := m.chatView.ActiveKeys()

	// hint builds one row, and returns nothing when the binding is unbound
	// — a configuration that leaves an action without a key must take its
	// hint with it rather than leave a blank in the row.
	hint := func(key, label string) []hintbar.Hint {
		if key == "" {
			return nil
		}
		return []hintbar.Hint{{Key: key, Label: label}}
	}
	// pair renders the two halves of one motion as a single row ("j/k
	// move"), dropping the half a configuration left unbound.
	pair := func(a, b, label string) []hintbar.Hint {
		switch {
		case a == "" && b == "":
			return nil
		case a == "":
			return hint(b, label)
		case b == "":
			return hint(a, label)
		}
		return []hintbar.Hint{{Key: a + "/" + b, Label: label}}
	}
	join := func(groups ...[]hintbar.Hint) []hintbar.Hint {
		var out []hintbar.Hint
		for _, g := range groups {
			out = append(out, g...)
		}
		return out
	}

	switch s {
	case SurfaceChatView:
		return join(
			hint("j/k", "message"),
			hint(cv.Reply, "reply"),
			hint("y", "yank"),
			hint(cv.Edit, "edit"),
			hint(k.search, "find"),
			hint("h", "chats"),
			hint(k.compose, "compose"),
			hint(k.quitBrowsing, "quit"),
			hint(k.help, "keymap"),
		)

	case SurfaceComposerInsert:
		return join(
			hint("enter", "send"),
			hint("esc", "leave"),
			hint("ctrl+j", "newline"),
			hint("ctrl+t", "attach"),
			hint("ctrl+o", "editor"),
		)

	case SurfaceComposerVi:
		// Nothing in this set is in the insert set, which is the whole
		// argument for the fourth badge (I-12): the composer's command
		// state shares a mode with the browsing panels and shares none of
		// their keys either.
		return join(
			hint("i", "insert"),
			hint("esc", "leave"),
			hint("o", "open line"),
			hint("dd", "delete line"),
			hint(":", "command"),
		)

	case SurfacePalette:
		return join(
			hint("enter", "run"),
			hint("tab", "complete"),
			hint("esc", "cancel"),
		)

	case SurfaceAttach:
		return join(
			hint("enter", "attach"),
			hint("tab", "complete"),
			hint("esc", "cancel"),
		)

	case SurfaceForward:
		// The confirmation is a second screen of the same surface, and
		// esc means a different thing on each — back on one, out on the
		// other. The row says what the current screen answers rather than
		// naming both, which is what the picker's own footer does too.
		return join(
			hint("enter", "choose"),
			hint("↑↓", "move"),
			hint("esc", "cancel"),
		)

	case SurfaceContacts:
		// The way out of a filter leads while one is applied, for the
		// reason it does in the chat list: a narrowed list with nothing
		// saying how to widen it reads as contacts going missing.
		if m.contacts.FilterActive() {
			return join(
				hint("esc", "clear"),
				hint("enter", "keep"),
			)
		}
		var clear []hintbar.Hint
		if m.contacts.FilterQuery() != "" {
			clear = hint("esc", "clear filter")
		}
		return join(
			clear,
			hint("j/k", "move"),
			hint("enter", "open"),
			hint(k.search, "filter"),
			hint(k.contacts, "close"),
		)

	case SurfaceSearch:
		return join(
			hint("enter", "search"),
			hint("tab", "scope"),
			hint("esc", "close"),
		)

	case SurfaceDialog:
		// The answer keys come first because they are the ones a dialog is
		// asking about — and they are read off the dialog that is actually
		// up, because they differ per dialog. A confirm answers to n and y;
		// the delete choice answers to n, m and e, and a bar that said
		// "y/n" there advertised an inert key on the one surface where a
		// wrong press is destructive.
		//
		// The dialog's button set is the single authority: this row and the
		// dialog's own hint line are two renderings of it, not two copies.
		// With no dialog up there are no accelerators to name, and hint()
		// drops the row.
		accels := ""
		if m.dialog != nil {
			accels = m.dialog.Accelerators()
		}
		return join(
			hint(accels, "answer"),
			hint("←/→", "choose"),
			hint("enter", "accept"),
			hint("esc", "cancel"),
		)

	case SurfaceHelp:
		return join(
			hint("esc", "close"),
			hint("j/k", "scroll"),
		)

	case SurfaceMedia:
		return join(
			hint("esc", "close"),
			hint("s", "save"),
			hint("o", "open externally"),
		)

	case SurfaceReactions:
		// Enter's label depends on whether this account already reacted:
		// pressing it on your own reaction takes it off, and a row that
		// said "pick" would be describing the opposite of what happens.
		pick := "pick"
		if m.reactions.Mine() != "" {
			pick = "takes yours off"
		}
		return join(
			hint("enter", pick),
			hint("esc", "cancel"),
		)

	case SurfaceAuth, SurfaceLoading:
		// Neither has a keymap worth a row: the auth form is a text field
		// and the loading screen accepts nothing.
		return nil

	default: // SurfaceChatList
		// A filter narrows the list and nothing on screen says how to widen
		// it again, which leaves a reader looking at a partial list and
		// wondering where their chats went. The way out leads while one is
		// applied — the same reason the reaction row's enter changes its
		// wording: a hint set that ignores the state it is drawn over is a
		// hint set that describes a surface the user is not on.
		//
		// The chat list's footer row used to carry this. It is gone (see
		// the amendment to I-6 in docs/interaction-model.md), so the one
		// bar carries it instead.
		var filter []hintbar.Hint
		if m.chatList.FilterActive() {
			filter = join(
				hint("esc", "clear"),
				hint("enter", "keep"),
			)
		} else if m.chatList.FilterQuery() != "" {
			filter = hint("esc", "clear filter")
		}

		return join(
			filter,
			hint("j/k", "move"),
			hint("l", "open"),
			hint(k.search, "filter"),
			pair(k.prevFolder, k.nextFolder, "folder"),
			hint(k.nextUnread, "unread"),
			hint(k.compose, "compose"),
			hint(k.quitBrowsing, "quit"),
			hint(k.help, "keymap"),
		)
	}
}
