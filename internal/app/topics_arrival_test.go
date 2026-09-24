package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
	"github.com/charmbracelet/x/ansi"
)

// A message arriving in a forum topic is published twice — once under the
// forum, once under the topic's synthetic chat ID — and the second copy has
// exactly two readers: the thread, when that topic is the one open, and the
// topic's own row in the drilled-in list. These tests drive the pair as the
// sequence of messages the listener really publishes, because which of the
// two copies goes where is the whole of what is being asserted.

// jobsArrival is everything internal/telegram publishes for one message in
// the forum's Jobs topic: the forum's own pair — the thread's copy, then the
// chat list's row — followed by the topic's, which is the same message filed
// under the topic's synthetic chat ID.
//
// All four, in that order, because that is what the running client sends and
// because a fix that only looked at one of the two copies would leave the
// other doing the damage. Telegram's own
// TestAMessageInAListedForumIsAlsoPublishedUnderItsTopic pins the pairing
// and the order from the other side.
func jobsArrival(text string) []tea.Msg {
	published := func(chatID int64) []tea.Msg {
		msg := &telegram.Message{
			ID: 4200, ChatID: chatID, TopicID: 7, Date: int32(time.Now().Unix()),
			SenderID: &telegram.MessageSenderUser{UserID: 77},
			Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: text}},
		}
		return []tea.Msg{
			telegram.NewMessageMsg{Message: msg},
			telegram.ChatLastMessageMsg{ChatId: chatID, LastMessage: msg},
		}
	}
	return append(published(testForumID), published(testJobsChatID)...)
}

// jobsTopicCopy is the second half of it alone: the copy filed under the
// topic. It is what a test uses when the forum's own copy would confuse the
// question — the forum IS a chat of the reader's, and its row moving is
// correct, so a test about rows that must not appear has to leave it out.
func jobsTopicCopy(text string) []tea.Msg { return jobsArrival(text)[2:] }

// deliverAll drives the app with a run of non-key messages, in order.
func deliverAll(t *testing.T, m Model, msgs []tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = deliver(t, m, msg)
	}
	return m
}

// leaveForum comes back up to the chats the way the reader does.
func leaveForum(t *testing.T, m Model) Model {
	t.Helper()
	m.setFocus(PanelChatList)
	m = update(t, m, "\x1b")
	if got := m.chatList.ForumChatID(); got != 0 {
		t.Fatalf("esc left the list drilled into %d", got)
	}
	return m
}

// enterForumByKey drills into the forum the way the reader does, through
// the chat list's own Enter.
//
// Not the same thing as handing the app a ChatSelectedMsg, which is what
// the plain enterForum helper does: it is the chat list's Enter that also
// records the forum as the chat this panel has open (see
// chatlist.Model.OpenCursor), and every test below about what notifies
// turns on both panels agreeing about that, as they do in the running app.
func enterForumByKey(t *testing.T, m Model) Model {
	t.Helper()
	m.setFocus(PanelChatList)

	m, cmd := updateCmd(t, m, "l") // the cursor starts on Go Serbia
	for _, msg := range messages(t, cmd) {
		m, _ = deliver(t, m, msg)
	}

	if got := m.chatList.ForumChatID(); got != testForumID {
		t.Fatalf("the chat list is drilled into %d, want the forum %d", got, testForumID)
	}
	if got := m.chatList.ActiveChatId(); got != testForumID {
		t.Fatalf("the chat list has %d open, want the forum %d", got, testForumID)
	}
	return m
}

// openJobsTopic enters the forum and opens one of its topics, which is the
// state both defects are about.
func openJobsTopic(t *testing.T, m Model) Model {
	t.Helper()
	m = enterForumByKey(t, m)

	m, _ = deliver(t, m, chatlist.TopicSelectedMsg{Topic: jobsTopic()})
	if got := m.chatView.ChatId(); got != testJobsChatID {
		t.Fatalf("the thread shows %d, want the Jobs topic %d", got, testJobsChatID)
	}
	return m
}

// A topic is not a chat the reader has, and must never become a row in the
// top-level list.
//
// The chat list's own handling of ChatLastMessageMsg answers off the chat
// store, which invents an entry for any ID it has not seen — so the topic's
// copy of an arriving message minted a chat out of the synthetic ID, and the
// resolve behind it named that chat after the topic. The forum's topic then
// sat in the chat list beside the forum it lives in, and stayed there.
func TestAMessageInATopicDoesNotBecomeAChatListRow(t *testing.T) {
	m := forumModel(t, &fakeForums{topics: goSerbiaTopics()})
	before := ansi.Strip(m.chatList.View())

	m = enterForum(t, m)
	m = deliverAll(t, m, jobsTopicCopy("remote Go role"))
	m = leaveForum(t, m)

	after := ansi.Strip(m.chatList.View())
	if strings.Contains(after, "remote Go role") {
		t.Errorf("a row in the chat list previews the topic's message:\n%s", after)
	}
	if after != before {
		t.Errorf("a message in a topic changed the chat list's rows.\nbefore:\n%s\nafter:\n%s",
			before, after)
	}
}

// And the withholding is from the chat list's store-backed path alone: the
// same message still has to reach the thread it is being read in and the
// topic's own row, or the fix would have bought its quiet by dropping the
// message.
func TestAMessageInTheOpenTopicStillReachesItsThreadAndItsRow(t *testing.T) {
	m := openJobsTopic(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))

	m = deliverAll(t, m, jobsArrival("remote Go role"))

	if got := ansi.Strip(m.chatView.View()); !strings.Contains(got, "remote Go role") {
		t.Errorf("the open topic's thread does not show the message that arrived:\n%s", got)
	}
	if got := ansi.Strip(m.chatList.View()); !strings.Contains(got, "remote Go role") {
		t.Errorf("the Jobs row does not preview the message that arrived:\n%s", got)
	}
}

// notifyingForumModel is forumModel for an app that will actually notify.
// The default test config has notifications off, which would make every
// assertion below pass for the wrong reason — see notifyModel.
func notifyingForumModel(t *testing.T, f *fakeForums) Model {
	t.Helper()
	return forumChats(t, sized(onMainScreen(notifyModel(t), PanelChatList)), f)
}

// notifications is every notification sequence a run of messages produced,
// the held ones included.
//
// A notice for a chat this client cannot name is HELD rather than posted
// and goes out when the grace runs out (see notice.go), so the run ends by
// letting that backstop fire. A test that stopped at the last message would
// read a held notification as silence — which is exactly how a topic's
// notification hides, because no chat store entry ever names a topic.
func notifications(t *testing.T, m Model, msgs []tea.Msg) []string {
	t.Helper()

	var out []string
	for _, msg := range msgs {
		var cmd tea.Cmd
		m, cmd = deliver(t, m, msg)
		out = append(out, rawSequences(t, cmd)...)
	}
	_, cmd := deliver(t, m, noticeGraceMsg{})
	return append(out, rawSequences(t, cmd)...)
}

// The conversation on screen never notifies, and a topic is a conversation.
//
// The test the suppression used to make was against the chat list's active
// chat, which is set from chat ROWS only: while a topic is open it still
// held the forum, so the topic's copy of the message matched nothing and
// the very conversation being read rang.
func TestTheTopicOnScreenDoesNotNotify(t *testing.T) {
	m := openJobsTopic(t, notifyingForumModel(t, &fakeForums{topics: goSerbiaTopics()}))

	if got := notifications(t, m, jobsArrival("remote Go role")); len(got) != 0 {
		t.Errorf("the topic on screen produced %d notification(s): %q", len(got), got)
	}
}

// And a topic nobody is reading still rings — once, under the name of the
// forum it is in, which is the chat the reader actually has.
//
// The other half of the same fix: a message in a topic is published twice,
// so silencing the reader's own topic must not be done by silencing one of
// the two copies everywhere, and announcing the other must not be done
// twice.
func TestATopicNobodyIsReadingNotifiesOnceUnderItsForum(t *testing.T) {
	m := notifyingForumModel(t, &fakeForums{topics: goSerbiaTopics()})
	// In and straight back out: the listing is what tells the app these
	// synthetic IDs are topics, and the reader is elsewhere by the time the
	// message lands.
	m = leaveForum(t, enterForumByKey(t, m))
	m = openPlainChat(t, m)

	got := notifications(t, m, jobsArrival("remote Go role"))
	if len(got) != 1 {
		t.Fatalf("a topic nobody is reading produced %d notifications, want 1: %q", len(got), got)
	}
	if !strings.Contains(got[0], "Go Serbia") || !strings.Contains(got[0], "remote Go role") {
		t.Errorf("the notification says %q, want the forum's name and the message", got[0])
	}
}

// openPlainChat opens the ordinary chat through the chat list's own keys,
// which is what records it as the open one (see chatlist.Model.OpenCursor).
// Driven that way rather than by handing the app a ChatSelectedMsg, because
// the premise of the two tests below is that both panels agree about which
// chat is open.
func openPlainChat(t *testing.T, m Model) Model {
	t.Helper()
	m.setFocus(PanelChatList)
	m = update(t, m, "j") // off Go Serbia, onto Ana
	m = update(t, m, "l")

	if got := m.chatView.ChatId(); got != testPlainChatID {
		t.Fatalf("the thread shows %d, want Ana %d", got, testPlainChatID)
	}
	if got := m.chatList.ActiveChatId(); got != testPlainChatID {
		t.Fatalf("the chat list has %d open, want Ana %d", got, testPlainChatID)
	}
	return m
}

// An ordinary chat on screen is as silent as it ever was.
func TestTheOrdinaryChatOnScreenStillDoesNotNotify(t *testing.T) {
	m := openPlainChat(t, notifyingForumModel(t, &fakeForums{topics: goSerbiaTopics()}))

	_, cmd := deliver(t, m, incoming(testPlainChatID))
	if got := rawSequences(t, cmd); len(got) != 0 {
		t.Errorf("the chat on screen notified: %q", got)
	}
}

// And a chat nobody is reading still rings, or the test above passes by
// notifying nobody about anything.
func TestAChatNobodyIsReadingStillNotifies(t *testing.T) {
	m := openPlainChat(t, notifyingForumModel(t, &fakeForums{topics: goSerbiaTopics()}))

	_, cmd := deliver(t, m, incoming(testForumID))
	if got := rawSequences(t, cmd); len(got) != 1 {
		t.Fatalf("a chat nobody is reading produced %d notifications, want 1: %q", len(got), got)
	}
}
