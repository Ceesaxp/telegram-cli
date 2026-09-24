package chatlist

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/sigil"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/Ceesaxp/telegram-cli/internal/ui/widgets"
	"github.com/charmbracelet/lipgloss"
)

// A forum opens like a directory (docs/topics.md, "The interaction").
//
// Enter on a forum row replaces this panel's CONTENTS with that forum's
// topics — the same column, the same two-row cells, the same filter, one
// level down. It is deliberately not a third column: at 72 cells there is no
// room for one, and a file manager is the interaction every reader already
// knows.
//
// This component never asks Telegram for anything here. It is handed a
// forum and its topics and hands back the topic the reader chose; the
// fetching and the opening are the app's, which is what keeps the drill-in
// testable without a client.

// forumState is the chat list while it is drilled into a forum, plus what
// it has to put back when it comes up again.
//
// Heap-allocated behind a pointer on Model for the reason list and dirty
// are: renderRow is a METHOD VALUE bound to a copy of the model at restyle
// time (see [Model.restyle]), so a topic recorded into the model by value
// would be invisible to the renderer that has to draw it.
type forumState struct {
	// chatID is the open forum, 0 when the list is showing chats. It is
	// the whole of "am I drilled in": there is no second flag to disagree
	// with it.
	chatID int64
	title  string

	// topics is the list as the server gave it, kept in that order — the
	// server already puts pinned topics first, and sorting again here
	// would be a second opinion about an ordering that has one.
	topics []*telegram.Topic

	// byID is topics indexed by topic ID, for the row renderer: it is
	// handed a widgets.ListItem carrying nothing but the ID, and a linear
	// scan per row would be a scan per row per frame.
	byID map[int64]*telegram.Topic

	// prevFilter and prevSelected are the chat list this drill-in
	// interrupted. A forum is one level down of the same panel, so coming
	// back up puts the reader where they were rather than at the top of an
	// unfiltered list they did not ask for.
	prevFilter   string
	prevSelected string
}

// EnterForum drills the list into a forum: the rows become that forum's
// topics, and stay topics until [Model.LeaveForum].
//
// The list is EMPTY until SetTopics arrives, because the fetch is
// asynchronous and leaving the chats on screen under a forum's name would
// say the forum holds them.
//
// The chat list's filter does not come down with it. A query typed against
// chat titles says nothing about topic titles, and one applied to both would
// hide rows for a reason the reader cannot see from the row that is left.
// LeaveForum puts it back.
func (m *Model) EnterForum(chatID int64, title string) {
	if chatID == 0 {
		return
	}
	if m.forum == nil {
		m.forum = &forumState{}
	}
	// Recorded only on the way IN. Topics do not nest, so a second
	// EnterForum is a redirect rather than a second level, and overwriting
	// here would lose the chat list under both of them.
	if !m.inForum() {
		m.forum.prevFilter = m.filter
		m.forum.prevSelected = m.list.SelectedID()
	}

	m.forum.chatID = chatID
	m.forum.title = title
	m.forum.topics = nil
	m.forum.byID = nil

	m.filter = ""
	m.filterInput.Reset()
	m.closeFilterInput()
	m.refreshList()
}

// SetTopics installs the topics of the open forum.
//
// chatID names the forum they belong to, and an answer for any OTHER forum
// is dropped — including one for a forum the reader has already left. The
// fetch is asynchronous, so a slow answer can arrive after Esc, and
// redrawing the list under somebody who has walked away from it is the bug
// this guard exists for.
func (m *Model) SetTopics(chatID int64, topics []*telegram.Topic) {
	if !m.inForum() || m.forum.chatID != chatID {
		return
	}

	m.forum.topics = slices.Clone(topics)
	m.forum.byID = make(map[int64]*telegram.Topic, len(topics))
	for _, t := range topics {
		if t != nil {
			m.forum.byID[t.ID] = t
		}
	}
	m.refreshList()
}

// TopicMessage lands an arriving message on the topic row it belongs to,
// the way [telegram.ChatLastMessageMsg] lands one on a chat row.
//
// chatID is the topic's SYNTHETIC chat ID, which is what a message in a
// topic is published under. It has to be matched here rather than through
// this component's ordinary handling of that message, because that answers
// off the store and a topic is not in it: while the list is drilled in its
// rows come from [Model.SetTopics], so an arriving message would otherwise
// leave the column exactly as the listing left it until the reader walked
// out of the forum and back in.
//
// Called by the app, which is the only thing that knows a synthetic chat ID
// names a topic at all.
func (m *Model) TopicMessage(chatID int64, msg *telegram.Message) {
	if !m.inForum() || msg == nil {
		return
	}
	topic := m.topicByChat(chatID)
	if topic == nil {
		return
	}

	if topicCountsAsUnread(topic, msg) {
		topic.UnreadCount++
		// Under the same guard, so a replayed mention cannot count twice.
		if msg.UnreadMention {
			topic.UnreadMentionsCount++
		}
	}
	// The preview moves with the pointer, never on its own. A reconnect
	// replays messages this client already has, and an older one taking the
	// row said the topic's last word was something said before the word the
	// row was already showing — while the pointer, one line up, correctly
	// refused to move.
	if msg.ID > topic.TopMessageID {
		topic.TopMessageID = msg.ID
		topic.LastMessage = msg
	}

	// refreshList rather than markDirty: the preview, the time and the
	// badge are all built in topicItems, so redrawing the rows the list
	// already holds would show the message nowhere.
	m.refreshList()
}

// TopicRead clears a topic's badge and moves its read pointer, the way
// [telegram.ChatMarkedReadMsg] does both for a chat in the store.
//
// chatID is the topic's SYNTHETIC chat ID, for TopicMessage's reason: the
// mark is published under the ID the reader's thread was keyed by, and only
// the app knows that such an ID names a topic at all, so it calls this.
//
// The rule is the store's own (store.MarkReadUpTo), applied to the topic's
// numbers: the pointer moves forward and never back, because reads arrive
// out of order and an older one must not make read messages count again;
// and the count goes to zero only when the mark has reached the newest
// message the row knows of, because a partial read has left something
// unread.
//
// Without it nothing anywhere took a topic's count DOWN. TopicMessage put
// arrivals on it, a topic read to the end kept every one of them, and three
// more arriving while the reader read them made it six.
func (m *Model) TopicRead(chatID int64, maxID int64) {
	topic := m.topicByChat(chatID)
	if topic == nil {
		return
	}

	if maxID > topic.ReadInboxMaxID {
		topic.ReadInboxMaxID = maxID
	}
	if maxID >= topic.TopMessageID {
		topic.UnreadCount = 0
	}

	// refreshList rather than markDirty, for TopicMessage's reason: the
	// badge is built in topicItems, so redrawing the rows the list already
	// holds would leave it on screen.
	m.refreshList()
}

// topicByChat is the open forum's topic with the given synthetic chat ID.
//
// A linear scan, unlike [Model.topicFor]: this runs once per arriving
// message rather than once per row per frame, and a second index keyed the
// other way would be a second thing to keep in step with the listing.
func (m Model) topicByChat(chatID int64) *telegram.Topic {
	if !m.inForum() || chatID == 0 {
		return nil
	}
	for _, t := range m.forum.topics {
		if t != nil && t.TopicChatID == chatID {
			return t
		}
	}
	return nil
}

// topicCountsAsUnread says whether an arriving message adds one to a
// topic's badge. It is the chat store's rule (see store.countsAsUnread),
// applied to the topic's own read pointer: only a message from the other
// side, with a server ID, past what the account has read and past what the
// row already counts. The same message can arrive twice — a replay after a
// reconnect — and counting it twice is how a badge comes to describe a
// conversation nobody can find the unread messages in.
func topicCountsAsUnread(topic *telegram.Topic, msg *telegram.Message) bool {
	if msg.IsOutgoing || msg.ID <= 0 {
		return false
	}
	return msg.ID > topic.ReadInboxMaxID && msg.ID > topic.TopMessageID
}

// LeaveForum comes back up to the chats, restoring the list exactly as the
// drill-in found it: the same filter, and the same chat under the cursor.
//
// It emits nothing. Leaving is a change of what this panel is showing, not
// an event in the conversation; the app reads [Model.ForumChatID] when it
// needs to know which of the two it is looking at.
func (m *Model) LeaveForum() {
	if !m.inForum() {
		return
	}
	f := m.forum

	m.filter = f.prevFilter
	m.filterInput.Value = f.prevFilter
	m.filterInput.Cursor = m.filterInput.Len()
	m.closeFilterInput()

	f.chatID, f.title = 0, ""
	f.topics, f.byID = nil, nil
	m.refreshList()

	m.selectRow(f.prevSelected)
	f.prevFilter, f.prevSelected = "", ""
}

// ForumChatID is the forum the list is drilled into, and 0 when it is
// showing chats. It is how the app tells the two apart — see LeaveForum.
func (m Model) ForumChatID() int64 {
	if m.forum == nil {
		return 0
	}
	return m.forum.chatID
}

// ForumTitle is the open forum's name, "" when the list is showing chats.
func (m Model) ForumTitle() string {
	if m.forum == nil {
		return ""
	}
	return m.forum.title
}

// inForum is the one question every source-dependent branch in this package
// asks. Phrased against chatID rather than a flag of its own so there is
// nothing for it to disagree with.
func (m Model) inForum() bool { return m.ForumChatID() != 0 }

// CursorTopic is the topic under the highlight, nil when the list is not
// drilled into a forum.
//
// The counterpart of [Model.CursorChatId], which answers 0 while a topic is
// under the cursor: a topic's chat ID is the app's to work out, not this
// component's (see [TopicSelectedMsg]).
func (m Model) CursorTopic() *telegram.Topic {
	item := m.list.SelectedItem()
	if item == nil {
		return nil
	}
	return m.topicFor(item.ID)
}

// topicFor is the topic a row stands for, nil for a chat row.
//
// It is the seam the whole drill-in hangs off: the row renderer is handed a
// widgets.ListItem, which is a shared widget type this package does not own
// and so cannot add a topic to.
func (m Model) topicFor(id string) *telegram.Topic {
	if !m.inForum() || m.forum.byID == nil {
		return nil
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil
	}
	return m.forum.byID[n]
}

// selectRow puts the cursor back on a row by ID, if it is still there.
func (m *Model) selectRow(id string) {
	if id == "" {
		return
	}
	for i, item := range m.list.Items {
		if item.ID == id {
			m.list.SelectIndex(i)
			return
		}
	}
}

// topicItems builds the rows from the open forum's topics, the way
// [Model.chatItems] builds them from the store.
//
// The second return is how many topics the forum holds before the query —
// the denominator the header's "shown/total" prints — counted in this pass
// for chatItems' reason.
func (m *Model) topicItems() ([]widgets.ListItem, int) {
	topics := m.forum.topics
	items := make([]widgets.ListItem, 0, len(topics))
	listed := 0

	for _, t := range topics {
		if t == nil {
			continue
		}
		// Hidden is only ever set on General, and a hidden General is one
		// the forum has chosen not to show. Absent, not dimmed — and not
		// part of the total either, or the count would describe a list with
		// a row nobody can reach.
		if t.Hidden {
			continue
		}
		listed++

		if m.filter != "" && !strings.Contains(strings.ToLower(t.Title), strings.ToLower(m.filter)) {
			continue
		}

		preview := ""
		metaAt := int32(0)
		if t.LastMessage != nil {
			// The instant, not a sentence about it — ageMeta turns it into
			// "2m" on the way to the screen, as it does for a chat.
			metaAt = t.LastMessage.Date
			// A forum is a supergroup, so a topic always has several
			// speakers and the preview always says which one.
			preview = m.previewOf(t.LastMessage, true)
		}

		badge := ""
		if t.UnreadCount > 0 {
			// Never muted: per-topic notification settings are out of the
			// first wave (docs/topics.md, "Out of scope"), so a topic is as
			// loud as the forum it is in.
			badge = unreadBadge(t.UnreadCount, false)
		}

		items = append(items, widgets.ListItem{
			ID:       fmt.Sprintf("%d", t.ID),
			Title:    t.Title,
			Subtitle: preview,
			Badge:    badge,
			MetaAt:   metaAt,
			Mention:  t.UnreadMentionsCount > 0,
		})
	}

	return items, listed
}

// Telegram picks a topic's fallback icon colour from exactly six fixed
// values (docs/topics.md, "What Telegram actually does"). They are listed
// here as the protocol's own RGB rather than translated at the edge,
// because the protocol is what a reader checking this table against
// core.telegram.org/api/forum has in front of them.
const (
	topicIconBlue   int32 = 0x6FB9F0
	topicIconYellow int32 = 0xFFD67E
	topicIconPurple int32 = 0xCB86DB
	topicIconGreen  int32 = 0x8EEE98
	topicIconPink   int32 = 0xFF93B2
	topicIconRed    int32 = 0xFB6F5F
)

// topicColour maps one of Telegram's six icon colours onto the palette, and
// reports whether it recognised it.
//
// By HUE, not by handing out six distinct roles: the palette has five
// accents that are not the focus cyan, so pink and red both land on Red,
// which is the one warm-red role there is. Two topics that Telegram draws a
// shade apart are the same colour here, and that is a smaller lie than
// drawing one of them green to keep the six apart.
//
// A colour this table does not know is not guessed at — see [Model.topicSigil].
func topicColour(icon int32, r theme.Roles) (lipgloss.Color, bool) {
	switch icon {
	case topicIconBlue:
		return r.Blue, true
	case topicIconYellow:
		return r.Amber, true
	case topicIconPurple:
		return r.Mauve, true
	case topicIconGreen:
		return r.Green, true
	case topicIconPink, topicIconRed:
		return r.Red, true
	}
	return "", false
}

// topicSigil is a topic's mark and its colour.
//
// The GLYPH is the group's, because a topic is part of one and the sigil is
// a shared vocabulary (internal/ui/sigil) rather than this component's to
// extend. Only the COLOUR is the topic's own, taken from icon_color rather
// than hashed out of the title the way a sender's name is: Telegram already
// chose one, and a second opinion would be the same topic in two colours
// depending on which client you were looking at.
//
// An icon_color the palette has no answer for falls back to the forum's own
// sigil colour, which is the honest failure: the topic looks like the group
// it lives in rather than like a colour this package invented.
func (m Model) topicSigil(t *telegram.Topic) (string, lipgloss.Color) {
	mark, group := sigil.For(telegram.ChatTypeSupergroup, false, m.roles)
	if colour, ok := topicColour(t.IconColor, m.roles); ok {
		return mark, colour
	}
	return mark, group
}
