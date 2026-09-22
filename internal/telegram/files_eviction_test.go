package telegram

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"

	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// fileServer stands in for the server a download talks to. It answers
// messages.getMessages with messages and upload.getFile with payload, and
// counts what it was asked.
//
// When gate is set, upload.getFile announces itself on started and then
// waits for gate to close: a transfer held open for as long as a test needs
// one in flight.
type fileServer struct {
	messages []tg.MessageClass
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
// so rather than downloading something else — or than calling it an unknown
// file, which reads as a bug in the client when the message was deleted.
func TestADownloadTheRefetchCannotRecoverSaysWhy(t *testing.T) {
	for name, answer := range map[string]tg.MessageClass{
		// Deleted on the server: messages.getMessages answers for the ID
		// with messageEmpty.
		"the message was deleted": &tg.MessageEmpty{ID: 42},
		// Edited to carry another file.
		"the message carries another file": documentMessage(42, 8, 4),
	} {
		t.Run(name, func(t *testing.T) {
			srv := &fileServer{messages: []tg.MessageClass{answer}}
			c := serverClient(t, srv, newFileRegistry())

			_, err := c.DownloadMessageFile(5, 42, "doc:7")
			if err == nil {
				t.Fatal("a file the message no longer carries was downloaded")
			}
			if !strings.Contains(err.Error(), "no longer carries this file") {
				t.Errorf("error = %q, want it to say the message no longer carries the file", err)
			}
			if n := srv.transferCount(); n != 0 {
				t.Errorf("%d transfers started, want none", n)
			}
		})
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

// size is how many entries the registry holds.
func (r *fileRegistry) size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

// holds reports whether key is registered, without counting as a use of it.
func (r *fileRegistry) holds(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.entries[key]
	return ok
}

// The registry used to keep every file the client was ever told about, for
// the life of the process (#33): every photo size, document and thumbnail of
// every message converted, and the avatar of every chat and user seen.
func TestFileRegistryStaysWithinItsCapacity(t *testing.T) {
	const capacity = 8
	r := newFileRegistryOf(capacity)

	for i := 0; i < 100*capacity; i++ {
		r.put(fmt.Sprintf("doc:%d", i), &fileEntry{name: "f"})
		if n := r.size(); n > capacity {
			t.Fatalf("after %d registrations the registry holds %d entries, want at most %d",
				i+1, n, capacity)
		}
	}
}

// What goes is what was used longest ago, and a lookup is a use: the photo
// a reader is looking at is the one about to be opened.
func TestFileRegistryEvictsTheLeastRecentlyUsed(t *testing.T) {
	r := newFileRegistryOf(3)
	for _, key := range []string{"a", "b", "c"} {
		r.put(key, &fileEntry{name: key})
	}

	r.snapshot("a")
	r.put("d", &fileEntry{name: "d"})

	for key, want := range map[string]bool{"a": true, "b": false, "c": true, "d": true} {
		if got := r.holds(key); got != want {
			t.Errorf("holds(%q) = %v, want %v", key, got, want)
		}
	}
}

// A transfer in flight keeps its entry. Evicting it mid-download would drop
// the done mark the download is about to set, and the next open would go
// round again for a file already on disk.
func TestATransferInFlightIsNeverEvicted(t *testing.T) {
	const capacity = 2
	payload := []byte("jpeg")
	srv := &fileServer{
		payload: payload,
		started: make(chan struct{}, 1),
		gate:    make(chan struct{}),
	}
	c := serverClient(t, srv, newFileRegistryOf(capacity))
	c.files.put("doc:1", &fileEntry{
		location: &tg.InputDocumentFileLocation{ID: 1},
		size:     int64(len(payload)),
		name:     "a.bin",
	})

	done := make(chan error, 1)
	go func() {
		_, err := c.DownloadFileSync("doc:1")
		done <- err
	}()
	select {
	case <-srv.started:
	case err := <-done:
		t.Fatalf("the download ended before it reached the server: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the download never reached the server")
	}

	for i := 2; i < 2+10*capacity; i++ {
		c.files.put(fmt.Sprintf("doc:%d", i), &fileEntry{name: "f"})
	}
	if !c.files.holds("doc:1") {
		t.Fatal("the entry of a transfer in flight was evicted")
	}
	// A transfer in flight may hold the registry past its capacity, and
	// only by itself.
	if n := c.files.size(); n > capacity+1 {
		t.Errorf("mid-transfer the registry holds %d entries, want at most %d", n, capacity+1)
	}

	close(srv.gate)
	if err := <-done; err != nil {
		t.Fatalf("DownloadFileSync: %v", err)
	}
	if snap, ok := c.files.snapshot("doc:1"); !ok || !snap.done {
		t.Errorf("the finished download did not land: registered=%v done=%v", ok, snap.done)
	}
	if n := c.files.size(); n > capacity {
		t.Errorf("after the transfer the registry holds %d entries, want at most %d", n, capacity)
	}
}

// pushOut registers more files than the registry holds, so that everything
// registered before is gone.
func pushOut(r *fileRegistry) {
	for i := 0; i <= r.capacity; i++ {
		r.put(fmt.Sprintf("doc:%d", 1_000_000+i), &fileEntry{name: "f"})
	}
}

// The whole of it: a message on screen, its file pushed out of the registry
// by everything converted since, and the reader opening it anyway.
func TestAnEvictedFileOfAMessageStillShownDownloads(t *testing.T) {
	payload := []byte("%PDF")
	shown := documentMessage(42, 7, int64(len(payload)))
	srv := &fileServer{messages: []tg.MessageClass{shown}, payload: payload}
	c := serverClient(t, srv, newFileRegistryOf(4))

	c.messageClassFromTG(shown)
	pushOut(c.files)
	if c.files.holds("doc:7") {
		t.Fatal("the setup did not evict the file")
	}

	file, err := c.DownloadMessageFile(5, 42, "doc:7")
	if err != nil {
		t.Fatalf("DownloadMessageFile after eviction: %v", err)
	}
	if !file.Downloaded {
		t.Error("the file came back as not downloaded")
	}
	if got := len(srv.refetches()); got != 1 {
		t.Errorf("the message was fetched %d times, want once", got)
	}
}

// An evicted file that was downloaded already is not downloaded again: the
// refetch registers it, and the cache on disk answers for it.
func TestAnEvictedDownloadIsServedFromTheCache(t *testing.T) {
	payload := []byte("%PDF")
	shown := documentMessage(42, 7, int64(len(payload)))
	srv := &fileServer{messages: []tg.MessageClass{shown}, payload: payload}
	c := serverClient(t, srv, newFileRegistryOf(4))

	c.messageClassFromTG(shown)
	first, err := c.DownloadMessageFile(5, 42, "doc:7")
	if err != nil {
		t.Fatalf("first download: %v", err)
	}
	pushOut(c.files)

	again, err := c.DownloadMessageFile(5, 42, "doc:7")
	if err != nil {
		t.Fatalf("download after eviction: %v", err)
	}
	if again.Path != first.Path {
		t.Errorf("path = %q, want the cached %q", again.Path, first.Path)
	}
	if n := srv.transferCount(); n != 1 {
		t.Errorf("%d transfers, want the first one only", n)
	}
}

// The acceptance test #33 asks for: far more unique media than the registry
// holds, converted the way messages are, leaves it at its documented bound.
func TestConvertingFarMoreMediaThanFitsStaysBounded(t *testing.T) {
	c := &Client{files: newFileRegistry()}

	for i := int64(1); i <= fileRegistryCapacity; i++ {
		c.photoFromTG(&tg.Photo{ID: i, AccessHash: i, Sizes: []tg.PhotoSizeClass{
			&tg.PhotoSize{Type: "s", W: 90, H: 90, Size: 1000},
			&tg.PhotoSize{Type: "m", W: 320, H: 320, Size: 10000},
			&tg.PhotoSize{Type: "x", W: 800, H: 800, Size: 100000},
		}})
		c.contentFromDocument(&tg.Document{ID: i, AccessHash: i, Size: 5, Thumbs: []tg.PhotoSizeClass{
			&tg.PhotoSize{Type: "m", W: 320, H: 320},
		}}, nil)
	}

	// Five entries a round, so five times the capacity went in.
	if n := c.files.size(); n > fileRegistryCapacity {
		t.Errorf("the registry holds %d entries, want at most %d", n, fileRegistryCapacity)
	}
}

// Registration, lookup and download from many goroutines at once, for the
// race detector: the recency list is mutated by every one of them.
func TestTheRegistryIsSafeForConcurrentUse(t *testing.T) {
	const capacity = 8
	srv := &fileServer{payload: []byte("data")}
	c := serverClient(t, srv, newFileRegistryOf(capacity))

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("doc:%d", (g*7+i)%32)
				c.files.put(key, &fileEntry{
					location: &tg.InputDocumentFileLocation{ID: int64(i)},
					size:     4,
					name:     "f.bin",
				})
				c.files.snapshot(key)
				if i%10 == 0 {
					// The entry may be gone by now; an unknown file is
					// an answer, a race is not.
					_, _ = c.DownloadFileSync(key)
				}
			}
		}(g)
	}
	wg.Wait()

	if n := c.files.size(); n > capacity {
		t.Errorf("with nothing in flight the registry holds %d entries, want at most %d", n, capacity)
	}
	c.files.mu.Lock()
	defer c.files.mu.Unlock()
	if len(c.files.inflight) != 0 {
		t.Errorf("keys still held after every download returned: %v", c.files.inflight)
	}
}
