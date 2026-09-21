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

// mentionsClearFailedMsg is a clear of the given mentions that the server
// refused.
type mentionsClearFailedMsg struct {
	chatID int64
	ids    []int64
}

// readMentionsOnOpen owes a clear for the mentions on the first page of an
// open at the newest messages. That open reads them, on the terms
// readOnOpen marks the chat read up to its newest message, and under its
// condition for its reason: an older page is the reader scrolling back,
// and a chat opened at a search hit, a link or a reply jump has its newest
// messages below the fold.
//
// The page is the evidence, not the store's count. The message flags say
// which mentions the open showed, and the count can be stale either way.
func (m *Model) readMentionsOnOpen(msg historyLoadedMsg) tea.Cmd {
	if msg.fromID != 0 || m.targetMsgID != 0 {
		return nil
	}
	return m.noteUnreadMentions(seenMentions(msg.messages))
}

// seenMentions are the unread mentions among msgs that being seen clears.
// A voice note or a round video is not heard by being seen: TDLib leaves
// those for playing to clear, and so does this client.
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
	var fresh []int64
	for _, id := range ids {
		if !m.askedMentions.has(m.chatID, id) &&
			!slices.Contains(m.pendingMentionsRead, id) && !slices.Contains(fresh, id) {
			fresh = append(fresh, id)
		}
	}
	if len(fresh) > 0 {
		// A new slice, never an append into the old one's spare capacity:
		// the model is a value, and an older copy shares that capacity.
		m.pendingMentionsRead = slices.Concat(m.pendingMentionsRead, fresh)
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
	chatID, tg := m.chatID, m.tg
	// Asked for again only if nothing has asked meanwhile: g@ inside the
	// window clears the mention it lands on at once.
	ids := make([]int64, 0, len(m.pendingMentionsRead))
	for _, id := range m.pendingMentionsRead {
		if !m.askedMentions.has(chatID, id) {
			ids = append(ids, id)
		}
	}
	m.pendingMentionsRead = nil
	if len(ids) == 0 {
		return nil
	}
	sortInt64s(ids)
	// Consumed and recorded before the client is checked, as flushRead
	// does, so the owed clears behave the same with and without one.
	m.askedMentions.record(chatID, ids...)
	if tg == nil {
		return nil
	}
	return clearMentionsCmd(tg, chatID, ids)
}

// maxAskedChats bounds how many chats a mentionLedger remembers. The
// ledger outlives a chat switch for the sake of a jump back into the chat,
// and 32 is the jump list's own depth (see the app's maxJumps).
const maxAskedChats = 32

// maxAskedPerChat bounds how many asks a mentionLedger remembers for one
// chat: two first pages' worth of mentions.
const maxAskedPerChat = 100

// mentionLedger remembers mentions, chat by chat, for the session. There
// are two: the mentions this client has already asked the server to clear,
// so that none is asked for twice, and the ones a g@ jump could not reach,
// so that the next g@ goes past them.
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

// forget takes ids out of what the ledger remembers for chatID.
func (l *mentionLedger) forget(chatID int64, ids ...int64) {
	chats := make([]askedChat, 0, len(l.chats))
	for _, c := range l.chats {
		if c.chatID == chatID {
			c.ids = slices.DeleteFunc(slices.Clone(c.ids), func(id int64) bool {
				return slices.Contains(ids, id)
			})
		}
		chats = append(chats, c)
	}
	l.chats = chats
}

// mentionListLimit is how many unread mentions g@ asks for at a time. It
// takes the oldest of them, so a page is only there to say how many more
// there are.
const mentionListLimit = 10

// mentionsListedMsg is the answer to g@: the open chat's unread mentions,
// oldest first, or the error that stopped the server saying.
type mentionsListedMsg struct {
	chatID int64
	ids    []int64
	err    error
}

// nextMention is g@: ask which mentions are still unread, where there can
// be any.
func (m Model) nextMention() (Model, tea.Cmd) {
	if !m.CanHaveMentions() {
		m.notice = "no mentions in this chat"
		return m, nil
	}
	return m, m.listMentionsCmd()
}

// listMentionsCmd is g@'s question: which mentions in the open chat are
// still unread. Nil with no chat open or no client to ask.
func (m Model) listMentionsCmd() tea.Cmd {
	if m.chatID == 0 || m.tg == nil {
		return nil
	}
	chatID, tg := m.chatID, m.tg
	return func() tea.Msg {
		ids, err := tg.UnreadMentions(chatID, mentionListLimit)
		return mentionsListedMsg{chatID: chatID, ids: ids, err: err}
	}
}

// MentionJumpMsg is where g@ goes: the next unread mention, handed to the
// host to go to, the way [TelegramLinkMsg] hands over a link. The panel
// reads, the host navigates, so every way of opening a chat stays one
// function and a way back is recorded for ctrl+o.
type MentionJumpMsg struct {
	ChatId    int64
	MessageId int64
	// Remaining is how many more unread mentions there are after this
	// one, for the notice the host shows on arrival.
	Remaining int
}

// handleMentionsListed is g@'s answer arriving: go to the oldest unread
// mention this client has neither asked to clear already nor found out of
// a jump's reach.
func (m Model) handleMentionsListed(msg mentionsListedMsg) (Model, tea.Cmd) {
	// The chat, not the generation: a g@ jump reopens the same chat, and
	// the answer to a second g@ pressed meanwhile is still about it.
	if msg.chatID != m.chatID {
		return m, nil
	}
	if msg.err != nil {
		m.notice = "could not load mentions"
		return m, nil
	}
	var fresh []int64
	for _, id := range msg.ids {
		if !m.askedMentions.has(m.chatID, id) && !m.unreachableMentions.has(m.chatID, id) {
			fresh = append(fresh, id)
		}
	}
	if len(fresh) == 0 {
		m.notice = "no unread mentions"
		return m, m.correctMentionCount()
	}
	// Nothing is cleared yet. The jump may not reach the message — its
	// hunt pages back only so far — and a mention cleared unseen is gone
	// from the listing for good. landOnMention clears it on arrival.
	id := fresh[0]
	m.mentionTarget = mentionRef{chatID: m.chatID, id: id}
	chatID := m.chatID
	remaining := m.mentionsAfter(len(fresh), len(msg.ids) == mentionListLimit)
	return m, func() tea.Msg {
		return MentionJumpMsg{ChatId: chatID, MessageId: id, Remaining: remaining}
	}
}

// mentionRef names one mention: a message in a chat. The zero value is
// none.
type mentionRef struct {
	chatID, id int64
}

// landOnMention is a jump arriving at message id. If that is the mention
// g@ went to, it is cleared now, at once rather than through the window:
// the reader chose to go there, which is also why a voice note is cleared
// here and not by being on screen.
func (m *Model) landOnMention(id int64) tea.Cmd {
	if m.mentionTarget != (mentionRef{chatID: m.chatID, id: id}) {
		return nil
	}
	m.mentionTarget = mentionRef{}
	if m.askedMentions.has(m.chatID, id) {
		return nil
	}
	chatID, tg := m.chatID, m.tg
	m.askedMentions.record(chatID, id)
	if tg == nil {
		return nil
	}
	return clearMentionsCmd(tg, chatID, []int64{id})
}

// clearMentionsCmd asks the server to clear ids in chatID. The asks are
// recorded before it runs, so a refusal comes back as
// mentionsClearFailedMsg to take them out again: left in, the mentions
// would never be asked for again, and g@ would call the chat empty while
// the server still has them.
func clearMentionsCmd(tg *telegram.Client, chatID int64, ids []int64) tea.Cmd {
	return func() tea.Msg {
		if err := tg.ReadMentions(chatID, ids); err != nil {
			return mentionsClearFailedMsg{chatID: chatID, ids: ids}
		}
		return nil
	}
}

// missMention is a jump giving up on message id. If that is the mention
// g@ went to, it is left unread — nobody saw it — and set aside for the
// session, so the next g@ goes on to the one after instead of into the
// same wall. It reports whether it was.
func (m *Model) missMention(id int64) bool {
	if m.mentionTarget != (mentionRef{chatID: m.chatID, id: id}) {
		return false
	}
	m.mentionTarget = mentionRef{}
	m.unreachableMentions.record(m.chatID, id)
	return true
}

// mentionsAfter is how many unread mentions are left once the one being
// jumped to is cleared, given how many the listing still had and whether
// it was full. A listing shorter than mentionListLimit is every unread
// mention there is. A full one may stop short of the rest, and then the
// store's count, which knows of them, is the better guess if it is the
// larger.
func (m Model) mentionsAfter(listed int, full bool) int {
	counted := 0
	if entry, ok := m.store.Chats.Get(m.chatID); ok && full {
		counted = int(entry.UnreadMentionsCount)
	}
	return max(listed-1, counted-1, 0)
}

// correctMentionCount answers a listing with nothing left in it. If the
// store still counts mentions for the chat, the count is stale — one was
// cleared on another device, say — and the @ on the row would stay for
// ever, so it is zeroed.
//
// This is a local correction, not a server clear: the server has just
// said there is nothing to clear. It goes out as the message a finished
// ReadAllMentions announces because that is what the chat list already
// zeroes a count from, and nothing else needs to know the difference.
func (m Model) correctMentionCount() tea.Cmd {
	entry, ok := m.store.Chats.Get(m.chatID)
	if !ok || entry.UnreadMentionsCount <= 0 {
		return nil
	}
	chatID := m.chatID
	return func() tea.Msg {
		return telegram.ChatMentionsReadMsg{ChatId: chatID, All: true}
	}
}

// CanHaveMentions reports whether the open chat is a kind that has
// mentions at all: a basic group or a supergroup. A DM is addressed to the
// reader already, and a broadcast channel's posts name nobody, so asking
// either about mentions is a request whose answer is always none.
//
// A supergroup and a broadcast channel are both a channel on the wire; the
// client tells them apart by the channel's broadcast flag when it builds
// the chat (see telegram's chatFromChannel), which is what Type carries.
func (m Model) CanHaveMentions() bool {
	entry, ok := m.store.Chats.Get(m.chatID)
	if !ok || entry.Chat == nil {
		return false
	}
	switch entry.Chat.Type {
	case telegram.ChatTypeBasicGroup, telegram.ChatTypeSupergroup:
		return true
	}
	return false
}

// ReadAllMentionsCmd clears every unread mention in the open chat, for
// :read-mentions. Like MarkReadCmd it does not wait for focus or for the
// window: it was asked for. When the request returns it reports so with
// [ReadAllMentionsDoneMsg].
//
// Nil with no chat open, no client to ask, or a chat that cannot have
// mentions, so a caller can treat nil as nothing sent.
func (m *Model) ReadAllMentionsCmd() tea.Cmd {
	if m.chatID == 0 || m.tg == nil || !m.CanHaveMentions() {
		return nil
	}
	// The owed clears are now redundant: this covers them. Dropping them
	// stops the window asking for them again afterwards.
	m.pendingMentionsRead = nil

	chatID, tg := m.chatID, m.tg
	return func() tea.Msg {
		// The error says nothing the announcement does not: see
		// ReadAllMentionsDoneMsg.
		_ = tg.ReadAllMentions(chatID)
		return ReadAllMentionsDoneMsg{ChatId: chatID}
	}
}

// ReadAllMentionsDoneMsg is :read-mentions' request returning. It does not
// say whether the clear finished, because the request cannot: it returns
// no error when it stops at the client's cap of repeats, as well as when
// the server says it is done. What does say is the client's
// telegram.ChatMentionsReadMsg with All set, which it announces only for a
// finished clear, and before the request returns — so a host that has not
// seen that announcement by the time this arrives knows the clear did not
// finish.
type ReadAllMentionsDoneMsg struct {
	ChatId int64
}
