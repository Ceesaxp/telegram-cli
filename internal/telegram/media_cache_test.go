package telegram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotd/td/tg"

	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// cacheClient is a client with a registry and a files_dir and nothing else.
// A cache hit must return before anything reaches the network, so a nil api
// is the strictest possible assertion about that.
func cacheClient(t *testing.T) (*Client, string) {
	t.Helper()
	dir := t.TempDir()
	return &Client{
		files:  newFileRegistry(),
		config: &config.Config{Storage: config.StorageConfig{FilesDir: dir}},
	}, dir
}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestACachedFileIsNotDownloadedAgainAfterARestart.
//
// files_dir is documented as "the media CACHE: where downloads land so a
// photo drawn twice is fetched once", and that held only within one process
// — done is in-memory and starts false in every one, so a restart re-fetched
// every thumbnail, avatar and document already on disk.
//
// The client here has no api at all: reaching the network would be a nil
// dereference, which is the point.
func TestACachedFileIsNotDownloadedAgainAfterARestart(t *testing.T) {
	c, dir := cacheClient(t)

	const key = "doc:12345"
	c.files.put(key, &fileEntry{
		location: &tg.InputDocumentFileLocation{ID: 12345},
		size:     64,
		name:     "report.pdf",
	})

	snap, _ := c.files.snapshot(key)
	writeFile(t, c.cachePath(key, snap), 64)

	file, err := c.DownloadFileSync(key)
	if err != nil {
		t.Fatalf("DownloadFileSync on a cached file: %v", err)
	}
	if !file.Downloaded {
		t.Error("a cached file came back as not downloaded")
	}
	if filepath.Dir(file.Path) != dir {
		t.Errorf("path = %q, want it under files_dir", file.Path)
	}
	if _, err := os.Stat(file.Path); err != nil {
		t.Errorf("the returned path is not there: %v", err)
	}

	// The registry learns, so the next draw of the same photo answers from
	// memory instead of stat'ing the disk again. Thumbnails are asked for
	// on every scroll.
	if snap, _ := c.files.snapshot(key); !snap.done || snap.path != file.Path {
		t.Errorf("a cache hit left the registry at done=%v path=%q",
			snap.done, snap.path)
	}
}

// TestWhatMayStandInForADownload.
//
// The decision itself, rather than provoking a download to see whether one
// happens: gotd fetches on goroutines it owns, so a nil-api panic there
// takes the process down instead of failing a test.
func TestWhatMayStandInForADownload(t *testing.T) {
	dir := t.TempDir()

	present := filepath.Join(dir, "present")
	writeFile(t, present, 100)

	empty := filepath.Join(dir, "empty")
	writeFile(t, empty, 0)

	for _, tc := range []struct {
		name string
		path string
		size int64
		want bool
	}{
		{"the size agrees", present, 100, true},
		{"the size is not known", present, 0, true},
		// A truncated download from a previous run, or a different file
		// that landed on the name. Half a photo draws as a broken photo,
		// which reads as a bug in the client rather than a cache miss.
		{"the file is short", present, 500, false},
		{"the file is long", present, 20, false},
		// Zero bytes is what an interrupted download used to leave, and it
		// is never a picture.
		{"the file is empty", empty, 0, false},
		{"there is no file", filepath.Join(dir, "absent"), 100, false},
		{"the name is a directory", dir, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := usableCache(tc.path, tc.size)
			if ok != tc.want {
				t.Errorf("usableCache(%q, %d) = %v, want %v", tc.path, tc.size, ok, tc.want)
			}
			if ok && got != tc.path {
				t.Errorf("returned %q, want %q", got, tc.path)
			}
		})
	}
}

// TestAnUnknownSizeFallsBackToExistence. A stripped thumbnail has no size in
// the registry, and refusing to cache those would be refusing to cache the
// files drawn most often.
func TestAnUnknownSizeFallsBackToExistence(t *testing.T) {
	c, _ := cacheClient(t)

	const key = "doc:7:thumb"
	c.files.put(key, &fileEntry{
		location: &tg.InputDocumentFileLocation{ID: 7},
		name:     "thumb.jpg",
	})
	snap, _ := c.files.snapshot(key)
	writeFile(t, c.cachePath(key, snap), 128)

	file, err := c.DownloadFileSync(key)
	if err != nil {
		t.Fatalf("a thumbnail with no recorded size was not served: %v", err)
	}
	if !file.Downloaded {
		t.Error("it came back as not downloaded")
	}
}

// TestAnAvatarsPathCarriesItsGeneration.
//
// Every other key names immutable content — a document ID, a photo ID and a
// size. "avatar:<chatID>" names a SLOT, and the picture in it changes: a
// chat that changed its photo would be served the old one out of the cache
// forever.
func TestAnAvatarsPathCarriesItsGeneration(t *testing.T) {
	c, _ := cacheClient(t)

	const key = "avatar:4242"
	first := fileSnap{avatar: &avatarRef{chatID: 4242, photoID: 111}, name: "avatar.jpg"}
	second := fileSnap{avatar: &avatarRef{chatID: 4242, photoID: 222}, name: "avatar.jpg"}

	if a, b := c.cachePath(key, first), c.cachePath(key, second); a == b {
		t.Errorf("both generations of an avatar cache to %q", a)
	}
	// And the same generation is stable, or nothing would ever hit.
	if a, b := c.cachePath(key, first), c.cachePath(key, first); a != b {
		t.Errorf("the same avatar cached to %q and %q", a, b)
	}
}

// TestACachePathIsStableAcrossProcesses. The whole mechanism rests on a
// later run deriving the same name from the same key.
func TestACachePathIsStableAcrossProcesses(t *testing.T) {
	c, _ := cacheClient(t)
	snap := fileSnap{size: 10, name: "a b/c.jpg"}

	first := c.cachePath("doc:1", snap)
	second := c.cachePath("doc:1", snap)
	if first != second {
		t.Errorf("one key gave two paths: %q and %q", first, second)
	}
	if filepath.Base(first) == "" || filepath.Dir(first) == "." {
		t.Errorf("path = %q, want it under files_dir", first)
	}
	// The separator in the remote name must not become a directory.
	if len(filepath.SplitList(first)) != 1 || filepath.Dir(first) != filepath.Dir(second) {
		t.Errorf("a remote name escaped its directory: %q", first)
	}
}

// TestAnInterruptedDownloadLeavesNothingBehind.
//
// gotd's ToPath opens the destination with os.Create, so it truncated the
// good copy BEFORE the transfer — an interrupted download left an empty file
// exactly where the next run looks for a cached one, and the cache check
// above would then have to distrust it. Renaming into place means the
// destination either does not exist or is complete.
func TestAnInterruptedDownloadLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")

	err := fetchIntoPlace(context.Background(), path, func(_ context.Context, tmp string) error {
		// A transfer that started and died, as ToPath would leave it.
		writeFile(t, tmp, 3)
		return errors.New("connection reset")
	})
	if err == nil {
		t.Fatal("a failed transfer reported success")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a failed transfer left a file at the destination")
	}

	// And nothing partial left lying around either.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the directory still holds %d files after a failed transfer", len(entries))
	}
}

// TestAnInterruptedDownloadDoesNotDestroyTheGoodCopy — the specific harm of
// truncating the destination up front.
func TestAnInterruptedDownloadDoesNotDestroyTheGoodCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")
	writeFile(t, path, 500)

	_ = fetchIntoPlace(context.Background(), path, func(_ context.Context, tmp string) error {
		writeFile(t, tmp, 3)
		return errors.New("connection reset")
	})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the cached file is gone: %v", err)
	}
	if info.Size() != 500 {
		t.Errorf("the cached file is now %d bytes, want the original 500", info.Size())
	}
}

// TestASuccessfulDownloadLandsAtTheDestination.
func TestASuccessfulDownloadLandsAtTheDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")

	var tmpDir string
	if err := fetchIntoPlace(context.Background(), path, func(_ context.Context, tmp string) error {
		tmpDir = filepath.Dir(tmp)
		if tmp == path {
			t.Error("the transfer was pointed at the destination itself")
		}
		writeFile(t, tmp, 42)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Beside the destination, not in the system temp directory: a rename
	// across filesystems fails, and files_dir is routinely somewhere else
	// entirely — a home directory on a network mount, an SD card.
	if tmpDir != dir {
		t.Errorf("the transfer wrote to %q, want it beside the destination in %q", tmpDir, dir)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("nothing at the destination: %v", err)
	}
	if info.Size() != 42 {
		t.Errorf("destination is %d bytes, want 42", info.Size())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("%d files left in the directory, want just the destination", len(entries))
	}
}
