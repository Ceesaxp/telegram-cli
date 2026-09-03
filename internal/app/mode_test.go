package app

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/dialog"
)

// --- The rules, as a pure function ---------------------------------------

// TestResolveMode covers the rules directly. The precedence between them is
// the part worth pinning: an overlay owns the keyboard regardless of what is
// focused behind it, so "confirm dialog over a focused composer" must be
// NORMAL, not INSERT.
func TestResolveMode(t *testing.T) {
	tests := []struct {
		name string
		in   modeInputs
		want InteractionMode
	}{
		{
			name: "browsing the chat list",
			in:   modeInputs{screen: ScreenMain, focus: PanelChatList},
			want: ModeNormal,
		},
		{
			name: "browsing the chat view",
			in:   modeInputs{screen: ScreenMain, focus: PanelChatView},
			want: ModeNormal,
		},
		{
			name: "composer with an emacs editor types",
			in:   modeInputs{screen: ScreenMain, focus: PanelComposer},
			want: ModeInsert,
		},
		{
			name: "composer in vi command state does not type",
			in: modeInputs{screen: ScreenMain, focus: PanelComposer,
				composerViNormal: true},
			want: ModeNormal,
		},
		{
			name: "search box collects text",
			in: modeInputs{screen: ScreenMain, focus: PanelChatList,
				textOverlayOpen: true},
			want: ModeInsert,
		},
		{
			name: "help card navigates",
			in: modeInputs{screen: ScreenMain, focus: PanelChatList,
				navOverlayOpen: true},
			want: ModeNormal,
		},
		{
			name: "the auth form is a text form",
			in:   modeInputs{screen: ScreenAuth},
			want: ModeInsert,
		},
		{
			name: "the loading screen accepts nothing",
			in:   modeInputs{screen: ScreenLoading},
			want: ModeNormal,
		},

		// Precedence.
		{
			name: "a nav overlay outranks a focused composer",
			in: modeInputs{screen: ScreenMain, focus: PanelComposer,
				navOverlayOpen: true},
			want: ModeNormal,
		},
		{
			name: "a text overlay outranks a vi composer in command state",
			in: modeInputs{screen: ScreenMain, focus: PanelComposer,
				composerViNormal: true, textOverlayOpen: true},
			want: ModeInsert,
		},
		{
			name: "a text overlay outranks a nav overlay",
			in: modeInputs{screen: ScreenMain, focus: PanelChatList,
				textOverlayOpen: true, navOverlayOpen: true},
			want: ModeInsert,
		},
		{
			name: "the palette outranks everything",
			in: modeInputs{screen: ScreenMain, focus: PanelComposer,
				paletteOpen: true, textOverlayOpen: true, navOverlayOpen: true},
			want: ModeCommand,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveMode(tc.in); got != tc.want {
				t.Errorf("resolveMode(%+v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestResolveModeIsTotal checks every combination of the boolean inputs
// against every screen and panel, asserting only that a mode comes back and
// that it is one of the three. The resolver feeds a badge that is always on
// screen, so "no answer" is not an option for any state the app can reach.
func TestResolveModeIsTotal(t *testing.T) {
	screens := []ScreenState{ScreenAuth, ScreenLoading, ScreenMain}
	panels := []FocusPanel{PanelChatList, PanelChatView, PanelComposer,
		PanelSearch, PanelContacts}
	bools := []bool{false, true}

	for _, screen := range screens {
		for _, panel := range panels {
			for _, palette := range bools {
				for _, text := range bools {
					for _, nav := range bools {
						for _, vi := range bools {
							in := modeInputs{
								screen: screen, focus: panel,
								paletteOpen:      palette,
								textOverlayOpen:  text,
								navOverlayOpen:   nav,
								composerViNormal: vi,
							}
							switch resolveMode(in) {
							case ModeNormal, ModeInsert, ModeCommand:
							default:
								t.Fatalf("resolveMode(%+v) returned an unknown mode", in)
							}
						}
					}
				}
			}
		}
	}
}

func TestInteractionModeString(t *testing.T) {
	for mode, want := range map[InteractionMode]string{
		ModeNormal:  "NORMAL",
		ModeInsert:  "INSERT",
		ModeCommand: "COMMAND",
	} {
		if got := mode.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", mode, got, want)
		}
	}
}

// --- The wiring, through a real Model ------------------------------------

// viComposerModel is mainModel with the composer pinned to vi, which
// mainModel deliberately pins to emacs. The vi submode is the case the mode
// badge exists for, so these tests ask for it explicitly rather than
// inheriting whatever the developer's $EDITOR implies.
func viComposerModel(t *testing.T, focus FocusPanel) Model {
	t.Helper()
	m := mainModel(t, focus)
	m.composer.SetEditingMode(composer.ModeVi)
	return m
}

// TestModeFollowsFocus is the basic wiring check: the resolver reads the
// live model rather than a stored copy that could go stale.
func TestModeFollowsFocus(t *testing.T) {
	m := mainModel(t, PanelChatList)
	if got := m.Mode(); got != ModeNormal {
		t.Errorf("chat list: Mode() = %v, want NORMAL", got)
	}

	m.setFocus(PanelComposer)
	if got := m.Mode(); got != ModeInsert {
		t.Errorf("composer: Mode() = %v, want INSERT", got)
	}

	m.setFocus(PanelChatView)
	if got := m.Mode(); got != ModeNormal {
		t.Errorf("chat view: Mode() = %v, want NORMAL", got)
	}
}

// TestModeTracksTheViSubmode is the case the badge exists for. In vi mode a
// focused composer is INSERT while it accepts text and NORMAL once Escape
// returns it to its command state — without focus moving at all.
func TestModeTracksTheViSubmode(t *testing.T) {
	m := viComposerModel(t, PanelComposer)

	if got := m.Mode(); got != ModeInsert {
		t.Fatalf("vi composer starts in insert: Mode() = %v, want INSERT", got)
	}

	// Escape leaves vi insert. Focus must not move — that is the whole
	// point: same panel, different mode.
	updated, _ := m.Update(decodeKey(t, "\x1b"))
	m = updated.(Model)

	if m.focus != PanelComposer {
		t.Fatalf("Escape moved focus to %v; it must stay on the composer", m.focus)
	}
	if got := m.Mode(); got != ModeNormal {
		t.Errorf("vi composer in command state: Mode() = %v, want NORMAL", got)
	}
}

// TestModeAfterEmacsEscape is the contrast to the test above, and the reason
// the resolver asks the composer rather than assuming. Emacs has no nested
// command state, so Escape leaves the composer entirely instead of changing
// its submode — and the mode follows for a different reason.
func TestModeAfterEmacsEscape(t *testing.T) {
	m := mainModel(t, PanelComposer)

	updated, _ := m.Update(decodeKey(t, "\x1b"))
	m = updated.(Model)

	if m.focus == PanelComposer {
		t.Fatal("emacs Escape should leave the composer, not hold it in a submode")
	}
	if got := m.Mode(); got != ModeNormal {
		t.Errorf("after leaving the composer: Mode() = %v, want NORMAL", got)
	}
}

// TestModeForOverlays checks the wiring picks the right question for each
// overlay: the attach picker collects text, a confirm does not.
func TestModeForOverlays(t *testing.T) {
	// The picker inherited this from the prompt dialog it replaced, and it
	// is INSERT rather than COMMAND for the reason the prompt was: its
	// printables build a path. A badge reading COMMAND over a surface that
	// types would be describing the wrong keyboard.
	t.Run("attach picker collects text", func(t *testing.T) {
		m := mainModel(t, PanelChatList)
		m.attach.Open(t.TempDir())
		if got := m.Mode(); got != ModeInsert {
			t.Errorf("attach picker: Mode() = %v, want INSERT", got)
		}
	})

	t.Run("confirm dialog navigates", func(t *testing.T) {
		m := mainModel(t, PanelComposer) // INSERT behind the dialog
		d := dialog.NewConfirm(m.roles, "quit", "Quit", "Sure?")
		m.dialog = &d
		if got := m.Mode(); got != ModeNormal {
			t.Errorf("confirm over a focused composer: Mode() = %v, want NORMAL", got)
		}
	})

	t.Run("help card navigates", func(t *testing.T) {
		m := mainModel(t, PanelComposer)
		m.help.SetVisible(true)
		if got := m.Mode(); got != ModeNormal {
			t.Errorf("help over a focused composer: Mode() = %v, want NORMAL", got)
		}
	})
}

// TestCommandModeMeansThePaletteIsOpen pins the biconditional: COMMAND
// exactly when the palette owns input, never as a side effect of focus.
//
// This replaced TestModeIsNotCommandYet when the palette landed. The mode
// existed and was tested before it was reachable, so wiring it was a one-line
// change at the single place that fills the resolver's inputs.
func TestCommandModeMeansThePaletteIsOpen(t *testing.T) {
	m := mainModel(t, PanelChatList)

	for _, panel := range []FocusPanel{PanelChatList, PanelChatView, PanelComposer} {
		m.setFocus(panel)
		if got := m.Mode(); got == ModeCommand {
			t.Errorf("focus %v reported COMMAND with the palette closed", panel)
		}
	}

	m.setFocus(PanelChatList)
	m.palette.Open()
	if got := m.Mode(); got != ModeCommand {
		t.Errorf("Mode() = %v with the palette open, want COMMAND", got)
	}

	// And it outranks whatever is behind it.
	m.setFocus(PanelComposer)
	if got := m.Mode(); got != ModeCommand {
		t.Errorf("Mode() = %v with the palette open over a composer, want COMMAND", got)
	}

	m.palette.Close()
	if got := m.Mode(); got == ModeCommand {
		t.Error("Mode() stayed COMMAND after the palette closed")
	}
}

// TestModeDoesNotChangeKeyRouting is the guard for decision 3: the badge
// describes the existing routing, it does not alter it. A vi composer in
// command state reports NORMAL, but `?` must still NOT open the help overlay
// there — the existing guard is "focus is not the composer", and reading the
// mode instead would silently change behaviour.
//
// This is the trap that makes Mode() unsafe to retrofit onto the guards in
// Update, and it is why keys_test passes unmodified.
func TestModeDoesNotChangeKeyRouting(t *testing.T) {
	m := viComposerModel(t, PanelComposer)

	updated, _ := m.Update(decodeKey(t, "\x1b")) // vi insert -> vi command state
	m = updated.(Model)

	if got := m.Mode(); got != ModeNormal {
		t.Fatalf("precondition: Mode() = %v, want NORMAL", got)
	}
	if m.focus != PanelComposer {
		t.Fatalf("precondition: focus = %v, want the composer", m.focus)
	}

	updated, _ = m.Update(decodeKey(t, "?"))
	m = updated.(Model)

	if m.help.IsVisible() {
		t.Error("`?` opened the help overlay from a vi composer in command state; " +
			"the mode badge must describe key routing, never change it")
	}
}

// TestModeIsDerivedNotStored is a structural guard. The mode must stay a
// function of existing state: a stored field could be updated in one code
// path and forgotten in another, which is precisely the contradiction the
// badge is supposed to make impossible.
func TestModeIsDerivedNotStored(t *testing.T) {
	m := mainModel(t, PanelComposer)

	// Repeated calls with no intervening state change must agree.
	first, second := m.Mode(), m.Mode()
	if first != second {
		t.Fatalf("Mode() is not stable: %v then %v", first, second)
	}
	if first != ModeInsert {
		t.Fatalf("Mode() = %v, want INSERT", first)
	}

	// Change the state through the component the resolver reads — there is
	// no mode field to set — and the answer must follow.
	m.composer.SetEditingMode(composer.ModeVi)
	if m.composer.IsViNormalMode() {
		t.Fatal("SetEditingMode(vi) should start in insert, not command state")
	}
	if got := m.Mode(); got != ModeInsert {
		t.Errorf("after SetEditingMode(vi): Mode() = %v, want INSERT", got)
	}
}
