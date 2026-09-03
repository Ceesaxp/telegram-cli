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
	"golang.org/x/sync/singleflight"
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
	sf      singleflight.Group
}

func newFileRegistry() *fileRegistry {
	return &fileRegistry{entries: make(map[string]*fileEntry)}
}

func (r *fileRegistry) do(key string, fn func() (any, error)) (any, error) {
	v, err, _ := r.sf.Do(key, fn)
	return v, err
}

// fileSnap is a copy of the fields DownloadFileSync needs so the registry
// lock is not held across the network download.
type fileSnap struct {
	location tg.InputFileLocationClass
	avatar   *avatarRef
	size     int64
	name     string
	path     string
	done     bool
}

func (r *fileRegistry) snapshot(key string) (fileSnap, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[key]
	if !ok {
		return fileSnap{}, false
	}
	return fileSnap{
		location: e.location,
		avatar:   e.avatar,
		size:     e.size,
		name:     e.name,
		path:     e.path,
		done:     e.done,
	}, true
}

func (r *fileRegistry) put(key string, e *fileEntry) *File {
	r.mu.Lock()
	defer r.mu.Unlock()
	if previous, ok := r.entries[key]; ok && reusableLocalFile(previous, e) {
		e.path = previous.path
		e.done = true
	}
	r.entries[key] = e
	return fileFromEntry(key, e)
}

// reusableLocalFile reports whether a completed immutable media download can
// survive a metadata refresh. Avatar keys are intentionally excluded: their
// stable chat-based key can point at a different photo generation (#34).
func reusableLocalFile(previous, refreshed *fileEntry) bool {
	if previous == nil || refreshed == nil || previous.avatar != nil || refreshed.avatar != nil ||
		!previous.done || previous.path == "" {
		return false
	}
	if !sameImmutableMedia(previous.location, refreshed.location) {
		return false
	}
	if previous.size > 0 && refreshed.size > 0 && previous.size != refreshed.size {
		return false
	}
	info, err := os.Stat(previous.path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	expectedSize := refreshed.size
	if expectedSize == 0 {
		expectedSize = previous.size
	}
	if expectedSize == 0 {
		return info.Size() > 0
	}
	return info.Size() == expectedSize
}

func sameImmutableMedia(previous, refreshed tg.InputFileLocationClass) bool {
	switch old := previous.(type) {
	case *tg.InputDocumentFileLocation:
		current, ok := refreshed.(*tg.InputDocumentFileLocation)
		return ok && old.ID == current.ID && old.ThumbSize == current.ThumbSize
	case *tg.InputPhotoFileLocation:
		current, ok := refreshed.(*tg.InputPhotoFileLocation)
		return ok && old.ID == current.ID && old.ThumbSize == current.ThumbSize
	default:
		return false
	}
}

func fileFromEntry(key string, e *fileEntry) *File {
	file := &File{ID: key, Size: e.size}
	if e.done && e.path != "" {
		file.Path = e.path
		file.Downloaded = true
	}
	return file
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
// and returns its local state. Concurrent calls for the same key share
// one in-flight download via the registry's singleflight group.
func (c *Client) DownloadFileSync(key string) (*File, error) {
	v, err := c.files.do(key, func() (any, error) {
		snap, ok := c.files.snapshot(key)
		if !ok {
			return nil, fmt.Errorf("unknown file %q", key)
		}

		if snap.done && snap.path != "" {
			if _, err := os.Stat(snap.path); err == nil {
				return &File{ID: key, Path: snap.path, Size: snap.size, Downloaded: true}, nil
			}
		}

		ctx, cancel := transferCtx()
		defer cancel()

		location := snap.location
		if location == nil && snap.avatar != nil {
			peer, err := c.inputPeer(ctx, snap.avatar.chatID)
			if err != nil {
				return nil, fmt.Errorf("download %s: %w", key, err)
			}
			location = &tg.InputPeerPhotoFileLocation{
				Peer:    peer,
				PhotoID: snap.avatar.photoID,
			}
		}
		if location == nil {
			return nil, fmt.Errorf("file %q has no location", key)
		}

		name := fmt.Sprintf("%s_%s",
			sanitizeDownloadFileName(key),
			sanitizeDownloadFileName(snap.name))
		path := filepath.Join(c.config.Storage.FilesDir, name)

		if _, err := downloader.NewDownloader().Download(c.api, location).ToPath(ctx, path); err != nil {
			return nil, fmt.Errorf("download %s: %w", key, err)
		}

		c.files.markDone(key, path)
		file := &File{ID: key, Path: path, Size: snap.size, Downloaded: true}
		c.send(FileUpdateMsg{File: file})
		return file, nil
	})
	if err != nil {
		return nil, err
	}
	file, _ := v.(*File)
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
