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
		if !m.askedMentions.has(m.chatID, id) && !slices.Contains(m.pendingMentionsRead, id) {
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
	// Consumed and recorded before the client is checked, as flushRead
	// does, so the owed clears behave the same with and without one.
	chatID, tg := m.chatID, m.tg
	m.askedMentions.record(chatID, ids...)
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

// maxAskedChats bounds how many chats a mentionLedger remembers. The
// ledger outlives a chat switch for the sake of a jump back into the chat,
// and 32 is the jump list's own depth (see the app's maxJumps).
const maxAskedChats = 32

// maxAskedPerChat bounds how many asks a mentionLedger remembers for one
// chat: two first pages' worth of mentions.
const maxAskedPerChat = 100

// mentionLedger remembers, chat by chat, the unread mentions this client
// has already asked the server to clear, so that none is asked for twice.
//
// It outlives a chat switch on purpose. A clear the server has not answered
// yet leaves the flags on the page, and a reopen of the same chat brings
// that page back: g@ reopens the chat to jump, and ctrl+o reopens it to
// return.
//
// Both levels are kept in the order they were last touched, oldest first,
// so the bounds drop the stalest. Every change builds new slices rather
// than writing into the old ones: the model is a value, and a copy of it
// taken earlier must not see its ledger change underneath it.
type mentionLedger struct {
	chats []askedChat
}

// askedChat is one chat's entry in a mentionLedger: the asks, oldest first.
type askedChat struct {
	chatID int64
	ids    []int64
}

// has reports whether this client has asked to clear mention id in chatID.
func (l mentionLedger) has(chatID, id int64) bool {
	for _, c := range l.chats {
		if c.chatID == chatID {
			return slices.Contains(c.ids, id)
		}
	}
	return false
}

// record notes that this client has asked to clear ids in chatID, which
// makes chatID the chat most recently asked about.
func (l *mentionLedger) record(chatID int64, ids ...int64) {
	entry := askedChat{chatID: chatID}
	chats := make([]askedChat, 0, len(l.chats)+1)
	for _, c := range l.chats {
		if c.chatID == chatID {
			entry.ids = c.ids
			continue
		}
		chats = append(chats, c)
	}
	entry.ids = append(slices.Clone(entry.ids), ids...)
	entry.ids = entry.ids[max(0, len(entry.ids)-maxAskedPerChat):]
	chats = append(chats, entry)
	l.chats = chats[max(0, len(chats)-maxAskedChats):]
}
