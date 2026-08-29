package telegram

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAllowedSendPath(t *testing.T) {
	filesDir := t.TempDir()
	cwdRoot := t.TempDir()
	outside := t.TempDir()

	underFiles := filepath.Join(filesDir, "doc.pdf")
	if err := os.WriteFile(underFiles, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	underCwd := filepath.Join(cwdRoot, "note.txt")
	if err := os.WriteFile(underCwd, []byte("txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "id_rsa")
	if err := os.WriteFile(secret, []byte("ssh"), 0o600); err != nil {
		t.Fatal(err)
	}
	escapeLink := filepath.Join(filesDir, "escape")
	if err := os.Symlink(secret, escapeLink); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(filesDir, "subdir")
	if err := os.Mkdir(subDir, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Run("file under files_dir", func(t *testing.T) {
		got, err := ResolveAllowedSendPath(underFiles, filesDir, cwdRoot)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want, err := filepath.EvalSymlinks(underFiles)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("file under cwd root", func(t *testing.T) {
		got, err := ResolveAllowedSendPath(underCwd, filesDir, cwdRoot)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want, err := filepath.EvalSymlinks(underCwd)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("path outside roots", func(t *testing.T) {
		_, err := ResolveAllowedSendPath(secret, filesDir, cwdRoot)
		if err == nil {
			t.Fatal("expected error for path outside roots")
		}
		if !strings.Contains(err.Error(), "outside the allowed directories") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("symlink that escapes the root", func(t *testing.T) {
		_, err := ResolveAllowedSendPath(escapeLink, filesDir, cwdRoot)
		if err == nil {
			t.Fatal("expected error for escaping symlink")
		}
		if !strings.Contains(err.Error(), "outside the allowed directories") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("directory is allowed by jail", func(t *testing.T) {
		// The helper jails paths; uploadForSend still rejects directories.
		got, err := ResolveAllowedSendPath(subDir, filesDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want, err := filepath.EvalSymlinks(subDir)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("missing file is rejected", func(t *testing.T) {
		_, err := ResolveAllowedSendPath(filepath.Join(filesDir, "nope.bin"), filesDir)
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("empty roots are skipped", func(t *testing.T) {
		_, err := ResolveAllowedSendPath(underFiles, "", filesDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
