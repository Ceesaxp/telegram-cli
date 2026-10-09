package attach

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/render"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	uv "github.com/charmbracelet/ultraviolet"
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

// TestEnterAttachesTheFileTheWayTheToggleAskedFor. Enter lands the path on
// the cursored row, and that reload used to take the send mode with it: the
// state row said "document", the reader pressed Enter, and the image was
// staged as a photo and recompressed.
func TestEnterAttachesTheFileTheWayTheToggleAskedFor(t *testing.T) {
	m := open(t, tree(t, "shot.png"))
	if !m.AsPhoto() {
		t.Fatal("precondition: the image does not default to a photo")
	}

	m, _ = press(t, m, keyCtrlT)
	if m.AsPhoto() {
		t.Fatal("precondition: ctrl+t did not ask for a document")
	}

	m, action := press(t, m, keyEnter)
	if action != ActionAttach {
		t.Fatalf("enter gave action %v, want ActionAttach", action)
	}
	path, asPhoto, ok := m.Chosen()
	if !ok {
		t.Fatal("enter attached nothing")
	}
	if !strings.HasSuffix(path, "shot.png") {
		t.Fatalf("enter chose %q, want shot.png", path)
	}
	if asPhoto {
		t.Error("enter staged the image as a photo although ctrl+t asked for the original bytes")
	}
}

// TestCompletingTheNameKeepsTheSendMode. Tab extends the path onto the row
// the cursor is already on, so it is not a move and must not clear the mode.
func TestCompletingTheNameKeepsTheSendMode(t *testing.T) {
	m := open(t, tree(t, "shot.png"))

	m, _ = press(t, m, keyCtrlT)
	m, _ = press(t, m, keyTab)
	if _, tail := splitPath(m.Typed()); tail != "shot.png" {
		t.Fatalf("precondition: tab completed the path to %q", m.Typed())
	}
	if m.AsPhoto() {
		t.Error("tab completing the cursored name lost the document mode set on that very file")
	}
}

// TestAnEditThatLandsOnAnotherFileLosesTheSendMode, even though the cursor
// index never changed.
func TestAnEditThatLandsOnAnotherFileLosesTheSendMode(t *testing.T) {
	m := open(t, tree(t, "a.png", "b.png"))

	m, _ = press(t, m, keyCtrlT)
	if m.AsPhoto() {
		t.Fatal("precondition: ctrl+t did not flip the first image")
	}

	m = typeText(t, m, "b")
	if entry, _ := m.Selected(); entry.Name != "b.png" {
		t.Fatalf("precondition: the cursor is on %q, want b.png", entry.Name)
	}
	if !m.AsPhoto() {
		t.Error("the toggle carried onto another file that merely sits at the same cursor index")
	}
}

// TestTheSendModeIsLostOnTheSameNameInAnotherDirectory. The toggle belongs
// to a file, and two files sharing a name are still two files — which is why
// the cursored row is identified as (directory, name) and not by name.
func TestTheSendModeIsLostOnTheSameNameInAnotherDirectory(t *testing.T) {
	root := tree(t, "shot.png", "sub/shot.png")
	m := typeText(t, open(t, root), "shot")
	if entry, _ := m.Selected(); entry.Name != "shot.png" {
		t.Fatalf("precondition: the cursor is on %q, want shot.png", entry.Name)
	}

	m, _ = press(t, m, keyCtrlT)
	if m.AsPhoto() {
		t.Fatal("precondition: ctrl+t did not ask for a document")
	}

	m = m.Paste(root + "sub")
	if entry, _ := m.Selected(); entry.Name != "shot.png" {
		t.Fatalf("precondition: sub/ opens on %q, want its own shot.png", entry.Name)
	}
	if !m.AsPhoto() {
		t.Error("the toggle followed into another directory onto a file that merely shares the name")
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
	if m.listErr != nil {
		t.Fatalf("an empty path reports %v", m.listErr)
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

// TestAnExactlyTypedNameTakesTheCursor even when something else sorts above
// it.
//
// Directories sort first, so typing "notes.txt" in a directory that also
// holds a folder called "notes.txt.d" leaves the FOLDER highlighted: it
// matches the prefix filter and it sorts above the file. Everything
// downstream reads the cursor, so the state row would describe the folder
// and Enter would descend into it — a reader who typed a filename in full
// getting a directory instead.
func TestAnExactlyTypedNameTakesTheCursor(t *testing.T) {
	m := typeText(t, open(t, tree(t, "notes.txt", "notes.txt.d/")), "notes.txt")

	if got := len(m.Matches()); got != 2 {
		t.Fatalf("precondition: %d matches, want the file and the folder", got)
	}
	if first := m.Matches()[0]; !first.Dir {
		t.Fatalf("precondition: %q sorts first, so the cursor is not being moved", first.Name)
	}

	selected, ok := m.Selected()
	if !ok || selected.Name != "notes.txt" || selected.Dir {
		t.Fatalf("the cursor is on %q (dir=%v), want the file that was typed",
			selected.Name, selected.Dir)
	}

	_, action := press(t, m, keyEnter)
	if action != ActionAttach {
		t.Errorf("enter gave %v — a fully typed filename descended into a folder", action)
	}
}

// TestAnExactlyTypedNameOutranksTheCaseInsensitiveFilter.
//
// The filter is case-insensitive on purpose, but that leniency and an
// exactly typed path can name different files: on a case-sensitive
// filesystem holding both Foo.txt and foo.txt, typing "foo.txt" matches
// both and Foo.txt sorts first. Everything downstream reads the cursor, so
// the picker would show, describe and attach a file the reader did not
// type — the one substitution a file picker must never make.
func TestAnExactlyTypedNameOutranksTheCaseInsensitiveFilter(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Foo.txt", "foo.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Skip("this filesystem is case-insensitive, so the two cannot collide")
	}

	m := typeText(t, open(t, dir+"/"), "foo.txt")

	if got := len(m.Matches()); got != 2 {
		t.Fatalf("precondition: %d matches, want both spellings", got)
	}
	selected, ok := m.Selected()
	if !ok || selected.Name != "foo.txt" {
		t.Errorf("the cursor is on %q, want the name that was typed", selected.Name)
	}

	_, action := press(t, m, keyEnter)
	if action != ActionAttach {
		t.Fatalf("enter gave %v", action)
	}
	path, _, _ := m.Chosen()
	if filepath.Base(path) != "foo.txt" {
		t.Errorf("typed foo.txt and staged %s — a different file from the one "+
			"the reader named", filepath.Base(path))
	}
}

// TestASymlinkedDirectoryIsSomewhereToGo.
//
// os.DirEntry describes the LINK, so IsDir is false for a symlink to a
// directory. Left at that, an arrowed-to symlinked folder is drawn as a
// file and Enter stages the link as an attachment, which the uploader then
// rejects — while typing the same name in full worked, because enter uses
// os.Stat. Two answers to one question is the bug.
func TestASymlinkedDirectoryIsSomewhereToGo(t *testing.T) {
	root := tree(t, "real/inner.txt", "loose.txt")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skipf("this filesystem will not take a symlink: %v", err)
	}

	m := typeText(t, open(t, root), "link")
	entry, ok := m.Selected()
	if !ok {
		t.Fatal("the symlink is not in the listing")
	}
	if !entry.Dir {
		t.Fatal("a symlink to a directory is drawn as a file, so enter would attach it")
	}

	m, action := press(t, m, keyEnter)
	if action != ActionNone {
		t.Errorf("enter on a symlinked directory gave %v, want it to descend", action)
	}
	if got := names(m.Matches()); len(got) != 1 || got[0] != "inner.txt" {
		t.Errorf("after descending the listing is %v, want the target's contents", got)
	}
}

// TestABrokenSymlinkIsNotADirectory. Nothing can be entered, so calling it
// one would offer a way in that goes nowhere.
func TestABrokenSymlinkIsNotADirectory(t *testing.T) {
	root := tree(t, "a.txt")
	if err := os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "dangling")); err != nil {
		t.Skipf("this filesystem will not take a symlink: %v", err)
	}

	m := typeText(t, open(t, root), "dangling")
	entry, ok := m.Selected()
	if !ok {
		t.Fatal("the broken link is not in the listing")
	}
	if entry.Dir {
		t.Error("a broken symlink is offered as a directory to enter")
	}
}

// TestTIFFIsNotOfferedAsAPhoto.
//
// Telegram's InputMediaUploadedPhoto rejects TIFF, which is why the
// clipboard path has always excluded it. This package asks that path
// rather than keeping a second list: two lists of image extensions are two
// things to keep in step, and the one that drifts is the one whose failure
// only shows up at send time.
func TestTIFFIsNotOfferedAsAPhoto(t *testing.T) {
	m := open(t, tree(t, "scan.tiff", "shot.png"))

	for _, entry := range m.Matches() {
		switch entry.Name {
		case "scan.tiff":
			if entry.Image {
				t.Error("a TIFF is offered as a photo; the send would fail")
			}
			// It is still a picture to whoever is looking for one.
			if got := kindOf(entry); got != "image" {
				t.Errorf("a TIFF is drawn as %q, want it to look like a picture", got)
			}
		case "shot.png":
			if !entry.Image {
				t.Error("a PNG is not offered as a photo")
			}
		}
	}

	if IsImage("x.tiff") || IsImage("x.tif") {
		t.Error("IsImage accepts TIFF, disagreeing with the send path")
	}
	if !IsImage("x.png") {
		t.Error("IsImage rejects PNG")
	}
}

// TestTheCursorStaysInsideTheDrawnWindow.
//
// The listing draws six rows. A cursor that could walk past them was a
// cursor on a file the reader could not see, with the selection marker
// gone from the surface entirely — and enter would still attach it.
func TestTheCursorStaysInsideTheDrawnWindow(t *testing.T) {
	files := make([]string, 0, 10)
	for _, n := range "abcdefghij" {
		files = append(files, string(n)+".txt")
	}
	m := open(t, tree(t, files...))

	for range 9 {
		m, _ = press(t, m, keyDown)
	}
	selected, ok := m.Selected()
	if !ok {
		t.Fatal("nothing is selected at the bottom of the listing")
	}
	if selected.Name != "j.txt" {
		t.Fatalf("the cursor stopped on %s, want the last entry", selected.Name)
	}

	rows, _ := m.Window()
	found := false
	for _, row := range rows {
		if row.Name == selected.Name {
			found = true
		}
	}
	if !found {
		t.Errorf("the cursored %s is not among the %d drawn rows", selected.Name, len(rows))
	}
	if !strings.Contains(plain(m.View()), "j.txt") {
		t.Error("the cursored entry is not on screen")
	}

	// And back up, so the window follows in both directions.
	for range 9 {
		m, _ = press(t, m, keyUp)
	}
	if _, top := m.Window(); top != 0 {
		t.Errorf("the window stayed at %d after returning to the top", top)
	}
}

// TestOpeningADirectoryCostsOneReadPlusWhatIsDrawn.
//
// Counting every subdirectory up front made opening a home directory cost
// one whole extra directory read per child, all of it synchronous on Bubble
// Tea's update path — a network mount or a directory of large children
// froze the client. Counts are gathered for the six rows on screen and
// nothing else.
func TestOpeningADirectoryCostsOneReadPlusWhatIsDrawn(t *testing.T) {
	names := make([]string, 0, 12)
	for _, n := range "abcdefghijkl" {
		names = append(names, string(n)+"/")
	}
	m := open(t, tree(t, names...))

	counted := 0
	for _, entry := range m.Matches() {
		if entry.counted {
			counted++
		}
	}
	if counted > maxRows {
		t.Errorf("%d of %d directories were counted on open; only the %d drawn "+
			"rows should be", counted, len(m.Matches()), maxRows)
	}
	if counted == 0 {
		t.Error("no directory was counted, so the size column is empty on open")
	}

	// Scrolling brings the rest into view, and counts them then.
	for range 11 {
		m, _ = press(t, m, keyDown)
	}
	last, _ := m.Selected()
	if !last.counted {
		t.Error("a directory scrolled into view was never counted")
	}
}

// TestACountIsTakenOnceAndKept.
//
// The count is the expensive half — a whole extra directory read — and the
// window is recomputed on every keystroke and every move. Without the memo
// the same six directories are re-read each time the cursor passes them,
// which is the cost this was supposed to have removed.
//
// Observable only as staleness: the count is a snapshot from when the row
// was first drawn, and that is the guarantee being pinned.
func TestACountIsTakenOnceAndKept(t *testing.T) {
	root := tree(t, "sub/one.txt")
	m := open(t, root)

	first, ok := m.Selected()
	if !ok || !first.Dir || !first.counted {
		t.Fatalf("precondition: selected %+v", first)
	}
	if first.Items != 1 {
		t.Fatalf("precondition: counted %d items, want 1", first.Items)
	}

	// Change what is in it, then walk the cursor over the row again.
	if err := os.WriteFile(filepath.Join(strings.TrimSuffix(root, "/"), "sub", "two.txt"),
		[]byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = press(t, m, keyDown, keyUp)

	again, _ := m.Selected()
	if again.Items != 1 {
		t.Errorf("the directory was counted again (now %d items); the memo is "+
			"not holding and every move re-reads the disk", again.Items)
	}
}

// TestAFileThatLandedWhileClosedIsThereOnReopen.
//
// The reported defect. The listing is cached on (directory, dotfiles) and
// Open restores the path to that same directory, so every one of those
// keys matched and the picker reused the listing it read on the very first
// Ctrl+T — a file downloaded into ~/Downloads afterwards was invisible for
// the life of the process, with the state row saying "no match" about a
// file sitting right there.
func TestAFileThatLandedWhileClosedIsThereOnReopen(t *testing.T) {
	root := tree(t, "old.txt")
	m := open(t, root)
	if got := names(m.Matches()); len(got) != 1 {
		t.Fatalf("precondition: the listing is %v", got)
	}

	m.Close()
	land(t, root, "new.pdf")
	m.Open(root)

	if got := names(m.Matches()); !slices.Contains(got, "new.pdf") {
		t.Errorf("reopened on %v; a file that landed while the picker was "+
			"closed is not in the listing", got)
	}
}

// land writes a file into an already-open directory, which is what a
// download finishing looks like to the picker.
func land(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(strings.TrimSuffix(dir, "/"), name)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReopeningAfterTheCursorMovedDoesNotPanic.
//
// Close keeps the match list and the cursor, and Open drops the listing they
// index — so Selected held a cursor that was in range for the match list and
// out of range for the entries it points into. Bounds-checked at the
// dereference rather than by having Open reset the cursor too: the order
// reload reads the cursored row in is then one more thing every caller has to
// get right, and the caller that gets it wrong crashes the client.
func TestReopeningAfterTheCursorMovedDoesNotPanic(t *testing.T) {
	root := tree(t, "a.txt", "b.txt", "c.txt")
	m := open(t, root)

	m, _ = press(t, m, keyDown)
	if m.cursor == 0 {
		t.Fatal("precondition: the cursor did not move off the first row")
	}

	m.Close()
	m.Open(root)

	if m.cursor != 0 {
		t.Errorf("reopened with the cursor at %d, want the top of the listing", m.cursor)
	}
	if entry, ok := m.Selected(); !ok || entry.Name != "a.txt" {
		t.Errorf("reopened on %q, want the first row", entry.Name)
	}
}

// TestAFileThatLandsWhileOpenMatchesAsItsNameIsTyped.
//
// The other half of the reported defect, and the one the reader meets as a
// lie: the download finishes while the picker is open, they type the name,
// and the state row says "no match" about a file that is right there. The
// listing costs no disk per keystroke on purpose, so nothing about typing
// would ever re-read it — which is right while something matches and wrong
// the moment nothing does.
func TestAFileThatLandsWhileOpenMatchesAsItsNameIsTyped(t *testing.T) {
	root := tree(t, "old.txt")
	m := open(t, root)

	land(t, root, "new.pdf")
	m = typeText(t, m, "new")

	if got := names(m.Matches()); !slices.Contains(got, "new.pdf") {
		t.Errorf("typing the name of a file that just landed matched %v", got)
	}
	if state := plain(m.stateLine()); strings.Contains(state, "no match") {
		t.Errorf("the state row says %q about a file that is right there", strings.TrimSpace(state))
	}
}

// TestZeroMatchesLooksAgainOnceAndThenStops.
//
// The look-again is for a listing that has gone stale, and a listing can only
// be stale once — so it is budgeted per listing rather than taken per
// keystroke. Unbudgeted, every further character typed into a directory where
// nothing matches costs a whole directory read, which in a directory of
// thousands of files is the cost the per-keystroke cache exists to avoid.
//
// Observable only as staleness, the way the item counts are: after the budget
// is spent, a file that lands cannot be seen until something re-lists.
func TestZeroMatchesLooksAgainOnceAndThenStops(t *testing.T) {
	root := tree(t, "old.txt")
	m := open(t, root)

	m = typeText(t, m, "z")
	if !m.probed {
		t.Fatal("precondition: nothing matched and the listing was not looked at again")
	}

	land(t, root, "zz.pdf")
	m = typeText(t, m, "z")

	if got := names(m.Matches()); len(got) != 0 {
		t.Errorf("a second zero-match keystroke re-read the directory and matched %v; "+
			"the look-again is not budgeted, so typing in a large directory pays "+
			"for a read per character", got)
	}
}

// TestTheLookAgainBudgetBelongsToOneListing.
//
// The budget is spent on the listing it was taken against, so walking into
// another directory gets its own. Carried over, a reader who mistyped a name
// upstairs would silently lose the look-again everywhere they went next —
// and the defect this exists for is back in the directory they are standing
// in now.
func TestTheLookAgainBudgetBelongsToOneListing(t *testing.T) {
	root := tree(t, "old.txt", "sub/keep.txt")
	m := open(t, root)

	// Spend it here, where nothing matches.
	m = typeText(t, m, "zz")
	if !m.probed {
		t.Fatal("precondition: the look-again was not spent in the first directory")
	}

	m, _ = press(t, m, keyBackspace, keyBackspace)
	m = typeText(t, m, "sub/")
	if got := names(m.Matches()); len(got) != 1 || got[0] != "keep.txt" {
		t.Fatalf("precondition: sub/ lists %v", got)
	}

	land(t, root+"sub", "new.pdf")
	m = typeText(t, m, "n")

	if got := names(m.Matches()); !slices.Contains(got, "new.pdf") {
		t.Errorf("typing in sub/ matched %v; the look-again was already spent "+
			"upstairs, so the stale listing was never re-read", got)
	}
}

// TestEnterOnAFullyTypedPathAttachesAFileTheListingNeverSaw.
//
// Enter stats the typed path, so it knows the file exists — but Chosen reads
// the CURSOR, and a file missing from the listing has no row to put the
// cursor on. The keypress reported ActionAttach, the app asked Chosen, Chosen
// said no, and Ctrl+T did nothing at all for a file the reader had named in
// full.
//
// The look-again cannot cover this on its own: a mistyped name spends it
// before the file even lands, and backspacing over the typo re-reads nothing
// — which is why the typo is cleared with backspace here and not with ctrl+u,
// whose empty path changes the cache key and would re-list for free.
func TestEnterOnAFullyTypedPathAttachesAFileTheListingNeverSaw(t *testing.T) {
	root := tree(t, "old.txt")
	m := open(t, root)

	m = typeText(t, m, "nw")
	if !m.probed {
		t.Fatal("precondition: the typo did not spend the look-again")
	}

	land(t, root, "new.pdf")
	m, _ = press(t, m, keyBackspace, keyBackspace)
	m = typeText(t, m, "new.pdf")
	if got := names(m.Matches()); len(got) != 0 {
		t.Fatalf("precondition: the listing is no longer stale, it matched %v", got)
	}

	m, action := press(t, m, keyEnter)
	if action != ActionAttach {
		t.Fatalf("enter on a fully typed path gave %v, want ActionAttach", action)
	}
	path, _, ok := m.Chosen()
	if !ok {
		t.Fatal("enter promised an attachment and Chosen has nothing to give; " +
			"the keypress silently did nothing")
	}
	if filepath.Base(path) != "new.pdf" {
		t.Errorf("attached %q, want new.pdf", path)
	}
}

// TestAnImageTypedInFullIsStillAPhotoAfterTheReRead.
//
// The defect this component was built for, guarded on the path that now
// re-reads the directory before it answers: an image attached by typing its
// name in full has to arrive as a photo, because Ctrl+V sends the same file
// that way and two ways to attach one file must not disagree about what it
// is.
func TestAnImageTypedInFullIsStillAPhotoAfterTheReRead(t *testing.T) {
	root := tree(t, "old.txt")
	m := open(t, root)

	m = typeText(t, m, "zz")
	land(t, root, "shot.png")
	m, _ = press(t, m, keyBackspace, keyBackspace)
	m = typeText(t, m, "shot.png")

	m, action := press(t, m, keyEnter)
	if action != ActionAttach {
		t.Fatalf("enter gave %v, want ActionAttach", action)
	}
	path, asPhoto, ok := m.Chosen()
	if !ok || filepath.Base(path) != "shot.png" {
		t.Fatalf("attached %q", path)
	}
	if !asPhoto {
		t.Error("an image named in full was staged as a document; Ctrl+V would " +
			"have sent the same file as a photo")
	}
}

// TestEnterOnAFullyTypedPathKeepsTheSendMode.
//
// The toggle belongs to the file it was pressed on, and the file it was
// pressed on is the one being attached. The re-read Enter now does swaps the
// listing under the match list, so the cursored row has to be identified
// BEFORE the swap — a file landing in the meantime shifts every index, and an
// identity read afterwards names whatever row the stale index now points at.
// The reader would have asked for the original bytes and been sent a
// recompressed photo.
func TestEnterOnAFullyTypedPathKeepsTheSendMode(t *testing.T) {
	root := tree(t, "shot.png")
	m := typeText(t, open(t, root), "shot.png")

	m, _ = press(t, m, keyCtrlT)
	if m.AsPhoto() {
		t.Fatal("precondition: ctrl+t did not ask for the original bytes")
	}

	// Something lands ahead of it in the listing, so the index the match
	// list holds now points at a different file.
	land(t, root, "a.txt")

	m, action := press(t, m, keyEnter)
	if action != ActionAttach {
		t.Fatalf("enter gave %v, want ActionAttach", action)
	}
	path, asPhoto, ok := m.Chosen()
	if !ok || filepath.Base(path) != "shot.png" {
		t.Fatalf("attached %q, want shot.png", path)
	}
	if asPhoto {
		t.Error("the re-read lost the document mode set on the file being attached; " +
			"the image is recompressed after the reader asked for the original bytes")
	}
}

// TestAFullyTypedNameInTheWrongCaseLandsOnTheFileAndNotItsNeighbour.
//
// The filter is case-insensitive, so a name typed in the wrong case matches
// everything the right case would — including a DIRECTORY whose name merely
// starts with it, which sorts first and takes the cursor. The byte-exact rule
// misses by a letter's case, so a reader on a case-insensitive filesystem who
// typed a real path in full got a folder: Enter found nothing to attach and
// navigated into it instead, erasing the path they had typed.
//
// A case-insensitive exact match therefore outranks the first prefix match,
// and byte-exact outranks both — see match.
func TestAFullyTypedNameInTheWrongCaseLandsOnTheFileAndNotItsNeighbour(t *testing.T) {
	root := tree(t, "doc.pdf", "doc.pdf_old/")
	m := typeText(t, open(t, root), "Doc.pdf")

	if first := m.Matches()[0]; !first.Dir {
		t.Fatalf("precondition: %q sorts first, so the cursor is not being moved", first.Name)
	}

	m, action := press(t, m, keyEnter)
	if action != ActionAttach {
		t.Fatalf("enter gave %v, want ActionAttach", action)
	}
	path, _, ok := m.Chosen()
	if !ok || filepath.Base(path) != "doc.pdf" {
		t.Errorf("attached %q, want the doc.pdf that was typed", path)
	}
	if strings.HasSuffix(m.Typed(), "_old/") {
		t.Errorf("the path was replaced with %q — enter navigated into an "+
			"unrelated directory and erased what the reader typed", m.Typed())
	}
}

// TestATypedPathThatResolvesToNoRowDoesNothing.
//
// The one case where the typed path stats fine and no row can be found for
// it: a name stored decomposed and typed composed. The filesystem resolves
// both spellings, the filter compares bytes, so the file the reader named is
// not in the match list at all — while a neighbour whose name merely starts
// with the composed spelling is.
//
// Doing nothing is the backstop. Acting on the cursor instead navigated into
// that neighbour and erased the path the reader had typed, which is worse
// than the silence it was replacing: the keypress did something, and the
// something was wrong. Normalising the comparison is a separate defect; this
// pins what happens until it is fixed.
func TestATypedPathThatResolvesToNoRowDoesNothing(t *testing.T) {
	const (
		decomposed = "café.txt"     // what is on disk
		composed   = "café.txt"      // what gets typed
		neighbour  = "café.txt_old/" // a directory sorting ahead of it
	)
	root := tree(t, decomposed, neighbour)
	if _, err := os.Stat(filepath.Join(strings.TrimSuffix(root, "/"), composed)); err != nil {
		t.Skip("this filesystem is normalization-sensitive, so the typed path cannot stat")
	}

	m := typeText(t, open(t, root), composed)
	if got := names(m.Matches()); len(got) != 1 || got[0] != strings.TrimSuffix(neighbour, "/") {
		t.Fatalf("precondition: the match list is %v, want only the neighbour", got)
	}

	m, action := press(t, m, keyEnter)
	if action != ActionNone {
		t.Errorf("enter gave %v for a path no row resolves to", action)
	}
	if _, tail := splitPath(m.Typed()); tail != composed {
		t.Errorf("the path is now %q, want the %q that was typed left alone",
			m.Typed(), composed)
	}
}

// TestTheCursorTiebreakIsByteExactThenCaseThenFirstRow.
//
// Built from a listing rather than a directory, because the pairs this turns
// on cannot all exist on one filesystem: a case-insensitive volume will not
// hold FOO.txt beside foo.txt, so on the machines most readers use the
// ranking between the two is otherwise never exercised at all.
func TestTheCursorTiebreakIsByteExactThenCaseThenFirstRow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rows  []string
		typed string
		want  string
	}{
		{"an exactly typed name outranks one differing only in case",
			[]string{"FOO.txt", "foo.txt"}, "foo.txt", "foo.txt"},
		{"a name differing only in case outranks a mere prefix match",
			[]string{"doc.pdf_old", "doc.pdf"}, "Doc.pdf", "doc.pdf"},
		{"between two names differing only in case, the first row",
			[]string{"FOO.txt", "FoO.txt"}, "foo.txt", "FOO.txt"},
		{"with nothing typed exactly, the first prefix match",
			[]string{"notes.txt", "notes.txt.bak"}, "notes", "notes.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(roles())
			m.typed = tc.typed
			for _, row := range tc.rows {
				m.entries = append(m.entries, Entry{Name: row})
			}
			m.match()

			entry, ok := m.Selected()
			if !ok {
				t.Fatalf("nothing in %v matched %q", tc.rows, tc.typed)
			}
			if entry.Name != tc.want {
				t.Errorf("typing %q in %v put the cursor on %q, want %q",
					tc.typed, tc.rows, entry.Name, tc.want)
			}
		})
	}
}
