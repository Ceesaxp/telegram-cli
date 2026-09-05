package chatview

import (
	"path/filepath"
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

// The destination shown is the destination opened. An edit landing between
// gx and enter used to re-derive the link at the same index, so enter could
// open a URI the reader was never shown — the same mistake the forward
// picker's captured Source exists to prevent.
func TestTheArmedDestinationIsFrozen(t *testing.T) {
	m := linkModel(t)
	m, _ = press(m, "g", "x")

	shown := m.armed.uri
	if shown != "https://first.example" {
		t.Fatalf("precondition: armed %q", shown)
	}

	// The message is replaced under the decision, same entity position,
	// different destination.
	m.store.Messages.UpdateMessage(testChatID, 1, linkMessage(1, "see one and two here",
		textURL(4, 3, "https://attacker.example"),
		textURL(12, 3, "https://second.example"),
	))

	if got := m.armed.uri; got != shown {
		t.Errorf("armed URI changed under an edit: %q, was %q", got, shown)
	}
	if got := m.armed.safeURI; got != "https://first.example" {
		t.Errorf("the URI that would be opened is %q, want the one that was shown", got)
	}
}

// And the other half: a replaced message releases the cursor, because the
// marked range now describes text that is not there any more.
func TestAReplacedMessageDropsTheArmedLink(t *testing.T) {
	m := linkModel(t)
	m, _ = press(m, "g", "x")
	if !m.HasArmedLink() {
		t.Fatal("precondition: nothing armed")
	}

	m, _ = m.Update(refetchedMsg{chatID: testChatID, messages: []*telegram.Message{
		linkMessage(1, "see one and two here", textURL(4, 3, "https://attacker.example")),
	}})
	if m.HasArmedLink() {
		t.Error("the armed link survived its message being replaced")
	}
}

// A link the reader cannot see cannot be armed: a hidden spoiler paints
// foreground and background alike, so the mark would be invisible, and
// revealing it to show the mark would defeat the spoiler.
func TestSpoileredLinksAreNotArmedUntilRevealed(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Activate(testChatID)
	m.store.Messages.Append(testChatID, &telegram.Message{
		ID: 1, ChatID: testChatID,
		Content: &telegram.MessageText{Text: &telegram.FormattedText{
			Text: "open secret here",
			Entities: []*telegram.TextEntity{
				textURL(5, 6, "https://hidden.example"),
				{Offset: 5, Length: 6, Type: &telegram.TextEntityTypeSpoiler{}},
			},
		}},
	})
	m.MarkLoadedForTest()
	m.moveCursor(0)

	m, cmd := press(m, "g", "x")
	if m.HasArmedLink() {
		t.Error("a link under an unrevealed spoiler was armed — its mark would be invisible")
	}
	if got := infoOf(cmd); !strings.Contains(got, "spoiler") {
		t.Errorf("notice = %q, want it to say to reveal them first", got)
	}

	// After x, it arms normally.
	m.revealedID = 1
	m, _ = press(m, "g", "x")
	if !m.HasArmedLink() {
		t.Error("a revealed spoiler's link still could not be armed")
	}
}

// The opener must never be a command interpreter.
//
// SafeLinkURI deliberately passes `&`, `|`, `<`, `>` and `^` through — they
// are printable ASCII and legal in a query string — so `cmd /c start` would
// read `https://example.invalid/?x&calc` as a URL AND a second command, from
// a string a stranger put in a message. Go's Windows argument quoting does
// not help: it quotes for spaces and quotes, not for shell metacharacters,
// because there is normally no shell.
func TestTheOpenerIsNeverAShell(t *testing.T) {
	const hostile = "https://example.invalid/?x&calc"

	// Every platform, not just this one. The branch that mattered is the one
	// nobody running these tests would ever execute.
	for _, goos := range []string{"darwin", "windows", "linux", "freebsd"} {
		cmd := openCmdFor(goos, hostile)
		if cmd == nil {
			t.Errorf("%s: no opener", goos)
			continue
		}
		for _, banned := range []string{"cmd", "cmd.exe", "sh", "bash", "powershell", "powershell.exe"} {
			if strings.EqualFold(filepath.Base(cmd.Path), banned) {
				t.Errorf("%s: opener is %q — a message-controlled URL must not go through an interpreter",
					goos, cmd.Path)
			}
		}
		// And the URL survives as one argument: an opener that split it
		// would be opening something other than what was shown.
		found := false
		for _, a := range cmd.Args {
			if a == hostile {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: the URL is not passed as a single argument: %q", goos, cmd.Args)
		}
	}
}

// A failed start is not a success. On a machine with no xdg-open, saying
// "opened" is how a reader waits for a window that is never coming.
func TestAFailedOpenIsReported(t *testing.T) {
	m := linkModel(t)
	m, _ = press(m, "g", "x")

	// Point the opener at something that cannot start.
	m.armed.safeURI = "https://example.invalid/ok"
	_, cmd, handled := m.openArmedLink()
	if !handled {
		t.Fatal("enter did not take the armed link")
	}
	if cmd == nil {
		t.Fatal("no command returned")
	}
	// The message either opened or said why; what it must never do is claim
	// success without having started anything.
	msg, ok := cmd().(MediaPlayMsg)
	if !ok {
		t.Fatalf("unexpected message %T", cmd())
	}
	if msg.Status != "opened" && msg.Status != "error" {
		t.Errorf("status = %q, want opened or error", msg.Status)
	}
}
