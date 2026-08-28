// Package clipboard reads image and file data out of the host system
// clipboard and spools it to disk, so it can be attached to a message.
package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrNoImage is returned when the clipboard holds nothing attachable.
	ErrNoImage = errors.New("clipboard holds no image or file")
	// ErrUnsupported is returned when the platform has no clipboard reader.
	ErrUnsupported = errors.New("clipboard paste is not supported on this platform")
	// ErrNoTool is returned when the required helper binary is missing.
	ErrNoTool = errors.New("no clipboard helper found (install wl-clipboard or xclip)")
)

// Result describes what came out of the clipboard.
type Result struct {
	Path    string // local file path holding the pasted data
	IsImage bool   // true when the file is an image and can be sent as a photo
	Spooled bool   // true when the file was written by us and may be deleted
}

var (
	spoolMu  sync.Mutex
	spoolDir string
	spoolSeq atomic.Uint64
)

// Paste resolves the current clipboard contents to a local file path. A file
// reference (an image copied in Finder or a file manager) is used in place;
// raw image data is spooled to a temporary file.
func Paste() (Result, error) {
	if path, err := readFilePath(); err == nil && path != "" {
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			return Result{Path: path, IsImage: IsImagePath(path)}, nil
		}
	}

	dir, err := spool()
	if err != nil {
		return Result{}, err
	}

	path, err := readImage(dir)
	if err != nil {
		return Result{}, err
	}
	// clipboard_unix.go can spool image/tiff (a tiff-only clipboard should
	// still be attachable), but Telegram's InputMediaUploadedPhoto rejects
	// TIFF, so such files must go out as documents, not photos.
	return Result{Path: path, IsImage: IsImagePath(path), Spooled: true}, nil
}

// Cleanup removes the spool directory and everything in it. It exists for
// tests; production code does not call it at exit. Bubble Tea v2 never waits
// for in-flight tea.Cmd goroutines on quit, so deleting the spool dir at
// exit races any upload still reading from it. Instead, the spool directory
// is named deterministically per process and the next process to start
// sweeps up any directory left behind by a process that is no longer
// running (see spool and sweepStaleSpools).
func Cleanup() {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	if spoolDir == "" {
		return
	}
	os.RemoveAll(spoolDir)
	spoolDir = ""
}

// Remove deletes path if — and only if — it is a file we spooled ourselves.
func Remove(path string) {
	spoolMu.Lock()
	dir := spoolDir
	spoolMu.Unlock()
	if dir == "" || path == "" {
		return
	}
	if filepath.Dir(path) != dir {
		return
	}
	os.Remove(path)
}

// IsImagePath reports whether path looks like an image Telegram can render
// inline as a photo.
func IsImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return true
	}
	return false
}

// spoolDirPrefix names the per-process spool directory, deterministically,
// as telegram-cli-paste-<pid>. A deterministic name (rather than the random
// suffix os.MkdirTemp would generate) is what lets the next process find and
// sweep up a directory abandoned by this one. When the deterministic path
// can't be trusted (see createSpoolDir), the fallback directory still
// carries this prefix — with a random os.MkdirTemp suffix instead of a pid —
// so sweepStaleSpools still finds and eventually reclaims it.
const spoolDirPrefix = "telegram-cli-paste-"

// staleSpoolAge is how old a spool directory must be, untouched, before
// sweepStaleSpools will consider removing it — regardless of what its name
// or owning pid look like. See isStaleSpoolDir for the full policy.
const staleSpoolAge = 48 * time.Hour

// tempRoot returns the directory spool directories are created under. It's
// a var — rather than calling os.TempDir() directly everywhere — purely so
// tests can point it at a sandbox instead of sweeping the developer's real
// system temp directory.
var tempRoot = os.TempDir

// spool returns the per-process directory that pasted data is written to,
// creating it on first use. The first call in a process also sweeps up spool
// directories left behind by processes that are no longer running. Every
// call — including cache hits — refreshes the directory's mtime, so an
// actively used spool dir never looks abandoned to another process's sweep.
func spool() (string, error) {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	if spoolDir != "" {
		touchSpoolDir(spoolDir)
		return spoolDir, nil
	}
	dir, err := createSpoolDir()
	if err != nil {
		return "", err
	}
	spoolDir = dir
	sweepStaleSpools(dir)
	touchSpoolDir(dir)
	return spoolDir, nil
}

// createSpoolDir creates this process's deterministic spool directory,
// telegram-cli-paste-<pid>. It uses os.Mkdir (not MkdirAll) so that a
// pre-existing path is never silently accepted: on a shared machine, an
// attacker who can predict our pid could pre-create that exact path ahead of
// us — as a symlink into a sensitive location, or as a world-writable
// directory seeded with a same-named file — hoping we'll write through it.
// When the path already exists, it's trusted only if isSafeExistingSpoolDir
// confirms it's a genuine, privately-owned directory (see the per-platform
// implementations); otherwise we fall back to a randomly-suffixed
// os.MkdirTemp directory, which an attacker cannot predict in advance.
func createSpoolDir() (string, error) {
	path := filepath.Join(tempRoot(), fmt.Sprintf("%s%d", spoolDirPrefix, os.Getpid()))
	err := os.Mkdir(path, 0o700)
	if err == nil {
		return path, nil
	}
	if !os.IsExist(err) {
		return "", fmt.Errorf("clipboard: create spool dir: %w", err)
	}
	if isSafeExistingSpoolDir(path) {
		return path, nil
	}
	dir, err := os.MkdirTemp(tempRoot(), spoolDirPrefix)
	if err != nil {
		return "", fmt.Errorf("clipboard: create spool dir: %w", err)
	}
	return dir, nil
}

// touchSpoolDir refreshes dir's modification time to now. Best-effort: a
// failure here isn't worth failing a paste over.
func touchSpoolDir(dir string) {
	now := time.Now()
	_ = os.Chtimes(dir, now, now)
}

// sweepStaleSpools removes sibling spool directories abandoned by processes
// that are no longer running. own is this process's own spool dir and is
// never touched. Errors are ignored — this is best-effort housekeeping, not
// something worth failing a paste over.
func sweepStaleSpools(own string) {
	matches, err := filepath.Glob(filepath.Join(tempRoot(), spoolDirPrefix+"*"))
	if err != nil {
		return
	}
	for _, dir := range matches {
		if dir == own {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		pid, ok := spoolDirPID(dir)
		if isStaleSpoolDir(pid, ok, info) {
			os.RemoveAll(dir)
		}
	}
}

// spoolDirPID extracts the pid encoded in a spool directory name. ok is
// false when the name doesn't carry a parseable pid — e.g. a directory left
// behind by an older version of this package, which used a random
// os.MkdirTemp suffix instead of the pid.
func spoolDirPID(dir string) (pid int, ok bool) {
	suffix := strings.TrimPrefix(filepath.Base(dir), spoolDirPrefix)
	n, err := strconv.Atoi(suffix)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// timeoutErr returns a clear "timed out" error wrapping
// context.DeadlineExceeded when ctx's deadline is what caused err; otherwise
// it returns err unchanged.
func timeoutErr(ctx context.Context, op string, err error) error {
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("clipboard: %s: timed out: %w", op, context.DeadlineExceeded)
	}
	return err
}

// spoolPath builds a unique destination path inside dir for the given
// extension (without a leading dot). It also refreshes dir's mtime — see
// touchSpoolDir — since this runs right before a file is actually written
// into a spool directory that might have been sitting untouched since spool
// was last called.
func spoolPath(dir, ext string) string {
	touchSpoolDir(dir)
	return filepath.Join(dir, fmt.Sprintf("paste-%d.%s", spoolSeq.Add(1), ext))
}

// hasData reports whether path exists and is non-empty. Some clipboard
// helpers exit 0 while writing nothing at all.
func hasData(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// extForMime maps a clipboard MIME type to a file extension.
func extForMime(mime string) string {
	switch mime {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	case "image/bmp", "image/x-bmp":
		return "bmp"
	case "image/tiff":
		return "tiff"
	}
	return ""
}
