package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// A forum opens like a directory (docs/topics.md, "The interaction"), and
// this file is the app's half of that: the chat list draws the topics and
// says which one was chosen, and everything about WHICH chat a topic is,
// which one to show on the way in and what the thread calls it, is decided
// here.
//
// It has to be here rather than in the chat list, for the same reason
// [chatlist.TopicSelectedMsg] carries a topic rather than a chat ID: a
// topic's synthetic chat ID is a fact about the client's model of the
// world, and the panel drawing the rows has no business knowing it.

// forumLister is what the app needs of the Telegram client to list a
// forum's topics. See [telegram.Client.ForumTopics].
type forumLister interface {
	ForumTopics(chatID int64) ([]*telegram.Topic, error)
}

// topicsLoadedMsg is the answer to one forum's topic listing. It carries
// the forum it was asked about, because the chat list drops an answer for
// any other — a slow listing can land after the reader has walked away.
type topicsLoadedMsg struct {
	chatID int64
	topics []*telegram.Topic
	err    error
}

// forumThreadMsg asks for the thread that goes beside a forum's topic list.
//
// A message rather than a step inside the drill-in, because the two are
// different questions: which rows the chat list shows is settled the
// moment the forum is selected, while which conversation the thread should
// be showing is a preference this session has been keeping (see
// [Model.lastTopic]). Every other "take the reader somewhere" in this app
// is a message of its own for the same reason — openDiscussionMsg,
// telegramLinkResolvedMsg, chatview.MentionJumpMsg.
type forumThreadMsg struct {
	chatID int64
}

// topicSeparator is what stands between a forum and its topic in the
// thread's header. The header is one string today, so the pair is composed
// here rather than handed to the chat view as two fields — reworking that
// panel's width arithmetic is a bigger change than a forum deserves.
const topicSeparator = " › "

// forumChat reports whether a chat is a forum, and what it is called.
//
// Read off the store rather than asked of the client: whether a supergroup
// is a forum arrives with the dialog, and a round trip to find out would
// put a wait in front of every Enter in the list.
func (m Model) forumChat(chatID int64) (title string, ok bool) {
	entry, found := m.store.Chats.Get(chatID)
	if !found || entry.Chat == nil || !entry.Chat.IsForum {
		return "", false
	}
	return entry.Chat.Title, true
}

// openSelectedChat is what choosing a row in the chat list means: an
// ordinary chat opens, and a forum drills in.
func (m *Model) openSelectedChat(chatID int64) tea.Cmd {
	if title, ok := m.forumChat(chatID); ok {
		return m.enterForum(chatID, title)
	}
	return m.openChatAt(chatID, 0)
}

// enterForum replaces the chat list's rows with a forum's topics and asks
// for them.
//
// The list is empty until the answer lands — see [chatlist.Model.EnterForum]
// — so the fetch and the thread are both started here and the reader watches
// the column fill.
func (m *Model) enterForum(chatID int64, title string) tea.Cmd {
	m.chatList.EnterForum(chatID, title)
	return tea.Batch(
		m.forumTopicsCmd(chatID),
		func() tea.Msg { return forumThreadMsg{chatID: chatID} },
	)
}

// forumTopicsCmd asks the client for a forum's topics.
func (m *Model) forumTopicsCmd(chatID int64) tea.Cmd {
	forums := m.forums
	if forums == nil {
		return nil
	}
	return func() tea.Msg {
		topics, err := forums.ForumTopics(chatID)
		return topicsLoadedMsg{chatID: chatID, topics: topics, err: err}
	}
}

// applyTopics hands a listing to the chat list, or says why there is none.
//
// A forum whose topics will not load must say so: the drill-in has already
// emptied the column, and an empty column under a forum's name reads as a
// forum with nothing in it.
func (m *Model) applyTopics(msg topicsLoadedMsg) {
	if msg.err != nil {
		m.notify(fmt.Sprintf("⚠ could not load topics: %v", msg.err))
		return
	}
	m.chatList.SetTopics(msg.chatID, msg.topics)
}

// openForumThread shows the thread that belongs beside a forum's topics:
// the last topic read in it, or the forum's own flat stream the first time
// (docs/topics.md, "Resolved" 2).
//
// The focus the app had is put back afterwards. Opening a chat normally
// means the reader is going there and openChatAt takes them, but a drill-in
// is a move INTO a list — the next thing they do is walk the topics — and
// handing the thread the focus would cost an h before every one of them.
func (m *Model) openForumThread(chatID int64) tea.Cmd {
	target := chatID
	if last, ok := m.lastTopic[chatID]; ok {
		target = last
	}

	focus := m.focus
	cmd := m.openChatAt(target, 0)
	m.setFocus(focus)
	return cmd
}

// openTopic opens a topic as the chat it is modelled as, through the same
// path every other chat takes — so history, read marks, the composer and
// its draft need no case of their own.
func (m *Model) openTopic(topic *telegram.Topic) tea.Cmd {
	if topic == nil || topic.TopicChatID == 0 {
		return nil
	}
	m.rememberTopic(topic)
	return m.openChatAt(topic.TopicChatID, 0)
}

// rememberTopic records what the app has to know about a topic once it is
// no longer the chat list's to answer for: which topic its forum was last
// read at, and what the thread calls it.
//
// Session-scoped on purpose. Synthetic chat IDs are allocated per session
// (docs/topics.md, "The model"), so a remembered one would name a different
// topic — or nothing — the next time the client starts.
func (m *Model) rememberTopic(topic *telegram.Topic) {
	if m.lastTopic == nil {
		m.lastTopic = map[int64]int64{}
	}
	m.lastTopic[topic.ChatID] = topic.TopicChatID

	if m.topicHeaders == nil {
		m.topicHeaders = map[int64]string{}
	}
	m.topicHeaders[topic.TopicChatID] = topicHeader(m.forumTitleOf(topic.ChatID), topic.Title)
}

// forumTitleOf is a forum's name for the header a topic opens under.
//
// The open drill-in answers first: it was named from the store on the way
// in, and it is still the right name for a forum that has since fallen off
// the loaded dialog page.
func (m Model) forumTitleOf(chatID int64) string {
	if m.chatList.ForumChatID() == chatID {
		if title := m.chatList.ForumTitle(); title != "" {
			return title
		}
	}
	if entry, ok := m.store.Chats.Get(chatID); ok && entry.Chat != nil {
		return entry.Chat.Title
	}
	return ""
}

// topicHeader is the pair the thread's header shows for a topic, so the
// reader always knows which of the two they are in.
//
// Untruncated: how much of it fits is the header's own arithmetic, and
// cutting it here would cut the forum's name on a wide terminal that had
// room for both.
func topicHeader(forum, topic string) string {
	if forum == "" {
		return topic
	}
	return forum + topicSeparator + topic
}

// cursorChatID is the chat under the chat list's cursor, whether that row
// is a chat or a topic.
//
// While the list is drilled in, CursorChatId withholds an answer rather
// than hand back a topic's message ID as though it were a chat ID (see
// [chatlist.Model.CursorChatId]), so every browsing key that acts on "the
// row under the cursor" has to ask this instead.
func (m Model) cursorChatID() int64 {
	if topic := m.chatList.CursorTopic(); topic != nil {
		return topic.TopicChatID
	}
	return m.chatList.CursorChatId()
}
