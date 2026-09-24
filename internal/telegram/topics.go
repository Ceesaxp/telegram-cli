package telegram

import (
	"fmt"
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

	// TopicChatID is the chat ID the rest of the client knows this topic
	// by, allocated by [Client.topicChatID]. It is what the caller acts
	// on: ChatID above names the FORUM, and opening a topic, reading it
	// or posting to it all take the topic's own ID.
	//
	// Zero until a listing allocates one, because [topicFromTG] converts a
	// record without a client to allocate from. See [Client.ForumTopics].
	TopicChatID int64
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

	// records is the last thing the server said about each topic, by
	// synthetic chat ID. GetChat answers from it: a topic is not a peer,
	// so there is nothing to resolve and nowhere else the title and the
	// counters could come from.
	records map[int64]*Topic

	// listed names the forums this session has listed the topics of. It is
	// how an arriving message can be routed to a topic's store rather than
	// the forum's: outside a listed forum there are no topic stores to
	// route to, and guessing would file the message under an ID nothing
	// has ever drawn.
	listed map[int64]bool

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

// remember records what a listing said about one topic, replacing whatever
// the previous listing said.
//
// Replacing rather than merging is the honest answer: every field on a
// topic record — the counters, the read pointer, the title, the closed flag
// — is what the server believes right now, and a listing is a complete
// statement about the topic rather than a patch to one. Nothing here is
// persisted, for the reason the type's own comment gives.
func (r *topicRegistry) remember(id int64, t *Topic) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.records == nil {
		r.records = make(map[int64]*Topic)
	}
	r.records[id] = t
}

// recall returns the last listing's record for a synthetic chat ID, and
// whether there is one. An allocated ID that was never listed answers
// false: the ID exists, the topic behind it was never described.
func (r *topicRegistry) recall(id int64) (*Topic, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.records[id]
	return t, ok
}

// markListed records that this session has listed chatID's topics.
func (r *topicRegistry) markListed(chatID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listed == nil {
		r.listed = make(map[int64]bool)
	}
	r.listed[chatID] = true
}

// hasListed reports whether this session has listed chatID's topics, and so
// whether the topics of that forum are chats anything above knows about.
func (r *topicRegistry) hasListed(chatID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listed[chatID]
}

// topicChat is the chat a forum topic IS, built from what the last listing
// said about it.
//
// There is no round trip and there cannot be one: a topic names no peer, so
// there is nothing to resolve and nowhere else a title or a counter could
// come from. The listing is the source, the registry is where it is kept,
// and this is the whole of the translation.
//
// The ID is the synthetic one it was asked about, not the forum's, because
// the caller keys everything on it — the message store, the draft, the read
// state, the jump stack. The title is the topic's alone: the thread header
// composes "Forum › Topic" because a reader inside one wants both, and a
// notification wants what the phone says, which is the topic.
//
// Muted is deliberately left alone. Per-topic notification settings exist
// and this client does not read them yet, and of the two ways to be wrong,
// a topic that rings when the reader silenced it is the one a messaging
// client is allowed to pick — the same trade [peerMuted] makes.
func (c *Client) topicChat(chatID int64) (*Chat, error) {
	topic, ok := c.topics.recall(chatID)
	if !ok {
		return nil, fmt.Errorf("get chat %d: no forum topic is known by that ID — "+
			"nothing persists the topic registry, so an ID from a previous session "+
			"or from a topic nobody has listed names nothing", chatID)
	}

	return &Chat{
		ID:                     chatID,
		Type:                   ChatTypeSupergroup,
		Title:                  topic.Title,
		LastMessage:            topic.LastMessage,
		UnreadCount:            topic.UnreadCount,
		UnreadMentionsCount:    topic.UnreadMentionsCount,
		UnreadReactionsCount:   topic.UnreadReactionsCount,
		LastReadInboxMessageID: topic.ReadInboxMaxID,
	}, nil
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

// refuseTopic is how a call says no to a forum topic, and nil for every
// other chat ID.
//
// It is for the handful of questions a topic cannot be asked because they
// are about something a topic is not — a basic group's membership, a
// broadcast post's comments — as opposed to the many that are about the
// forum and are answered by splitting. because completes the sentence "chat
// N is a forum topic: ...", so it says what the call is about rather than
// restating that the chat is a topic.
//
// [Client.inputPeer] refuses too, and that refusal is the net under every
// peer this package resolves. This one is for the calls that resolve no
// peer at all: messages.getFullChat takes a bare chat ID, so nothing would
// have stopped a topic's synthetic ID from being narrowed into a plain one
// and asked about. Both are listed as refusals by the guard in
// topics_guard_test.go, which holds each of them to actually refusing.
func refuseTopic(chatID int64, because string) error {
	if !isSyntheticChatID(chatID) {
		return nil
	}
	return fmt.Errorf("chat %d is a forum topic: %s", chatID, because)
}

// forumTopicsPageSize is how many topics one messages.getForumTopics asks
// for. The server is free to answer with fewer than it was asked for even
// when more exist — the API page says so explicitly — which is why a short
// page is NOT what ends the walk below.
const forumTopicsPageSize = 100

// maxForumTopicPages bounds the walk at 2000 topics, which is far more than
// any forum a person reads has and small enough that hitting it costs a
// couple of seconds rather than a wedged UI.
//
// It is the backstop, not the ordinary way to stop — the count and an empty
// page are — and it is here because the other two are both things the
// SERVER says. An offset it does not honour, or a count it never reaches,
// turns a loop that trusts them into one that never ends, and a client that
// hangs on a forum is worse than one that lists the first two thousand
// topics of it.
const maxForumTopicPages = 20

// ForumTopics lists a forum's topics, walking the pages until it has them.
//
// chatID is split first, so asking a TOPIC for its forum's topics works:
// that is the ordinary way back up out of one, and the topic's own ID
// could never reach the wire anyway.
//
// Every topic comes back with a chat ID of its own, allocated here, and is
// remembered in the registry — those two together are what let the caller
// open a topic as a chat and what lets GetChat answer for it afterwards
// without a round trip.
//
// One context covers the whole walk rather than one per page, so the
// listing as a whole is bounded by opTimeout however many pages it takes.
// That is the second wall around the same failure the page cap guards: the
// cap bounds the requests, the deadline bounds the time.
func (c *Client) ForumTopics(chatID int64) ([]*Topic, error) {
	real, _ := c.splitTopic(chatID)

	ctx, cancel := opCtx()
	defer cancel()

	peer, err := c.inputPeer(ctx, real)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}

	req := &tg.MessagesGetForumTopicsRequest{Peer: peer, Limit: forumTopicsPageSize}

	var (
		out []*Topic
		// received counts the RECORDS the server sent, not the topics that
		// survived conversion, because it is the count the server's own
		// Count is comparable with. A page of deleted topics still moves
		// the walk along even though it adds no rows.
		received int
	)
	for range maxForumTopicPages {
		resp, err := c.api.MessagesGetForumTopics(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("list topics: %w", err)
		}
		if len(resp.Topics) == 0 {
			break
		}
		received += len(resp.Topics)
		out = append(out, c.topicsFromPage(real, resp)...)

		// A short page is deliberately NOT an end: this call is documented
		// to return fewer topics than asked for whenever the server feels
		// like it, so the usual "short page means done" rule would stop the
		// walk in the middle of a large forum. Count is what says how many
		// there are; a page with nothing on it is the backstop for a Count
		// that overshoots.
		if resp.Count > 0 && received >= resp.Count {
			break
		}
		if !advanceForumTopics(req, resp) {
			break
		}
	}

	c.topics.markListed(real)
	return out, nil
}

// advanceForumTopics moves the request on to the page after resp, and
// reports whether there was anything to move it to.
//
// The cursor is the LAST topic of the page, all three offsets from that one
// topic: its ID, its top message, and a date. Which date is the response's
// to say — order_by_create_date means the list is ordered by when topics
// were made, and the cursor is then in the topic's own date; otherwise the
// order is by last message, and the date is that message's.
//
// The last topic that CONVERTED, rather than the last record, because a
// forumTopicDeleted carries an ID and nothing else: there is no date and no
// top message on one to build a cursor from. The deleted record is then
// re-served on the next page and skipped again, which costs nothing.
func advanceForumTopics(req *tg.MessagesGetForumTopicsRequest, resp *tg.MessagesForumTopics) bool {
	var last *tg.ForumTopic
	for _, record := range resp.Topics {
		if raw, ok := record.(*tg.ForumTopic); ok {
			last = raw
		}
	}
	if last == nil {
		return false
	}

	date := last.Date
	if !resp.OrderByCreateDate {
		// Falling back to the topic's own date when the message is not in
		// the page: the messages beside a page are "related", so a deleted
		// top message simply is not there. A zero date would ask for the
		// newest topics again and walk the same page forever.
		for _, m := range resp.Messages {
			if m.GetID() != last.TopMessage {
				continue
			}
			if full, ok := m.AsNotEmpty(); ok {
				date = full.GetDate()
			}
			break
		}
	}

	req.OffsetTopic = last.ID
	req.OffsetID = last.TopMessage
	req.OffsetDate = date
	return true
}

// topicsFromPage converts one page of topic records, attaching each
// topic's last message and allocating its chat ID.
//
// The server's order is kept as it stands. It already puts pinned topics
// first, which is the order the topic list wants; sorting again here would
// only risk disagreeing with what the phone shows.
func (c *Client) topicsFromPage(forumID int64, page *tg.MessagesForumTopics) []*Topic {
	messages := make(map[int64]tg.MessageClass, len(page.Messages))
	for _, m := range page.Messages {
		messages[int64(m.GetID())] = m
	}

	out := make([]*Topic, 0, len(page.Topics))
	for _, record := range page.Topics {
		// A forumTopicDeleted carries an ID and nothing else. It is there so
		// a client holding a stale list can drop the topic; this client
		// holds none between calls, so it is simply not a row.
		raw, ok := record.(*tg.ForumTopic)
		if !ok {
			continue
		}

		topic := topicFromTG(forumID, raw)
		if m, ok := messages[topic.TopMessageID]; ok {
			topic.LastMessage = c.messageClassFromTG(m)
		}
		topic.TopicChatID = c.topicChatID(forumID, topic.ID)
		c.topics.remember(topic.TopicChatID, topic)
		out = append(out, topic)
	}
	return out
}
