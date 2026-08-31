package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Where a save goes is not where a download is cached.
//
// files_dir is the media CACHE: it names files by their server-side id and
// exists so a photo drawn twice is fetched once. download_dir is where `s`
// puts a copy under the sender's own filename, and it defaults to the place
// every other program on the machine puts downloads — because a save the
// reader cannot find is not a save.
func TestTheDownloadDirectoryIsNotTheMediaCache(t *testing.T) {
	if DefaultDownloadDir == DefaultFilesDir {
		t.Fatalf("saves default into the media cache: %q", DefaultDownloadDir)
	}
	if got := filepath.Base(DefaultDownloadDir); !strings.EqualFold(got, "Downloads") {
		t.Errorf("the default save directory is %q, want the Downloads folder",
			DefaultDownloadDir)
	}

	// The literal stays a literal — a config written with an absolute home
	// baked in is not portable between machines or users.
	if !strings.HasPrefix(DefaultDownloadDir, "~") {
		t.Errorf("DefaultDownloadDir = %q, want a tilde path", DefaultDownloadDir)
	}

	cfg := defaultConfig()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory to expand against")
	}
	if want := filepath.Join(home, "Downloads"); cfg.Storage.DownloadDir != want {
		t.Errorf("the default config saves to %q, want %q",
			cfg.Storage.DownloadDir, want)
	}
	if cfg.Storage.DownloadDir == cfg.Storage.FilesDir {
		t.Error("the default config saves into the media cache")
	}
}
