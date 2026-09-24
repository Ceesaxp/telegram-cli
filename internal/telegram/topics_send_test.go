package telegram

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// forumChannelID is the channel the shared fake server knows (see
// readHistoryInvoker), standing in here for a supergroup with topics. Its
// access hash resolves, so a send to it reaches the wire the way a real one
// would — which is the only way to see what the request carried.
const forumChannelID = 9

// forumInvoker stands in for the server for a send into a forum: it records
// every send, answers each with the server's own copy of the message, and
// refuses with refusal when one is set.
//
// The copy it answers with is filed under the CHANNEL, with the reply
// header the request asked for — exactly as Telegram answers, and the
// reason the echo's chat ID is worth a test: the message comes back
// belonging to the forum, not to the topic the reader is looking at.
type forumInvoker struct {
	sends []*tg.MessagesSendMessageRequest
	media []*tg.MessagesSendMediaRequest

	// refusal is what the server says no with, for the closed-topic case.
	refusal error

	// shortSent answers text sends with updateShortSentMessage, which is
	// what Telegram really answers for a send to a chat it knows the
	// client has: there is no message in it, so the client builds its own.
	shortSent bool

	mu       sync.Mutex
	uploaded bytes.Buffer
}

func (f *forumInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	switch req := input.(type) {
	case *tg.MessagesSendMessageRequest:
		f.sends = append(f.sends, req)
		if f.refusal != nil {
			return f.refusal
		}
		if f.shortSent {
			output.(*tg.UpdatesBox).Updates = &tg.UpdateShortSentMessage{ID: 200, Date: 1}
			return nil
		}
		output.(*tg.UpdatesBox).Updates = updatesWith(sentInForum(200, req.Message, req.ReplyTo))
		return nil
	case *tg.MessagesSendMediaRequest:
		f.media = append(f.media, req)
		if f.refusal != nil {
			return f.refusal
		}
		output.(*tg.UpdatesBox).Updates = updatesWith(sentInForum(201, req.Message, req.ReplyTo))
		return nil
	case *tg.UploadSaveFilePartRequest:
		f.mu.Lock()
		defer f.mu.Unlock()
		f.uploaded.Write(req.Bytes)
		output.(*tg.BoolBox).Bool = &tg.BoolTrue{}
		return nil
	default:
		return readHistoryInvoker{}.Invoke(ctx, input, output)
	}
}

// replyTo is the header of the one send the invoker took, whichever kind of
// send it was. Every test here makes exactly one.
func (f *forumInvoker) replyTo(t *testing.T) tg.InputReplyToClass {
	t.Helper()
	switch {
	case len(f.sends)+len(f.media) != 1:
		t.Fatalf("made %d sends and %d media sends, want exactly one send",
			len(f.sends), len(f.media))
		return nil
	case len(f.sends) == 1:
		return f.sends[0].ReplyTo
	default:
		return f.media[0].ReplyTo
	}
}

// peer is the peer the one send named.
func (f *forumInvoker) peer(t *testing.T) tg.InputPeerClass {
	t.Helper()
	switch {
	case len(f.sends)+len(f.media) != 1:
		t.Fatalf("made %d sends and %d media sends, want exactly one send",
			len(f.sends), len(f.media))
		return nil
	case len(f.sends) == 1:
		return f.sends[0].Peer
	default:
		return f.media[0].Peer
	}
}

// wireBytes is the one send, encoded exactly as it goes out. A chat ID that
// was never split would be in here somewhere.
func (f *forumInvoker) wireBytes(t *testing.T) []byte {
	t.Helper()
	var b bin.Buffer
	var err error
	switch {
	case len(f.sends)+len(f.media) != 1:
		t.Fatalf("made %d sends and %d media sends, want exactly one send",
			len(f.sends), len(f.media))
	case len(f.sends) == 1:
		err = f.sends[0].Encode(&b)
	default:
		err = f.media[0].Encode(&b)
	}
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}
	return b.Buf
}

// updatesWith is the updates result a send answers with, carrying one new
// channel message: the shape a supergroup send really comes back in.
func updatesWith(m *tg.Message) tg.UpdatesClass {
	return &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateNewChannelMessage{Message: m}}}
}

// sentInForum is the server's copy of a message sent with the given reply
// header: in the forum, outgoing, and carrying the forum reply header
// Telegram files every topic message under.
func sentInForum(id int, text string, sent tg.InputReplyToClass) *tg.Message {
	m := &tg.Message{
		ID:      id,
		PeerID:  &tg.PeerChannel{ChannelID: forumChannelID},
		Out:     true,
		Message: text,
	}
	if h, ok := sent.(*tg.InputReplyToMessage); ok {
		header := &tg.MessageReplyHeader{ForumTopic: true}
		header.SetReplyToMsgID(h.ReplyToMsgID)
		if top, ok := h.GetTopMsgID(); ok {
			header.SetReplyToTopID(top)
		}
		m.SetReplyTo(header)
	}
	return m
}

// forumClient is a client talking to a forumInvoker, with the messages it
// publishes captured: the echo a send announces is half of what topic
// sending has to get right.
func forumClient(t *testing.T) (*Client, *forumInvoker, *[]tea.Msg) {
	t.Helper()
	inv := &forumInvoker{}
	api := tg.NewClient(inv)
	c := &Client{
		api:    api,
		peers:  peers.Options{}.Build(api),
		config: &config.Config{},
		files:  newFileRegistry(),
	}
	var published []tea.Msg
	c.setMsgSink(func(m tea.Msg) { published = append(published, m) })
	return c, inv, &published
}

// topicSenders are the ways a message goes out. Every one of them ends at a
// reply header, so every one of them has to build it by the same rule.
var topicSenders = map[string]func(t *testing.T, c *Client, chatID, replyToMessageID, placeholderID int64) error{
	"a text message": func(_ *testing.T, c *Client, chatID, replyToMessageID, placeholderID int64) error {
		_, err := c.SendTextMessage(chatID, "hi", replyToMessageID, placeholderID)
		return err
	},
	"a file": func(t *testing.T, c *Client, chatID, replyToMessageID, placeholderID int64) error {
		_, err := c.SendFileMessage(chatID, uploaded(t, c), "", replyToMessageID, placeholderID)
		return err
	},
	"a photo": func(t *testing.T, c *Client, chatID, replyToMessageID, placeholderID int64) error {
		_, err := c.SendPhotoMessage(chatID, uploaded(t, c), "", replyToMessageID, placeholderID)
		return err
	},
	"an opened file": func(t *testing.T, c *Client, chatID, replyToMessageID, placeholderID int64) error {
		f, err := os.Open(uploaded(t, c))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		_, err = c.SendOpenedFileMessage(chatID, f, "", replyToMessageID, placeholderID)
		return err
	},
}

// A plain post into a topic is a reply to the topic's root message and
// nothing else: that is how Telegram files it under the topic, and a send
// without it lands in the forum's General for everyone to see.
func TestAPlainPostIntoATopicRepliesToTheTopic(t *testing.T) {
	for name, send := range topicSenders {
		t.Run(name, func(t *testing.T) {
			c, inv, _ := forumClient(t)
			topic := c.topicChatID(channelChatID(forumChannelID), 5)

			if err := send(t, c, topic, 0, 0); err != nil {
				t.Fatalf("send: %v", err)
			}

			assertReplyHeader(t, inv.replyTo(t), 5, 0)
		})
	}
}

// assertReplyHeader holds a send's reply header to what the rule says it
// must be: the message it replies to, and the topic it names on top, with 0
// meaning the field must be absent rather than zero.
func assertReplyHeader(t *testing.T, got tg.InputReplyToClass, wantReplyTo, wantTopMsgID int) {
	t.Helper()

	if wantReplyTo == 0 {
		if got != nil {
			t.Fatalf("reply header = %s, want none", describeReplyHeader(got))
		}
		return
	}
	header, ok := got.(*tg.InputReplyToMessage)
	if !ok {
		t.Fatalf("reply header = %s, want a reply to message %d", describeReplyHeader(got), wantReplyTo)
	}
	if header.ReplyToMsgID != wantReplyTo {
		t.Errorf("replied to message %d, want %d", header.ReplyToMsgID, wantReplyTo)
	}
	top, hasTop := header.GetTopMsgID()
	switch {
	case wantTopMsgID == 0 && hasTop:
		t.Errorf("named topic %d on top, want no top_msg_id at all", top)
	case wantTopMsgID != 0 && !hasTop:
		t.Errorf("named no topic on top, want topic %d", wantTopMsgID)
	case wantTopMsgID != 0 && top != wantTopMsgID:
		t.Errorf("named topic %d on top, want %d", top, wantTopMsgID)
	}
}

// describeReplyHeader renders a header for a failure message: a pointer
// says nothing, and the flagged top_msg_id does not print with %#v.
func describeReplyHeader(h tg.InputReplyToClass) string {
	header, ok := h.(*tg.InputReplyToMessage)
	if !ok {
		return fmt.Sprintf("%#v", h)
	}
	if top, ok := header.GetTopMsgID(); ok {
		return fmt.Sprintf("a reply to %d in topic %d", header.ReplyToMsgID, top)
	}
	return fmt.Sprintf("a reply to %d, with no topic", header.ReplyToMsgID)
}

// A reply to a message inside a topic names the message, and the topic on
// top of it. Without the topic a reply to a message that is deleted before
// the send finishes falls back to General, in front of the whole forum.
func TestAReplyInsideATopicNamesTheMessageAndTheTopic(t *testing.T) {
	for name, send := range topicSenders {
		t.Run(name, func(t *testing.T) {
			c, inv, _ := forumClient(t)
			topic := c.topicChatID(channelChatID(forumChannelID), 5)

			if err := send(t, c, topic, 42, 0); err != nil {
				t.Fatalf("send: %v", err)
			}

			assertReplyHeader(t, inv.replyTo(t), 42, 5)
		})
	}
}

// Replying to the topic's own root message is the plain post, not a reply
// to itself: gotd's note on top_msg_id rules out naming the topic twice.
func TestAReplyToATopicsRootIsAPlainPost(t *testing.T) {
	c, inv, _ := forumClient(t)
	topic := c.topicChatID(channelChatID(forumChannelID), 5)

	if _, err := c.SendTextMessage(topic, "hi", 5, 0); err != nil {
		t.Fatalf("SendTextMessage: %v", err)
	}

	assertReplyHeader(t, inv.replyTo(t), 5, 0)
}

// General is the topic named by not naming it: a plain send lands there,
// and a header saying so would be a reply to the forum's first message.
func TestAPlainPostIntoGeneralCarriesNoReplyHeader(t *testing.T) {
	for name, send := range topicSenders {
		t.Run(name, func(t *testing.T) {
			c, inv, _ := forumClient(t)
			general := c.topicChatID(channelChatID(forumChannelID), generalTopicID)

			if err := send(t, c, general, 0, 0); err != nil {
				t.Fatalf("send: %v", err)
			}

			assertReplyHeader(t, inv.replyTo(t), 0, 0)
		})
	}
}

// And a reply inside General is an ordinary reply, for the same reason.
func TestAReplyInsideGeneralIsAnOrdinaryReply(t *testing.T) {
	for name, send := range topicSenders {
		t.Run(name, func(t *testing.T) {
			c, inv, _ := forumClient(t)
			general := c.topicChatID(channelChatID(forumChannelID), generalTopicID)

			if err := send(t, c, general, 42, 0); err != nil {
				t.Fatalf("send: %v", err)
			}

			assertReplyHeader(t, inv.replyTo(t), 42, 0)
		})
	}
}

// Almost every chat is not a topic, and splitting a chat ID that names none
// has to leave the send exactly as it was: no header without a reply, and a
// bare reply with one.
func TestAnOrdinaryChatSendsNoTopic(t *testing.T) {
	for name, send := range topicSenders {
		t.Run(name, func(t *testing.T) {
			t.Run("with no reply", func(t *testing.T) {
				c, inv, _ := forumClient(t)

				if err := send(t, c, channelChatID(forumChannelID), 0, 0); err != nil {
					t.Fatalf("send: %v", err)
				}

				assertReplyHeader(t, inv.replyTo(t), 0, 0)
			})

			t.Run("with a reply", func(t *testing.T) {
				c, inv, _ := forumClient(t)

				if err := send(t, c, channelChatID(forumChannelID), 42, 0); err != nil {
					t.Fatalf("send: %v", err)
				}

				assertReplyHeader(t, inv.replyTo(t), 42, 0)
			})
		})
	}
}

// A topic is not a peer, and its chat ID names nothing Telegram has ever
// heard of. What goes on the wire is the forum's peer — and the synthetic
// ID appears nowhere in the request at all, not as a peer and not as a
// message ID.
func TestASendToATopicNamesTheForumAndNeverTheSyntheticID(t *testing.T) {
	for name, send := range topicSenders {
		t.Run(name, func(t *testing.T) {
			c, inv, _ := forumClient(t)
			topic := c.topicChatID(channelChatID(forumChannelID), 5)

			if err := send(t, c, topic, 42, 0); err != nil {
				t.Fatalf("send: %v", err)
			}

			peer, ok := inv.peer(t).(*tg.InputPeerChannel)
			if !ok {
				t.Fatalf("sent to %#v, want the forum's channel peer", inv.peer(t))
			}
			if peer.ChannelID != forumChannelID {
				t.Errorf("sent to channel %d, want the forum %d", peer.ChannelID, forumChannelID)
			}
			if containsInt64(inv.wireBytes(t), topic) {
				t.Errorf("the synthetic chat ID %d is somewhere in the request the "+
					"client encoded — a topic's ID must never reach the wire", topic)
			}
		})
	}
}

// And the search above finds what it is looking for when it IS there: a
// check that can never fail holds nothing.
func TestTheWireSearchFindsASyntheticIDThatIsThere(t *testing.T) {
	stray := syntheticChatIDBase + 3
	var b bin.Buffer
	req := &tg.MessagesSendMessageRequest{
		Peer:     &tg.InputPeerChannel{ChannelID: stray, AccessHash: 1},
		Message:  "hi",
		RandomID: 1,
	}
	if err := req.Encode(&b); err != nil {
		t.Fatalf("encoding the request: %v", err)
	}

	if !containsInt64(b.Buf, stray) {
		t.Errorf("a request naming the synthetic ID %d as its peer was searched and "+
			"came back clean", stray)
	}
}

// The echo the thread is waiting for lives in the topic's chat, and the
// server answers with a message belonging to the forum. Published as it
// came back, the placeholder would never resolve and the message would
// appear in the forum's flat stream instead of the topic being read.
func TestTheEchoOfASendIntoATopicBelongsToTheTopic(t *testing.T) {
	for name, send := range topicSenders {
		t.Run(name, func(t *testing.T) {
			c, _, published := forumClient(t)
			topic := c.topicChatID(channelChatID(forumChannelID), 5)

			if err := send(t, c, topic, 0, 77); err != nil {
				t.Fatalf("send: %v", err)
			}

			got := onlySendSucceeded(t, *published)
			if got.OldMessageId != 77 {
				t.Errorf("replaced placeholder %d, want 77", got.OldMessageId)
			}
			if got.Message.ChatID != topic {
				t.Errorf("the sent message belongs to chat %d, want the topic's %d "+
					"(the forum is %d)", got.Message.ChatID, topic, channelChatID(forumChannelID))
			}
			if got.Message.TopicID != 5 {
				t.Errorf("the sent message names topic %d, want 5", got.Message.TopicID)
			}
		})
	}
}

// The text send has a second path: when the server answers with
// updateShortSentMessage there is no message to rewrite, and the local copy
// is built here. It has to be built into the topic too.
func TestTheEchoBuiltFromAShortSentAnswerBelongsToTheTopic(t *testing.T) {
	c, inv, published := forumClient(t)
	inv.shortSent = true
	topic := c.topicChatID(channelChatID(forumChannelID), 5)

	if _, err := c.SendTextMessage(topic, "hi", 0, 77); err != nil {
		t.Fatalf("SendTextMessage: %v", err)
	}

	got := onlySendSucceeded(t, *published)
	if got.Message.ChatID != topic || got.Message.TopicID != 5 {
		t.Errorf("the local copy is in chat %d topic %d, want chat %d topic 5",
			got.Message.ChatID, got.Message.TopicID, topic)
	}
}

// And a send to an ordinary chat is published as the server wrote it: the
// rewrite is for topics, and a chat that is not one must come through
// untouched.
func TestTheEchoOfAnOrdinarySendIsTheServersOwn(t *testing.T) {
	c, _, published := forumClient(t)

	if _, err := c.SendTextMessage(channelChatID(forumChannelID), "hi", 0, 77); err != nil {
		t.Fatalf("SendTextMessage: %v", err)
	}

	got := onlySendSucceeded(t, *published)
	if got.Message.ChatID != channelChatID(forumChannelID) {
		t.Errorf("the sent message belongs to chat %d, want %d",
			got.Message.ChatID, channelChatID(forumChannelID))
	}
	if got.Message.TopicID != 0 {
		t.Errorf("the sent message names topic %d, want none", got.Message.TopicID)
	}
}

// A closed topic takes no messages, and the server says so in the only
// language it has. The reader sees this text as the send failure, so it has
// to be a sentence rather than an error code.
func TestATopicThatRefusesASendSaysSoInASentence(t *testing.T) {
	refusals := map[string]struct {
		code string
		want string
	}{
		"a closed topic":  {"TOPIC_CLOSED", "closed"},
		"a deleted topic": {"TOPIC_DELETED", "deleted"},
		"an unknown one":  {"TOPIC_ID_INVALID", "does not know that topic"},
	}
	for name, refusal := range refusals {
		for sender, send := range topicSenders {
			t.Run(name+", sending "+sender, func(t *testing.T) {
				c, inv, published := forumClient(t)
				inv.refusal = tgerr.New(400, refusal.code)
				topic := c.topicChatID(channelChatID(forumChannelID), 5)

				err := send(t, c, topic, 0, 77)

				if err == nil {
					t.Fatal("the send reported success for a message the server refused")
				}
				if !strings.Contains(err.Error(), refusal.want) {
					t.Errorf("the failure reads %q, which does not say %q", err, refusal.want)
				}
				if strings.Contains(err.Error(), refusal.code) {
					t.Errorf("the failure reads %q, which shows the raw %s rather than "+
						"a sentence", err, refusal.code)
				}
				if len(*published) != 0 {
					t.Errorf("a refused send published %#v", *published)
				}
			})
		}
	}
}

// onlySendSucceeded is the one success this send published, and a failure
// naming what was published instead if there is not exactly one.
func onlySendSucceeded(t *testing.T, published []tea.Msg) MessageSendSucceededMsg {
	t.Helper()
	if len(published) != 1 {
		t.Fatalf("published %#v, want one send success", published)
	}
	got, ok := published[0].(MessageSendSucceededMsg)
	if !ok {
		t.Fatalf("published %#v, want a send success", published[0])
	}
	if got.Message == nil {
		t.Fatal("published a send success with no message")
	}
	return got
}

// containsInt64 reports whether want is one of the values in the encoded
// request, wherever in it it hid.
//
// Searched only at four-byte boundaries, which is where TL puts every field
// — it pads strings and bytes to a multiple of four. An unaligned search
// finds the synthetic base in any two adjacent small IDs, because 1<<48 is
// six zero bytes, a 1 and a zero, and so is the tail of one int64 followed
// by the head of the next.
func containsInt64(encoded []byte, want int64) bool {
	var needle [8]byte
	binary.LittleEndian.PutUint64(needle[:], uint64(want))
	for at := 0; at+8 <= len(encoded); at += 4 {
		if bytes.Equal(encoded[at:at+8], needle[:]) {
			return true
		}
	}
	return false
}
