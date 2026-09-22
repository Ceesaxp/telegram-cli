package app

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/palette"
)

// pickCommand is a command whose argument has candidates, for the tests
// here: no shipped command offers any yet. The values come from *fruits
// each time they are asked for, and Run records what it was given in *got.
func pickCommand(fruits *[]palette.Arg, got *[]string) Command {
	return Command{
		Name:        "pick",
		Arg:         ArgRequired,
		Placeholder: "<fruit>",
		Description: "pick a fruit",
		ArgCandidates: func(Model) []palette.Arg {
			return *fruits
		},
		Run: func(m Model, arg string) (Model, tea.Cmd, string) {
			*got = append(*got, arg)
			return m, nil, "picked " + arg
		},
	}
}

func fruitArgs(names ...string) []palette.Arg {
	out := make([]palette.Arg, 0, len(names))
	for _, n := range names {
		out = append(out, palette.Arg{Value: n, Description: "fruit"})
	}
	return out
}

// addTestCommand registers c in the command registry for the rest of the
// test.
func addTestCommand(t *testing.T, c Command) {
	t.Helper()
	testCommands = append(testCommands, c)
	t.Cleanup(func() { testCommands = nil })
}

// typeKeys sends each rune of s to the app as its own keypress.
func typeKeys(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = update(t, m, string(r))
	}
	return m
}

// TestPaletteItemsCarryTheCandidates: the projection hands the palette a
// command's values, and nothing to a command that has none.
func TestPaletteItemsCarryTheCandidates(t *testing.T) {
	fruits, got := fruitArgs("apple", "cherry"), []string(nil)
	m := mainModel(t, PanelChatList)
	addTestCommand(t, pickCommand(&fruits, &got))

	for _, it := range m.paletteItems() {
		switch it.Name {
		case "pick":
			if !reflect.DeepEqual(it.Candidates, fruits) {
				t.Errorf("pick offers %+v, want %+v", it.Candidates, fruits)
			}
		case "theme":
			// Its own: see themecmd_test.go.
		default:
			if it.Candidates != nil {
				t.Errorf("%q offers %+v, but has no candidates", it.Name, it.Candidates)
			}
		}
	}
}

// TestACommandRunsWithTheChosenValue is the whole path: `:` opens the
// palette, a partial argument filters the values, Enter closes it, and the
// command's Run hears the value that was highlighted — not the letters that
// were typed.
func TestACommandRunsWithTheChosenValue(t *testing.T) {
	fruits, got := fruitArgs("apple", "banana", "cherry"), []string(nil)
	m := mainModel(t, PanelChatList)
	addTestCommand(t, pickCommand(&fruits, &got))

	m = update(t, m, ":")
	m = typeKeys(t, m, "pick ch")
	m = update(t, m, "\r")

	if m.palette.IsVisible() {
		t.Error("the palette stayed open after running the command")
	}
	if !reflect.DeepEqual(got, []string{"cherry"}) {
		t.Errorf("Run was given %q, want [cherry]", got)
	}
}

// TestTheArrowsChooseTheValueThatRuns: moving the highlight changes what
// Enter runs, since what runs is what is highlighted.
func TestTheArrowsChooseTheValueThatRuns(t *testing.T) {
	fruits, got := fruitArgs("apple", "banana", "cherry"), []string(nil)
	m := mainModel(t, PanelChatList)
	addTestCommand(t, pickCommand(&fruits, &got))

	m = update(t, m, ":")
	m = typeKeys(t, m, "pick ")
	m = update(t, m, "\x1b[B") // down, to banana
	m = update(t, m, "\r")

	if !reflect.DeepEqual(got, []string{"banana"}) {
		t.Errorf("Run was given %q, want [banana]", got)
	}
}

// TestAnUnmatchedValueReachesTheCommandAsTyped: the command is the one that
// knows what a bad value is, so it has to hear one to say so.
func TestAnUnmatchedValueReachesTheCommandAsTyped(t *testing.T) {
	fruits, got := fruitArgs("apple"), []string(nil)
	m := mainModel(t, PanelChatList)
	addTestCommand(t, pickCommand(&fruits, &got))

	m = update(t, m, ":")
	m = typeKeys(t, m, "pick durian")
	m = update(t, m, "\r")

	if !reflect.DeepEqual(got, []string{"durian"}) {
		t.Errorf("Run was given %q, want the typed [durian]", got)
	}
}

// TestTheValuesAreReadWhenThePaletteOpens: the list reflects the world as
// it is when `:` is pressed — a theme file dropped in since startup, or
// since the palette was last open, is there to choose.
func TestTheValuesAreReadWhenThePaletteOpens(t *testing.T) {
	fruits, got := fruitArgs("apple"), []string(nil)
	m := mainModel(t, PanelChatList)
	addTestCommand(t, pickCommand(&fruits, &got))

	m = update(t, m, ":")
	m = update(t, m, "\x1b") // close it again

	fruits = fruitArgs("apple", "quince")
	m = update(t, m, ":")
	m = typeKeys(t, m, "pick ")

	var listed []string
	for _, a := range m.palette.ArgMatches() {
		listed = append(listed, a.Value)
	}
	if !reflect.DeepEqual(listed, []string{"apple", "quince"}) {
		t.Errorf("the reopened palette lists %v, want [apple quince]", listed)
	}
}
