package app

import "github.com/imtaqin/telegram-cli/internal/ui/components/dialog"

// InteractionMode is the app-level answer to one question: will the next
// printable key be typed as text, or acted on as a command?
//
// TUI 2.0 makes that question answerable at a glance (docs/tui-2.0.md,
// "Mode integration", resolved by decision 3). The mode is **derived**, never
// stored: it is computed from focus, the composer's own editing state, and
// which overlay owns the keyboard. There is deliberately no mode field to set,
// because a second source of truth beside FocusPanel could contradict what the
// app actually does with a keystroke — and a badge that lies about that is
// worse than no badge.
type InteractionMode int

const (
	// ModeNormal means printable keys act rather than type. It covers the
	// browsing panels, the overlays that navigate rather than collect text,
	// and — importantly — a vi composer that has returned to its command
	// state. In that last case the composer still owns its vi commands; the
	// badge is telling the truth when it says the next letter will not be
	// inserted.
	ModeNormal InteractionMode = iota
	// ModeInsert means printable keys are inserted as text: the composer
	// with an editor that will accept them, a text-collecting overlay, or
	// the auth form.
	ModeInsert
	// ModeCommand means the command palette owns input.
	ModeCommand
)

func (m InteractionMode) String() string {
	switch m {
	case ModeInsert:
		return "INSERT"
	case ModeCommand:
		return "COMMAND"
	default:
		return "NORMAL"
	}
}

// modeInputs is the complete set of state [resolveMode] reads. Gathering it
// into a struct keeps the rules a pure function of explicit inputs, which is
// what makes every combination testable without standing up a Model — and
// what makes it obvious, when a new overlay is added, that its effect on the
// mode has to be decided rather than inherited by accident.
type modeInputs struct {
	screen ScreenState
	focus  FocusPanel

	// paletteOpen is the command palette owning input. It outranks every
	// other clause: while the palette is up, nothing behind it sees a key.
	paletteOpen bool

	// textOverlayOpen is an overlay that COLLECTS text — the search box, or
	// a prompt dialog such as the attach-file path. Printables go into it.
	textOverlayOpen bool

	// navOverlayOpen is an overlay that owns the keyboard but navigates
	// rather than collects: the help card, contacts, a confirm or alert
	// dialog. Printables do not type.
	navOverlayOpen bool

	// composerViNormal is composer.IsViNormalMode(): vi editing is selected
	// and the editor has returned to its command state.
	composerViNormal bool
}

// resolveMode maps the app's state onto the mode the user is in.
//
// The clauses are precedence-ordered, and the order is the interesting part:
// an overlay owns the keyboard regardless of which panel is focused behind
// it, so overlays are consulted before focus. Getting that backwards would
// report INSERT while a confirm dialog is up over the composer.
func resolveMode(in modeInputs) InteractionMode {
	switch {
	case in.paletteOpen:
		return ModeCommand

	// Overlays outrank focus: whatever is behind them cannot see the key.
	case in.textOverlayOpen:
		return ModeInsert
	case in.navOverlayOpen:
		return ModeNormal

	// The auth form is a text form; the loading screen accepts nothing.
	case in.screen == ScreenAuth:
		return ModeInsert
	case in.screen != ScreenMain:
		return ModeNormal

	// Focus, finally. A composer in vi's command state is NORMAL: the next
	// letter runs a vi command instead of being inserted, which is exactly
	// what the badge promises to report.
	case in.focus == PanelComposer:
		if in.composerViNormal {
			return ModeNormal
		}
		return ModeInsert

	default:
		return ModeNormal
	}
}

// Mode reports the current interaction mode.
//
// This is the single source the mode badge, the context-sensitive hint bar,
// and the palette's `:` routing must consult, so that all three agree with
// what Update actually does with a keystroke.
//
// It is NOT a drop-in replacement for the existing focus checks in Update,
// and retrofitting it onto them would change behaviour: ModeNormal includes a
// vi composer in command state, so a guard written as "mode is NORMAL" would
// let `?` open the help overlay while the composer holds a draft, where
// today's "focus is not the composer" correctly does not. Decision 3 requires
// the badge to describe the existing key routing, not alter it.
func (m Model) Mode() InteractionMode {
	return resolveMode(m.modeInputs())
}

// modeInputs gathers the resolver's inputs from the live model. It is the
// only place that knows which component answers which question, so a new
// overlay is wired up here and nowhere else.
func (m Model) modeInputs() modeInputs {
	dialogOpen := m.dialog != nil && m.dialog.IsVisible()
	promptOpen := dialogOpen && m.dialog.Kind() == dialog.KindPrompt

	return modeInputs{
		screen: m.screen,
		focus:  m.focus,

		paletteOpen: m.palette.IsVisible(),

		textOverlayOpen: m.search.IsVisible() || promptOpen,
		navOverlayOpen: m.help.IsVisible() ||
			m.contacts.IsVisible() ||
			(dialogOpen && !promptOpen),

		composerViNormal: m.composer.IsViNormalMode(),
	}
}
