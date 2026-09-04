package forward

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
)

func testSource() Source {
	return Source{ChatID: 10, MessageID: 99, Preview: "nadia: the patch landed"}
}

func testCandidates() []Chat {
	return []Chat{
		{ID: 1, Title: "Nadia Petrova", Sigil: "●", Handle: "@nadia"},
		{ID: 2, Title: "Release team", Sigil: "▣"},
		{ID: 3, Title: "Saved Messages", Sigil: "★"},
	}
}

func openPicker(t *testing.T) Model {
	t.Helper()
	m := New(theme.DarkRoles(false))
	m.Open(testSource(), testCandidates())
	return m
}

// typeInto feeds printable text through Update the way a terminal would,
// one key at a time, and returns the last action.
func typeInto(m Model, text string) (Model, Action) {
	action := ActionNone
	for _, r := range text {
		m, action = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m, action
}

func press(m Model, key string) (Model, Action) {
	switch key {
	case "enter":
		return m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	case "esc":
		return m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	case "up":
		return m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	case "down":
		return m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	case "backspace":
		return m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	panic("unknown key " + key)
}

func titles(chats []Chat) []string {
	out := make([]string, 0, len(chats))
	for _, c := range chats {
		out = append(out, c.Title)
	}
	return out
}

func TestOpensOnTheLoadedChats(t *testing.T) {
	m := openPicker(t)

	if !m.IsVisible() || m.Step() != StepPick {
		t.Fatalf("picker opened visible=%v step=%v", m.IsVisible(), m.Step())
	}
	if got := titles(m.Matches()); len(got) != 3 {
		t.Fatalf("matches = %v, want all three candidates", got)
	}
	// An empty query listing nothing would make the picker useless for the
	// common case: forwarding to a chat you were just in.
	if dest, ok := m.Destination(); !ok || dest.ID != 1 {
		t.Fatalf("destination = %+v ok=%v, want the first candidate", dest, ok)
	}
}

func TestFiltersByTitle(t *testing.T) {
	m := openPicker(t)

	m, action := typeInto(m, "rel")
	if action != ActionQueryChanged {
		t.Fatalf("action = %v, want ActionQueryChanged", action)
	}
	if got := titles(m.Matches()); len(got) != 1 || got[0] != "Release team" {
		t.Fatalf("matches = %v, want only Release team", got)
	}
}

// Matching the handle is what makes the picker independent of mention
// autocomplete (#41): nobody has to know a @username, but somebody who does
// can use it.
func TestFiltersByHandle(t *testing.T) {
	m := openPicker(t)

	m, _ = typeInto(m, "@nad")
	if got := titles(m.Matches()); len(got) != 1 || got[0] != "Nadia Petrova" {
		t.Fatalf("matches = %v, want the chat with that handle", got)
	}
}

func TestServerResultsAreAppendedAndDeduplicated(t *testing.T) {
	m := openPicker(t)
	m, _ = typeInto(m, "na")

	m.SetResults([]Chat{
		{ID: 1, Title: "Nadia Petrova", Handle: "@nadia"}, // already local
		{ID: 7, Title: "Nadia Support", Handle: "@nadia_support", Note: "not in your chats"},
	})

	got := titles(m.Matches())
	if len(got) != 2 || got[0] != "Nadia Petrova" || got[1] != "Nadia Support" {
		t.Fatalf("matches = %v, want the local match then the new one", got)
	}
}

// A result set arriving mid-type must not reorder the rows under the
// cursor: the reader is aiming at a row, not at an index.
func TestServerResultsDoNotReorderLocalMatches(t *testing.T) {
	m := openPicker(t)
	m.SetResults([]Chat{{ID: 7, Title: "Aaaa", Note: "not in your chats"}})

	got := titles(m.Matches())
	if got[0] != "Nadia Petrova" || got[len(got)-1] != "Aaaa" {
		t.Fatalf("matches = %v, want the local candidates first", got)
	}
}

func TestSearchFailureKeepsLocalMatches(t *testing.T) {
	m := openPicker(t)
	m, _ = typeInto(m, "na")
	before := len(m.Matches())

	m.SetSearchFailed()

	if got := len(m.Matches()); got != before {
		t.Fatalf("matches = %d after a failed search, want the %d local ones kept", got, before)
	}
	if !strings.Contains(ansi.Strip(m.View()), "search unavailable") {
		t.Error("the failure is not reported anywhere in the view")
	}
}

func TestArrowsMoveAndTheCursorResetsOnEdit(t *testing.T) {
	m := openPicker(t)

	m, _ = press(m, "down")
	if dest, _ := m.Destination(); dest.ID != 2 {
		t.Fatalf("after down, destination = %d, want 2", dest.ID)
	}
	// Typing another character usually removes the highlighted row, so a
	// held index would confirm a chat nobody is looking at.
	m, _ = typeInto(m, "a")
	if dest, ok := m.Destination(); ok && dest != m.Matches()[0] {
		t.Fatalf("cursor did not reset to the top after an edit: %+v", dest)
	}
}

func TestEnterMovesToConfirmThenForwards(t *testing.T) {
	m := openPicker(t)

	m, action := press(m, "enter")
	if action != ActionNone || m.Step() != StepConfirm {
		t.Fatalf("first enter: action=%v step=%v, want the confirmation", action, m.Step())
	}
	// The confirmation names the destination and shows the message, so
	// what is about to happen is legible before it happens.
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Nadia Petrova") || !strings.Contains(view, "the patch landed") {
		t.Fatalf("confirmation does not name both sides:\n%s", view)
	}

	m, action = press(m, "enter")
	if action != ActionForward {
		t.Fatalf("second enter: action = %v, want ActionForward", action)
	}
	if src := m.Source(); src.ChatID != 10 || src.MessageID != 99 {
		t.Fatalf("source = %+v, want the message the picker opened on", src)
	}
}

// Escape means "undo the last step" everywhere else in this client, and a
// reader who picked the wrong chat wants the list back, not to start over.
func TestEscapeStepsBackFromConfirmThenCancels(t *testing.T) {
	m := openPicker(t)
	m, _ = press(m, "enter")

	m, action := press(m, "esc")
	if action != ActionNone || m.Step() != StepPick {
		t.Fatalf("esc at the confirmation: action=%v step=%v, want back at the list", action, m.Step())
	}

	if _, action = press(m, "esc"); action != ActionCancel {
		t.Fatalf("esc at the list: action = %v, want ActionCancel", action)
	}
}

// Enter with nothing matched is neither a cancel nor a forward: there is no
// destination to confirm, so doing nothing is the only honest answer.
func TestEnterOnAnEmptyListDoesNothing(t *testing.T) {
	m := openPicker(t)
	m, _ = typeInto(m, "zzzzz")

	m, action := press(m, "enter")
	if action != ActionNone || m.Step() != StepPick {
		t.Fatalf("action=%v step=%v, want nothing to have happened", action, m.Step())
	}
}

// Close must not leave the previous target behind: a picker reopened on a
// different message with a stale Source is the one failure this surface
// cannot afford.
func TestCloseDropsEverything(t *testing.T) {
	m := openPicker(t)
	m, _ = typeInto(m, "rel")
	m.Close()

	if m.IsVisible() {
		t.Error("still visible after Close")
	}
	if m.Query() != "" || m.Source() != (Source{}) || len(m.Matches()) != 0 {
		t.Fatalf("Close left state behind: query=%q source=%+v matches=%d",
			m.Query(), m.Source(), len(m.Matches()))
	}
}

// Every line is exactly Width cells, or the frame shears where the card is
// placed.
func TestEveryLineIsExactlyWidthCells(t *testing.T) {
	m := openPicker(t)
	m.SetResults([]Chat{{ID: 7, Title: strings.Repeat("very long title ", 8), Handle: "@long"}})

	for _, step := range []Step{StepPick, StepConfirm} {
		m.step = step
		for i, line := range strings.Split(ansi.Strip(m.View()), "\n") {
			// Width is the content; the frame adds a border and a cell of
			// padding on each side.
			const framed = Width + 4
			if got := cell.Width(line); got != framed {
				t.Errorf("step %v line %d is %d cells, want %d: %q", step, i, got, framed, line)
			}
		}
	}
}

// A key that produces no text must not append the word for it: "space"
// arriving as five characters is how a query stops matching anything.
func TestNonTextKeysDoNotEnterTheQuery(t *testing.T) {
	m := openPicker(t)

	m, action := m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	if action != ActionNone || m.Query() != "" {
		t.Fatalf("query = %q after F1, want it untouched", m.Query())
	}
}

func TestBackspaceEditsTheQuery(t *testing.T) {
	m := openPicker(t)
	m, _ = typeInto(m, "rel")

	m, action := press(m, "backspace")
	if action != ActionQueryChanged || m.Query() != "re" {
		t.Fatalf("query = %q action = %v, want \"re\" and a re-search", m.Query(), action)
	}

	// Backspace on an empty query is not an edit, so it must not spend a
	// search on it.
	m, _ = press(m, "backspace")
	m, _ = press(m, "backspace")
	if _, action = press(m, "backspace"); action != ActionNone {
		t.Fatalf("backspace on an empty query returned %v, want ActionNone", action)
	}
}
