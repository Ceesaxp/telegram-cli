package telegram

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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

	t.Run("file under a second configured root", func(t *testing.T) {
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

	// An allowlist with nothing usable in it must reject everything. The
	// opposite reading — no roots means no restriction — is the failure
	// mode issue #48 is about, and it would arrive silently.
	t.Run("no usable root rejects every path", func(t *testing.T) {
		for _, roots := range [][]string{nil, {""}, {"", ""}} {
			if _, err := ResolveAllowedSendPath(underFiles, roots...); err == nil {
				t.Fatalf("roots %q: expected rejection with no usable root", roots)
			}
		}
	})

	// The caller is an operator or the agent they configured, and a bare
	// "outside the allowed directories" cannot be told apart from a typo.
	t.Run("rejection names the roots it searched", func(t *testing.T) {
		_, err := ResolveAllowedSendPath(secret, filesDir, "", cwdRoot)
		if err == nil {
			t.Fatal("expected error for path outside roots")
		}
		for _, want := range []string{filesDir, cwdRoot} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name root %q", err, want)
			}
		}
	})
}

// sendRoot is a directory to send from, holding file.txt, and a file
// outside it, secret.txt: the one a remote caller must never get.
func sendRoot(t *testing.T) (root, file, secret string) {
	t.Helper()
	root = t.TempDir()
	file = filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("the file"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret = filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("the secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, file, secret
}

// contents reads what f holds and closes it.
func contents(t *testing.T, f *os.File) string {
	t.Helper()
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read %s: %v", f.Name(), err)
	}
	return string(b)
}

func TestOpenAllowedSendFileOpensAFileInsideARoot(t *testing.T) {
	root, file, _ := sendRoot(t)

	f, err := OpenAllowedSendFile(file, root)
	if err != nil {
		t.Fatalf("OpenAllowedSendFile(%s): %v", file, err)
	}
	// The os.Root it was opened through is closed by now, and the file
	// still reads: it is not tied to the root.
	if got := contents(t, f); got != "the file" {
		t.Fatalf("read %q, want %q", got, "the file")
	}
}

// wantRefused checks that opening path was refused as outside the roots,
// with nothing opened, and that the refusal names every root it searched.
func wantRefused(t *testing.T, f *os.File, err error, path string, roots ...string) {
	t.Helper()
	if err == nil {
		f.Close()
		t.Fatalf("opened %s, want it refused", path)
	}
	if f != nil {
		t.Errorf("refusal returned a file as well as %v", err)
	}
	if !strings.Contains(err.Error(), "outside the allowed directories") {
		t.Fatalf("error = %v, want an out-of-root refusal", err)
	}
	if want := fmt.Sprintf("path %q is outside", path); !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to name the path as %s", err, want)
	}
	for _, root := range roots {
		if !strings.Contains(err.Error(), root) {
			t.Errorf("error = %v, does not name root %q", err, root)
		}
	}
}

func TestOpenAllowedSendFileRefusesAPathOutsideEveryRoot(t *testing.T) {
	root, _, secret := sendRoot(t)
	other := t.TempDir()

	f, err := OpenAllowedSendFile(secret, root, "", other)

	wantRefused(t, f, err, secret, root, other)
}

// A link is a name inside the root for a file outside it. Written either
// way — absolute, or relative and climbing out — it must not be followed.
func TestOpenAllowedSendFileRefusesASymlinkThatLeavesTheRoot(t *testing.T) {
	root, _, secret := sendRoot(t)
	relative, err := filepath.Rel(root, secret)
	if err != nil {
		t.Fatal(err)
	}

	for name, target := range map[string]string{"absolute": secret, "relative": relative} {
		t.Run(name, func(t *testing.T) {
			link := filepath.Join(root, name+".txt")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}

			f, err := OpenAllowedSendFile(link, root)

			wantRefused(t, f, err, link, root)
		})
	}
}

// A relative link that stays inside the root is followed, through a
// subdirectory and back out of it.
func TestOpenAllowedSendFileFollowsASymlinkThatStaysInside(t *testing.T) {
	root, _, _ := sendRoot(t)
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sub, "link.txt")
	if err := os.Symlink(filepath.Join("..", "file.txt"), link); err != nil {
		t.Fatal(err)
	}

	f, err := OpenAllowedSendFile(link, root)
	if err != nil {
		t.Fatalf("OpenAllowedSendFile(%s): %v", link, err)
	}
	if got := contents(t, f); got != "the file" {
		t.Fatalf("read %q, want %q", got, "the file")
	}
}

// os.Root refuses every absolute link, including one that names a file
// inside the root. That is its rule rather than this function's, and it
// is pinned here so the narrowing is a decision rather than a surprise.
func TestOpenAllowedSendFileRefusesAnAbsoluteSymlinkEvenIntoTheRoot(t *testing.T) {
	root, file, _ := sendRoot(t)
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}

	f, err := OpenAllowedSendFile(link, root)

	wantRefused(t, f, err, link, root)
}

// wantNotRegular checks that opening path was refused because it is not a
// regular file, with nothing left open.
func wantNotRegular(t *testing.T, f *os.File, err error, path string) {
	t.Helper()
	if err == nil {
		f.Close()
		t.Fatalf("opened %s, want it refused", path)
	}
	if f != nil {
		t.Errorf("refusal returned a file as well as %v", err)
	}
	if want := fmt.Sprintf("%q: not a regular file", path); !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

// Only a regular file can be sent. A directory used to pass the root check
// and fail later, at the upload; now it fails where the file is opened.
func TestOpenAllowedSendFileRefusesADirectory(t *testing.T) {
	root, _, _ := sendRoot(t)
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{"a subdirectory": sub, "the root itself": root} {
		t.Run(name, func(t *testing.T) {
			f, err := OpenAllowedSendFile(path, root)

			wantNotRegular(t, f, err, path)
		})
	}
}

// A configured root may be a link: /tmp is one on macOS, to /private/tmp.
// A path is accepted under the root as written and under where it really
// is, because a caller handed the resolved form of a root path (by a
// shell, or by EvalSymlinks) still means the same directory. The root is
// configuration, not something a caller can swap, so resolving it is not
// the race that resolving the file would be.
func TestOpenAllowedSendFileAcceptsARootThatIsASymlink(t *testing.T) {
	real, _, _ := sendRoot(t)
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(real, root); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"under the root as configured": filepath.Join(root, "file.txt"),
		"under where it really is":     filepath.Join(resolved, "file.txt"),
	} {
		t.Run(name, func(t *testing.T) {
			f, err := OpenAllowedSendFile(path, root)
			if err != nil {
				t.Fatalf("OpenAllowedSendFile(%s): %v", path, err)
			}
			if got := contents(t, f); got != "the file" {
				t.Fatalf("read %q, want %q", got, "the file")
			}
		})
	}
}

// An allowlist with nothing usable in it must refuse everything. The
// opposite reading — no roots means no restriction — is the failure mode
// issue #48 is about, and it would arrive silently.
func TestOpenAllowedSendFileWithNoUsableRootRefusesEveryPath(t *testing.T) {
	_, file, _ := sendRoot(t)

	for _, roots := range [][]string{nil, {""}, {"", ""}} {
		f, err := OpenAllowedSendFile(file, roots...)

		wantRefused(t, f, err, file)
		if !strings.HasSuffix(err.Error(), "()") {
			t.Errorf("roots %q: error = %v, want it to name no root", roots, err)
		}
	}
}

// A missing file inside a root is missing, not outside: the caller named
// the right directory, and the error has to say what is actually wrong.
func TestOpenAllowedSendFileSaysAMissingFileIsMissing(t *testing.T) {
	root, _, _ := sendRoot(t)
	path := filepath.Join(root, "nope.bin")

	f, err := OpenAllowedSendFile(path, root)
	if err == nil {
		f.Close()
		t.Fatalf("opened %s, want it refused", path)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error = %v, want one that says the file does not exist", err)
	}
	if strings.Contains(err.Error(), "outside") {
		t.Errorf("error = %v, want no talk of outside for a path that is inside", err)
	}
}

// unclean joins parts as a caller might type them: filepath.Join would
// clean the ".." away before the function under test ever saw it.
func unclean(parts ...string) string {
	return strings.Join(parts, string(filepath.Separator))
}

// ".." is refused whether it is spelled out in the path or hidden behind a
// directory link partway along it.
func TestOpenAllowedSendFileRefusesDotDotEscapes(t *testing.T) {
	root, _, secret := sendRoot(t)
	outside := filepath.Dir(secret)
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	up, err := filepath.Rel(root, outside)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(up, filepath.Join(root, "up")); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"spelled out":             unclean(sub, "..", "..", filepath.Base(outside), "secret.txt"),
		"behind a directory link": filepath.Join(root, "up", "secret.txt"),
	} {
		t.Run(name, func(t *testing.T) {
			f, err := OpenAllowedSendFile(path, root)

			wantRefused(t, f, err, path, root)
		})
	}

	// And one that climbs but never leaves is just a path.
	t.Run("staying inside", func(t *testing.T) {
		path := unclean(sub, "..", "file.txt")
		f, err := OpenAllowedSendFile(path, root)
		if err != nil {
			t.Fatalf("OpenAllowedSendFile(%s): %v", path, err)
		}
		if got := contents(t, f); got != "the file" {
			t.Fatalf("read %q, want %q", got, "the file")
		}
	})
}
