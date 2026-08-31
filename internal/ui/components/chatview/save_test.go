package chatview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSaveIntoCopiesUnderTheGivenName(t *testing.T) {
	src := writeFile(t, filepath.Join(t.TempDir(), "1234567890"), "payload")
	dir := filepath.Join(t.TempDir(), "Downloads")

	got, err := saveInto(dir, "report.pdf", src)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "report.pdf"); got != want {
		t.Errorf("saved to %q, want %q", got, want)
	}
	body, err := os.ReadFile(got)
	if err != nil || string(body) != "payload" {
		t.Errorf("saved file reads %q, %v", body, err)
	}
}

// A save that silently replaces an earlier file of the same name is data
// loss wearing the clothes of a convenience — and "photo.jpg" is what every
// phone camera calls its output, so the collision is the common case.
func TestSaveIntoNeverOverwrites(t *testing.T) {
	src := writeFile(t, filepath.Join(t.TempDir(), "cached"), "new")
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "photo.jpg"), "original")

	got, err := saveInto(dir, "photo.jpg", src)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "photo (2).jpg"); got != want {
		t.Errorf("saved to %q, want %q", got, want)
	}
	// The suffix goes before the extension, so the result is still a .jpg.
	if filepath.Ext(got) != ".jpg" {
		t.Errorf("the suffix broke the extension: %q", got)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, "photo.jpg")); string(body) != "original" {
		t.Errorf("the existing file was overwritten: %q", body)
	}
}

// The filename comes off the wire and is not trustworthy. A document called
// "../../.ssh/authorized_keys" must land in the download directory under a
// harmless name, not wherever that path resolves to.
func TestSaveIntoConfinesTheFilename(t *testing.T) {
	src := writeFile(t, filepath.Join(t.TempDir(), "cached"), "payload")
	dir := t.TempDir()

	// A name that reduces to nothing, "." or ".." is not a file name at
	// all — dir/. is the directory itself — so those fall back to a name
	// the reader can at least find. A name that is merely dotted is a
	// legitimate filename and is kept.
	tests := []struct{ name, want string }{
		{"../../.ssh/authorized_keys", "authorized_keys"},
		{"/etc/passwd", "passwd"},
		{"..", "telegram-file"},
		{".", "telegram-file"},
		{"", "telegram-file"},
		{"   ", "telegram-file"},
		{".bashrc", ".bashrc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := saveInto(dir, tt.name, src)
			if err != nil {
				t.Fatal(err)
			}
			if parent := filepath.Dir(got); parent != dir {
				t.Errorf("%q escaped to %q", tt.name, parent)
			}
			base := filepath.Base(got)
			if strings.Contains(base, "..") {
				t.Errorf("%q kept a traversal segment: %q", tt.name, got)
			}
			// Later runs of the same want collide and get suffixed, which
			// is the other rule and is tested above.
			if !strings.HasPrefix(base, strings.TrimSuffix(tt.want, filepath.Ext(tt.want))) {
				t.Errorf("%q was saved as %q, want %q", tt.name, base, tt.want)
			}
			if info, err := os.Stat(got); err != nil || info.IsDir() {
				t.Errorf("%q did not produce a regular file: %v", tt.name, err)
			}
		})
	}
}

// No destination means the save is refused, not guessed at.
//
// Without the guard the empty path joins to a bare filename and the file
// lands in the process's working directory — which for a TUI is wherever the
// user happened to launch it from, and is never where they meant.
func TestSaveIntoRefusesWithNoDirectory(t *testing.T) {
	src := writeFile(t, filepath.Join(t.TempDir(), "cached"), "payload")

	// Run from a directory of our own so the check below cannot be fooled
	// by, or pollute, the package directory.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sandbox := t.TempDir()
	if err := os.Chdir(sandbox); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })

	_, saveErr := saveInto("", "a.txt", src)
	if saveErr == nil {
		t.Error("saving with no download directory reported success")
	}
	// The message is shown to the user as "⚠ save failed: …", so it has to
	// name the cause they can do something about. Letting MkdirAll fail
	// instead produces "mkdir : no such file or directory", which reads as
	// a bug in the client rather than an unset download_dir.
	if saveErr != nil && !strings.Contains(saveErr.Error(), "download directory") {
		t.Errorf("the refusal reads %q, and does not mention the download directory",
			saveErr)
	}
	entries, err := os.ReadDir(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the refused save wrote %d file(s) into the working directory",
			len(entries))
	}
}

// A failed copy leaves nothing behind: a half-written file in Downloads is
// worse than no file, because it looks like it worked.
func TestSaveIntoLeavesNoPartialFile(t *testing.T) {
	// Two failures, on either side of the destination being created: a
	// source that cannot be opened at all, and one that opens and then
	// fails to read. Only the second exercises the cleanup — the first
	// never gets as far as creating anything.
	tests := map[string]string{
		"a source that does not exist": filepath.Join(t.TempDir(), "missing"),
		"a source that is a directory": t.TempDir(),
	}

	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := saveInto(dir, "a.txt", src); err == nil {
				t.Fatal("the save reported success")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("a failed save left %d file(s) behind", len(entries))
			}
		})
	}
}
