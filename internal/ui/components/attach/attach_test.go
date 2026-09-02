package attach

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/imtaqin/telegram-cli/internal/render"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
	"github.com/muesli/termenv"

	"github.com/charmbracelet/lipgloss"
)

// TestMain pins the colour profile.
//
// lipgloss resolves to Ascii under `go test`, which strips every colour and
// makes any assertion about styling pass whatever the style was. Four
// packages shipped without this and ran four phases of a redesign under a
// green suite; see docs/tui-2.0.md, divergence 19.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

func roles() theme.Roles { return theme.DarkRoles(true) }

// tree builds a directory to pick from, with a fixed mtime so the mtime
// column is the same on every machine and in every timezone.
func tree(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	stamp := time.Date(2026, 8, 26, 14, 22, 0, 0, time.UTC)

	for _, name := range names {
		path := filepath.Join(root, name)
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	return root + "/"
}

// open returns a picker showing dir. The clock is pinned for the test's
// lifetime, because the mtime column asks what "today" is.
func open(t *testing.T, dir string) Model {
	t.Helper()
	t.Cleanup(render.PinClock(time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)))
	m := New(roles())
	m.Open(dir)
	return m
}

// press feeds keystrokes through the real decoder, so a test cannot assert
// against a spelling the terminal never produces.
func press(t *testing.T, m Model, keys ...string) (Model, Action) {
	t.Helper()
	action := ActionNone
	for _, k := range keys {
		var last Action
		m, last = m.Update(decode(t, k))
		if last != ActionNone {
			action = last
		}
	}
	return m, action
}

func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func decode(t *testing.T, raw string) tea.KeyPressMsg {
	t.Helper()
	var d uv.EventDecoder
	n, event := d.Decode([]byte(raw))
	if n == 0 {
		t.Fatalf("decoder consumed nothing from %q", raw)
	}
	key, ok := event.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("%q decoded to %T, want a key press", raw, event)
	}
	return tea.KeyPressMsg(key)
}

const (
	keyUp        = "\x1b[A"
	keyDown      = "\x1b[B"
	keyLeft      = "\x1b[D"
	keyTab       = "\t"
	keyEnter     = "\r"
	keyEsc       = "\x1b"
	keyBackspace = "\x7f"
	keyCtrlT     = "\x14"
	keyCtrlU     = "\x15"
)

// TestAnImageIsStagedAsAPhotoAndEverythingElseAsADocument.
//
// The defect this component was built for. The prompt it replaces passed a
// hardcoded false for asPhoto, so Ctrl+T always attached as a document while
// Ctrl+V attached the very same image as a photo — two ways to attach one
// file that disagreed about what it was.
func TestAnImageIsStagedAsAPhotoAndEverythingElseAsADocument(t *testing.T) {
	dir := tree(t, "shot.png", "notes.txt")

	for _, tc := range []struct {
		name    string
		want    bool
		typedAs string
	}{
		{"shot.png", true, "photo"},
		{"notes.txt", false, "document"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := typeText(t, open(t, dir), tc.name)
			_, action := press(t, m, keyEnter)
			if action != ActionAttach {
				t.Fatalf("enter on %s gave %v, want ActionAttach", tc.name, action)
			}
			path, asPhoto, ok := m.Chosen()
			if !ok {
				t.Fatal("nothing was chosen")
			}
			if filepath.Base(path) != tc.name {
				t.Errorf("chose %q, want %s", path, tc.name)
			}
			if asPhoto != tc.want {
				t.Errorf("%s stages asPhoto=%v, want %v — it would send as the wrong kind",
					tc.name, asPhoto, tc.want)
			}
		})
	}
}

// TestTheSendModeToggleAppliesToImagesAndSaysSoOtherwise.
func TestTheSendModeToggleAppliesToImagesAndSaysSoOtherwise(t *testing.T) {
	dir := tree(t, "shot.png", "notes.txt")

	image := typeText(t, open(t, dir), "shot")
	if !image.AsPhoto() {
		t.Fatal("an image does not default to a photo")
	}
	image, _ = press(t, image, keyCtrlT)
	if image.AsPhoto() {
		t.Error("ctrl+t did not turn the photo into a document")
	}
	image, _ = press(t, image, keyCtrlT)
	if !image.AsPhoto() {
		t.Error("ctrl+t does not toggle back")
	}

	doc := typeText(t, open(t, dir), "notes")
	doc, _ = press(t, doc, keyCtrlT)
	if doc.AsPhoto() {
		t.Error("ctrl+t made a text file into a photo")
	}
	if hint := plain(doc.hintLine()); !strings.Contains(hint, "document only") {
		t.Errorf("the hint does not say the toggle is inert here: %q", hint)
	}
}

// TestTheSendModeBelongsToTheFileItWasSetOn. A toggle that outlived the
// cursor would send the next file the way the last one was asked for.
func TestTheSendModeBelongsToTheFileItWasSetOn(t *testing.T) {
	m := open(t, tree(t, "a.png", "b.png"))

	m, _ = press(t, m, keyCtrlT)
	if m.AsPhoto() {
		t.Fatal("precondition: ctrl+t did not flip the first image")
	}

	m, _ = press(t, m, keyDown)
	if !m.AsPhoto() {
		t.Error("the toggle followed the cursor onto a different file")
	}
}

// TestBackspaceDeletesACharacterAndThenGoesUp.
//
// One key, two readings that never overlap — which is what removes the need
// for ctrl+h. Outside the Kitty protocol a terminal sends 0x08 for both, so
// binding them to different things would work on some terminals and quietly
// do the wrong thing on the rest.
func TestBackspaceDeletesACharacterAndThenGoesUp(t *testing.T) {
	root := tree(t, "sub/inner.txt")
	m := open(t, root+"sub/")

	m = typeText(t, m, "inn")
	m, _ = press(t, m, keyBackspace)
	if _, tail := splitPath(m.Typed()); tail != "in" {
		t.Fatalf("backspace on a tail gave %q, want the tail shortened to %q", m.Typed(), "in")
	}

	m, _ = press(t, m, keyBackspace, keyBackspace)
	if _, tail := splitPath(m.Typed()); tail != "" {
		t.Fatalf("the tail is %q, want it emptied first", tail)
	}

	before := m.Typed()
	m, _ = press(t, m, keyBackspace)
	if m.Typed() == before {
		t.Fatal("backspace on an empty tail did nothing, so there is no way up")
	}
	if !strings.HasSuffix(m.Typed(), "/") {
		t.Errorf("went up to %q, want a directory", m.Typed())
	}
	if strings.Contains(m.Typed(), "sub") {
		t.Errorf("went up to %q, which is still inside sub/", m.Typed())
	}
}

// TestLeftGoesUpOneDirectory, the explicit spelling of the same move.
func TestLeftGoesUpOneDirectory(t *testing.T) {
	root := tree(t, "sub/inner.txt")
	m := open(t, root+"sub/")

	m, _ = press(t, m, keyLeft)
	if strings.Contains(m.Typed(), "sub") {
		t.Errorf("left left the path at %q", m.Typed())
	}
}

// TestGoingUpIsNotFencedAtHome. The spec floored backspace at "~/", which
// leaves no route to /etc/hosts and offers no other one.
func TestGoingUpIsNotFencedAtHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		t.Skip("no usable home directory")
	}

	m := open(t, home)
	if m.Typed() != "~/" {
		t.Fatalf("precondition: opened at %q, want the home directory collapsed to ~/", m.Typed())
	}

	m, _ = press(t, m, keyLeft)
	if m.Typed() == "~/" || strings.HasPrefix(m.Typed(), "~") {
		t.Fatalf("going up from ~/ stayed at %q — the filesystem above home is unreachable", m.Typed())
	}
}

// TestTabCompletesToTheCursoredEntry, and a directory keeps its separator so
// the next keystroke filters inside it rather than renaming it.
func TestTabCompletesToTheCursoredEntry(t *testing.T) {
	dir := tree(t, "patches/", "backoff.patch")

	file := typeText(t, open(t, dir), "back")
	file, _ = press(t, file, keyTab)
	if _, tail := splitPath(file.Typed()); tail != "backoff.patch" {
		t.Errorf("tab completed to %q, want backoff.patch", file.Typed())
	}

	folder := typeText(t, open(t, dir), "patc")
	folder, _ = press(t, folder, keyTab)
	if !strings.HasSuffix(folder.Typed(), "patches/") {
		t.Errorf("tab completed a directory to %q, want a trailing separator", folder.Typed())
	}
}

// TestEnterOpensADirectoryAndAttachesAFile.
func TestEnterOpensADirectoryAndAttachesAFile(t *testing.T) {
	dir := tree(t, "patches/inner.txt", "backoff.patch")

	folder := typeText(t, open(t, dir), "patc")
	folder, action := press(t, folder, keyEnter)
	if action != ActionNone {
		t.Errorf("enter on a directory gave %v, want it to descend", action)
	}
	if !strings.HasSuffix(folder.Typed(), "patches/") {
		t.Errorf("enter left the path at %q", folder.Typed())
	}
	if names := names(folder.Matches()); len(names) != 1 || names[0] != "inner.txt" {
		t.Errorf("after descending the listing is %v, want the directory's contents", names)
	}

	file := typeText(t, open(t, dir), "back")
	_, action = press(t, file, keyEnter)
	if action != ActionAttach {
		t.Errorf("enter on a file gave %v, want ActionAttach", action)
	}

	// The typed path naming a directory EXACTLY, which is the case the
	// "a whole typed path wins" rule has to get right in both directions:
	// it must not attach a directory just because the path resolves.
	whole := typeText(t, open(t, dir), "patches")
	whole, action = press(t, whole, keyEnter)
	if action != ActionNone {
		t.Errorf("enter on a fully typed directory gave %v, want it to descend", action)
	}
	if !strings.HasSuffix(whole.Typed(), "patches/") {
		t.Errorf("enter left the path at %q", whole.Typed())
	}
}

// TestEnterWithNothingTypedActsOnTheCursor.
//
// The typed path wins over the cursor only when there is a tail to it. With
// none it IS the directory being browsed — it names a real directory on
// every keystroke — so a rule that consulted it first would make Enter
// re-enter the current folder forever and the cursor would never be
// reachable at all.
func TestEnterWithNothingTypedActsOnTheCursor(t *testing.T) {
	m := open(t, tree(t, "a.txt", "b.txt"))
	if _, tail := splitPath(m.Typed()); tail != "" {
		t.Fatalf("precondition: opened with %q already typed", tail)
	}

	m, action := press(t, m, keyDown, keyEnter)
	if action != ActionAttach {
		t.Fatalf("enter gave %v — the cursor was never reachable", action)
	}
	path, _, ok := m.Chosen()
	if !ok || filepath.Base(path) != "b.txt" {
		t.Errorf("chose %q, want the cursored b.txt", path)
	}
}

// TestAFilenameContainingAnyLetterCanBeTyped.
//
// Re-homed from the prompt dialog this replaced, whose own test made the
// point: the surface that takes a path owns every printable, and paths
// contain j's and k's. Here it also covers the arrows staying navigation.
func TestAFilenameContainingAnyLetterCanBeTyped(t *testing.T) {
	dir := tree(t, "jk.txt", "other.txt")

	m := typeText(t, open(t, dir), "jk.txt")
	if _, tail := splitPath(m.Typed()); tail != "jk.txt" {
		t.Fatalf("typed path is %q — j and k must be text here", m.Typed())
	}

	// A space, too: msg.String() spells one "space", so a path with a space
	// in it would otherwise arrive with the separator dropped.
	spaced := typeText(t, open(t, tree(t, "my file.txt")), "my file")
	if _, tail := splitPath(spaced.Typed()); tail != "my file" {
		t.Errorf("typed path is %q, want the space kept", spaced.Typed())
	}
	if got := names(spaced.Matches()); len(got) != 1 {
		t.Errorf("a name with a space matched %v", got)
	}
}

// TestAWholeTypedPathWinsOverTheHighlightedRow.
//
// The data-loss regression, re-homed. Somebody who typed or pasted a whole
// path has said which file they mean, and attaching the highlighted row
// instead would attach a different one.
func TestAWholeTypedPathWinsOverTheHighlightedRow(t *testing.T) {
	dir := tree(t, "aaa.txt", "zzz.txt")

	m := typeText(t, open(t, dir), "zzz.txt")
	_, action := press(t, m, keyEnter)
	if action != ActionAttach {
		t.Fatalf("enter on a complete path gave %v", action)
	}
	path, _, ok := m.Chosen()
	if !ok || filepath.Base(path) != "zzz.txt" {
		t.Errorf("chose %q, want zzz.txt", path)
	}
}

// TestDotfilesAreHiddenUntilYouAskForThem, the way a shell hides them: a
// leading dot in the tail is the only signal available that somebody wants
// to see them, and without the rule a home directory is unusable.
func TestDotfilesAreHiddenUntilYouAskForThem(t *testing.T) {
	dir := tree(t, ".hidden", "visible.txt")

	m := open(t, dir)
	if got := names(m.Matches()); len(got) != 1 || got[0] != "visible.txt" {
		t.Errorf("the listing is %v, want the dotfile hidden", got)
	}

	m = typeText(t, m, ".")
	if got := names(m.Matches()); len(got) != 1 || got[0] != ".hidden" {
		t.Errorf("after typing a dot the listing is %v, want the dotfile", got)
	}
}

// TestMatchingIsCaseInsensitive. The default macOS filesystem is, so a
// case-sensitive filter would hide a file the reader can open by that exact
// name everywhere else on their machine.
func TestMatchingIsCaseInsensitive(t *testing.T) {
	m := typeText(t, open(t, tree(t, "Downloads/")), "down")
	if got := names(m.Matches()); len(got) != 1 {
		t.Errorf("%q matched %v, want Downloads/", "down", got)
	}
}

// TestDirectoriesSortFirst, then names, so the way through a tree is always
// at the top of the listing.
func TestDirectoriesSortFirst(t *testing.T) {
	// Mixed case on purpose. os.ReadDir already returns names in BYTE
	// order, which puts every capital ahead of every lowercase — so a
	// listing that looked sorted would still put Zeta above alpha.
	m := open(t, tree(t, "Zeta/", "alpha.txt", "beta/"))
	got := names(m.Matches())
	want := []string{"beta", "Zeta", "alpha.txt"}
	if len(got) != len(want) {
		t.Fatalf("listing is %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("listing is %v, want %v", got, want)
		}
	}
}

// TestCtrlUClearsThePath.
func TestCtrlUClearsThePath(t *testing.T) {
	m := typeText(t, open(t, tree(t, "a.txt")), "a.t")
	m, _ = press(t, m, keyCtrlU)
	if m.Typed() != "" {
		t.Errorf("ctrl+u left %q", m.Typed())
	}
}

// TestEscapeCancels, staging nothing.
func TestEscapeCancels(t *testing.T) {
	m := open(t, tree(t, "a.txt"))
	if _, action := press(t, m, keyEsc); action != ActionCancel {
		t.Errorf("esc gave %v, want ActionCancel", action)
	}
}

// TestAClosedPickerIgnoresKeys. Every overlay in this app owns the keyboard
// only while it is up; one that acted on a key after closing would act on a
// key aimed at the panel behind it.
func TestAClosedPickerIgnoresKeys(t *testing.T) {
	m := open(t, tree(t, "a.txt", "b.txt"))
	m.Close()

	before := m.Typed()
	m, action := press(t, m, keyDown, keyTab, keyEnter)
	if action != ActionNone {
		t.Errorf("a closed picker returned %v", action)
	}
	if m.Typed() != before {
		t.Errorf("a closed picker took a key: %q became %q", before, m.Typed())
	}
}

// TestTheDirectorySurvivesClosing but the half-typed tail does not.
// Attaching three files from one folder should not mean walking there three
// times; resuming a filter nobody remembers typing is a different thing.
func TestTheDirectorySurvivesClosing(t *testing.T) {
	root := tree(t, "sub/inner.txt")

	m := open(t, root)
	m = typeText(t, m, "sub/in")
	m.Close()
	m.Open(root)

	if _, tail := splitPath(m.Typed()); tail != "" {
		t.Errorf("reopened with the tail %q still typed", tail)
	}
	if !strings.HasSuffix(m.Typed(), "sub/") {
		t.Errorf("reopened at %q, want the directory that was open", m.Typed())
	}
}

// TestTheCursorResetsOnEveryEdit. After another character the previously
// highlighted row is usually gone, and a stale index would attach a file the
// reader is no longer looking at.
func TestTheCursorResetsOnEveryEdit(t *testing.T) {
	m := open(t, tree(t, "a.txt", "b.txt", "c.txt"))

	m, _ = press(t, m, keyDown, keyDown)
	if m.cursor == 0 {
		t.Fatal("precondition: the cursor did not move")
	}
	m = typeText(t, m, "a")
	if m.cursor != 0 {
		t.Errorf("the cursor stayed at %d after an edit", m.cursor)
	}
}

// TestADirectoryItCannotReadSaysNothingRatherThanZero. A directory you have
// no permission to list is a real thing to meet in a home directory, and
// "0 items" would be a lie about it.
func TestADirectoryItCannotReadSaysNothingRatherThanZero(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything, so there is no unreadable directory")
	}
	root := tree(t, "locked/")
	locked := filepath.Join(strings.TrimSuffix(root, "/"), "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Skipf("cannot make a directory unreadable: %v", err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	entries := open(t, root).Matches()
	if len(entries) != 1 {
		t.Fatalf("listing is %v", names(entries))
	}
	if entries[0].Items != -1 {
		t.Errorf("an unreadable directory reports %d items, want it unknown", entries[0].Items)
	}
	if got := formatSize(entries[0]); got != "" {
		t.Errorf("its size column reads %q, want nothing at all", got)
	}
}

// TestGoingUpFromAHalfTypedNameStaysInTheDirectory.
//
// The tail is what you were doing in this directory, so it is what the first
// move up drops. Leaving for the parent while a name is still typed would
// throw away two things at once and leave the reader somewhere they did not
// ask to be.
func TestGoingUpFromAHalfTypedNameStaysInTheDirectory(t *testing.T) {
	root := tree(t, "sub/inner.txt")
	m := typeText(t, open(t, root+"sub/"), "inn")

	m, _ = press(t, m, keyLeft)
	if !strings.HasSuffix(m.Typed(), "sub/") {
		t.Errorf("left from a half-typed name went to %q, want to stay in sub/", m.Typed())
	}
}

// TestAnEmptyPathIsTheWorkingDirectory, which is what makes a bare relative
// prefix behave the way it does in a shell — and what makes ctrl+u a way to
// start over rather than a way to break the listing.
func TestAnEmptyPathIsTheWorkingDirectory(t *testing.T) {
	m, _ := press(t, open(t, tree(t, "a.txt")), keyCtrlU)

	if m.Typed() != "" {
		t.Fatalf("ctrl+u left %q", m.Typed())
	}
	if m.listErr {
		t.Fatal("an empty path reports no such directory")
	}
	if len(m.Matches()) == 0 {
		t.Error("an empty path lists nothing; it should list the working directory")
	}
}

// TestALongPathKeepsItsEnd. The tail is what is being typed and what the
// suggestion continues, so a path too long for the row scrolls its leading
// directories out of sight rather than the part under the cursor.
func TestALongPathKeepsItsEnd(t *testing.T) {
	// Distinctly named segments, so which END survived the cut is visible
	// rather than merely how much of it did.
	const deep = "firstsegment-aaaaaaaaaaaaaaaaaaaa/" +
		"middlesegment-bbbbbbbbbbbbbbbbbb/" +
		"lastsegment-cccccccccccccccccccc/"
	root := tree(t, deep+"target.txt")

	m := typeText(t, open(t, root+deep), "targ")
	row := plain(m.promptLine())

	if !strings.Contains(row, "targ") {
		t.Errorf("the typed tail was cut off a long path: %q", row)
	}
	if !strings.Contains(row, "█") {
		t.Errorf("the cursor was cut off a long path: %q", row)
	}
	if !strings.Contains(row, "lastsegment") {
		t.Errorf("the directory nearest the cursor was cut: %q", row)
	}
	if strings.Contains(row, "firstsegment") {
		t.Errorf("the path was cut from its end rather than its front: %q", row)
	}
}

// TestALongNameDoesNotPushTheColumnsOffTheRow. The row is fitted to the
// pane either way, so an unbudgeted name does not shear the frame — it
// silently costs the size and mtime instead, which is the kind of loss a
// width assertion alone cannot see.
func TestALongNameDoesNotPushTheColumnsOffTheRow(t *testing.T) {
	m := open(t, tree(t, strings.Repeat("long", 30)+".txt"))
	entries := m.Matches()
	if len(entries) != 1 {
		t.Fatalf("listing is %v", names(entries))
	}

	row := plain(m.entryLine(entries[0], false))
	if !strings.Contains(row, "26 Aug") {
		t.Errorf("a long name pushed the mtime off the row: %q", row)
	}
	if !strings.Contains(row, "…") {
		t.Errorf("a long name was not marked as cut: %q", row)
	}
}

func names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}
