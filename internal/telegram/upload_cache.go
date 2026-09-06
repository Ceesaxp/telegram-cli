package telegram

import (
	"context"
	"sync"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// uploadFunc is the work an entry does: put path on Telegram's servers and
// hand back the reference a media send needs. It is a parameter rather than
// a field on the Client so the cache's state machine can be exercised
// without a connection — the only part of it that needs the network is this
// one function.
type uploadFunc func(ctx context.Context, path string) (tg.InputFileClass, error)

// uploadEntry is one file being put on Telegram's servers ahead of the send
// that will use it. file and err are written once, before done closes, so a
// reader that has seen the close sees both.
type uploadEntry struct {
	done   chan struct{}
	cancel context.CancelFunc
	file   tg.InputFileClass
	err    error
}

// uploadCache holds the uploads started when a file was attached, keyed by
// the path the composer is showing.
//
// The point is where the waiting happens. Uploading only after Enter meant
// the whole file went up between the keypress and anything appearing in the
// thread — the longest silence in the client, and one spent on a file the
// user chose seconds earlier and has done nothing with since. Starting at
// attach time spends it while they type the caption.
//
// The zero value is ready to use: clients built field-by-field (the tests
// do) get a working cache without a constructor to remember.
type uploadCache struct {
	mu      sync.Mutex
	entries map[string]*uploadEntry
}

// start begins uploading path in the background. An upload already in
// flight, or already finished and not yet consumed, is left alone: the
// entry is keyed by path and there is nothing a second one could add.
func (u *uploadCache) start(path string, run uploadFunc) {
	if path == "" || run == nil {
		return
	}

	u.mu.Lock()
	if u.entries == nil {
		u.entries = make(map[string]*uploadEntry)
	}
	if _, exists := u.entries[path]; exists {
		u.mu.Unlock()
		return
	}
	// The upload's own context, not the send's: it starts long before any
	// send exists, and the transfer timeout is what bounds it.
	ctx, cancel := transferCtx()
	entry := &uploadEntry{done: make(chan struct{}), cancel: cancel}
	u.entries[path] = entry
	u.mu.Unlock()

	go func() {
		entry.file, entry.err = run(ctx, path)
		close(entry.done)
	}()
}

// await takes path's entry and waits for it to finish, reporting whether
// there was one at all.
//
// The entry is dropped either way. On success the spool file behind it is
// deleted, and after a failure a retry has to read the file again rather
// than be served the same stale error forever.
func (u *uploadCache) await(ctx context.Context, path string) (tg.InputFileClass, bool, error) {
	entry := u.take(path)
	if entry == nil {
		return nil, false, nil
	}
	// Releases the transfer timeout on the way out, and stops the upload
	// when it is the send that gave up first.
	defer entry.cancel()

	select {
	case <-entry.done:
		return entry.file, true, entry.err
	case <-ctx.Done():
		return nil, true, ctx.Err()
	}
}

// cancel stops path's upload and drops the entry. Every path by which an
// attachment is discarded has to reach this: the goroutine is reading a
// spool file the caller is about to delete.
func (u *uploadCache) cancel(path string) {
	if entry := u.take(path); entry != nil {
		entry.cancel()
	}
}

// take removes path's entry and returns it, or nil when there is none.
func (u *uploadCache) take(path string) *uploadEntry {
	u.mu.Lock()
	defer u.mu.Unlock()
	entry, ok := u.entries[path]
	if !ok {
		return nil
	}
	delete(u.entries, path)
	return entry
}

// StartUpload begins uploading path so that a send of it later has nothing
// left to wait for. Calling it for a file that is already uploading, or is
// uploaded and unsent, does nothing.
func (c *Client) StartUpload(path string) {
	c.uploads.start(path, c.uploadFile)
}

// CancelUpload stops an eager upload and forgets it, for an attachment that
// was discarded before it was sent.
func (c *Client) CancelUpload(path string) {
	c.uploads.cancel(path)
}

// uploadFile puts path on Telegram's servers, reporting progress as it goes.
//
// gotd defaults to 1 upload thread and 128 KiB parts, which leaves both the
// network and Telegram's part-based protocol underused. 512 KiB is the
// largest part size Telegram accepts, and uploading with several threads
// lets those parts go out in parallel.
func (c *Client) uploadFile(ctx context.Context, path string) (tg.InputFileClass, error) {
	file, err := uploader.NewUploader(c.api).
		WithPartSize(512*1024).
		WithThreads(4).
		WithProgress(&uploadProgress{client: c, path: path, last: -1}).
		FromPath(ctx, path)
	if err != nil {
		// Say so, or the composer keeps showing a percentage that will
		// never advance. The send that follows re-uploads synchronously
		// and reports its own failure, so this is only about the chip.
		c.send(UploadProgressMsg{Path: path, Failed: true})
	}
	return file, err
}

// uploadProgress turns gotd's per-part callbacks into UI messages.
//
// One message per whole percent: the callback fires once per confirmed part,
// which for a 512 KiB part size is hundreds of times on a large file, and
// every one of them would otherwise be a full redraw of the frame. The parts
// are confirmed by several upload threads at once, hence the lock.
type uploadProgress struct {
	client *Client
	path   string

	mu   sync.Mutex
	last int // the last percent reported; -1 before the first part
}

func (p *uploadProgress) Chunk(_ context.Context, state uploader.ProgressState) error {
	// A stream upload has no total (it arrives as -1), and there is no
	// honest percentage to show for one.
	if state.Total <= 0 {
		return nil
	}
	percent := int(state.Uploaded * 100 / state.Total)

	p.mu.Lock()
	if percent == p.last {
		p.mu.Unlock()
		return nil
	}
	p.last = percent
	p.mu.Unlock()

	p.client.send(UploadProgressMsg{
		Path:     p.path,
		Uploaded: state.Uploaded,
		Total:    state.Total,
	})
	return nil
}
