package telegram

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
)

// The band's whole job is to be somewhere no Telegram peer can be.
//
// Every branch in this package that asks what kind of chat an ID names asks
// constant.TDLibPeerID, and several of them — ViewMessages, DeleteMessages,
// GetMessages, ReadMentions — pick a different RPC on the answer. If a
// synthetic ID answered yes to any of these, a topic would not fail: it
// would quietly take a real peer's code path and name a channel that does
// not exist.
//
// 1<<48 is above MaxTDLibUserID ((1<<40)-1), which is the top of the only
// positive band, and every other band is negative.
func TestASyntheticIDIsNoPeerTelegramKnows(t *testing.T) {
	if syntheticChatIDBase <= constant.MaxTDLibUserID {
		t.Fatalf("the band starts at %d, inside the user band which ends at %d",
			syntheticChatIDBase, int64(constant.MaxTDLibUserID))
	}

	c := &Client{}
	ids := map[string]int64{
		"the base itself":  syntheticChatIDBase,
		"a first topic":    c.topicChatID(channelChatID(9), 1),
		"a second topic":   c.topicChatID(channelChatID(9), 42),
		"another forum's":  c.topicChatID(channelChatID(11), 1),
		"far up the band":  syntheticChatIDBase + 1<<20,
		"the band's start": syntheticChatIDBase + 0,
	}
	for name, chatID := range ids {
		id := constant.TDLibPeerID(chatID)
		switch {
		case id.IsUser():
			t.Errorf("%s (%d) reads as a user", name, chatID)
		case id.IsChat():
			t.Errorf("%s (%d) reads as a basic group", name, chatID)
		case id.IsChannel():
			t.Errorf("%s (%d) reads as a channel", name, chatID)
		case id.IsMonoforum():
			t.Errorf("%s (%d) reads as a monoforum", name, chatID)
		}
		if !isSyntheticChatID(chatID) {
			t.Errorf("%s (%d) is not recognised as synthetic", name, chatID)
		}
	}
}

// A real chat ID is never mistaken for a topic, or opening a chat would
// look up a topic that was never allocated.
func TestARealChatIDIsNotSynthetic(t *testing.T) {
	for name, chatID := range map[string]int64{
		"a user":           7,
		"the largest user": constant.MaxTDLibUserID,
		"a basic group":    basicGroupChatID(5),
		"a channel":        channelChatID(9),
		"nothing":          0,
	} {
		if isSyntheticChatID(chatID) {
			t.Errorf("%s (%d) reads as a synthetic topic ID", name, chatID)
		}
	}
}

// The registry is a translation, not an allocation log: the same topic
// asked for twice is the same chat to the reader, and a chat whose ID
// changed between two asks would split its messages across two stores.
func TestTheSameTopicAlwaysGetsTheSameChatID(t *testing.T) {
	c := &Client{}
	forum := channelChatID(9)

	first := c.topicChatID(forum, 5)
	if again := c.topicChatID(forum, 5); again != first {
		t.Errorf("the same topic got %d and then %d", first, again)
	}

	if other := c.topicChatID(forum, 6); other == first {
		t.Errorf("two topics of one forum share the chat ID %d", other)
	}
	if elsewhere := c.topicChatID(channelChatID(11), 5); elsewhere == first {
		t.Errorf("topic 5 of two different forums share the chat ID %d", elsewhere)
	}
}

// And the translation goes back, which is the only reason to keep it: the
// client is handed a synthetic ID and has to name the forum and the topic
// on the wire.
func TestASyntheticIDSplitsBackIntoItsForumAndItsTopic(t *testing.T) {
	c := &Client{}
	forum := channelChatID(9)

	chatID := c.topicChatID(forum, 5)
	gotForum, gotTopic := c.splitTopic(chatID)

	if gotForum != forum || gotTopic != 5 {
		t.Errorf("split %d = (%d, %d), want (%d, 5)", chatID, gotForum, gotTopic, forum)
	}
}

// Every exported method will split its chat ID, and almost all of them are
// handed an ordinary one. Splitting has to be free of consequence there.
func TestAnOrdinaryChatIDSplitsToItself(t *testing.T) {
	c := &Client{}
	for name, chatID := range map[string]int64{
		"a user":        7,
		"a basic group": basicGroupChatID(5),
		"a channel":     channelChatID(9),
	} {
		gotChat, gotTopic := c.splitTopic(chatID)
		if gotChat != chatID || gotTopic != 0 {
			t.Errorf("%s: split %d = (%d, %d), want (%d, 0)",
				name, chatID, gotChat, gotTopic, chatID)
		}
	}
}

// Nothing persists the registry, so a synthetic ID from a previous session
// — or from a bug — names nothing at all. It must not be answered with a
// real chat: the caller would go on to read, mark or send somewhere the
// reader never asked for. It comes back unchanged instead, still synthetic,
// so the inputPeer guard refuses it.
func TestAnUnknownSyntheticIDIsNotAnsweredWithAChat(t *testing.T) {
	c := &Client{}
	stray := syntheticChatIDBase + 777

	gotChat, gotTopic := c.splitTopic(stray)

	if gotChat != stray {
		t.Errorf("split of an unallocated %d named chat %d", stray, gotChat)
	}
	if gotTopic != 0 {
		t.Errorf("split of an unallocated %d named topic %d", stray, gotTopic)
	}
	if !isSyntheticChatID(gotChat) {
		t.Errorf("split of an unallocated %d handed back a real chat ID %d", stray, gotChat)
	}
}

// The client is used from several goroutines at once — the update listener,
// whatever the UI is doing and every background fetch — so two of them can
// meet the same topic for the first time together. Run under -race this
// catches the map, and the assertion catches an allocation that raced: one
// topic, one ID, however many askers.
func TestTwoGoroutinesMeetingATopicAtOnceAgreeOnItsChatID(t *testing.T) {
	c := &Client{}
	forum := channelChatID(9)

	const askers = 50
	ids := make(chan int64, askers)
	var start, done sync.WaitGroup
	start.Add(1)
	done.Add(askers)
	for range askers {
		go func() {
			defer done.Done()
			start.Wait()
			ids <- c.topicChatID(forum, 5)
		}()
	}
	start.Done()
	done.Wait()
	close(ids)

	want := c.topicChatID(forum, 5)
	for id := range ids {
		if id != want {
			t.Fatalf("one topic got both %d and %d", id, want)
		}
	}
}

// A topic title is server-controlled text on its way to a terminal, exactly
// like a chat title, and goes through the same sanitizer. Without it a
// topic named with an OSC escape retitles the reader's terminal window when
// the topic list draws.
func TestATopicTitleCannotDriveTheTerminal(t *testing.T) {
	forum := channelChatID(9)
	topic := topicFromTG(forum, &tg.ForumTopic{
		ID:        7,
		Title:     "Jobs\x1b]0;pwned\x07",
		IconColor: 0x6FB9F0,
	})

	if strings.ContainsAny(topic.Title, "\x1b\x07") {
		t.Errorf("Title = %q, still carries an escape", topic.Title)
	}
	if want := "Jobs" + repl + "]0;pwned" + repl; topic.Title != want {
		t.Errorf("Title = %q, want %q", topic.Title, want)
	}
}

// The rest of the record is copied as it stands, which is worth a test
// only because the fields are numerous enough to mistype one for another.
func TestATopicKeepsWhatTelegramSaidAboutIt(t *testing.T) {
	forum := channelChatID(9)
	got := topicFromTG(forum, &tg.ForumTopic{
		My:                   true,
		Closed:               true,
		Pinned:               true,
		Hidden:               true,
		ID:                   1,
		Title:                "General",
		IconColor:            0xFFD67E,
		TopMessage:           412,
		ReadInboxMaxID:       400,
		UnreadCount:          12,
		UnreadMentionsCount:  2,
		UnreadReactionsCount: 3,
	})

	want := &Topic{
		ID:                   1,
		ChatID:               forum,
		Title:                "General",
		IconColor:            0xFFD67E,
		TopMessageID:         412,
		ReadInboxMaxID:       400,
		UnreadCount:          12,
		UnreadMentionsCount:  2,
		UnreadReactionsCount: 3,
		Closed:               true,
		Pinned:               true,
		Hidden:               true,
		My:                   true,
	}
	if *got != *want {
		t.Errorf("topicFromTG =\n%+v\nwant\n%+v", *got, *want)
	}
}

// wireWatcher answers nothing and remembers being asked. Any request that
// reaches it is a request that would have gone to Telegram.
type wireWatcher struct{ asked bool }

func (w *wireWatcher) Invoke(_ context.Context, _ bin.Encoder, _ bin.Decoder) error {
	w.asked = true
	return errors.New("no connection")
}

// A topic names no peer, so looking one up is a translation bug, and the
// peer lookup is where every RPC in this package gets the peer it names.
// Refusing there turns that bug into a sentence about topics instead of a
// request against an ID Telegram has never heard of.
func TestAPeerLookupForATopicIsRefused(t *testing.T) {
	w := &wireWatcher{}
	api := tg.NewClient(w)
	c := &Client{api: api, peers: peers.Options{}.Build(api)}
	topicChat := c.topicChatID(channelChatID(9), 5)

	_, err := c.inputPeer(context.Background(), topicChat)

	if err == nil {
		t.Fatal("inputPeer resolved a forum topic as though it were a peer")
	}
	if !strings.Contains(err.Error(), "forum topic") {
		t.Errorf("inputPeer said %q, which does not say the ID is a forum topic", err)
	}
	if w.asked {
		t.Error("a forum topic's chat ID reached the wire")
	}
}

// And an ID from the band that this session never handed out is refused the
// same way: it is the shape that is wrong, not the bookkeeping.
func TestAPeerLookupForAnUnknownSyntheticIDIsRefused(t *testing.T) {
	w := &wireWatcher{}
	api := tg.NewClient(w)
	c := &Client{api: api, peers: peers.Options{}.Build(api)}

	_, err := c.inputPeer(context.Background(), syntheticChatIDBase+777)

	if err == nil {
		t.Fatal("inputPeer resolved an unallocated synthetic ID as though it were a peer")
	}
	if !strings.Contains(err.Error(), "forum topic") {
		t.Errorf("inputPeer said %q, which does not say the ID is a forum topic", err)
	}
	if w.asked {
		t.Error("an unallocated synthetic chat ID reached the wire")
	}
}

// Whether a supergroup is a forum is the one fact that decides whether a
// chat row drills into topics or opens a thread, and it arrives on every
// tg.Channel this client ever converts. It was dropped everywhere.
//
// The table is the build paths, not the call sites: chatFromChannel is the
// only place a Chat is built from a channel (TestPeerChatsAreBuiltInOnePlace
// holds that), and the other two are the ways a channel reaches it — the
// entities map that search and update payloads carry, and the dialog list,
// which is also how a folder's chats are materialized.
func TestAForumSaysSoOnEveryPathAChatIsBuilt(t *testing.T) {
	paths := map[string]func(*Client, *tg.Channel) *Chat{
		"chatFromChannel": func(c *Client, ch *tg.Channel) *Chat {
			return c.chatFromChannel(ch)
		},
		"chatFromPeer, the entities path": func(c *Client, ch *tg.Channel) *Chat {
			chat, err := c.chatFromPeer(&tg.PeerChannel{ChannelID: ch.ID}, tg.Entities{
				Channels: map[int64]*tg.Channel{ch.ID: ch},
			})
			if err != nil {
				t.Fatalf("chatFromPeer: %v", err)
			}
			return chat
		},
		"chatsFromDialogParts, the dialog list": func(c *Client, ch *tg.Channel) *Chat {
			chats, _, _ := c.chatsFromDialogParts(
				[]tg.DialogClass{&tg.Dialog{Peer: &tg.PeerChannel{ChannelID: ch.ID}}},
				nil, nil,
				[]tg.ChatClass{ch},
			)
			if len(chats) != 1 {
				t.Fatalf("chatsFromDialogParts returned %d chats, want 1", len(chats))
			}
			return chats[0]
		},
	}

	for name, build := range paths {
		t.Run(name, func(t *testing.T) {
			c := &Client{files: newFileRegistry()}

			forum := build(c, &tg.Channel{ID: 9, Title: "Go Serbia", Forum: true})
			if !forum.IsForum {
				t.Errorf("a channel with Forum set built a chat with IsForum false")
			}

			// The other half, or the field could be hardcoded true and
			// every supergroup would offer a topic list it has not got.
			plain := build(c, &tg.Channel{ID: 11, Title: "Gophers"})
			if plain.IsForum {
				t.Errorf("a channel without Forum set built a chat with IsForum true")
			}
		})
	}
}
