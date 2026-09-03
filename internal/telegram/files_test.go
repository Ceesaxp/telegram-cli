package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

func TestSanitizeDownloadFileName(t *testing.T) {
	tests := map[string]string{
		"photo:5211047124492479647:y.jpg": "photo_5211047124492479647_y.jpg",
		`question<1>:"draft"?.pdf`:        "question_1___draft__.pdf",
		"trailing. ":                      "trailing",
	}
	for input, want := range tests {
		if got := sanitizeDownloadFileName(input); got != want {
			t.Errorf("sanitizeDownloadFileName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFileRegistryDoCoalesces(t *testing.T) {
	r := newFileRegistry()
	var calls atomic.Int32
	release := make(chan struct{})
	fn := func() (any, error) {
		calls.Add(1)
		<-release
		return "ok", nil
	}

	const n = 2
	errc := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			v, err := r.do("k", fn)
			if err != nil {
				errc <- err
				return
			}
			if v != "ok" {
				errc <- fmt.Errorf("got %v", v)
				return
			}
			errc <- nil
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	for i := 0; i < n; i++ {
		if err := <-errc; err != nil {
			t.Fatalf("do: %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fn ran %d times, want 1", got)
	}
}

func TestFileRegistryReregistrationPreservesCompletedImmutableFile(t *testing.T) {
	r := newFileRegistry()
	path := filepath.Join(t.TempDir(), "document.bin")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	r.put("doc:7", &fileEntry{
		location: &tg.InputDocumentFileLocation{ID: 7, AccessHash: 1},
		size:     4,
		name:     "old.bin",
	})
	r.markDone("doc:7", path)
	file := r.put("doc:7", &fileEntry{
		location: &tg.InputDocumentFileLocation{ID: 7, AccessHash: 2},
		size:     4,
		name:     "refreshed.bin",
	})

	if !file.Downloaded || file.Path != path {
		t.Fatalf("refreshed File = %+v, want completed path %q", file, path)
	}
	snap, ok := r.snapshot("doc:7")
	if !ok || !snap.done || snap.path != path || snap.name != "refreshed.bin" {
		t.Fatalf("refreshed snapshot = %+v, ok=%v", snap, ok)
	}
	location, ok := snap.location.(*tg.InputDocumentFileLocation)
	if !ok || location.AccessHash != 2 {
		t.Fatalf("location = %#v, want refreshed access hash 2", snap.location)
	}
}

func TestFileRegistryDoesNotPreserveInvalidLocalState(t *testing.T) {
	for _, tc := range []struct {
		name      string
		oldSize   int64
		newSize   int64
		remove    bool
		empty     bool
		avatarOld *avatarRef
		avatarNew *avatarRef
	}{
		{name: "missing file", oldSize: 4, newSize: 4, remove: true},
		{name: "changed size", oldSize: 4, newSize: 5},
		{name: "empty file with unknown size", empty: true},
		{name: "mutable avatar", oldSize: 4, newSize: 4,
			avatarOld: &avatarRef{chatID: 1, photoID: 10},
			avatarNew: &avatarRef{chatID: 1, photoID: 11}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newFileRegistry()
			path := filepath.Join(t.TempDir(), "cached.bin")
			contents := []byte("data")
			if tc.empty {
				contents = nil
			}
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			old := &fileEntry{
				location: &tg.InputDocumentFileLocation{ID: 7},
				size:     tc.oldSize,
				avatar:   tc.avatarOld,
			}
			fresh := &fileEntry{
				location: &tg.InputDocumentFileLocation{ID: 7},
				size:     tc.newSize,
				avatar:   tc.avatarNew,
			}
			r.put("key", old)
			r.markDone("key", path)
			if tc.remove {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}

			file := r.put("key", fresh)
			if file.Downloaded || file.Path != "" {
				t.Fatalf("refreshed File retained invalid state: %+v", file)
			}
			snap, _ := r.snapshot("key")
			if snap.done || snap.path != "" {
				t.Fatalf("refreshed snapshot retained invalid state: %+v", snap)
			}
		})
	}
}

func TestDownloadFileSyncUnknownKey(t *testing.T) {
	c := &Client{files: newFileRegistry()}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.DownloadFileSync("missing")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	n := 0
	for err := range errs {
		n++
		if err == nil {
			t.Fatal("expected error for unknown key")
		}
	}
	if n != 2 {
		t.Fatalf("got %d results, want 2", n)
	}
}
