package chatview

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// Unread mentions: the @ on a chat's row, one for each message that names
// the reader and that they have not seen.
//
// The official clients clear a mention when its message is viewed — TDLib
// does it in view_messages, with messages.readMessageContents — and the
// read receipt does not: readHistory leaves every @ standing. So the chat
// view clears the mentions it shows, through a window of its own like the
// reactions' (coalesce.go), and each clear names its messages.

// mentionsFlushMsg flushes the owed clears of the open chat's unread
// mentions. It carries its chat for the reason readFlushMsg does.
type mentionsFlushMsg struct {
	chatID int64
}

// readMentionsOnOpen owes a clear for the mentions on the first page of an
// open at the newest messages: those are on screen, so they have been
// seen. The condition is readOnOpen's, for its reason — an older page is
// the reader scrolling back, and a chat opened at a search hit, a link or a
// reply jump has its newest messages below the fold.
//
// The page is the evidence, not the store's count. The message flags say
// which mentions are on screen, and the count can be stale either way.
func (m *Model) readMentionsOnOpen(msg historyLoadedMsg) tea.Cmd {
	if msg.fromID != 0 || m.targetMsgID != 0 {
		return nil
	}
	return m.noteUnreadMentions(seenMentions(msg.messages))
}

// seenMentions are the unread mentions among msgs that being on screen
// clears. A voice note or a round video is not heard by being seen: TDLib
// leaves those for playing to clear, and so does this client.
func seenMentions(msgs []*telegram.Message) []int64 {
	var ids []int64
	for _, v := range msgs {
		if !v.UnreadMention {
			continue
		}
		switch v.Content.(type) {
		case *telegram.MessageVoiceNote, *telegram.MessageVideoNote:
			continue
		}
		ids = append(ids, v.ID)
	}
	return ids
}

// noteUnreadMentions records that the open chat owes a clear of the given
// mentions, and schedules the flush if one is not already pending.
// Blurred, it only records: FocusMsg sends the clear.
//
// A clear names its messages, unlike the reactions', so the owed IDs
// accumulate and N mentions in the window cost one request carrying all
// of them.
func (m *Model) noteUnreadMentions(ids []int64) tea.Cmd {
	for _, id := range ids {
		if !slices.Contains(m.pendingMentionsRead, id) {
			m.pendingMentionsRead = append(m.pendingMentionsRead, id)
		}
	}
	if len(m.pendingMentionsRead) == 0 || m.blurred || m.mentionsFlushPending {
		return nil
	}
	m.mentionsFlushPending = true
	chatID := m.chatID
	return tea.Tick(coalesceWindow, func(time.Time) tea.Msg {
		return mentionsFlushMsg{chatID: chatID}
	})
}

// flushMentionsRead sends the owed clears, unless the terminal is in the
// background: then FocusMsg sends them instead, as it does the read
// receipt.
func (m *Model) flushMentionsRead() tea.Cmd {
	if len(m.pendingMentionsRead) == 0 || m.blurred {
		return nil
	}
	ids := m.pendingMentionsRead
	m.pendingMentionsRead = nil
	sortInt64s(ids)
	// Consumed before the client is checked, as flushRead does, so the
	// owed clears behave the same with and without one.
	chatID, tg := m.chatID, m.tg
	if tg == nil {
		return nil
	}
	return func() tea.Msg {
		// Dropped, as the background receipt's error is: a clear that
		// failed leaves the @ standing, which is what it said before.
		_ = tg.ReadMentions(chatID, ids)
		return nil
	}
}
