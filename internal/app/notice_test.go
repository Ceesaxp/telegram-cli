package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// resolved is what the client sends once it has been told who a chat is.
func resolved(chatID int64, title string, muted bool) telegram.ChatUpdateMsg {
	return telegram.ChatUpdateMsg{
		Chat: &telegram.Chat{
			ID: chatID, Type: telegram.ChatTypePrivate, Title: title, Muted: muted,
		},
		FromPeer: true,
	}
}

// The first message from a chat this client has never heard of used to ring
// regardless of whether it was muted.
//
// NewMessageMsg and ChatLastMessageMsg are emitted for the same message, in
// that order, and the decision to notify was made on the first — while the
// chat was still absent from the store, with no mute flag to read. It failed
// open. So a muted chat below the first dialog page rang once, every time it
// came up in conversation after a restart, and only went quiet from the
// second message onwards.
func TestAMessageFromAnUnknownChatWaitsForTheAnswer(t *testing.T) {
	m := notifyModel(t)

	m, cmd := deliver(t, m, incoming(7))
	if got := rawSequences(t, cmd); len(got) != 0 {
		t.Fatalf("an unresolved chat notified immediately: %q", got)
	}
	if len(m.pendingNotices) != 1 {
		t.Fatalf("%d notifications held, want 1", len(m.pendingNotices))
	}
}

// A second message arriving while the first fetch is still in flight waits
// too. By then the chat IS in the store — as the stub the store invented to
// hold the first message — and a stub reports Muted=false, so deciding on it
// is the same guess with a bigger lie behind it.
func TestASecondMessageWaitsWhileTheFetchIsInFlight(t *testing.T) {
	m := notifyModel(t)
	m, _ = deliver(t, m, incoming(7))

	// What the chat list does with the same message: invent an entry.
	m.store.Chats.UpdateLastMessage(7, &telegram.Message{ID: 1, ChatID: 7})
	if entry, ok := m.store.Chats.Get(7); !ok || !entry.Unresolved {
		t.Fatal("precondition: the store did not invent an unresolved entry")
	}

	m, cmd := deliver(t, m, incoming(7))
	if got := rawSequences(t, cmd); len(got) != 0 {
		t.Errorf("a message arriving mid-fetch notified: %q", got)
	}
	if len(m.pendingNotices) != 2 {
		t.Errorf("%d notifications held, want 2", len(m.pendingNotices))
	}

	// And both go out together when the answer says the chat is not muted.
	m, cmd = deliver(t, m, resolved(7, "Ana", false))
	if got := rawSequences(t, cmd); len(got) != 2 {
		t.Errorf("%d notifications released, want 2", len(got))
	}
}

// Holding schedules its own release. Without the backstop a held
// notification depends entirely on a fetch coming back, which is the one
// thing it cannot assume — and the failure is silent, which is the worst
// way for a notification to fail.
func TestHoldingSchedulesTheBackstop(t *testing.T) {
	m := notifyModel(t)

	_, cmd := deliver(t, m, incoming(7))

	var scheduled bool
	for _, msg := range messages(t, cmd) {
		if _, ok := msg.(noticeGraceMsg); ok {
			scheduled = true
		}
	}
	if !scheduled {
		t.Error("holding a notification scheduled no release")
	}
}

// And is posted, or dropped, once the answer arrives.
func TestAHeldNotificationFollowsTheMuteFlag(t *testing.T) {
	tests := []struct {
		name  string
		muted bool
		want  int
	}{
		{"the chat turns out to be muted", true, 0},
		{"the chat turns out to be unmuted", false, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := notifyModel(t)
			m, _ = deliver(t, m, incoming(7))

			m, cmd := deliver(t, m, resolved(7, "Ana", tt.muted))

			got := rawSequences(t, cmd)
			if len(got) != tt.want {
				t.Fatalf("%d notifications, want %d: %q", len(got), tt.want, got)
			}
			if len(m.pendingNotices) != 0 {
				t.Errorf("%d still held after the answer", len(m.pendingNotices))
			}
			if tt.want == 1 {
				// The title comes from the answer, so a notification that
				// waited is better labelled than one that did not.
				if !strings.Contains(got[0], "Ana") {
					t.Errorf("the notification does not name the chat: %q", got[0])
				}
				if !strings.Contains(got[0], "hello") {
					t.Errorf("the notification lost the message: %q", got[0])
				}
			}
		})
	}
}

// Waiting cannot mean waiting forever. If the fetch never comes back — no
// connection, a peer that will not resolve — the message still has to be
// announced, because a late notification is much better than none.
func TestAHeldNotificationIsReleasedIfTheAnswerNeverComes(t *testing.T) {
	m := notifyModel(t)
	m, _ = deliver(t, m, incoming(7))

	m, cmd := deliver(t, m, noticeGraceMsg{})

	got := rawSequences(t, cmd)
	if len(got) != 1 {
		t.Fatalf("%d notifications after the grace period, want 1: %q", len(got), got)
	}
	if !strings.Contains(got[0], "hello") {
		t.Errorf("the message was lost: %q", got[0])
	}
	if len(m.pendingNotices) != 0 {
		t.Errorf("%d still held after the grace period", len(m.pendingNotices))
	}
}

// A chat that is already known is decided on the spot: waiting for an answer
// nobody is going to send would hold every notification forever.
func TestAKnownChatIsNotHeld(t *testing.T) {
	m := notifyModel(t)
	m.store.Chats.Set(&telegram.Chat{ID: 7, Type: telegram.ChatTypePrivate, Title: "Ana"})

	m, cmd := deliver(t, m, incoming(7))
	if got := rawSequences(t, cmd); len(got) != 1 {
		t.Errorf("a known chat produced %d notifications, want 1", len(got))
	}
	if len(m.pendingNotices) != 0 {
		t.Errorf("a known chat was held: %d", len(m.pendingNotices))
	}
}

// Held notifications are per chat: resolving one must not release another's.
func TestReleasingOneChatLeavesTheOthersHeld(t *testing.T) {
	m := notifyModel(t)
	m, _ = deliver(t, m, incoming(7))
	m, _ = deliver(t, m, incoming(8))

	m, cmd := deliver(t, m, resolved(7, "Ana", false))

	if got := rawSequences(t, cmd); len(got) != 1 {
		t.Errorf("resolving one chat posted %d notifications, want 1", len(got))
	}
	if len(m.pendingNotices) != 1 || m.pendingNotices[0].chatID != 8 {
		t.Errorf("the other chat's notification did not stay held: %+v", m.pendingNotices)
	}
}

// Holding is bounded. If resolution stops working entirely, the queue must
// not grow without limit — and the overflow is released rather than dropped,
// because the reader is still owed the message.
func TestHoldingIsBounded(t *testing.T) {
	m := notifyModel(t)

	released := 0
	for i := 0; i < maxPendingNotices+5; i++ {
		var cmd tea.Cmd
		m, cmd = deliver(t, m, incoming(int64(1000+i)))
		released += len(rawSequences(t, cmd))
	}

	if len(m.pendingNotices) > maxPendingNotices {
		t.Errorf("%d held, want at most %d", len(m.pendingNotices), maxPendingNotices)
	}
	if released != 5 {
		t.Errorf("%d notifications released by overflow, want 5", released)
	}
}

// messages runs a command and returns every message it produces, flattening
// batches.
func messages(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}

	var out []tea.Msg
	var walk func(tea.Msg)
	walk = func(msg tea.Msg) {
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					walk(c())
				}
			}
			return
		}
		out = append(out, msg)
	}
	walk(cmd())
	return out
}

// deliver drives the app with a non-key message.
func deliver(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	out, cmd := m.Update(msg)
	next, ok := out.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want app.Model", out)
	}
	return next, cmd
}
