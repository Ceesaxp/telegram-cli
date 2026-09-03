package chatlist

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/charmbracelet/x/ansi"
)

// TestDraftPreviewOutranksTheLastMessage (decision 13).
//
// What somebody else said is still in the chat when you open it; that you
// left something half-written in there is a thing you would otherwise have to
// remember on your own — and unsent work is invisible the moment you look
// away from it.
func TestDraftPreviewOutranksTheLastMessage(t *testing.T) {
	m := newTestModel()
	m.SetSize(38, 20)
	m.loading = false // New starts loading; a spinner is not a list

	for _, id := range []int64{42, 43} {
		m.store.Chats.Set(&telegram.Chat{
			ID: id, Type: telegram.ChatTypePrivate, Title: "chat", Order: id,
			LastMessage: &telegram.Message{
				ID: 1, ChatID: id, Date: 1700000000,
				Content: &telegram.MessageText{
					Text: &telegram.FormattedText{Text: "what they said"},
				},
			},
		})
	}

	// The list rebuilds from the store on demand; nothing has marked it
	// stale yet because the chats were put there behind its back.
	*m.dirty = true

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "what they said") {
		t.Fatalf("precondition: the last message is not previewed:\n%s", view)
	}

	m.SetDraftChats(map[int64]bool{42: true})
	view = ansi.Strip(m.View())

	if !strings.Contains(view, "draft: saved locally") {
		t.Errorf("the parked draft is not shown:\n%s", view)
	}
	// The other chat keeps its preview: the marker is per chat, not a mode.
	if !strings.Contains(view, "what they said") {
		t.Errorf("a chat with no draft lost its preview:\n%s", view)
	}
}

// TestDraftPreviewIsDroppedWhenTheDraftIs. The list reads a projection of the
// composer's drafts, so a sent message has to take the marker with it.
func TestDraftPreviewIsDroppedWhenTheDraftIs(t *testing.T) {
	m := newTestModel()
	m.SetSize(38, 20)
	m.loading = false // New starts loading; a spinner is not a list
	m.store.Chats.Set(&telegram.Chat{
		ID: 42, Type: telegram.ChatTypePrivate, Title: "chat", Order: 42,
	})

	m.SetDraftChats(map[int64]bool{42: true})
	if !strings.Contains(ansi.Strip(m.View()), "draft: saved locally") {
		t.Fatal("precondition: the draft marker was never drawn")
	}

	m.SetDraftChats(nil)
	if strings.Contains(ansi.Strip(m.View()), "draft: saved locally") {
		t.Error("the marker outlived the draft")
	}
}
