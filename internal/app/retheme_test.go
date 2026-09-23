package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/forward"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/hintbar"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A theme switched while the app is running reaches everything on screen,
// and nothing drawn in the old palette survives it.
//
// TestEveryComponentUsesThePaletteItWasGiven asks whether a component draws
// the palette its constructor was handed. This asks the harder question a
// live switch raises: whether it draws the palette it was handed LAST. A
// component can pass the first and fail this one three ways — it has no
// setter at all; its setter replaces the palette but not the styles its
// constructor built from it (a text input's, a spinner's, a list's); or it
// keeps output it already rendered, and serves the old colours back from a
// cache. Every case here is drawn once in one marker palette, switched to a
// second, and drawn again, so all three show up as a colour from the first.
//
// Adding a surface to the app means adding it here. A component that is not
// in this table is one nobody has checked a switch reaches.
func TestEveryComponentFollowsARetheme(t *testing.T) {
	before, knownBefore := theme.MarkerRoles()
	after, knownAfter := theme.SecondMarkerRoles()
	// One colour each, from different roles, so a thread or rail still on
	// the old ramp names people in a colour the new palette does not have.
	rampBefore := []lipgloss.Color{before.Green}
	rampAfter := []lipgloss.Color{after.Red}

	tests := []struct {
		name string
		// bare builds the empty app rather than the golden scene, for the
		// states the scene is past: a chat list still loading, a list
		// with nothing in it.
		bare bool
		// open puts the surface on screen.
		open func(t *testing.T, m *Model)
		// view draws it. shows is text it must contain, so a case whose
		// open quietly failed cannot pass by drawing something else.
		view  func(m Model) string
		shows string
	}{
		{name: "the assembled screen", shows: "infra-oncall",
			view: func(m Model) string { return m.View().Content }},
		{name: "the top bar", shows: "connected",
			view: func(m Model) string { return m.topBar.View() }},
		{name: "the hint bar", shows: "keymap",
			view: func(m Model) string { return m.hintBar.View() }},
		{name: "the hint bar's notice", shows: "upload failed",
			open: func(t *testing.T, m *Model) { m.notify("⚠ upload failed") },
			view: func(m Model) string { return m.hintBar.View() }},
		{name: "the chat list", shows: "relay-protocol",
			view: func(m Model) string { return m.chatList.View() }},
		{name: "the chat list while it loads", bare: true, shows: "Loading chats",
			view: func(m Model) string { return m.chatList.View() }},
		{name: "an empty chat list", bare: true, shows: "No items",
			open: func(t *testing.T, m *Model) { m.chatList.MarkLoadedForTest() },
			view: func(m Model) string { return m.chatList.View() }},
		{name: "the thread", shows: "Canary",
			view: func(m Model) string { return m.chatView.View() }},
		{name: "the thread's find bar", shows: "search:",
			open: func(t *testing.T, m *Model) { m.chatView.OpenFind() },
			view: func(m Model) string { return m.chatView.View() }},
		{name: "the composer", shows: "nadia",
			view: func(m Model) string { return m.composer.View() }},
		{name: "the expanded composer", shows: "make",
			open: func(t *testing.T, m *Model) {
				m.setFocus(PanelComposer)
				*m = typeText(t, *m, "run `make` now")
				m.composer.SetExpanded(true)
			},
			view: func(m Model) string { return m.composer.View() }},
		{name: "the mention picker", shows: "nadia",
			open: func(t *testing.T, m *Model) {
				m.composer.SetMentionsEnabled(true)
				m.composer.SetMentionCandidates(sceneInfra, []*telegram.User{
					{ID: sceneNadia, FirstName: "nadia", Username: "nadia"},
				})
				m.setFocus(PanelComposer)
				if *m = typeText(t, *m, "@"); !m.composer.MentionActive() {
					t.Fatal("precondition: a typed @ did not open the picker")
				}
			},
			view: func(m Model) string {
				lines, _ := m.composer.MentionPicker(60, 4)
				return strings.Join(lines, "\n")
			}},
		{name: "the rail", shows: "PINNED",
			view: func(m Model) string { return m.rail.View() }},
		{name: "contacts", shows: "mira",
			open: func(t *testing.T, m *Model) {
				m.contacts.SetVisible(true)
				m.contacts.SetContactsForTest([]*telegram.User{
					{ID: sceneMira, FirstName: "mira"},
					{ID: sceneJonas, FirstName: "jonas"},
				})
			},
			view: func(m Model) string { return m.contacts.View() }},
		{name: "contacts with none", bare: true, shows: "No items",
			open: func(t *testing.T, m *Model) { m.contacts.SetVisible(true) },
			view: func(m Model) string { return m.contacts.View() }},
		{name: "search", shows: "Messages",
			open: func(t *testing.T, m *Model) {
				m.search.SetVisible(true)
				m.search.SetQuery("deploy")
			},
			view: func(m Model) string { return m.search.View() }},
		{name: "help", shows: "Quit",
			open: func(t *testing.T, m *Model) { m.help.SetVisible(true) },
			view: func(m Model) string { return m.help.View() }},
		{name: "the palette", shows: "mark-read",
			open: func(t *testing.T, m *Model) { m.palette.Open() },
			view: func(m Model) string { return m.palette.View() }},
		{name: "the attach picker", shows: "notes.txt",
			open: func(t *testing.T, m *Model) {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				m.attach.Open(dir + string(filepath.Separator))
			},
			view: func(m Model) string { return m.attach.View() }},
		{name: "the forward picker", shows: "Nadia Feld",
			open: func(t *testing.T, m *Model) {
				m.forward.Open(
					forward.Source{ChatID: sceneInfra, MessageID: 2105, Preview: "rebased"},
					[]forward.Chat{{ID: sceneNadiaDM, Title: "Nadia Feld", Sigil: "@"}})
			},
			view: func(m Model) string { return m.forward.View() }},
		{name: "the reaction row", shows: "👍",
			open: func(t *testing.T, m *Model) { m.reactions.Open(sceneInfra, 2105, "") },
			view: func(m Model) string { return m.reactions.View() }},
		{name: "the media overlay", shows: "downloading",
			open: func(t *testing.T, m *Model) { m.mediaView.Open("photo · nadia", "downloading…") },
			view: func(m Model) string { return m.mediaView.View() }},
		{name: "a dialog", shows: "Quit",
			open: func(t *testing.T, m *Model) {
				// Unsent work is what makes quitting ask.
				m.setFocus(PanelComposer)
				*m = typeText(t, *m, "half a thought")
				next, _ := m.quitConfirming()
				*m = next.(Model)
				if m.dialog == nil {
					t.Fatal("precondition: quitting with a draft did not ask")
				}
			},
			view: func(m Model) string { return m.dialog.View() }},
		{name: "an overlay's surround", shows: "Quit",
			open: func(t *testing.T, m *Model) { m.help.SetVisible(true) },
			view: func(m Model) string { return m.View().Content }},
		{name: "the sign-in screen", shows: "phone",
			open: func(t *testing.T, m *Model) { m.screen = ScreenAuth },
			view: func(m Model) string { return m.View().Content }},
		{name: "the fatal error", shows: "Disconnected",
			open: func(t *testing.T, m *Model) { m.fatalError = "connection closed" },
			view: func(m Model) string { return m.View().Content }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := onMainScreen(markedModel(before, rampBefore), PanelChatList)
			if tt.bare {
				updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
				m = updated.(Model)
			} else {
				m = dressScene(t, m, mainScene(), 120, 30, true)
			}
			if tt.open != nil {
				tt.open(t, &m)
			}

			// Drawn first, so anything that caches what it drew has.
			drawn := tt.view(m)
			if !strings.Contains(ansi.Strip(drawn), tt.shows) {
				t.Fatalf("precondition: %s does not show %q:\n%s",
					tt.name, tt.shows, ansi.Strip(drawn))
			}
			assertDrawnIn(t, "before the switch", drawn, knownBefore, nil)

			m.applyRoles(after, rampAfter)
			assertDrawnIn(t, "after the switch", tt.view(m), knownAfter, knownBefore)
		})
	}
}

// A theme applied to a running app is resolved at the colour depth decided
// at startup. The environment is not consulted again — it may say something
// else by now, and a 256-colour palette on a truecolour terminal or the
// reverse is a different app from the one that started — and the terminal
// is never asked at all. What the theme warns about comes back to the
// caller, which is the one that can show it.
func TestAThemeIsAppliedAtTheStartupColourDepth(t *testing.T) {
	spec := &config.ThemeSpec{
		Name: "test", Source: "test.toml", Inherit: config.ThemeDark,
		Colors: map[string]string{"cyan": "#123456", "red": "not a colour"},
	}

	for _, trueColor := range []bool{true, false} {
		t.Run(map[bool]string{true: "truecolour", false: "256"}[trueColor], func(t *testing.T) {
			// The environment now says the opposite of what startup decided.
			t.Setenv("TERM", "xterm-256color")
			t.Setenv("COLORTERM", map[bool]string{true: "", false: "truecolor"}[trueColor])
			if theme.SupportsTrueColor() == trueColor {
				t.Fatal("precondition: the environment agrees with startup")
			}

			marker, _ := theme.MarkerRoles()
			cfg := &config.Config{}
			m := newModel(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg), trueColor, marker, nil)
			warnings := m.applyThemeSpec(spec, config.ThemeDark)

			want, _, wantWarnings := theme.RolesForSpec(spec, config.ThemeDark, trueColor)
			if m.roles != want {
				t.Errorf("the app's palette is not the theme at the startup depth:\n got %+v\nwant %+v",
					m.roles, want)
			}
			if len(wantWarnings) == 0 {
				t.Fatal("precondition: the spec draws no warning")
			}
			if strings.Join(warnings, "\n") != strings.Join(wantWarnings, "\n") {
				t.Errorf("warnings = %q, want the theme's own %q", warnings, wantWarnings)
			}

			// And it went where a palette goes: the hint bar's keys are cyan.
			m.hintBar.SetWidth(80)
			m.hintBar.SetHints([]hintbar.Hint{{Key: "q", Label: "quit"}})
			if bar := m.hintBar.View(); !strings.Contains(bar, foregroundSeq(want.Cyan)) {
				t.Errorf("the hint bar does not draw the theme's cyan:\n%s",
					strings.ReplaceAll(bar, "\x1b", "ESC"))
			}
		})
	}
}

// markedModel is the app built in a marker palette, through the one
// constructor the real app uses, with nothing in it.
func markedModel(roles theme.Roles, ramp []lipgloss.Color) Model {
	cfg := &config.Config{}
	return newModel(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg), true, roles, ramp)
}

// assertDrawnIn fails for every colour in view that is not in want, naming
// the role when it is one of old's — the palette a switch should have
// replaced. Each colour is reported once, however often it was drawn.
func assertDrawnIn(t *testing.T, when, view string, want, old map[string]string) {
	t.Helper()
	found := truecolourSeq.FindAllStringSubmatch(view, -1)
	if len(found) == 0 {
		t.Fatalf("%s: drew no colour at all:\n%s", when, ansi.Strip(view))
	}
	reported := map[string]bool{}
	for _, c := range found {
		rgb := c[1]
		if _, ok := want[rgb]; ok || reported[rgb] {
			continue
		}
		reported[rgb] = true
		if role, ok := old[rgb]; ok {
			t.Errorf("%s: still draws the old palette's %s", when, role)
			continue
		}
		t.Errorf("%s: drew rgb(%s), which is not in the palette", when, rgb)
	}
}
