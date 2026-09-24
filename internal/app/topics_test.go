package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
	"github.com/charmbracelet/x/ansi"
)

// A forum opens like a directory (docs/topics.md, "The interaction"): Enter
// on one replaces the chat list's rows with its topics rather than opening a
// conversation, and a topic opens as the chat it is modelled as.
//
// These tests drive that as the sequence of messages it is — the selection,
// the answer to the topic listing, the thread the drill-in should show —
// because every one of those steps is a message in the running app and a
// test that called the methods instead would not notice one of them being
// dropped from the wiring.

const (
	testForumID       int64 = -1001500
	testPlainChatID   int64 = -1001501
	testGeneralChatID int64 = 1 << 52
	testJobsChatID    int64 = 1<<52 + 1
)

// fakeForums stands in for the Telegram client's topic listing, the way
// fakeSender stands in for its sends.
type fakeForums struct {
	asked  []int64
	topics []*telegram.Topic
	err    error
}

func (f *fakeForums) ForumTopics(chatID int64) ([]*telegram.Topic, error) {
	f.asked = append(f.asked, chatID)
	if f.err != nil {
		return nil, f.err
	}
	return f.topics, nil
}

// goSerbiaTopics is the forum's listing as the client would answer it, with
// the synthetic chat IDs already allocated.
func goSerbiaTopics() []*telegram.Topic {
	return []*telegram.Topic{
		{ID: 1, ChatID: testForumID, Title: "General", TopicChatID: testGeneralChatID},
		{ID: 7, ChatID: testForumID, Title: "Jobs", TopicChatID: testJobsChatID},
	}
}

func jobsTopic() *telegram.Topic { return goSerbiaTopics()[1] }

// forumModel is a main-screen app whose chat list holds one forum and one
// ordinary chat, with the topic listing answered by a fake.
func forumModel(t *testing.T, f *fakeForums) Model {
	t.Helper()
	return forumChats(t, sizedMainModel(t, PanelChatList), f)
}

// forumChats gives a model the two rows every drill-in test browses, and
// the fake that answers for the forum's topics.
//
// Separate from forumModel so a test needing an app built some other way —
// one that will actually notify, say — gets the same two rows rather than
// restating them.
func forumChats(t *testing.T, m Model, f *fakeForums) Model {
	t.Helper()
	m.forums = f
	m.store.Chats.Set(&telegram.Chat{
		ID: testForumID, Title: "Go Serbia",
		Type: telegram.ChatTypeSupergroup, IsForum: true, Order: 2,
	})
	m.store.Chats.Set(&telegram.Chat{
		ID: testPlainChatID, Title: "Ana",
		Type: telegram.ChatTypePrivate, Order: 1,
	})
	m.chatList.MarkLoadedForTest()
	_ = m.chatList.View()
	return m
}

// enterForum selects the forum and lets both of the drill-in's answers land:
// the topic listing and the thread that goes with it.
func enterForum(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := deliver(t, m, chatlist.ChatSelectedMsg{ChatId: testForumID})
	for _, msg := range messages(t, cmd) {
		m, _ = deliver(t, m, msg)
	}
	return m
}

func TestSelectingAForumDrillsIntoItsTopics(t *testing.T) {
	f := &fakeForums{topics: goSerbiaTopics()}
	m := forumModel(t, f)

	m, cmd := deliver(t, m, chatlist.ChatSelectedMsg{ChatId: testForumID})

	if got := m.chatList.ForumChatID(); got != testForumID {
		t.Fatalf("the chat list is drilled into %d, want the forum %d", got, testForumID)
	}
	if got := m.chatList.ForumTitle(); got != "Go Serbia" {
		t.Errorf("the drill-in is headed %q, want %q", got, "Go Serbia")
	}
	messages(t, cmd)
	if len(f.asked) != 1 || f.asked[0] != testForumID {
		t.Errorf("ForumTopics was asked for %v, want one listing of the forum", f.asked)
	}
}

func TestSelectingAnOrdinaryChatStillOpensIt(t *testing.T) {
	f := &fakeForums{topics: goSerbiaTopics()}
	m := forumModel(t, f)

	m, _ = deliver(t, m, chatlist.ChatSelectedMsg{ChatId: testPlainChatID})

	if got := m.chatList.ForumChatID(); got != 0 {
		t.Errorf("an ordinary chat drilled the list into %d", got)
	}
	if got := m.chatView.ChatId(); got != testPlainChatID {
		t.Errorf("the thread shows %d, want the chat that was selected", got)
	}
	if len(f.asked) != 0 {
		t.Errorf("an ordinary chat asked for topics: %v", f.asked)
	}
}

func TestTheForumsTopicsReachTheChatList(t *testing.T) {
	m := enterForum(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))

	view := ansi.Strip(m.chatList.View())
	for _, want := range []string{"General", "Jobs"} {
		if !strings.Contains(view, want) {
			t.Errorf("the drilled-in list has no %q row:\n%s", want, view)
		}
	}
}

func TestAForumWhoseTopicsWillNotLoadSaysSo(t *testing.T) {
	m := enterForum(t, forumModel(t, &fakeForums{err: errors.New("flood wait")}))

	notice := ansi.Strip(m.hintBar.View())
	if !strings.Contains(notice, "topics") {
		t.Errorf("a failed topic listing said %q, want a notice naming topics", notice)
	}
}

func TestSelectingATopicOpensItsOwnChat(t *testing.T) {
	m := enterForum(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))

	m, _ = deliver(t, m, chatlist.TopicSelectedMsg{Topic: jobsTopic()})

	if got := m.chatView.ChatId(); got != testJobsChatID {
		t.Errorf("the thread shows %d, want the topic's own chat %d", got, testJobsChatID)
	}
	if got := m.composer.ChatId(); got != testJobsChatID {
		t.Errorf("the composer sends to %d, want the topic's own chat %d", got, testJobsChatID)
	}
}

// The flat forum stream, but only until a topic in it has been opened
// (docs/topics.md, "Resolved" 2).
func TestEnteringAForumTheFirstTimeOpensTheForum(t *testing.T) {
	m := enterForum(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))

	if got := m.chatView.ChatId(); got != testForumID {
		t.Errorf("the thread shows %d on first entry, want the forum %d", got, testForumID)
	}
}

func TestReenteringAForumOpensTheLastTopicRead(t *testing.T) {
	m := enterForum(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))
	m, _ = deliver(t, m, chatlist.TopicSelectedMsg{Topic: jobsTopic()})

	// The esc ladder: opening the topic put the focus in the thread, so the
	// first press gives the panel back and the second leaves the forum.
	m = update(t, m, "\x1b")
	m = update(t, m, "\x1b")
	if got := m.chatList.ForumChatID(); got != 0 {
		t.Fatalf("esc left the list drilled into %d", got)
	}

	m = enterForum(t, m)

	if got := m.chatView.ChatId(); got != testJobsChatID {
		t.Errorf("re-entering the forum shows %d, want the last topic read %d", got, testJobsChatID)
	}
}

func TestTheThreadHeaderNamesTheForumAndTheTopic(t *testing.T) {
	m := enterForum(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))

	m, _ = deliver(t, m, chatlist.TopicSelectedMsg{Topic: jobsTopic()})

	if got := ansi.Strip(m.chatView.View()); !strings.Contains(got, "Go Serbia › Jobs") {
		t.Errorf("the thread header does not read \"Go Serbia › Jobs\":\n%s", got)
	}
}

// A message arriving in a topic lands on that topic's row.
//
// The chat list's own ChatLastMessageMsg handling answers off the store,
// and a topic is not in it: its rows come from the listing (see
// [chatlist.Model.SetTopics]), so without this the drilled-in column stayed
// as the listing left it until the reader walked out and back in.
func TestAMessageArrivingInATopicUpdatesItsRow(t *testing.T) {
	m := enterForum(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))

	m, _ = deliver(t, m, telegram.ChatLastMessageMsg{
		ChatId: testJobsChatID,
		LastMessage: &telegram.Message{
			ID: 4200, ChatID: testJobsChatID, Date: int32(time.Now().Unix()),
			SenderID: &telegram.MessageSenderUser{UserID: 77},
			Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "remote Go role"}},
		},
	})

	view := ansi.Strip(m.chatList.View())
	if !strings.Contains(view, "remote Go role") {
		t.Errorf("the Jobs row does not preview the message that just arrived:\n%s", view)
	}
	if !strings.Contains(view, "[1]") {
		t.Errorf("the Jobs row carries no unread badge for the message that just arrived:\n%s", view)
	}
}

// l on a topic row is Enter on it: the cursored ROW is what the browsing
// keys act on, and while the list is drilled in that row is a topic.
func TestLOnATopicRowOpensThatTopic(t *testing.T) {
	m := enterForum(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))
	m.setFocus(PanelChatList)
	m = update(t, m, "j") // off General, onto Jobs

	m = update(t, m, "l")

	if got := m.chatView.ChatId(); got != testJobsChatID {
		t.Errorf("l on the Jobs row opened %d, want the topic's own chat %d", got, testJobsChatID)
	}
}

// i is l plus the composer, on a topic row as on a chat row.
func TestIOnATopicRowComposesToThatTopic(t *testing.T) {
	m := enterForum(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))
	m.setFocus(PanelChatList)
	m = update(t, m, "j") // off General, onto Jobs

	m = update(t, m, "i")

	if m.focus != PanelComposer {
		t.Errorf("i on a topic row left the focus on %v, want the composer", m.focus)
	}
	if got := m.composer.ChatId(); got != testJobsChatID {
		t.Errorf("the composer sends to %d, want the topic under the cursor %d", got, testJobsChatID)
	}
}
