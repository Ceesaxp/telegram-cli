package composer

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// mentionComposer is a focused emacs composer in a chat where the host has
// switched completion on — a group, in the app.
func mentionComposer(t *testing.T) Model {
	t.Helper()
	m := newFocused()
	m.SetMentionsEnabled(true)
	return m
}

// queryIn returns the MentionQueryMsg among what a key produced, if any.
func queryIn(msg tea.Msg) (MentionQueryMsg, bool) {
	switch msg := msg.(type) {
	case MentionQueryMsg:
		return msg, true
	case tea.BatchMsg:
		for _, cmd := range msg {
			if cmd == nil {
				continue
			}
			if q, ok := queryIn(cmd()); ok {
				return q, true
			}
		}
	}
	return MentionQueryMsg{}, false
}

// ---------------------------------------------------------------------------
// Opening
// ---------------------------------------------------------------------------

// A typed @ where a mention can start opens completion, and asks the host
// for the chat's members straight away — with nothing typed yet, the query
// is empty, and a supergroup answers that with its recent members.
func TestATypedAtOpensCompletion(t *testing.T) {
	m, msg := send(t, mentionComposer(t), "@")

	if !m.MentionActive() {
		t.Fatal("MentionActive = false after typing @ into an empty draft")
	}
	q, ok := queryIn(msg)
	if !ok {
		t.Fatalf("typing @ produced %T, want a MentionQueryMsg", msg)
	}
	if q.ChatID != 42 || q.Anchor != 0 || q.Query != "" || q.Gen == 0 {
		t.Errorf("MentionQueryMsg = %+v, want chat 42, anchor 0, empty query, a generation", q)
	}
	if m.Draft() != "@" {
		t.Errorf("Draft = %q, want the @ itself typed", m.Draft())
	}
}

// Where a mention can start: at the start of the draft or of a line, and
// after whitespace or the punctuation that opens or separates a token. Never
// inside a word — an email address is the case that matters.
func TestAnAtOpensOnlyWhereAMentionCanStart(t *testing.T) {
	cases := []struct {
		typed string
		opens bool
	}{
		{"@", true},
		{"hi @", true},
		{"(@", true},
		{"«@", true},
		{`"@`, true},
		{"yes,@", true},
		{"name@", false},
		{"name@example.com", false},
		{"a.@", false},
		{"@@", true}, // the first opened it; the second is inside its token
	}
	for _, tc := range cases {
		m := chars(t, mentionComposer(t), tc.typed)
		if got := m.MentionActive(); got != tc.opens {
			t.Errorf("typing %q: MentionActive = %v, want %v", tc.typed, got, tc.opens)
		}
	}
}

// After a line break is the start of a line, which is as good a place to
// start a mention as the start of the draft.
func TestAnAtOpensAtTheStartOfALine(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi")
	m = typeSeq(t, m, "\n") // ctrl+j
	m = chars(t, m, "@")
	if !m.MentionActive() {
		t.Error("an @ at the start of the second line did not open completion")
	}
}

// Off is the default, and what a private chat or a channel gets: there is
// nobody to choose between.
func TestAnAtDoesNotOpenWhereMentionsAreOff(t *testing.T) {
	m, msg := send(t, newFocused(), "@")
	if m.MentionActive() {
		t.Error("completion opened with mentions switched off")
	}
	if _, ok := queryIn(msg); ok {
		t.Error("asked the host for members with mentions switched off")
	}
}

// Nor before a chat is open: there are no members to ask about.
func TestAnAtDoesNotOpenWithoutAChat(t *testing.T) {
	m := newFocusedNoChat()
	m.SetMentionsEnabled(true)
	if m, _ = send(t, m, "@"); m.MentionActive() {
		t.Error("completion opened with no chat open")
	}
}

// In vi's normal mode a key is a command, not text. @ is not one of the
// commands, but it must not become a way into completion either.
func TestAnAtInViNormalModeDoesNotOpen(t *testing.T) {
	m := viComposer(t)
	m.SetMentionsEnabled(true)
	m = chars(t, m, "hi ")
	m, _ = send(t, m, "\x1b")
	m = chars(t, m, "A") // back to insert, at the end...
	m, _ = send(t, m, "\x1b")
	if !m.IsViNormalMode() {
		t.Fatal("precondition: not in normal mode")
	}

	if m, _ = send(t, m, "@"); m.MentionActive() {
		t.Error("an @ pressed in normal mode opened completion")
	}
}

// Only a typed @ opens completion. A pasted one is text somebody already
// wrote — an address, a quote, a whole message — and a picker springing up
// from the middle of it would take the next Enter for itself.
func TestAPastedAtDoesNotOpen(t *testing.T) {
	for _, paste := range []string{"@", "ask @nadia", "hi @"} {
		m, msg := mentionComposer(t).Update(tea.PasteMsg{Content: paste})
		if m.MentionActive() {
			t.Errorf("pasting %q opened completion", paste)
		}
		if msg != nil {
			if _, ok := queryIn(msg()); ok {
				t.Errorf("pasting %q asked the host for members", paste)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// The query
// ---------------------------------------------------------------------------

// Every key that changes the query asks again, under a newer generation, so
// the host can tell the answer to "na" from the answer to "n".
func TestTypingAfterTheAtFollowsTheQuery(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi ")

	var gens []uint64
	for _, r := range "@na" {
		var msg tea.Msg
		m, msg = send(t, m, string(r))
		q, ok := queryIn(msg)
		if !ok {
			t.Fatalf("typing %q asked nothing", r)
		}
		gens = append(gens, q.Gen)
		if r == 'a' {
			want := MentionQueryMsg{ChatID: 42, Anchor: 3, Query: "na", Gen: q.Gen}
			if q != want {
				t.Errorf("MentionQueryMsg = %+v, want %+v", q, want)
			}
		}
	}
	if !(gens[0] < gens[1] && gens[1] < gens[2]) {
		t.Errorf("generations %v, want each newer than the last", gens)
	}
	if got := m.mention.query; got != "na" {
		t.Errorf("query = %q, want %q", got, "na")
	}
}

// Raw terminal sequences for the keys the dismissal cases press.
const (
	keyBackspace = "\x7f"
	keyLeft      = "\x1b[D"
	keyRight     = "\x1b[C"
	keyUp        = "\x1b[A"
	keyDown      = "\x1b[B"
	keyTab       = "\t"
	keyEsc       = "\x1b"
	keyEnter     = "\r"
	keyCtrlA     = "\x01"
	keyCtrlJ     = "\n"
	keyCtrlW     = "\x17"
)

// Leaving the token closes completion, however it is left, and leaves the
// draft exactly as the keys made it: nothing is completed on the way out.
func TestLeavingTheTokenClosesCompletion(t *testing.T) {
	cases := []struct {
		name  string
		typed string   // typed first, a character at a time
		keys  []string // then these
		draft string
	}{
		{"a space right after the @ keeps the literal @", "hi @", []string{" "}, "hi @ "},
		{"a space after the query", "hi @na", []string{" "}, "hi @na "},
		{"a line break", "hi @na", []string{keyCtrlJ}, "hi @na\n"},
		{"a comma", "hi @na", []string{","}, "hi @na,"},
		{"a full stop", "hi @na", []string{"."}, "hi @na."},
		{"a closing bracket", "(@na", []string{")"}, "(@na)"},
		{"backspacing the @ away", "hi @na", []string{keyBackspace, keyBackspace, keyBackspace}, "hi "},
		{"killing the word", "hi @na", []string{keyCtrlW}, "hi "},
		{"the cursor moving left of the @", "hi @na", []string{keyLeft, keyLeft, keyLeft}, "hi @na"},
		{"the cursor jumping to the line start", "hi @na", []string{keyCtrlA}, "hi @na"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := chars(t, mentionComposer(t), tc.typed)
			if !m.MentionActive() {
				t.Fatalf("precondition: typing %q did not open completion", tc.typed)
			}
			m = typeSeq(t, m, tc.keys...)
			if m.MentionActive() {
				t.Errorf("still open with the draft at %q", m.Draft())
			}
			if m.Draft() != tc.draft {
				t.Errorf("Draft = %q, want %q", m.Draft(), tc.draft)
			}
		})
	}
}

// The cursor can also leave to the right, when there is text after the
// token: an @ typed in front of a word, then the cursor walked on past it.
func TestTheCursorLeavingRightClosesCompletion(t *testing.T) {
	m := chars(t, mentionComposer(t), " rest")
	m = typeSeq(t, m, keyCtrlA)
	m = chars(t, m, "@na")
	if !m.MentionActive() {
		t.Fatal("precondition: completion not open")
	}

	m = typeSeq(t, m, keyRight)
	if m.MentionActive() {
		t.Errorf("still open with the cursor past the token in %q", m.Draft())
	}
}

// Inside the token the cursor may move, and the query follows it: the
// query is what lies between the @ and the cursor.
func TestTheCursorInsideTheTokenNarrowsTheQuery(t *testing.T) {
	m := chars(t, mentionComposer(t), "@nad")
	m, msg := send(t, m, keyLeft)

	if !m.MentionActive() {
		t.Fatal("moving left inside the token closed completion")
	}
	if q, ok := queryIn(msg); !ok || q.Query != "na" {
		t.Errorf("moving left asked %+v, want the query %q", q, "na")
	}
}

// Once closed, it stays closed: typing on in the same word is typing, and
// nothing but another @ opens completion again.
func TestAClosedTokenDoesNotReopen(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")
	m = typeSeq(t, m, keyLeft, keyLeft, keyLeft) // out, to the left
	m = typeSeq(t, m, keyRight, keyRight, keyRight)
	m = chars(t, m, "dia")
	if m.MentionActive() {
		t.Error("walking back into a closed token reopened it")
	}
}

// ---------------------------------------------------------------------------
// Closing from outside
// ---------------------------------------------------------------------------

// A chat switch closes completion, and coming back brings the draft back
// without it: the members it was offering were the other chat's.
func TestAChatSwitchClosesCompletion(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")

	m.SetChatId(43)
	if m.MentionActive() {
		t.Error("completion survived the switch to another chat")
	}
	m.SetChatId(42)
	m.SetMentionsEnabled(true)
	if m.MentionActive() {
		t.Error("coming back reopened completion")
	}
	if m.Draft() != "hi @na" {
		t.Errorf("Draft = %q, want the parked draft back", m.Draft())
	}
}

// Whether completion applies is a property of the chat, so it does not
// follow the composer into the next one. The host says so again for each.
func TestAChatSwitchSwitchesMentionsOff(t *testing.T) {
	m := mentionComposer(t)
	m.SetChatId(43)
	if m, _ = send(t, m, "@"); m.MentionActive() {
		t.Error("an @ opened completion in a chat the host never enabled it for")
	}
}

// Losing focus closes it, and getting focus back does not reopen it.
func TestLosingFocusClosesCompletion(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")

	m.SetFocused(false)
	if m.MentionActive() {
		t.Error("completion survived losing focus")
	}
	m.SetFocused(true)
	if m.MentionActive() {
		t.Error("getting focus back reopened completion")
	}
}

func TestSwitchingMentionsOffClosesCompletion(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")
	m.SetMentionsEnabled(false)
	if m.MentionActive() {
		t.Error("completion survived mentions being switched off")
	}
}

// Anything that replaces the draft wholesale closes it: the token it was
// completing is gone with the text.
func TestReplacingTheDraftClosesCompletion(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")
	m.EnterEditMode(9, "@na")
	if m.MentionActive() {
		t.Error("completion survived entering edit mode")
	}

	m = chars(t, mentionComposer(t), "hi @na")
	m, _ = m.Update(editorFinishedMsg{text: "hi @na", ok: true})
	if m.MentionActive() {
		t.Error("completion survived the external editor")
	}
}
