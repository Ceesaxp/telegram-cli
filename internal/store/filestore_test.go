package store

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// downloaded is the File a finished download reports.
func downloaded(id string) *telegram.File {
	return &telegram.File{ID: id, Path: "/cache/" + id, Size: 4, Downloaded: true}
}

// size is how many states the store holds.
func (s *FileStore) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.files)
}

// holds reports whether fileID has a state, without counting as a use of it.
func (s *FileStore) holds(fileID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.files[fileID]
	return ok
}

// The store used to keep the state of every file downloaded, for the life
// of the process (#33) — a thumbnail for every photo scrolled past.
func TestFileStoreStaysWithinItsCapacity(t *testing.T) {
	const capacity = 8
	s := newFileStoreOf(capacity)

	for i := 0; i < 100*capacity; i++ {
		s.Update(downloaded(fmt.Sprintf("photo:%d:m", i)))
		if n := s.size(); n > capacity {
			t.Fatalf("after %d downloads the store holds %d states, want at most %d",
				i+1, n, capacity)
		}
	}
}

// What goes is what was used longest ago, and a read is a use: the photo a
// render just asked about is on screen.
func TestFileStoreEvictsTheLeastRecentlyUsed(t *testing.T) {
	s := newFileStoreOf(3)
	for _, id := range []string{"a", "b", "c"} {
		s.Update(downloaded(id))
	}

	s.Get("a")
	s.Update(downloaded("d"))

	for id, want := range map[string]bool{"a": true, "b": false, "c": true, "d": true} {
		if got := s.holds(id); got != want {
			t.Errorf("holds(%q) = %v, want %v", id, got, want)
		}
	}
}

// A download still running keeps its state, however old: dropping it would
// make the file read as never asked for.
func TestFileStoreNeverEvictsADownloadInProgress(t *testing.T) {
	const capacity = 2
	s := newFileStoreOf(capacity)
	s.Update(&telegram.File{ID: "doc:1", Size: 1 << 20})

	for i := 0; i < 10*capacity; i++ {
		s.Update(downloaded(fmt.Sprintf("photo:%d:m", i)))
	}

	if !s.holds("doc:1") {
		t.Fatal("the state of a download in progress was evicted")
	}
	if n := s.size(); n > capacity+1 {
		t.Errorf("the store holds %d states, want at most %d", n, capacity+1)
	}

	// Once it finishes it is like any other, and the store trims back.
	s.Update(downloaded("doc:1"))
	for i := 0; i < capacity; i++ {
		s.Update(downloaded(fmt.Sprintf("doc:%d", 100+i)))
	}
	if n := s.size(); n > capacity {
		t.Errorf("with nothing in progress the store holds %d states, want at most %d", n, capacity)
	}
}

// An evicted state reads as a file not downloaded yet — not as a failure —
// which is what sends the chat view to download it again. That download is
// answered by the cache on disk without a transfer (the telegram package's
// TestAnEvictedDownloadIsServedFromTheCache), and its File puts the state
// back as it was.
func TestAnEvictedStateReadsAsNotDownloadedAndComesBack(t *testing.T) {
	s := newFileStoreOf(1)
	file := downloaded("photo:1:m")
	s.Update(file)
	s.Update(downloaded("photo:2:m"))

	if _, ok := s.Get(file.ID); ok {
		t.Fatal("the setup did not evict the state")
	}
	if s.IsComplete(file.ID) || s.LocalPath(file.ID) != "" || s.Progress(file.ID) != 0 {
		t.Errorf("an evicted state reads as complete=%v path=%q progress=%v, want a file not downloaded",
			s.IsComplete(file.ID), s.LocalPath(file.ID), s.Progress(file.ID))
	}

	s.Update(file)
	if !s.IsComplete(file.ID) || s.LocalPath(file.ID) != file.Path {
		t.Errorf("after the re-resolve the state is complete=%v path=%q, want %q",
			s.IsComplete(file.ID), s.LocalPath(file.ID), file.Path)
	}
}

// Downloads report from their own goroutines while the render reads, so
// the recency list is mutated from many at once.
func TestFileStoreIsSafeForConcurrentUse(t *testing.T) {
	const capacity = 8
	s := newFileStoreOf(capacity)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("photo:%d:m", (g*7+i)%32)
				s.Update(downloaded(id))
				s.Get(id)
				s.IsComplete(id)
				s.LocalPath(id)
				s.Progress(id)
			}
		}(g)
	}
	wg.Wait()

	if n := s.size(); n > capacity {
		t.Errorf("the store holds %d states, want at most %d", n, capacity)
	}
}
