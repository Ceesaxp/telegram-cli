package clipboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsImagePath(t *testing.T) {
	cases := map[string]bool{
		"/tmp/shot.png":     true,
		"/tmp/shot.JPG":     true,
		"/tmp/a.jpeg":       true,
		"/tmp/a.webp":       true,
		"/tmp/a.gif":        true,
		"/tmp/notes.txt":    false,
		"/tmp/archive.tar":  false,
		"/tmp/no-extension": false,
		"":                  false,
	}
	for path, want := range cases {
		if got := IsImagePath(path); got != want {
			t.Errorf("IsImagePath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestExtForMime(t *testing.T) {
	cases := map[string]string{
		"image/png":  "png",
		"image/jpeg": "jpg",
		"text/plain": "",
	}
	for mime, want := range cases {
		if got := extForMime(mime); got != want {
			t.Errorf("extForMime(%q) = %q, want %q", mime, got, want)
		}
	}
}

func TestSpoolPathsAreUnique(t *testing.T) {
	dir := t.TempDir()
	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		p := spoolPath(dir, "png")
		if seen[p] {
			t.Fatalf("duplicate spool path %q", p)
		}
		if filepath.Dir(p) != dir {
			t.Fatalf("spool path %q escaped %q", p, dir)
		}
		seen[p] = true
	}
}

// Remove must only delete files we spooled ourselves — never a file the user
// pointed us at via a clipboard file reference.
func TestRemoveOnlyTouchesSpooledFiles(t *testing.T) {
	withTempRoot(t)

	dir, err := spool()
	if err != nil {
		t.Fatalf("spool: %v", err)
	}

	spooled := spoolPath(dir, "png")
	if err := os.WriteFile(spooled, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "user-file.png")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	Remove(spooled)
	if _, err := os.Stat(spooled); !os.IsNotExist(err) {
		t.Errorf("spooled file %q survived Remove", spooled)
	}

	Remove(outside)
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("Remove deleted a file outside the spool dir: %v", err)
	}

	Cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("spool dir %q survived Cleanup", dir)
	}
}

func TestHasData(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if hasData(empty) {
		t.Error("hasData reported true for an empty file")
	}
	if hasData(filepath.Join(dir, "missing")) {
		t.Error("hasData reported true for a missing file")
	}
	full := filepath.Join(dir, "full")
	if err := os.WriteFile(full, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !hasData(full) {
		t.Error("hasData reported false for a non-empty file")
	}
}
