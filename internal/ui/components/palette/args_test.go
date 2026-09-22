package palette

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

// themeItems is testItems plus a command that offers its argument's values,
// the way :theme will list the themes it can apply.
func themeItems() []Item {
	return append(testItems(), Item{
		Name:        "theme",
		Args:        "<name>",
		Description: "switch the colour theme",
		Candidates: []Arg{
			{Value: "dark", Description: "builtin"},
			{Value: "light", Description: "builtin"},
			{Value: "dracula", Description: "~/.config/tele-tui/themes"},
			{Value: "gruvbox", Description: "~/.config/tele-tui/themes"},
		},
	})
}

func openWithCandidates(t *testing.T) Model {
	t.Helper()
	m := New(theme.DarkRoles(false))
	m.SetItems(themeItems())
	m.Open()
	return m
}

func values(args []Arg) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, a.Value)
	}
	return out
}

// --- Listing --------------------------------------------------------------

// TestTheCommandWordAndASpaceListEveryCandidate: once the name is typed and
// the space after it, what is left to choose is the argument, so that is
// what the palette lists — all of it, before a letter narrows it.
func TestTheCommandWordAndASpaceListEveryCandidate(t *testing.T) {
	m := typeString(t, openWithCandidates(t), "theme ")

	got := values(m.ArgMatches())
	want := []string{"dark", "light", "dracula", "gruvbox"}
	if len(got) != len(want) {
		t.Fatalf("%q listed %v, want every candidate %v", m.Query(), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%q listed %v, want %v in the order given", m.Query(), got, want)
			break
		}
	}
}

// TestAPartialArgumentFiltersFuzzily: the argument narrows by the command
// names' own rules — prefix matches first, then subsequences — so "dr"
// finds dracula, and dark behind it for holding a d and then an r.
func TestAPartialArgumentFiltersFuzzily(t *testing.T) {
	m := typeString(t, openWithCandidates(t), "theme dr")

	got := values(m.ArgMatches())
	if len(got) != 2 || got[0] != "dracula" || got[1] != "dark" {
		t.Errorf("%q listed %v, want [dracula dark]: the prefix match first", m.Query(), got)
	}
}

// TestArgumentMatchingIgnoresCase: a value is usually somebody's file name,
// and nobody remembers how they capitalised it.
func TestArgumentMatchingIgnoresCase(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetItems([]Item{{Name: "theme", Args: "<name>", Candidates: []Arg{{Value: "Nord"}, {Value: "dark"}}}})

	for _, typed := range []string{"theme no", "theme NO"} {
		m.Open()
		m = typeString(t, m, typed)
		if got := values(m.ArgMatches()); len(got) != 1 || got[0] != "Nord" {
			t.Errorf("%q listed %v, want [Nord]", typed, got)
		}
		m.Close()
	}
}

// TestOnlyAnExactCommandWordListsCandidates: the palette goes on listing
// commands until the command word is typed out in full and followed by a
// space. A half-typed name has not picked a command yet, and a name with no
// space after it is still being typed.
func TestOnlyAnExactCommandWordListsCandidates(t *testing.T) {
	for _, typed := range []string{"theme", "them ", "the"} {
		m := typeString(t, openWithCandidates(t), typed)
		if got := m.ArgMatches(); got != nil {
			t.Errorf("%q listed the values %v, want commands still", typed, values(got))
		}
	}
}

// TestACommandWithoutCandidatesIsUnchanged: search takes free text, so its
// argument lists nothing and the palette keeps showing the command, exactly
// as before candidates existed.
func TestACommandWithoutCandidatesIsUnchanged(t *testing.T) {
	for _, typed := range []string{"search ", "search foo"} {
		m := typeString(t, openWithCandidates(t), typed)
		if got := m.ArgMatches(); got != nil {
			t.Errorf("%q listed the values %v, want none", typed, values(got))
		}
		if got := names(m.Matches()); len(got) != 1 || got[0] != "search" {
			t.Errorf("%q matched %v, want [search]", typed, got)
		}
	}
}

// --- Keys -----------------------------------------------------------------

// TestTheArrowsMoveThroughTheValues: while the values are listed, the arrows
// walk them, clamped at both ends as the command list is.
func TestTheArrowsMoveThroughTheValues(t *testing.T) {
	const up, down = "\x1b[A", "\x1b[B"
	m := typeString(t, openWithCandidates(t), "theme ")

	steps := []struct {
		key, want string
	}{
		{"", "dark"},
		{down, "light"},
		{down, "dracula"},
		{up, "light"},
		{up, "dark"},
		{up, "dark"},
		{down, "light"},
		{down, "dracula"},
		{down, "gruvbox"},
		{down, "gruvbox"},
	}
	for i, s := range steps {
		if s.key != "" {
			m, _ = m.Update(decodeKey(t, s.key))
		}
		got, ok := m.SelectedArg()
		if !ok || got.Value != s.want {
			t.Fatalf("step %d: selected %q (ok=%v), want %q", i, got.Value, ok, s.want)
		}
	}
}

// TestSelectedIsTheCommandWhileChoosingItsArgument: the cursor is walking
// the values, not the commands, and must not be read as an index into the
// command list — two rows down that is nothing, or some other command.
func TestSelectedIsTheCommandWhileChoosingItsArgument(t *testing.T) {
	m := typeString(t, openWithCandidates(t), "theme ")
	m, _ = m.Update(decodeKey(t, "\x1b[B"))
	m, _ = m.Update(decodeKey(t, "\x1b[B"))

	if got, ok := m.Selected(); !ok || got.Name != "theme" {
		t.Errorf("Selected() = %q (ok=%v) while choosing its argument, want theme", got.Name, ok)
	}
}

// TestNothingIsSelectedWithNoMatchingValue, and nothing is selected while
// commands are listed either: SelectedArg is the argument Enter would take.
func TestNothingIsSelectedWithNoMatchingValue(t *testing.T) {
	for _, typed := range []string{"theme zzz", "theme", "search foo"} {
		m := typeString(t, openWithCandidates(t), typed)
		if got, ok := m.SelectedArg(); ok {
			t.Errorf("%q selected the value %q, want none", typed, got.Value)
		}
	}
}

// TestTabCompletesTheHighlightedValue: Tab writes the value out in full after
// the command word, the way it writes out a command's name.
func TestTabCompletesTheHighlightedValue(t *testing.T) {
	m := typeString(t, openWithCandidates(t), "theme dr")
	m, _ = m.Update(decodeKey(t, "\t"))
	if m.Query() != "theme dracula" {
		t.Errorf("Query() = %q after tab, want %q", m.Query(), "theme dracula")
	}

	m = typeString(t, openWithCandidates(t), "theme ")
	m, _ = m.Update(decodeKey(t, "\x1b[B"))
	m, _ = m.Update(decodeKey(t, "\t"))
	if m.Query() != "theme light" {
		t.Errorf("Query() = %q after down and tab, want %q", m.Query(), "theme light")
	}
}

func TestTabWithNoMatchingValueIsInert(t *testing.T) {
	m := typeString(t, openWithCandidates(t), "theme zzz")
	m, _ = m.Update(decodeKey(t, "\t"))
	if m.Query() != "theme zzz" {
		t.Errorf("Query() = %q, want it unchanged with nothing to complete", m.Query())
	}
}

// --- Enter ----------------------------------------------------------------

// TestEnterRunsTheCommandWithTheHighlightedValue: Enter still reports
// ActionRun, and Line is what the app runs — the value the user sees
// highlighted, whether or not they typed all of it.
func TestEnterRunsTheCommandWithTheHighlightedValue(t *testing.T) {
	m := typeString(t, openWithCandidates(t), "theme dr")
	m, action := m.Update(decodeKey(t, "\r"))
	if action != ActionRun {
		t.Fatalf("enter produced %v, want ActionRun", action)
	}
	if got := m.Line(); got != "theme dracula" {
		t.Errorf("Line() = %q, want %q", got, "theme dracula")
	}

	m = typeString(t, openWithCandidates(t), "theme ")
	m, _ = m.Update(decodeKey(t, "\x1b[B"))
	if got := m.Line(); got != "theme light" {
		t.Errorf("Line() = %q after moving down, want %q", got, "theme light")
	}
}

// TestEnterWithNoMatchingValueRunsWhatWasTyped: the command hears what the
// user typed and can say it is not a theme. Swallowing it here would close
// the palette on nothing, with no word about why.
func TestEnterWithNoMatchingValueRunsWhatWasTyped(t *testing.T) {
	m := typeString(t, openWithCandidates(t), "theme zzz")
	if got := m.Line(); got != "theme zzz" {
		t.Errorf("Line() = %q, want the typed %q", got, "theme zzz")
	}
}

// TestLineIsTheQueryWhileListingCommands: for everything that is not an
// argument with values, Enter runs the line as typed, exactly as before —
// including a half-typed name, which the app reports as unknown.
func TestLineIsTheQueryWhileListingCommands(t *testing.T) {
	for _, typed := range []string{"quit", "qu", "search foo bar", "theme", "them dr"} {
		m := typeString(t, openWithCandidates(t), typed)
		if got := m.Line(); got != typed {
			t.Errorf("Line() = %q, want the query %q", got, typed)
		}
	}
}

// TestAnExactlyTypedValueIsHighlightedFirst: Enter runs the highlighted
// value, and the order is the caller's. Were a longer value listed first,
// typing "nord" in full would still run "nord-light".
func TestAnExactlyTypedValueIsHighlightedFirst(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetItems([]Item{{Name: "theme", Args: "<name>", Candidates: []Arg{
		{Value: "neon-ride"}, {Value: "nord-light"}, {Value: "Nord"},
	}}})
	m.Open()
	m = typeString(t, m, "theme nord")

	got := values(m.ArgMatches())
	want := []string{"Nord", "nord-light", "neon-ride"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("%q listed %v, want the exact match first: %v", m.Query(), got, want)
	}
	if got := m.Line(); got != "theme Nord" {
		t.Errorf("Line() = %q, want %q", got, "theme Nord")
	}
}

// --- Rendering ------------------------------------------------------------

// TestTheValuesAreDrawnInsteadOfTheCommands: each row is a value and its
// description, and the other commands are gone from the list.
func TestTheValuesAreDrawnInsteadOfTheCommands(t *testing.T) {
	view := typeString(t, openWithCandidates(t), "theme ").View()

	for _, want := range []string{"dark", "light", "dracula", "gruvbox", "builtin", "~/.config/tele-tui/themes"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() does not show %q", want)
		}
	}
	for _, gone := range []string{"mark-read", "search all chats"} {
		if strings.Contains(view, gone) {
			t.Errorf("View() still shows the command row %q while listing values", gone)
		}
	}
}

// TestTheDescriptionIsDim: the value is what the row exists to show; where
// it lives is secondary copy, drawn as the overlays draw secondary copy.
func TestTheDescriptionIsDim(t *testing.T) {
	r := theme.DarkRoles(true)
	m := New(r)
	m.SetItems(themeItems())
	m.Open()
	m = typeString(t, m, "theme ")
	m, _ = m.Update(decodeKey(t, "\x1b[B")) // off the first row, so it is drawn unselected

	if want := theme.OverlayMuted(r).Render("builtin"); !strings.Contains(m.View(), want) {
		t.Errorf("the description is not drawn muted; want %q in:\n%s",
			want, strings.ReplaceAll(m.View(), "\x1b", "ESC"))
	}
}

func TestNoMatchingValueSaysSo(t *testing.T) {
	view := typeString(t, openWithCandidates(t), "theme zzz").View()
	if !strings.Contains(view, "no matching value") {
		t.Errorf("View() with no matching value does not say so:\n%s", view)
	}
}

// TestValueRowsAreExactlyWide is the frame-integrity property for the new
// rows: a long description, a wide-rune one — on the current row, beside
// its mark — and a value too long for the row on its own must all come out
// exactly as wide as every other row.
func TestValueRowsAreExactlyWide(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetItems([]Item{{Name: "theme", Args: "<name>", Candidates: []Arg{
		{Value: "dark", Description: strings.Repeat("a long description ", 10)},
		{Value: "四季", Description: strings.Repeat("四", 60), Current: true},
		{Value: strings.Repeat("very-long-theme-name-", 5), Description: "builtin"},
		{Value: strings.Repeat("長", 40)},
	}}})
	cases := map[string]string{
		"all values":  "theme ",
		"filtered":    "theme d",
		"no matches":  "theme zzz",
		"wide values": "theme 長",
	}
	for name, typed := range cases {
		t.Run(name, func(t *testing.T) {
			m.Open()
			view := typeString(t, m, typed).View()
			m.Close()
			assertUniformWidth(t, view)
		})
	}
}

// TestTheHighlightedValueStaysOnScreen: there are more themes than rows —
// nine examples ship, plus the two builtins — so walking down the list has
// to scroll it rather than walk the highlight off the bottom.
func TestTheHighlightedValueStaysOnScreen(t *testing.T) {
	var many []Arg
	for i := range maxRows + 4 {
		many = append(many, Arg{Value: "theme-" + itoa(i+10)})
	}
	m := New(theme.DarkRoles(false))
	m.SetItems([]Item{{Name: "theme", Args: "<name>", Candidates: many}})
	m.Open()
	m = typeString(t, m, "theme ")

	for range maxRows + 2 {
		m, _ = m.Update(decodeKey(t, "\x1b[B"))
	}
	selected, _ := m.SelectedArg()
	view := m.View()
	if !strings.Contains(view, selected.Value) {
		t.Errorf("the highlighted %q is not on screen:\n%s", selected.Value, view)
	}
	// Ten rows down a list of twelve: three scrolled off the top, one
	// still below.
	if !strings.Contains(view, "↑3 ↓1") {
		t.Errorf("View() does not count the rows it cannot show, three above and one below:\n%s", view)
	}
	assertUniformWidth(t, view)
}

// TestTheHiddenRowsAreCountedWhereTheyAre: once the list scrolls, rows are
// hidden above as well as below, and a single total says neither — twenty
// values opened on the sixteenth hide eight above and four below, not
// "twelve more". The wording is the help card's.
func TestTheHiddenRowsAreCountedWhereTheyAre(t *testing.T) {
	var twenty []Arg
	for i := range 20 {
		twenty = append(twenty, Arg{Value: "theme-" + itoa(i+10)})
	}
	withCurrent := func(i int) []Arg {
		args := append([]Arg(nil), twenty...)
		args[i].Current = true
		return args
	}
	tests := []struct {
		name    string
		current int
		want    string
	}{
		{"at the top", 0, "↓12 more"},
		{"scrolled into the middle", 15, "↑8 ↓4"},
		{"at the bottom", 19, "↑12 above"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(theme.DarkRoles(false))
			m.SetItems([]Item{{Name: "theme", Args: "<name>", Candidates: withCurrent(tt.current)}})
			m.Open()
			view := typeString(t, m, "theme ").View()

			if !strings.Contains(view, tt.want) {
				t.Errorf("View() does not say %q:\n%s", tt.want, view)
			}
			if strings.Contains(view, "+") {
				t.Errorf("View() still gives a single total:\n%s", view)
			}
			assertUniformWidth(t, view)
		})
	}
}

// TestDeletingIntoTheCommandWordListsCommandsAgain: the argument listing
// lasts exactly as long as the space that started it.
func TestDeletingIntoTheCommandWordListsCommandsAgain(t *testing.T) {
	m := typeString(t, openWithCandidates(t), "theme d")
	if m.ArgMatches() == nil {
		t.Fatal(`precondition: "theme d" listed no values`)
	}

	m, _ = m.Update(decodeKey(t, "\x7f")) // "theme "
	if m.ArgMatches() == nil {
		t.Errorf("%q stopped listing values with the space still there", m.Query())
	}

	m, _ = m.Update(decodeKey(t, "\x7f")) // "theme"
	if got := m.ArgMatches(); got != nil {
		t.Errorf("%q still lists the values %v, want commands", m.Query(), values(got))
	}
	if got := names(m.Matches()); len(got) != 1 || got[0] != "theme" {
		t.Errorf("%q matched %v, want [theme]", m.Query(), got)
	}
}

// --- The current value ----------------------------------------------------

// withCurrent is themeItems with one value marked as the one in use.
func withCurrent(value string) []Item {
	items := themeItems()
	cmd := &items[len(items)-1]
	for i := range cmd.Candidates {
		cmd.Candidates[i].Current = cmd.Candidates[i].Value == value
	}
	return items
}

func openWithCurrent(t *testing.T, value string) Model {
	t.Helper()
	m := New(theme.DarkRoles(false))
	m.SetItems(withCurrent(value))
	m.Open()
	return m
}

// TestTheListOpensOnTheCurrentValue: the command word and a space, and the
// highlight is on the value in use rather than on the first row. Enter
// straight away then keeps what is there, instead of switching to whatever
// happens to be listed first.
func TestTheListOpensOnTheCurrentValue(t *testing.T) {
	m := typeString(t, openWithCurrent(t, "dracula"), "theme ")

	if got, ok := m.SelectedArg(); !ok || got.Value != "dracula" {
		t.Errorf("selected %q (ok=%v) with nothing typed, want the current dracula", got.Value, ok)
	}
	if got := m.Line(); got != "theme dracula" {
		t.Errorf("Line() = %q, want %q", got, "theme dracula")
	}
}

// TestAFilterHighlightsTheBestMatchNotTheCurrentValue: once a letter is
// typed the user is choosing, and the best match is what they are choosing —
// the current value is not kept highlighted behind it.
func TestAFilterHighlightsTheBestMatchNotTheCurrentValue(t *testing.T) {
	m := typeString(t, openWithCurrent(t, "dark"), "theme dr")

	if got, ok := m.SelectedArg(); !ok || got.Value != "dracula" {
		t.Errorf("selected %q (ok=%v) for %q, want the prefix match dracula", got.Value, ok, m.Query())
	}
}

// TestClearingTheFilterReturnsToTheCurrentValue: back to nothing typed is
// back to the list as it opened.
func TestClearingTheFilterReturnsToTheCurrentValue(t *testing.T) {
	m := typeString(t, openWithCurrent(t, "gruvbox"), "theme l")
	m, _ = m.Update(decodeKey(t, "\x7f"))

	if got, ok := m.SelectedArg(); !ok || got.Value != "gruvbox" {
		t.Errorf("selected %q (ok=%v) after deleting the filter, want the current gruvbox", got.Value, ok)
	}
}

// TestWithNoCurrentValueTheListOpensAtTheTop: nothing is marked when the
// theme in use is not one the list can name — a file named by its path, say.
func TestWithNoCurrentValueTheListOpensAtTheTop(t *testing.T) {
	m := typeString(t, openWithCandidates(t), "theme ")

	if got, ok := m.SelectedArg(); !ok || got.Value != "dark" {
		t.Errorf("selected %q (ok=%v), want the first row", got.Value, ok)
	}
}

// TestTheCurrentValueIsMarked: the row in use says so, in the dim of the
// description beside it, and no other row does.
func TestTheCurrentValueIsMarked(t *testing.T) {
	r := theme.DarkRoles(true)
	m := New(r)
	m.SetItems(withCurrent("dracula"))
	m.Open()
	m = typeString(t, m, "theme ")
	m, _ = m.Update(decodeKey(t, "\x1b[B")) // off it, so it is drawn unselected

	for _, line := range strings.Split(m.View(), "\n") {
		plain := ansi.Strip(line)
		marked := strings.Contains(plain, "current")
		switch {
		case strings.Contains(plain, "dracula") && !marked:
			t.Errorf("the current row is not marked: %q", plain)
		case !strings.Contains(plain, "dracula") && marked:
			t.Errorf("a row that is not current is marked: %q", plain)
		}
	}
	if want := theme.OverlayMuted(r).Render("current"); !strings.Contains(m.View(), want) {
		t.Errorf("the mark is not drawn muted; want %q in:\n%s",
			want, strings.ReplaceAll(m.View(), "\x1b", "ESC"))
	}
	assertUniformWidth(t, m.View())
}
