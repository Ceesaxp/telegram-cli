package telegram

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

// The uploader itself needs a connection, so these drive the cache's state
// machine directly: the run function is a parameter for exactly that reason,
// and everything worth asserting about the cache is in what it does around
// that call rather than in the call.

// waitFor gives a background upload a bounded moment to reach a state. A
// bare sleep would either be flaky or slow, and this is neither.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// uploadedFile is a stand-in for the reference a real upload returns. Only
// its identity matters here — the point is that the cached one comes back
// rather than a second upload's.
func uploadedFile(id int64) tg.InputFileClass {
	return &tg.InputFile{ID: id}
}

// The send must use the upload that was already done, not start its own:
// that is the whole reason the upload starts at attach time.
func TestUploadCacheServesTheUploadStartedAtAttachTime(t *testing.T) {
	var cache uploadCache
	var calls atomic.Int64

	cache.start("/tmp/one.png", func(context.Context, string, uint64) (tg.InputFileClass, error) {
		calls.Add(1)
		return uploadedFile(7), nil
	})
	waitFor(t, "the upload to finish", func() bool { return calls.Load() == 1 })

	file, cached, err := cache.await(context.Background(), "/tmp/one.png")
	if !cached || err != nil {
		t.Fatalf("await = (%v, %v, %v), want the cached file", file, cached, err)
	}
	if got, ok := file.(*tg.InputFile); !ok || got.ID != 7 {
		t.Fatalf("await returned %#v, want the file the upload produced", file)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("the file was uploaded %d times, want once", n)
	}
}

// Enter can be pressed while the file is still going up — a large one on a
// slow link is the case this whole path exists for — and the send has to
// wait for that upload rather than start a competing one.
func TestUploadCacheAwaitWaitsForAnUploadStillInFlight(t *testing.T) {
	var cache uploadCache
	release := make(chan struct{})

	cache.start("/tmp/big.bin", func(context.Context, string, uint64) (tg.InputFileClass, error) {
		<-release
		return uploadedFile(9), nil
	})

	done := make(chan tg.InputFileClass, 1)
	go func() {
		file, _, _ := cache.await(context.Background(), "/tmp/big.bin")
		done <- file
	}()

	select {
	case file := <-done:
		t.Fatalf("await returned %#v before the upload finished", file)
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	select {
	case file := <-done:
		if got, ok := file.(*tg.InputFile); !ok || got.ID != 9 {
			t.Fatalf("await returned %#v, want the finished upload", file)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("await never returned after the upload finished")
	}
}

// A send that gives up must not sit on a stalled upload: the composer is
// waiting on its command, and the transfer timeout is ten minutes away.
func TestUploadCacheAwaitObeysTheSendsContext(t *testing.T) {
	var cache uploadCache
	uploadCtx := make(chan context.Context, 1)
	release := make(chan struct{})
	defer close(release)

	cache.start("/tmp/stalled.bin", func(ctx context.Context, _ string, _ uint64) (tg.InputFileClass, error) {
		uploadCtx <- ctx
		<-release
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	file, cached, err := cache.await(ctx, "/tmp/stalled.bin")
	if !cached {
		t.Fatal("await did not find the entry it was meant to be waiting on")
	}
	if file != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("await = (%v, %v), want the send's own cancellation", file, err)
	}
	// And the upload it abandoned is stopped rather than left running.
	select {
	case upload := <-uploadCtx:
		waitFor(t, "the abandoned upload to be cancelled", func() bool {
			return upload.Err() != nil
		})
	case <-time.After(2 * time.Second):
		t.Fatal("the upload never started")
	}
}

// A discarded attachment's spool file is deleted the moment it is dropped,
// so the upload reading it has to be stopped and forgotten.
func TestUploadCacheCancelStopsAndForgetsTheUpload(t *testing.T) {
	var cache uploadCache
	uploadCtx := make(chan context.Context, 1)

	cache.start("/tmp/dropped.png", func(ctx context.Context, _ string, _ uint64) (tg.InputFileClass, error) {
		uploadCtx <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	})
	upload := <-uploadCtx

	cache.cancel("/tmp/dropped.png")
	waitFor(t, "the cancelled upload to stop", func() bool { return upload.Err() != nil })

	if _, cached, _ := cache.await(context.Background(), "/tmp/dropped.png"); cached {
		t.Fatal("the cancelled entry is still in the cache")
	}
}

// An upload that failed must not be served to the send as an answer: the
// send falls back to uploading synchronously, and it can only do that if the
// cache reports the failure and drops the entry.
func TestUploadCacheReportsAFailureAndDropsTheEntry(t *testing.T) {
	var cache uploadCache
	boom := errors.New("connection reset")

	cache.start("/tmp/failed.png", func(context.Context, string, uint64) (tg.InputFileClass, error) {
		return nil, boom
	})

	file, cached, err := cache.await(context.Background(), "/tmp/failed.png")
	if !cached || file != nil || !errors.Is(err, boom) {
		t.Fatalf("await = (%v, %v, %v), want the upload's failure", file, cached, err)
	}
	// Dropped, so a retry reads the file again rather than being served the
	// same stale error forever.
	if _, cached, _ := cache.await(context.Background(), "/tmp/failed.png"); cached {
		t.Fatal("the failed entry survived the send that consumed it")
	}
}

// Attaching the same file twice, or revisiting a chat whose draft holds one,
// must not upload it twice.
func TestUploadCacheStartIsANoOpForAFileItAlreadyHas(t *testing.T) {
	var cache uploadCache
	starts := make(chan struct{}, 4)
	run := func(ctx context.Context, _ string, _ uint64) (tg.InputFileClass, error) {
		starts <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}

	cache.start("/tmp/one.png", run)
	<-starts
	cache.start("/tmp/one.png", run)
	cache.start("/tmp/one.png", run)

	select {
	case <-starts:
		t.Fatal("the same file was uploaded more than once")
	case <-time.After(20 * time.Millisecond):
	}
	cache.cancel("/tmp/one.png")
}

// Nothing to upload is not an upload: a blank path would otherwise take an
// entry keyed by "" and hold it against every other blank path.
func TestUploadCacheIgnoresAnEmptyPath(t *testing.T) {
	var cache uploadCache
	cache.start("", func(context.Context, string, uint64) (tg.InputFileClass, error) {
		t.Error("an empty path started an upload")
		return nil, nil
	})
	if _, cached, _ := cache.await(context.Background(), ""); cached {
		t.Fatal("an empty path took a cache entry")
	}
}

// Each attempt is numbered, and the numbers rise. Attaching a file,
// discarding it and attaching the same one again is two uploads under one
// path, and the consumer of their progress can only tell the abandoned one
// from the live one by which number it carries.
func TestUploadCacheNumbersEachAttempt(t *testing.T) {
	var cache uploadCache
	gens := make(chan uint64, 2)
	run := func(ctx context.Context, _ string, generation uint64) (tg.InputFileClass, error) {
		gens <- generation
		<-ctx.Done()
		return nil, ctx.Err()
	}

	cache.start("/tmp/same.png", run)
	first := <-gens
	cache.cancel("/tmp/same.png")

	cache.start("/tmp/same.png", run)
	second := <-gens
	cache.cancel("/tmp/same.png")

	if first == 0 {
		t.Error("the first attempt was numbered 0, which is indistinguishable from unnumbered")
	}
	if second <= first {
		t.Fatalf("attempts numbered %d then %d, want the later one higher", first, second)
	}
	// The synchronous upload a send falls back to is an attempt too, and
	// has to sort after both.
	if third := cache.nextGeneration(); third <= second {
		t.Fatalf("the fallback upload took %d, want it after %d", third, second)
	}
}
