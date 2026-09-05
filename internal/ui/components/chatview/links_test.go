package chatview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

func linkMessage(id int64, text string, entities ...*telegram.TextEntity) *telegram.Message {
	return &telegram.Message{
		ID: id, ChatID: testChatID,
		Content: &telegram.MessageText{
			Text: &telegram.FormattedText{Text: text, Entities: entities},
		},
	}
}

func textURL(off, length int32, url string) *telegram.TextEntity {
	return &telegram.TextEntity{Offset: off, Length: length, Type: &telegram.TextEntityTypeTextURL{URL: url}}
}

// linkModel is a thread with one message carrying two links under the cursor.
func linkModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel()
	m.store.Messages.Activate(testChatID)
	m.store.Messages.Append(testChatID, linkMessage(1, "see one and two here",
		textURL(4, 3, "https://first.example"),
		textURL(12, 3, "https://second.example"),
	))
	m.MarkLoadedForTest()
	m.moveCursor(0)
	return m
}

// press feeds a sequence of keys, reusing the package's own key/specialKey
// helpers so these tests type what every other test here types.
func press(m Model, keys ...string) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		switch k {
		case "enter":
			m, cmd = m.handleKey(specialKey(tea.KeyEnter))
		case "esc":
			m, cmd = m.handleKey(specialKey(tea.KeyEscape))
		case "home":
			m, cmd = m.handleKey(specialKey(tea.KeyHome))
		default:
			m, cmd = m.handleKey(key(rune(k[0])))
		}
	}
	return m, cmd
}

func infoOf(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	if msg, ok := cmd().(MediaPlayMsg); ok {
		return msg.Info
	}
	return ""
}

// g is a prefix now. Bare g does nothing — including not jumping to the top,
// which it used to do — and the header says a g is pending, because a prefix
// you cannot see is indistinguishable from a broken key.
func TestBareGIsAPrefixAndSaysSo(t *testing.T) {
	m := linkModel(t)
	m.scrollOffset = 0

	m, cmd := press(m, "g")
	if cmd != nil {
		t.Error("bare g produced a command")
	}
	if !m.pendingG {
		t.Fatal("g did not arm the prefix")
	}
	if m.scrollOffset != 0 {
		t.Errorf("bare g scrolled to %d — it should no longer be Top on its own", m.scrollOffset)
	}
	if got := m.prefixLabel(); got != "g" {
		t.Errorf("prefixLabel = %q, want %q", got, "g")
	}
}

// gg is what g used to be, and home still is — one implementation, two
// spellings.
func TestGGGoesToTheTop(t *testing.T) {
	m := linkModel(t)
	m.scrollOffset = 0

	gg, _ := press(m, "g", "g")
	home, _ := press(m, "home")

	if gg.scrollOffset != home.scrollOffset {
		t.Fatalf("gg scrolled to %d, home to %d — they must agree", gg.scrollOffset, home.scrollOffset)
	}
	if gg.pendingG {
		t.Error("gg left the prefix armed")
	}
}

// A prefix thought better of must not eat the next keystroke — the same rule
// the count prefix follows. Asserted as an equivalence: whatever j does on
// its own, g-then-j must do the same, and the k below proves the motion is
// actually observable here rather than the equivalence holding vacuously.
func TestAnUnknownSuffixStillDoesItsOwnJob(t *testing.T) {
	base := linkModel(t)
	base.store.Messages.Append(testChatID, linkMessage(2, "second"))
	base.MarkLoadedForTest()
	base.moveCursor(0)

	plain, _ := press(base, "k")
	if plain.cursorID == base.cursorID {
		t.Fatalf("precondition: k does not move the cursor in this fixture (still %d)", plain.cursorID)
	}

	prefixed, _ := press(base, "g", "k")
	if prefixed.pendingG {
		t.Error("the pending g survived an unknown suffix")
	}
	if prefixed.cursorID != plain.cursorID {
		t.Errorf("g then k left the cursor at %d, plain k at %d — the dropped prefix ate the key",
			prefixed.cursorID, plain.cursorID)
	}
}

func TestGXArmsAndCyclesLinks(t *testing.T) {
	m := linkModel(t)

	m, cmd := press(m, "g", "x")
	if m.armed.index != 1 {
		t.Fatalf("gx armed index %d, want 1", m.armed.index)
	}
	// The destination goes on screen — that is the whole reason gx arms
	// rather than opening.
	if got := infoOf(cmd); !strings.Contains(got, "https://first.example") {
		t.Errorf("armed notice = %q, want the destination", got)
	}

	m, cmd = press(m, "g", "x")
	if m.armed.index != 2 {
		t.Fatalf("second gx armed index %d, want 2", m.armed.index)
	}
	if got := infoOf(cmd); !strings.Contains(got, "https://second.example") {
		t.Errorf("second notice = %q", got)
	}

	// It wraps: three presses on two links is a reader changing their mind,
	// not one who wants the list to end.
	m, _ = press(m, "g", "x")
	if m.armed.index != 1 {
		t.Errorf("third gx armed index %d, want it to wrap to 1", m.armed.index)
	}
}

// The armed link is marked in the body, and only that link.
func TestTheArmedLinkIsVisible(t *testing.T) {
	m := linkModel(t)
	m.SetSize(60, 20)

	before := m.View()
	armed, _ := press(m, "g", "x")
	after := armed.View()

	if before == after {
		t.Fatal("arming a link changed nothing on screen")
	}
	if ansi.Strip(before) != ansi.Strip(after) {
		t.Error("arming a link changed the text, not just its styling")
	}
}

func TestEscapeDropsTheArmedLink(t *testing.T) {
	m := linkModel(t)
	m, _ = press(m, "g", "x")
	if !m.HasArmedLink() {
		t.Fatal("precondition: nothing armed")
	}

	m, _ = press(m, "esc")
	if m.HasArmedLink() {
		t.Error("escape did not drop the armed link")
	}
}

// A message with nothing to follow says so, rather than leaving the reader
// wondering whether the key works.
func TestGXOnALinklessMessageSaysSo(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Activate(testChatID)
	m.store.Messages.Append(testChatID, linkMessage(1, "just words"))
	m.MarkLoadedForTest()
	m.moveCursor(0)

	m, cmd := press(m, "g", "x")
	if m.HasArmedLink() {
		t.Error("something got armed on a message with no links")
	}
	if got := infoOf(cmd); !strings.Contains(got, "no links") {
		t.Errorf("notice = %q, want it to say there are none", got)
	}
}

// A scheme this client will not hand to the platform opener is still armed
// and still says why — it is visible on screen, so a key that skipped it
// silently would look broken.
func TestARefusedSchemeIsArmedButNotOpened(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Activate(testChatID)
	m.store.Messages.Append(testChatID, linkMessage(1, "grab it",
		textURL(0, 4, "ftp://files.example/x")))
	m.MarkLoadedForTest()
	m.moveCursor(0)

	m, cmd := press(m, "g", "x")
	if !m.HasArmedLink() {
		t.Fatal("an ftp link could not be armed at all")
	}
	if got := infoOf(cmd); !strings.Contains(got, "will not open") {
		t.Errorf("armed notice = %q, want a warning about the scheme", got)
	}

	m, cmd = press(m, "enter")
	if got := infoOf(cmd); !strings.Contains(got, "refusing to open") {
		t.Errorf("enter on a refused scheme = %q", got)
	}
}

// Switching chats drops the link cursor: it belonged to a message in the
// chat that is no longer open.
func TestOpeningAnotherChatDropsTheArmedLink(t *testing.T) {
	m := linkModel(t)
	m, _ = press(m, "g", "x")

	m.OpenChatAt(testChatID+1, "elsewhere", 0)
	if m.armed.index != 0 || m.pendingG {
		t.Errorf("the switch left state behind: armed=%d pendingG=%v", m.armed.index, m.pendingG)
	}
}
