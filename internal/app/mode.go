package app

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
	// browsing panels and the overlays that navigate rather than collect
	// text.
	ModeNormal InteractionMode = iota
	// ModeInsert means printable keys are inserted as text: the composer
	// with an editor that will accept them, a text-collecting overlay, or
	// the auth form.
	ModeInsert
	// ModeVi means the composer is in vi editing and has returned to its
	// command state: the next letter runs a vi command on the draft
	// (decision I-12).
	//
	// It shared NORMAL with the browsing panels for a release, while
	// sharing none of their keys: q, r, y, e and ? are all inert there,
	// and i and h/l mean something else. A badge whose job is "what does
	// the next key do" cannot honestly say NORMAL for two keymaps that
	// agree on nothing.
	ModeVi
	// ModeCommand means the command palette owns input.
	ModeCommand
)

func (m InteractionMode) String() string {
	switch m {
	case ModeInsert:
		return "INSERT"
	case ModeVi:
		return "VI"
	case ModeCommand:
		return "COMMAND"
	default:
		return "NORMAL"
	}
}

// Mode reports the current interaction mode.
//
// This is the single source the mode badge, the hint bar and the palette's
// `:` routing must consult, so that all three agree with what Update
// actually does with a keystroke. It is derived from the surface (see
// hints.go) rather than resolved a second time beside it: two derivations of
// one thing is how the bar and the badge came to disagree about which keymap
// was live.
//
// It is NOT a drop-in replacement for the focus checks in Update, and
// retrofitting it onto them would change behaviour — see the `:` and
// backtick gates, which consult it deliberately and differently.
func (m Model) Mode() InteractionMode {
	return m.surface().Mode()
}
