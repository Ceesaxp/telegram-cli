package telegram

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"

	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// fileServer stands in for the server a download talks to. It answers
// messages.getMessages with messages, channels.getChannels with channels
// and upload.getFile with payload, and counts what it was asked.
//
// When gate is set, upload.getFile announces itself on started and then
// waits for gate to close: a transfer held open for as long as a test needs
// one in flight.
type fileServer struct {
	messages []tg.MessageClass
	channels []tg.ChatClass
	payload  []byte

	started chan struct{}
	gate    chan struct{}

	mu        sync.Mutex
	refetched [][]tg.InputMessageClass
	transfers int
}

func (f *fileServer) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	switch req := input.(type) {
	case *tg.MessagesGetMessagesRequest:
		f.mu.Lock()
		f.refetched = append(f.refetched, req.ID)
		f.mu.Unlock()
		output.(*tg.MessagesMessagesBox).Messages = &tg.MessagesMessages{Messages: f.messages}
		return nil
	case *tg.ChannelsGetChannelsRequest:
		output.(*tg.MessagesChatsBox).Chats = &tg.MessagesChats{Chats: f.channels}
		return nil
	case *tg.UploadGetFileRequest:
		f.mu.Lock()
		f.transfers++
		f.mu.Unlock()
		if f.gate != nil {
			f.started <- struct{}{}
			select {
			case <-f.gate:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		output.(*tg.UploadFileBox).File = &tg.UploadFile{Type: &tg.StorageFileJpeg{}, Bytes: f.payload}
		return nil
	default:
		return fmt.Errorf("unexpected request %T", input)
	}
}

func (f *fileServer) refetches() [][]tg.InputMessageClass {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]tg.InputMessageClass(nil), f.refetched...)
}

func (f *fileServer) transferCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.transfers
}

// serverClient is a client that talks to srv, with a registry of the given
// capacity and a files_dir of its own.
func serverClient(t *testing.T, srv *fileServer, registry *fileRegistry) *Client {
	t.Helper()
	api := tg.NewClient(srv)
	return &Client{
		api:    api,
		peers:  peers.Options{}.Build(api),
		files:  registry,
		config: &config.Config{Storage: config.StorageConfig{FilesDir: t.TempDir()}},
	}
}

// documentMessage is message id in the private chat with user 5, carrying
// the document docID of size bytes.
func documentMessage(id int, docID int64, size int64) *tg.Message {
	msg := &tg.Message{ID: id, PeerID: &tg.PeerUser{UserID: 5}}
	msg.SetMedia(&tg.MessageMediaDocument{Document: &tg.Document{
		ID:            docID,
		AccessHash:    docID * 10,
		FileReference: []byte{1, 2, 3},
		Size:          size,
		MimeType:      "application/pdf",
	}})
	return msg
}

// The registry is bounded (#33), so the entry behind a key a reader is
// looking at can be gone by the time they press enter on it. The message
// that registered it is still there, and fetching it again registers it
// again — so the download goes ahead, where it used to report an unknown
// file.
func TestADownloadWhoseEntryIsGoneRefetchesItsMessage(t *testing.T) {
	payload := []byte("%PDF")
	srv := &fileServer{
		messages: []tg.MessageClass{documentMessage(42, 7, int64(len(payload)))},
		payload:  payload,
	}
	c := serverClient(t, srv, newFileRegistry())

	file, err := c.DownloadMessageFile(5, 42, "doc:7")
	if err != nil {
		t.Fatalf("DownloadMessageFile with the entry gone: %v", err)
	}

	if got := srv.refetches(); len(got) != 1 {
		t.Fatalf("the message was fetched %d times, want once", len(got))
	} else if id, ok := got[0][0].(*tg.InputMessageID); !ok || id.ID != 42 {
		t.Errorf("fetched %#v, want message 42", got[0])
	}
	if !file.Downloaded {
		t.Error("the file came back as not downloaded")
	}
	if data, err := os.ReadFile(file.Path); err != nil || !bytes.Equal(data, payload) {
		t.Errorf("the file on disk is %q (%v), want %q", data, err, payload)
	}
}

// A file whose entry is there needs no message: the refetch is the price of
// a miss, not of every download.
func TestADownloadWhoseEntryIsThereFetchesNoMessage(t *testing.T) {
	payload := []byte("%PDF")
	srv := &fileServer{payload: payload}
	c := serverClient(t, srv, newFileRegistry())
	c.contentFromMedia(documentMessage(42, 7, int64(len(payload))).Media, nil)

	if _, err := c.DownloadMessageFile(5, 42, "doc:7"); err != nil {
		t.Fatalf("DownloadMessageFile: %v", err)
	}
	if got := srv.refetches(); len(got) != 0 {
		t.Errorf("the message was fetched %d times, want none", len(got))
	}
}

// A message that no longer carries the file cannot bring it back, and says
// so rather than downloading something else.
func TestADownloadTheRefetchCannotRecoverFails(t *testing.T) {
	srv := &fileServer{messages: []tg.MessageClass{documentMessage(42, 8, 4)}}
	c := serverClient(t, srv, newFileRegistry())

	if _, err := c.DownloadMessageFile(5, 42, "doc:7"); err == nil {
		t.Fatal("a file the message no longer carries was downloaded")
	}
	if n := srv.transferCount(); n != 0 {
		t.Errorf("%d transfers started, want none", n)
	}
}

// A pending send has a negative ID and no server copy to fetch.
func TestADownloadForAPendingSendDoesNotAskTheServer(t *testing.T) {
	srv := &fileServer{}
	c := serverClient(t, srv, newFileRegistry())

	if _, err := c.DownloadMessageFile(5, -3, "doc:7"); err == nil {
		t.Fatal("an unknown file on a pending send was downloaded")
	}
	if got := srv.refetches(); len(got) != 0 {
		t.Errorf("asked the server for a pending send %d times", len(got))
	}
}

// An avatar's key names its chat, so an avatar needs no message to come
// back: asking for the chat again registers its current photo.
func TestAnAvatarWhoseEntryIsGoneIsRegisteredAgain(t *testing.T) {
	payload := []byte("jpeg")
	srv := &fileServer{
		channels: []tg.ChatClass{&tg.Channel{
			ID: 11, AccessHash: 110, Title: "c", Broadcast: true,
			Photo: &tg.ChatPhoto{PhotoID: 77},
		}},
		payload: payload,
	}
	c := serverClient(t, srv, newFileRegistry())

	file, err := c.DownloadFileSync(avatarKey(channelChatID(11)))
	if err != nil {
		t.Fatalf("DownloadFileSync on an avatar with its entry gone: %v", err)
	}
	if !file.Downloaded {
		t.Error("the avatar came back as not downloaded")
	}
	// The generation it was fetched under is the one the server has now.
	if !strings.Contains(file.Path, "_77_") {
		t.Errorf("path = %q, want it to carry photo 77", file.Path)
	}
	if n := srv.transferCount(); n != 1 {
		t.Errorf("%d transfers, want 1", n)
	}
}
