package store

import (
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

// FileStore tracks file download states.
type FileStore struct {
	mu    sync.RWMutex
	files map[string]*FileState // file key -> state
}

func NewFileStore() *FileStore {
	return &FileStore{
		files: make(map[string]*FileState),
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

	s.files[file.ID] = state
}

// Get returns the state of a file.
func (s *FileStore) Get(fileID string) (*FileState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.files[fileID]
	return state, ok
}

// IsComplete checks if a file download is complete.
func (s *FileStore) IsComplete(fileID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.files[fileID]
	if !ok {
		return false
	}
	return state.IsComplete
}

// LocalPath returns the local path of a downloaded file.
func (s *FileStore) LocalPath(fileID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.files[fileID]
	if !ok {
		return ""
	}
	return state.LocalPath
}

// Progress returns the download progress of a file (0.0 to 1.0).
func (s *FileStore) Progress(fileID string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.files[fileID]
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
