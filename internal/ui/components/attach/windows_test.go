package attach

import (
	"strings"
	"testing"
)

// Windows is a published target — the release workflow builds and packages
// windows/amd64 — but its CI job only cross-COMPILES, so nothing here would
// ever run there. These tests pin the separator instead, which is what makes
// the platform's own path rules reachable from a run anywhere.
//
// They are not hypothetical. The first version of this component could not
// open its own default directory on Windows: config expands download_dir to
// C:\Users\me\Downloads, collapseHome turned that into ~\Downloads, Open
// appended a "/", splitPath only knew about "/" and expandHome only knew
// about "~/", so the very first Ctrl+T targeted a literal relative path
// called `~\Downloads/` and the picker opened saying no such directory.

// pinWindows makes this test run under Windows' path rules: its separator,
// and a home directory on a drive.
func pinWindows(t *testing.T) {
	t.Helper()
	old := sep
	sep = '\\'
	t.Cleanup(func() { sep = old })
	t.Setenv("HOME", `C:\Users\me`)
	t.Setenv("USERPROFILE", `C:\Users\me`)
}

// TestANativePathBecomesOneSpelling on the way in, and goes back to the
// platform's on the way out. Everything between them is slash-separated, so
// splitPath, completion and the prompt row have one separator to know about
// rather than two.
func TestANativePathBecomesOneSpelling(t *testing.T) {
	pinWindows(t)

	for _, tc := range []struct{ name, in, want string }{
		{"a drive path collapses to the home", `C:\Users\me\Downloads`, "~/Downloads"},
		{"the home itself", `C:\Users\me`, "~"},
		{"somewhere else keeps its drive", `D:\work\notes`, "D:/work/notes"},
		{"a share", `\\server\share\docs`, "//server/share/docs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := display(tc.in); got != tc.want {
				t.Errorf("display(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	for _, tc := range []struct{ name, in, want string }{
		{"the home expands to a drive path", "~/Downloads", `C:\Users\me\Downloads`},
		{"a bare tilde", "~", `C:\Users\me`},
		{"a drive path goes back verbatim", "D:/work/notes", `D:\work\notes`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := native(tc.in); got != tc.want {
				t.Errorf("native(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestOpeningTheConfiguredDirectoryOnWindows — the exact failure, from the
// value config actually supplies.
func TestOpeningTheConfiguredDirectoryOnWindows(t *testing.T) {
	pinWindows(t)

	var m Model
	m.dir = withSlash(display(`C:\Users\me\Downloads`))

	if m.dir != "~/Downloads/" {
		t.Fatalf("the picker opens on %q, which no stat call will find", m.dir)
	}
	if strings.ContainsRune(m.dir, '\\') {
		t.Errorf("the internal path still carries a native separator: %q", m.dir)
	}

	dir, tail := splitPath(m.dir + "back")
	if dir != "~/Downloads/" || tail != "back" {
		t.Errorf("splitPath(%q) = %q, %q", m.dir+"back", dir, tail)
	}
	if got := native(dir); got != `C:\Users\me\Downloads\` {
		t.Errorf("the directory read is %q", got)
	}
}

// TestUpFromADriveRootStops. Every path has a top, and one more `←` at it
// must not walk off into a relative reference: `C:` on Windows means "the
// working directory on C", not the drive.
func TestUpFromADriveRootStops(t *testing.T) {
	pinWindows(t)

	for _, tc := range []struct{ name, from, want string }{
		{"a deep path", "D:/work/notes/", "D:/work/"},
		{"one below the root", "D:/work/", "D:/"},
		{"the root itself", "D:/", "D:/"},
		{"a share root", "//server/share/", "//server/share/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parentOf(tc.from); got != tc.want {
				t.Errorf("parentOf(%q) = %q, want %q", tc.from, got, tc.want)
			}
		})
	}
}

// TestADroppedWindowsPathKeepsItsSeparators.
//
// Nothing on Windows escapes with a backslash — it quotes instead — so
// unescaping there would turn every dropped C:\Users\me\x.png into
// C:Usersmex.png. Silently, on every drop, on the whole platform.
func TestADroppedWindowsPathKeepsItsSeparators(t *testing.T) {
	pinWindows(t)

	for _, tc := range []struct{ name, dropped, want string }{
		{"plain", `C:\Users\me\shot.png`, `C:\Users\me\shot.png`},
		{"quoted, which is how Windows escapes a space", `"C:\Users\me\my file.png"`, `C:\Users\me\my file.png`},
		{"a share", `\\server\share\shot.png`, `\\server\share\shot.png`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := UnquotePath(tc.dropped); got != tc.want {
				t.Errorf("UnquotePath(%q) = %q, want %q", tc.dropped, got, tc.want)
			}
		})
	}
}

// TestADriveLetterCountsAsRooted, so a native drop is recognised as naming a
// place rather than being read as prose.
func TestADriveLetterCountsAsRooted(t *testing.T) {
	pinWindows(t)

	for _, rootedPath := range []string{`C:\Users\me\shot.png`, `\\server\share\x.png`, `\tmp\x.png`} {
		if !rooted(rootedPath) {
			t.Errorf("rooted(%q) = false, so a native drop reads as prose", rootedPath)
		}
	}
	for _, relative := range []string{`shot.png`, `me\shot.png`, "just some words"} {
		if rooted(relative) {
			t.Errorf("rooted(%q) = true", relative)
		}
	}
}

// TestUnixIsUnaffected. The separator seam must not change the platform it
// was not written for: a backslash is a legal character in a Unix filename,
// and treating it as a separator would break every name containing one.
func TestUnixIsUnaffected(t *testing.T) {
	if sep != '/' {
		t.Skip("this run is not on a slash platform")
	}

	const odd = `/tmp/a\b.png`
	if got := display(odd); got != odd {
		t.Errorf("display(%q) = %q — a backslash in a name was read as a separator", odd, got)
	}
	if got := volume("C:/Users/me"); got != "" {
		t.Errorf("volume() found a drive on a slash platform: %q", got)
	}
	if rooted(`C:\Users\me\x.png`) {
		t.Error("a Windows path reads as rooted on a slash platform")
	}
	// Backslash escaping is still undone here, which is how macOS terminals
	// deliver a dropped path.
	if got := UnquotePath(`/tmp/my\ file.png`); got != "/tmp/my file.png" {
		t.Errorf("UnquotePath = %q, want the escape undone", got)
	}
}
