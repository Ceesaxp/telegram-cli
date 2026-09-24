package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A hint set that ignores the state it is drawn over describes a surface
// the user is not on (decision I-6). Inside a forum the chat list's own set
// did exactly that: it advertised [ / ] "folder" and u "unread", both inert
// there, and never named esc or backspace — the only two keys that come
// back out.

// hintLabels is the drawn hint set as label → key, for the surface the app
// is actually on.
func hintLabels(m Model) map[string]string {
	out := map[string]string{}
	for _, h := range m.hintsFor(m.surface()) {
		out[h.Label] = h.Key
	}
	return out
}

// drilledIn is the app standing in a forum's topic list, which is the
// surface these tests are about.
func drilledIn(t *testing.T) Model {
	t.Helper()
	m := enterForum(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))
	m.setFocus(PanelChatList)
	return m
}

func TestTheTopicListHasItsOwnSurface(t *testing.T) {
	m := drilledIn(t)

	if got := m.surface(); got != SurfaceForumTopics {
		t.Errorf("the surface inside a forum is %v, want the topic list's own", got)
	}
	if got := leaveForum(t, m).surface(); got != SurfaceChatList {
		t.Errorf("the surface after leaving a forum is %v, want the chat list", got)
	}
}

func TestTheTopicListHintsNameTheWayBackOut(t *testing.T) {
	got := hintLabels(drilledIn(t))

	if got["chats"] != "esc" {
		t.Errorf("the way out of a forum is advertised as %q, want esc", got["chats"])
	}
}

func TestTheTopicListHintsDropTheKeysThatDoNothingThere(t *testing.T) {
	got := hintLabels(drilledIn(t))

	for _, label := range []string{"folder", "unread"} {
		if key, ok := got[label]; ok {
			t.Errorf("the topic list advertises %q as %q, and it does nothing there",
				label, key)
		}
	}
}

// The keys that DO still work are still named, or the set above bought its
// honesty by describing nothing.
func TestTheTopicListHintsKeepTheKeysThatStillWork(t *testing.T) {
	got := hintLabels(drilledIn(t))

	for label, want := range map[string]string{
		"move": "j/k", "open": "l", "filter": "/",
		"compose": "i", "quit": "q", "keymap": "?",
	} {
		if got[label] != want {
			t.Errorf("the topic list advertises %q as %q, want %q", label, got[label], want)
		}
	}
}

// Esc stacks behind the filter (docs/topics.md, "Resolved" 1), so with one
// applied it clears rather than leaving — and the row says so, naming
// backspace as the key that goes up instead.
func TestTheTopicListHintsFollowTheFilter(t *testing.T) {
	m := drilledIn(t)
	m = update(t, m, "/")
	m = update(t, m, "J")
	m = update(t, m, "\r")

	got := hintLabels(m)
	if got["clear filter"] != "esc" {
		t.Errorf("with a filter applied, esc is advertised as %q, want \"clear filter\"",
			got["clear filter"])
	}
	if got["chats"] != "backspace" {
		t.Errorf("with a filter applied, the way out is advertised as %q, want backspace",
			got["chats"])
	}
}

// And the bar the reader actually sees is the one built above.
func TestTheDrawnHintBarDescribesTheTopicList(t *testing.T) {
	m := drilledIn(t)
	m.updateLayout()

	bar := ansi.Strip(m.hintBar.View())
	if !strings.Contains(bar, "esc") {
		t.Errorf("the hint bar inside a forum does not name esc:\n%s", bar)
	}
	if strings.Contains(bar, "folder") {
		t.Errorf("the hint bar inside a forum still advertises the folder keys:\n%s", bar)
	}
}
