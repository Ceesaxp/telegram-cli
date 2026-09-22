package store

import (
	"container/list"
	"sync"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// FileState tracks the download state of a file.
type FileState struct {
	File       *telegram.File
	LocalPath  string
	IsComplete bool
	Progress   float64 // 0.0 to 1.0
}

// fileStoreCapacity is how many download states the store remembers (#33).
//
// A state is written for every file a download finishes, which in a long
// session is a thumbnail for every photo scrolled past. The ones that matter
// are the ones on screen, and a render reads them, so they stay at the
// recent end. A state is a File and a path, a couple of hundred bytes.
//
// Losing one costs no transfer: an evicted state reads as a file not
// downloaded yet, the chat view asks for it again, and the download is
// answered from the cache on disk and puts the state back.
const fileStoreCapacity = 4096

// FileStore tracks file download states, keeping the fileStoreCapacity most
// recently used. A download still in progress is never evicted, so the
// store can stand above its capacity by those, and only while they run.
type FileStore struct {
	mu       sync.Mutex
	capacity int
	files    map[string]*list.Element // file key -> *keyedState
	recency  *list.List               // front is the most recently used
}

// keyedState is one state in the recency list, which has to know its key to
// take it out of the map when it falls off the end.
type keyedState struct {
	key   string
	state *FileState
}

func NewFileStore() *FileStore {
	return newFileStoreOf(fileStoreCapacity)
}

// newFileStoreOf is a store of another capacity, which is how a test gets
// one small enough to fill.
func newFileStoreOf(capacity int) *FileStore {
	return &FileStore{
		capacity: capacity,
		files:    make(map[string]*list.Element),
		recency:  list.New(),
	}
}

// Update processes a file update.
func (s *FileStore) Update(file *telegram.File) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := &FileState{
		File:      file,
		LocalPath: file.Path,
	}
	if file.Downloaded {
		state.IsComplete = true
		state.Progress = 1.0
	}

	if el, ok := s.files[file.ID]; ok {
		el.Value.(*keyedState).state = state
		s.recency.MoveToFront(el)
	} else {
		s.files[file.ID] = s.recency.PushFront(&keyedState{key: file.ID, state: state})
	}
	s.evict()
}

// lookup finds a file's state and counts it as used. The caller holds mu.
func (s *FileStore) lookup(fileID string) (*FileState, bool) {
	el, ok := s.files[fileID]
	if !ok {
		return nil, false
	}
	s.recency.MoveToFront(el)
	return el.Value.(*keyedState).state, true
}

// evict drops the least recently used states until the store fits its
// capacity, passing over downloads still in progress. The caller holds mu.
func (s *FileStore) evict() {
	for el := s.recency.Back(); el != nil && len(s.files) > s.capacity; {
		older := el
		el = el.Prev()
		item := older.Value.(*keyedState)
		if !item.state.IsComplete {
			continue
		}
		s.recency.Remove(older)
		delete(s.files, item.key)
	}
}

// Get returns the state of a file.
func (s *FileStore) Get(fileID string) (*FileState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookup(fileID)
}

// IsComplete checks if a file download is complete.
func (s *FileStore) IsComplete(fileID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.lookup(fileID)
	if !ok {
		return false
	}
	return state.IsComplete
}

// LocalPath returns the local path of a downloaded file.
func (s *FileStore) LocalPath(fileID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.lookup(fileID)
	if !ok {
		return ""
	}
	return state.LocalPath
}

// Progress returns the download progress of a file (0.0 to 1.0).
func (s *FileStore) Progress(fileID string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.lookup(fileID)
	if !ok {
		return 0
	}
	return state.Progress
}

// Store is the aggregate store holding all caches.
type Store struct {
	Chats    *ChatStore
	Messages *MessageStore
	Users    *UserStore
	Files    *FileStore
}

// NewStore creates a new aggregate store.
func NewStore() *Store {
	return &Store{
		Chats:    NewChatStore(),
		Messages: NewMessageStore(),
		Users:    NewUserStore(),
		Files:    NewFileStore(),
	}
}
