package telegram

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

// fileEntry is a registered downloadable file.
type fileEntry struct {
	location tg.InputFileLocationClass
	avatar   *avatarRef // set for peer photos (location built lazily)
	size     int64
	name     string
	path     string
	done     bool
}

// avatarRef keeps what is needed to build an InputPeerPhotoFileLocation.
type avatarRef struct {
	chatID  int64
	photoID int64
}

// fileRegistry maps string keys to downloadable tg file locations.
type fileRegistry struct {
	mu      sync.RWMutex
	entries map[string]*fileEntry
}

func newFileRegistry() *fileRegistry {
	return &fileRegistry{entries: make(map[string]*fileEntry)}
}

func (r *fileRegistry) put(key string, e *fileEntry) *File {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[key] = e
	return &File{ID: key, Size: e.size}
}

func (r *fileRegistry) get(key string) (*fileEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[key]
	return e, ok
}

func (r *fileRegistry) markDone(key, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[key]; ok {
		e.path = path
		e.done = true
	}
}

// registerDocument registers a tg document; key "doc:<id>".
func (c *Client) registerDocument(doc *tg.Document, fileName string) *File {
	key := fmt.Sprintf("doc:%d", doc.ID)
	if fileName == "" {
		fileName = key
	}
	return c.files.put(key, &fileEntry{
		location: &tg.InputDocumentFileLocation{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
		},
		size: doc.Size,
		name: fileName,
	})
}

// registerDocumentThumb registers a document thumbnail, if any;
// key "doc:<id>:thumb".
func (c *Client) registerDocumentThumb(doc *tg.Document) *File {
	for _, s := range doc.Thumbs {
		var typ string
		switch sz := s.(type) {
		case *tg.PhotoSize:
			typ = sz.Type
		case *tg.PhotoSizeProgressive:
			typ = sz.Type
		default:
			continue
		}
		key := fmt.Sprintf("doc:%d:thumb", doc.ID)
		return c.files.put(key, &fileEntry{
			location: &tg.InputDocumentFileLocation{
				ID:            doc.ID,
				AccessHash:    doc.AccessHash,
				FileReference: doc.FileReference,
				ThumbSize:     typ,
			},
			name: key + ".jpg",
		})
	}
	return nil
}

// registerPhotoSize registers one size of a tg photo; key "photo:<id>:<type>".
func (c *Client) registerPhotoSize(p *tg.Photo, thumbType string, size int64) *File {
	key := fmt.Sprintf("photo:%d:%s", p.ID, thumbType)
	return c.files.put(key, &fileEntry{
		location: &tg.InputPhotoFileLocation{
			ID:            p.ID,
			AccessHash:    p.AccessHash,
			FileReference: p.FileReference,
			ThumbSize:     thumbType,
		},
		size: size,
		name: key + ".jpg",
	})
}

// registerAvatar registers a peer avatar; key "avatar:<chatID>".
func (c *Client) registerAvatar(chatID, photoID int64) *File {
	key := fmt.Sprintf("avatar:%d", chatID)
	return c.files.put(key, &fileEntry{
		avatar: &avatarRef{chatID: chatID, photoID: photoID},
		name:   strings.ReplaceAll(key, ":", "_") + ".jpg",
	})
}

// DownloadFileSync downloads a registered file to the files dir
// and returns its local state.
func (c *Client) DownloadFileSync(key string) (*File, error) {
	entry, ok := c.files.get(key)
	if !ok {
		return nil, fmt.Errorf("unknown file %q", key)
	}

	if entry.done && entry.path != "" {
		if _, err := os.Stat(entry.path); err == nil {
			return &File{ID: key, Path: entry.path, Size: entry.size, Downloaded: true}, nil
		}
	}

	location := entry.location
	if location == nil && entry.avatar != nil {
		peer, err := c.inputPeer(context.Background(), entry.avatar.chatID)
		if err != nil {
			return nil, fmt.Errorf("download %s: %w", key, err)
		}
		location = &tg.InputPeerPhotoFileLocation{
			Peer:    peer,
			PhotoID: entry.avatar.photoID,
		}
	}
	if location == nil {
		return nil, fmt.Errorf("file %q has no location", key)
	}

	name := fmt.Sprintf("%s_%s",
		sanitizeDownloadFileName(key),
		sanitizeDownloadFileName(entry.name))
	path := filepath.Join(c.config.Storage.FilesDir, name)

	ctx := context.Background()
	if _, err := downloader.NewDownloader().Download(c.api, location).ToPath(ctx, path); err != nil {
		return nil, fmt.Errorf("download %s: %w", key, err)
	}

	c.files.markDone(key, path)
	file := &File{ID: key, Path: path, Size: entry.size, Downloaded: true}
	c.send(FileUpdateMsg{File: file})
	return file, nil
}

// sanitizeDownloadFileName makes Telegram-provided names safe on Windows too.
// Photo registry names contain ':' by design, and document names are remote
// input, so both halves of the generated local name must be sanitized.
func sanitizeDownloadFileName(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, " .")
	if name == "" {
		return "download"
	}
	return name
}

// inputPeer resolves a canonical chat ID to a tg InputPeer
// via the peers manager (handles access hashes).
func (c *Client) inputPeer(ctx context.Context, chatID int64) (tg.InputPeerClass, error) {
	peer, err := c.peers.ResolveTDLibID(ctx, constant.TDLibPeerID(chatID))
	if err != nil {
		return nil, fmt.Errorf("resolve peer %d: %w", chatID, err)
	}
	return peer.InputPeer(), nil
}
