package attach

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestADroppedPathIsUnquoted, in all three spellings terminals use.
//
// Getting this wrong fails on exactly the files people drag: the ones with a
// space in the name. Everything else already works by being typed.
func TestADroppedPathIsUnquoted(t *testing.T) {
	for _, tc := range []struct {
		name, dropped, want string
	}{
		{
			"backslash-escaped, as iTerm2 and Terminal.app send it",
			`/Users/a/My\ Files/x.png`, "/Users/a/My Files/x.png",
		},
		{
			"single-quoted",
			`'/Users/a/My Files/x.png'`, "/Users/a/My Files/x.png",
		},
		{
			"double-quoted",
			`"/Users/a/My Files/x.png"`, "/Users/a/My Files/x.png",
		},
		{
			"a file URL, percent-decoded",
			"file:///Users/a/My%20Files/x.png", "/Users/a/My Files/x.png",
		},
		{
			"a file URL with a localhost authority",
			"file://localhost/tmp/x.png", "/tmp/x.png",
		},
		{
			"plain, which is most of them",
			"/tmp/x.png", "/tmp/x.png",
		},
		{
			"trailing newline, which bracketed paste often carries",
			"/tmp/x.png\n", "/tmp/x.png",
		},
		{
			"an escaped quote inside a name",
			`/tmp/it\'s.png`, "/tmp/it's.png",
		},
		{
			"a name that ends in a backslash is a name, not a truncated escape",
			`/tmp/odd\`, `/tmp/odd\`,
		},
		{
			"a URL list takes the first, where the separation is unambiguous",
			"file:///tmp/a.png file:///tmp/b.png", "/tmp/a.png",
		},
		{
			"a path on another machine is not a path this client can read",
			"file://elsewhere/tmp/x.png", "",
		},
		{"nothing", "   \n ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := UnquotePath(tc.dropped); got != tc.want {
				t.Errorf("UnquotePath(%q) = %q, want %q", tc.dropped, got, tc.want)
			}
		})
	}
}

// TestASingleQuotedPathKeepsItsBackslashes. Single quotes are literal in
// every shell that emits them, so unescaping inside them would corrupt a
// name that legitimately contains one.
func TestASingleQuotedPathKeepsItsBackslashes(t *testing.T) {
	const dropped = `'/tmp/a\b.png'`
	if got := UnquotePath(dropped); got != `/tmp/a\b.png` {
		t.Errorf("UnquotePath(%q) = %q", dropped, got)
	}
}

// TestDroppingOnThePickerReplacesThePath rather than appending to it: a
// dropped path is absolute and complete, and appending it to whatever was
// half-typed produces a path that exists nowhere.
func TestDroppingOnThePickerReplacesThePath(t *testing.T) {
	dir := tree(t, "shot.png")
	m := typeText(t, open(t, dir), "half-typed")

	m = m.Paste(filepath.Join(strings.TrimSuffix(dir, "/"), "shot.png"))

	if strings.Contains(m.Typed(), "half-typed") {
		t.Errorf("the drop was appended to what was typed: %q", m.Typed())
	}
	path, asPhoto, ok := m.Chosen()
	if !ok || filepath.Base(path) != "shot.png" {
		t.Fatalf("after the drop, Chosen = %q, %v", path, ok)
	}
	if !asPhoto {
		t.Error("a dropped image would send as a document")
	}
}

// TestDroppingADirectoryOnThePickerOpensIt, since a directory is somewhere
// to go rather than something to attach.
func TestDroppingADirectoryOnThePickerOpensIt(t *testing.T) {
	root := tree(t, "sub/inner.txt")
	m := open(t, root).Paste(filepath.Join(strings.TrimSuffix(root, "/"), "sub"))

	if !strings.HasSuffix(m.Typed(), "sub/") {
		t.Fatalf("dropping a directory left the path at %q", m.Typed())
	}
	if got := names(m.Matches()); len(got) != 1 || got[0] != "inner.txt" {
		t.Errorf("the listing is %v, want the dropped directory's contents", got)
	}
}

// TestADropWithAnEscapedSpaceLandsOnTheFile — the case the whole unquoting
// exists for, run against a file that is really on disk.
func TestADropWithAnEscapedSpaceLandsOnTheFile(t *testing.T) {
	root := tree(t, "my file.png")
	dropped := strings.ReplaceAll(filepath.Join(strings.TrimSuffix(root, "/"), "my file.png"), " ", `\ `)

	m := open(t, root).Paste(dropped)
	path, _, ok := m.Chosen()
	if !ok || filepath.Base(path) != "my file.png" {
		t.Fatalf("Chosen = %q, %v — the escaped space was not undone", path, ok)
	}
}

// TestOnlyAnUnambiguousPathBecomesAnAttachment.
//
// The rule for a drop onto the composer with no picker open, where the same
// paste could reasonably be either. Deliberately strict: a paste that merely
// resembles a path must not silently become an attachment instead of the
// message somebody meant to send.
func TestOnlyAnUnambiguousPathBecomesAnAttachment(t *testing.T) {
	root := tree(t, "shot.png", "sub/")
	real := filepath.Join(strings.TrimSuffix(root, "/"), "shot.png")

	if !LooksLikePath(real) {
		t.Errorf("a real absolute path to a file was refused: %q", real)
	}
	if !LooksLikePath(strings.ReplaceAll(real, "shot", "shot")) {
		t.Error("the same path failed on a second reading")
	}

	for _, tc := range []struct {
		name, text string
	}{
		{"ordinary prose", "have a look at this"},
		{"prose that mentions a path", "see /etc/hosts for the answer"},
		// A file that really is in the working directory: without the
		// rooted-path rule this would pass the existence check and be
		// attached instead of sent.
		{"a relative path to a file that exists", "paste.go"},
		{"several lines", real + "\nand a second line"},
		{"a path that is not there", "/tmp/definitely-not-here-9f3a2b.png"},
		{"a directory", filepath.Join(strings.TrimSuffix(root, "/"), "sub")},
		{"nothing at all", "   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if LooksLikePath(tc.text) {
				t.Errorf("LooksLikePath(%q) = true — it would be attached "+
					"instead of sent", tc.text)
			}
		})
	}
}

// TestAPasteWithANewlineIsRefusedEvenWhenItNamesARealFile.
//
// A newline is legal in a filename on Unix, so the existence check alone
// would accept one — and a paste containing a newline is far more likely to
// be two things than one file with an odd name. Refusing costs a rare drop;
// accepting silently sends an attachment instead of the message somebody
// typed.
func TestAPasteWithANewlineIsRefusedEvenWhenItNamesARealFile(t *testing.T) {
	dir := t.TempDir()
	odd := filepath.Join(dir, "two\nlines.png")
	if err := os.WriteFile(odd, []byte("x"), 0o644); err != nil {
		t.Skipf("this filesystem will not take a newline in a name: %v", err)
	}
	if _, err := os.Stat(odd); err != nil {
		t.Skipf("the file is not there to test against: %v", err)
	}

	if LooksLikePath(odd) {
		t.Errorf("LooksLikePath(%q) = true — a paste with a newline in it "+
			"would be attached rather than sent", odd)
	}
}

// TestAHomeRelativeDropIsRecognised, since a path is only useful here after
// ~ has been expanded and the file has been found.
func TestAHomeRelativeDropIsRecognised(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no usable home directory")
	}
	file, err := os.CreateTemp(home, "teletui-drop-*.png")
	if err != nil {
		t.Skipf("cannot write to the home directory: %v", err)
	}
	t.Cleanup(func() { os.Remove(file.Name()) })
	file.Close()

	tilde := "~/" + filepath.Base(file.Name())
	if !LooksLikePath(tilde) {
		t.Errorf("LooksLikePath(%q) = false — ~ was not expanded", tilde)
	}
}
