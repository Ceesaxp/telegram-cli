package app

import (
	"strings"
	"testing"
	"time"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/charmbracelet/x/ansi"
)

// Reading a topic clears the topic's badge.
//
// A topic's unread count lives on the [telegram.Topic] the drilled-in list
// draws its rows from, not in the chat store, so none of the messages that
// clear a chat's badge touch it: nothing anywhere took a topic's count
// down, and a topic read to the end still showed everything that had ever
// arrived in it. The app is the only layer that knows a synthetic chat ID
// names a topic (see [Model.topicChats]), so the read mark is routed here
// even though the state it moves is the chat list's.

func TestReadingATopicClearsItsBadge(t *testing.T) {
	m := enterForum(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))
	m = deliverAll(t, m, jobsTopicCopy("remote Go role"))

	if got := ansi.Strip(m.chatList.View()); !strings.Contains(got, "[1]") {
		t.Fatalf("the Jobs row carries no badge for the message that arrived:\n%s", got)
	}

	// What reading the topic in the thread publishes, under the topic's own
	// synthetic chat ID (internal/telegram's ViewMessages).
	m, _ = deliver(t, m, telegram.ChatMarkedReadMsg{
		ChatId: testJobsChatID, MaxMessageId: 4200,
	})

	if got := ansi.Strip(m.chatList.View()); strings.Contains(got, "[1]") {
		t.Errorf("the Jobs row still carries its badge after the topic was read:\n%s", got)
	}
}

// And the pointer moves with it, or the next replay of the same message
// counts again: a reconnect re-delivers what it already delivered, and a
// badge that comes back after a read is a badge describing a conversation
// with nothing unread in it.
func TestAMessageReplayedAfterATopicWasReadDoesNotCountAgain(t *testing.T) {
	m := enterForum(t, forumModel(t, &fakeForums{topics: goSerbiaTopics()}))
	m = deliverAll(t, m, jobsTopicCopy("remote Go role"))
	m, _ = deliver(t, m, telegram.ChatMarkedReadMsg{
		ChatId: testJobsChatID, MaxMessageId: 4200,
	})

	m = deliverAll(t, m, jobsTopicCopy("remote Go role"))

	if got := ansi.Strip(m.chatList.View()); strings.Contains(got, "[1]") {
		t.Errorf("a replay of the message the reader had read raised the badge again:\n%s", got)
	}
}

// An ordinary chat's badge is the chat store's, and still clears the way it
// always has — the routing above must not have taken the message away from
// the panel that handles it.
func TestReadingAnOrdinaryChatStillClearsItsBadge(t *testing.T) {
	m := forumModel(t, &fakeForums{topics: goSerbiaTopics()})
	m, _ = deliver(t, m, telegram.ChatLastMessageMsg{
		ChatId: testPlainChatID,
		LastMessage: &telegram.Message{
			ID: 4200, ChatID: testPlainChatID, Date: int32(time.Now().Unix()),
			SenderID: &telegram.MessageSenderUser{UserID: 77},
			Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "hello"}},
		},
	})

	if got := ansi.Strip(m.chatList.View()); !strings.Contains(got, "[1]") {
		t.Fatalf("Ana's row carries no badge for the message that arrived:\n%s", got)
	}

	m, _ = deliver(t, m, telegram.ChatMarkedReadMsg{
		ChatId: testPlainChatID, MaxMessageId: 4200,
	})

	if got := ansi.Strip(m.chatList.View()); strings.Contains(got, "[1]") {
		t.Errorf("Ana's row still carries its badge after the chat was read:\n%s", got)
	}
}
