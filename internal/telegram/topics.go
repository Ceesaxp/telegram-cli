package telegram

import (
	"sync"

	"github.com/gotd/td/tg"
)

// Topic is the domain representation of one forum topic.
//
// A forum is a supergroup whose messages are filed under topics, and to
// everything above this package a topic IS a chat: it has its own title,
// its own unread counts, its own read pointer and its own history. This
// type is what the client knows about one before it becomes that chat; see
// [Client.topicChatID] for the translation.
type Topic struct {
	// ID is the topic's root message ID — the service message that created
	// it. The General topic is always 1 and always exists.
	ID int64

	// ChatID is the forum the topic belongs to, in this client's canonical
	// form (the TDLib channel ID, not the plain one).
	ChatID int64

	Title string

	// IconColor is the fallback topic icon's colour as RGB, one of six
	// fixed values Telegram picks from. The topic's sigil takes its colour
	// from it rather than hashing the title, which is what the sender
	// ramp does for a name that carries no colour of its own.
	IconColor int32

	// TopMessageID is the last message sent to the topic, which is what
	// orders the topic list.
	TopMessageID int64

	UnreadCount          int32
	UnreadMentionsCount  int32
	UnreadReactionsCount int32

	// ReadInboxMaxID is how far the account has read into the topic.
	// Per-topic read state is the reason a topic needs a chat of its own:
	// the forum's own read pointer says nothing about any one topic.
	ReadInboxMaxID int64

	// Closed means nobody may post except the topic's creator and anyone
	// who can manage topics; Hidden is only ever set on General; My means
	// this account created it.
	Closed bool
	Pinned bool
	Hidden bool
	My     bool

	// LastMessage is the message TopMessageID names, nil until something
	// has filled it in — listing topics returns the messages separately
	// from the topics themselves.
	LastMessage *Message
}

// topicFromTG converts a forum topic record into the domain type.
//
// chatID is passed in rather than read from the record's Peer: the peer is
// the forum, which the caller already knows because it asked for that
// forum's topics, and deriving it here would be a second answer to a
// question that already has one.
func topicFromTG(chatID int64, t *tg.ForumTopic) *Topic {
	return &Topic{
		ID:     int64(t.ID),
		ChatID: chatID,
		// Sanitized for the same reason every other title in this package
		// is: it is server-controlled text on its way to a terminal.
		Title:                sanitizeTerminal(t.Title),
		IconColor:            int32(t.IconColor),
		TopMessageID:         int64(t.TopMessage),
		UnreadCount:          int32(t.UnreadCount),
		UnreadMentionsCount:  int32(t.UnreadMentionsCount),
		UnreadReactionsCount: int32(t.UnreadReactionsCount),
		ReadInboxMaxID:       int64(t.ReadInboxMaxID),
		Closed:               t.Closed,
		Pinned:               t.Pinned,
		Hidden:               t.Hidden,
		My:                   t.My,
	}
}

// syntheticChatIDBase is where the chat IDs handed out to topics start.
//
// It has to be somewhere no Telegram peer can be, and it is: every
// constant.TDLibPeerID band is either negative (basic groups, channels,
// monoforums, secret chats) or ends at MaxTDLibUserID, which is (1<<40)-1.
// 1<<48 is far above that with room to spare, so IsUser, IsChat, IsChannel
// and IsMonoforum all answer false for every ID in this band.
//
// That is what keeps a leak harmless rather than dangerous. The branches in
// ViewMessages, DeleteMessages, GetMessages and ReadMentions choose an RPC
// on IsChannel; an ID that answered yes would be routed as a channel and
// name one that does not exist. Answering no to everything means a stray
// synthetic ID takes the peer path, and the peer path refuses it — see
// [Client.inputPeer]. TestASyntheticIDIsNoPeerTelegramKnows holds this.
const syntheticChatIDBase int64 = 1 << 48

// isSyntheticChatID reports whether a chat ID names a forum topic rather
// than a peer. Every ID at or above the base is one; there is no top, the
// band is open-ended and allocation is sequential from the bottom.
func isSyntheticChatID(id int64) bool {
	return id >= syntheticChatIDBase
}

// topicRef is a topic as Telegram names it: the forum, and the topic's root
// message inside it. It is the registry's key because neither half
// identifies a topic on its own — topic 1 exists in every forum.
type topicRef struct {
	chatID  int64
	topicID int64
}

// topicRegistry translates between a topic and the synthetic chat ID the
// rest of the client knows it by, in both directions.
//
// Session-scoped and never persisted. A synthetic ID means nothing outside
// the process that minted it, and a stored one — a saved layout, a session
// restore — would name a different topic or none at all in the next run.
// Keeping it in memory is what makes that impossible rather than merely
// discouraged.
//
// The zero value is ready to use, as [uploadCache]'s is: clients that the
// tests build field by field get a working registry with no constructor to
// remember.
type topicRegistry struct {
	mu   sync.Mutex
	ids  map[topicRef]int64
	refs map[int64]topicRef

	// allocated is how many IDs have been handed out, and so the offset of
	// the next one from the base. A counter rather than len(ids) because
	// the two only agree while nothing is ever removed, and that is an
	// invariant a reader would have to go looking for.
	allocated int64
}

// idFor returns the chat ID for a topic, allocating one the first time.
//
// Idempotent by construction: the same topic asked for twice is the same
// chat to the reader, and an ID that changed between two asks would split
// one conversation's messages, drafts and read state across two stores.
func (r *topicRegistry) idFor(ref topicRef) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	if id, ok := r.ids[ref]; ok {
		return id
	}
	if r.ids == nil {
		r.ids = make(map[topicRef]int64)
		r.refs = make(map[int64]topicRef)
	}
	id := syntheticChatIDBase + r.allocated
	r.allocated++
	r.ids[ref] = id
	r.refs[id] = ref
	return id
}

// refFor returns the topic a synthetic chat ID stands for, and whether this
// session ever allocated it.
func (r *topicRegistry) refFor(id int64) (topicRef, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ref, ok := r.refs[id]
	return ref, ok
}

// topicChatID is the chat ID under which the rest of the client knows one
// topic of one forum. Allocated on first ask and stable for the session.
func (c *Client) topicChatID(chatID, topicID int64) int64 {
	return c.topics.idFor(topicRef{chatID: chatID, topicID: topicID})
}

// splitTopic turns a chat ID into the chat to name on the wire and the
// topic inside it, and is the chokepoint every exported method that takes a
// chat ID goes through.
//
// An ordinary chat ID comes back unchanged with topic 0, which is what
// makes the call free of consequence for the great majority of chats that
// are not topics.
//
// A synthetic ID this session never allocated ALSO comes back unchanged,
// with topic 0 — deliberately, and this is the interesting case. Nothing
// persists the registry, so such an ID can arrive from a previous run or
// from a translation bug, and it names nothing. Answering it with some real
// chat would read, mark or send somewhere the reader never asked for; the
// ID is handed back still synthetic instead, so [Client.inputPeer] refuses
// it and the failure says what it is. Callers that need to tell the two
// apart ask isSyntheticChatID.
func (c *Client) splitTopic(chatID int64) (real, topic int64) {
	if !isSyntheticChatID(chatID) {
		return chatID, 0
	}
	ref, ok := c.topics.refFor(chatID)
	if !ok {
		return chatID, 0
	}
	return ref.chatID, ref.topicID
}
