package reactionpicker

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// TestMain pins a colour profile: under `go test` lipgloss resolves to Ascii
// and every styling assertion passes whatever the style was.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

func press(t *testing.T, seq string) tea.KeyPressMsg {
	t.Helper()
	var d uv.EventDecoder
	n, ev := d.Decode([]byte(seq))
	if n != len(seq) {
		t.Fatalf("decode(%q) consumed %d of %d bytes", seq, n, len(seq))
	}
	kp, ok := ev.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("decode(%q) produced %T, want uv.KeyPressEvent", seq, ev)
	}
	return tea.KeyPressMsg(kp)
}

func opened(t *testing.T, mine string) Model {
	t.Helper()
	m := New(theme.DarkRoles(false))
	m.SetWidth(120)
	m.Open(7, 42, mine)
	return m
}

// chosen runs a key through the picker and returns what it reported.
func chosen(t *testing.T, m Model, seq string) (Model, tea.Msg) {
	t.Helper()
	next, cmd := m.Update(press(t, seq))
	if cmd == nil {
		return next, nil
	}
	return next, cmd()
}

func TestPickingAReactionReportsIt(t *testing.T) {
	m := opened(t, "")

	_, msg := chosen(t, m, "1")
	got, ok := msg.(ChosenMsg)
	if !ok {
		t.Fatalf("got %T, want a ChosenMsg", msg)
	}
	if got.Emoji != Reactions[0] {
		t.Errorf("emoji = %q, want %q", got.Emoji, Reactions[0])
	}
	if got.ChatId != 7 || got.MessageId != 42 {
		t.Errorf("got %+v, want the message the picker was opened on", got)
	}
}

// TestTheRowIsNumberedOneThroughZero. The emoji are not typeable on most
// keyboards, so the row is also a list of ten.
func TestTheRowIsNumberedOneThroughZero(t *testing.T) {
	for key, want := range map[string]int{"1": 0, "5": 4, "9": 8, "0": 9} {
		_, msg := chosen(t, opened(t, ""), key)
		got, ok := msg.(ChosenMsg)
		if !ok {
			t.Fatalf("%s: got %T, want a ChosenMsg", key, msg)
		}
		if got.Emoji != Reactions[want] {
			t.Errorf("%s chose %q, want %q", key, got.Emoji, Reactions[want])
		}
	}
}

// TestChoosingTheOneYouLeftTakesItOff. Telegram models removing a reaction
// as sending an empty list, and every other client's picker toggles on the
// same press.
func TestChoosingTheOneYouLeftTakesItOff(t *testing.T) {
	m := opened(t, Reactions[2])

	// The picker opens ON it, so enter is the whole gesture.
	_, msg := chosen(t, m, "\r")
	got, ok := msg.(ChosenMsg)
	if !ok {
		t.Fatalf("got %T, want a ChosenMsg", msg)
	}
	if got.Emoji != "" {
		t.Errorf("emoji = %q, want empty — that is how a removal is spelled", got.Emoji)
	}

	// A different one is a replacement, not a removal.
	_, msg = chosen(t, opened(t, Reactions[2]), "1")
	if got := msg.(ChosenMsg); got.Emoji != Reactions[0] {
		t.Errorf("emoji = %q, want %q", got.Emoji, Reactions[0])
	}
}

func TestArrowsWalkTheRow(t *testing.T) {
	m := opened(t, "")
	if m.index != 0 {
		t.Fatalf("index = %d, want 0", m.index)
	}

	m, _ = m.Update(press(t, "\x1b[C")) // right
	if m.index != 1 {
		t.Errorf("right: index = %d, want 1", m.index)
	}

	// And it wraps at BOTH ends, so there is no edge of the row to get
	// stuck against in either direction.
	m, _ = m.Update(press(t, "\x1b[D")) // left
	m, _ = m.Update(press(t, "\x1b[D"))
	if m.index != len(Reactions)-1 {
		t.Errorf("left past the start: index = %d, want %d", m.index, len(Reactions)-1)
	}

	m, _ = m.Update(press(t, "\x1b[C")) // right, off the far end
	if m.index != 0 {
		t.Errorf("right past the end: index = %d, want 0", m.index)
	}
}

func TestEscapeLeavesTheMessageAlone(t *testing.T) {
	m, msg := chosen(t, opened(t, ""), "\x1b")
	if _, ok := msg.(CancelledMsg); !ok {
		t.Fatalf("got %T, want a CancelledMsg", msg)
	}
	if m.IsVisible() {
		t.Error("escape left the picker open")
	}
}

// TestAClosedPickerIgnoresKeys, so a keystroke that arrives after the row
// has gone cannot react a second time.
func TestAClosedPickerIgnoresKeys(t *testing.T) {
	m := New(theme.DarkRoles(false))
	if _, cmd := m.Update(press(t, "1")); cmd != nil {
		t.Fatal("a closed picker acted on a key")
	}
}

// TestTheRowFitsThePane, budgeted with cell.Reserve because every cell of it
// is an emoji — the one string the tables and the terminal disagree about.
func TestTheRowFitsThePane(t *testing.T) {
	for width := 20; width <= 200; width += 7 {
		m := New(theme.DarkRoles(false))
		m.SetWidth(width)
		m.Open(7, 42, "")

		view := m.View()
		if got := cell.Width(view); got > width {
			t.Fatalf("at width %d the row is %d cells: %q", width, got, ansi.Strip(view))
		}
		if open := cell.OpenStyle(view); open != "" {
			t.Fatalf("at width %d the row leaves %q open", width, open)
		}
	}
}

// TestTheRowMarksWhatYouAlreadyLeft, so the press that takes it off is not a
// guess.
func TestTheRowMarksWhatYouAlreadyLeft(t *testing.T) {
	plain, mine := opened(t, ""), opened(t, Reactions[2])
	if plain.View() == mine.View() {
		t.Fatal("a message you have reacted to draws the same row as one you have not")
	}
	if !strings.Contains(ansi.Strip(mine.View()), "takes yours off") {
		t.Errorf("the row does not say what enter will do:\n%s", ansi.Strip(mine.View()))
	}
}

func TestAHiddenPickerDrawsNothing(t *testing.T) {
	if got := New(theme.DarkRoles(false)).View(); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
