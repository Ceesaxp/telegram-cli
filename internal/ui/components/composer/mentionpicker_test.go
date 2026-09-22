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
