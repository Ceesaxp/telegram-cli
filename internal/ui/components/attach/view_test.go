package attach

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
)

func plain(s string) string { return ansi.Strip(s) }

// opensFg and opensBg are what a role looks like once lipgloss has rendered
// it — an RGB triple, not the hex the palette is written in. Comparing
// against the hex would pass on a string that never contained the colour,
// which is the vacuous-assertion trap a pinned colour profile exists to
// close; this closes the other half of it.
//
// The PARAMETERS only, without the introducer and the terminator: a
// foreground and a background set together are merged into one SGR
// sequence, so a probe carrying its own trailing "m" matches nothing.
func opensFg(c lipgloss.Color) string {
	return sgrParams(lipgloss.NewStyle().Foreground(c).Render("Z"))
}

func opensBg(c lipgloss.Color) string {
	return sgrParams(lipgloss.NewStyle().Background(c).Render("Z"))
}

func sgrParams(rendered string) string {
	return strings.TrimSuffix(
		strings.TrimPrefix(strings.Split(rendered, "Z")[0], "\x1b["), "m")
}

// TestEveryLineIsTheSameWidth, including one carrying a CJK filename.
//
// The overlay is placed with lipgloss.Place, which paints a ragged block if
// its lines disagree — and a wide rune is exactly where a picker built on
// rune counts would come apart, because a filename is the one string on
// this surface that the client does not choose.
func TestEveryLineIsTheSameWidth(t *testing.T) {
	for name, dir := range map[string]string{
		"ascii":      tree(t, "patches/", "backoff.patch", "auth-p95.png"),
		"cjk":        tree(t, "会議の議事録.txt", "スクリーンショット.png"),
		"emoji":      tree(t, "🎉 party.png", "notes.txt"),
		"long names": tree(t, strings.Repeat("verylongname", 12)+".txt"),
		"empty":      tree(t),
	} {
		t.Run(name, func(t *testing.T) {
			view := open(t, dir).View()
			lines := strings.Split(view, "\n")
			if len(lines) < 4 {
				t.Fatalf("View() is only %d lines:\n%s", len(lines), plain(view))
			}
			want := cell.Width(lines[0])
			if want < Width {
				t.Fatalf("the frame is %d cells, narrower than the picker's %d", want, Width)
			}
			for i, line := range lines {
				if got := cell.Width(line); got != want {
					t.Errorf("line %d is %d cells, want %d: %q", i, got, want, plain(line))
				}
			}
		})
	}
}

// TestAClosedPickerDrawsNothing.
func TestAClosedPickerDrawsNothing(t *testing.T) {
	m := open(t, tree(t, "a.txt"))
	m.Close()
	if got := m.View(); got != "" {
		t.Errorf("View() = %q while closed", got)
	}
}

// TestTheListingIsCappedAndSaysHowMuchItHid.
//
// Six rows rather than the palette's eight, which is not an inconsistency:
// this overlay carries a divider and a state row the palette does not, so
// six holds the whole surface at the palette's height — the thing that has
// to survive a 24-row terminal with the overlay anchored eight rows down.
func TestTheListingIsCappedAndSaysHowMuchItHid(t *testing.T) {
	files := make([]string, 0, 10)
	for _, n := range "abcdefghij" {
		files = append(files, string(n)+".txt")
	}
	m := open(t, tree(t, files...))

	view := plain(m.View())
	if strings.Contains(view, "j.txt") {
		t.Error("the listing drew past its cap")
	}
	if !strings.Contains(view, "+4 more") {
		t.Errorf("the listing hid four entries without saying so:\n%s", view)
	}

	// The constraint the cap exists for: anchored eight rows down, the whole
	// overlay has to fit a 24-row terminal.
	const anchor = 8
	rows := strings.Split(strings.TrimRight(m.View(), "\n"), "\n")
	if len(rows)+anchor > 24 {
		t.Errorf("the overlay is %d rows and sits %d down, so it runs off a "+
			"24-row terminal:\n%s", len(rows), anchor, view)
	}
}

// TestTheMarkerFollowsTheCursorIntoTheScrolledWindow.
//
// The window's own offset is what turns a cursor index into a drawn row.
// Ignore it and the marker stays on the top row while the cursor is
// somewhere else entirely — the surface pointing at one file and Enter
// attaching another.
func TestTheMarkerFollowsTheCursorIntoTheScrolledWindow(t *testing.T) {
	files := make([]string, 0, 10)
	for _, n := range "abcdefghij" {
		files = append(files, string(n)+".txt")
	}
	m := open(t, tree(t, files...))
	for range 9 {
		m, _ = press(t, m, keyDown)
	}

	selected, _ := m.Selected()
	if selected.Name != "j.txt" {
		t.Fatalf("precondition: the cursor is on %s", selected.Name)
	}

	var marked []string
	for _, line := range strings.Split(plain(m.View()), "\n") {
		if strings.Contains(line, "▌") {
			marked = append(marked, strings.TrimSpace(line))
		}
	}
	if len(marked) != 1 {
		t.Fatalf("%d rows carry the selection bar, want exactly one: %v", len(marked), marked)
	}
	// The frame's own border sits outside the row, so the bar is what comes
	// first inside it rather than first on the line.
	if !strings.Contains(marked[0], "▌▤ "+selected.Name) {
		t.Errorf("the bar is on %q, want it on %s", marked[0], selected.Name)
	}
}

// TestTheCursorSitsWhereTypingHappens.
//
// Before the ghost suggestion, not after it. A cursor states where the next
// character goes, and the suggestion is text that has not been entered —
// drawing the block past it would put it where typing does not happen,
// which is the defect divergences 28 and 46 were about. See divergence 50.
func TestTheCursorSitsWhereTypingHappens(t *testing.T) {
	m := typeText(t, open(t, tree(t, "backoff.patch")), "back")

	row := plain(m.promptLine())
	cursor := strings.Index(row, "█")
	if cursor < 0 {
		t.Fatalf("the prompt row has no cursor: %q", row)
	}
	typed := strings.Index(row, "back")
	if typed < 0 || cursor < typed {
		t.Fatalf("the cursor is not after the typed text: %q", row)
	}
	if !strings.Contains(row[cursor:], "off.patch") {
		t.Errorf("the suggestion is not offered after the cursor: %q", row)
	}
	if strings.Contains(row[:cursor], "off.patch") {
		t.Errorf("the cursor is drawn past the suggestion, where typing does not happen: %q", row)
	}
}

// TestThePromptSaysWhereYouAreAndWhichMatchYouAreOn.
func TestThePromptSaysWhereYouAreAndWhichMatchYouAreOn(t *testing.T) {
	m := open(t, tree(t, "a.txt", "b.txt", "c.txt"))

	if row := plain(m.promptLine()); !strings.Contains(row, "1 of 3") {
		t.Errorf("the prompt row does not give the cursor's position: %q", row)
	}
	m, _ = press(t, m, keyDown)
	if row := plain(m.promptLine()); !strings.Contains(row, "2 of 3") {
		t.Errorf("the position did not follow the cursor: %q", row)
	}
}

// TestTheStateRowSaysHowTheFileWouldSend, which is the answer the prompt
// this replaced could not give until after it had already attached.
func TestTheStateRowSaysHowTheFileWouldSend(t *testing.T) {
	dir := tree(t, "shot.png", "notes.txt")

	image := typeText(t, open(t, dir), "shot")
	if row := plain(image.stateLine()); !strings.Contains(row, "photo") {
		t.Errorf("an image's state row does not say it sends as a photo: %q", row)
	}

	image, _ = press(t, image, keyCtrlT)
	row := plain(image.stateLine())
	if !strings.Contains(row, "document") {
		t.Errorf("after the toggle the state row still says photo: %q", row)
	}
	if strings.Contains(row, "photo") {
		t.Errorf("the state row says both at once: %q", row)
	}

	doc := typeText(t, open(t, dir), "notes")
	if row := plain(doc.stateLine()); !strings.Contains(row, "original bytes") {
		t.Errorf("a document's state row does not say what it sends: %q", row)
	}
}

// TestADirectoryIsSomewhereToGoRatherThanSomethingToAttach.
func TestADirectoryIsSomewhereToGoRatherThanSomethingToAttach(t *testing.T) {
	m := typeText(t, open(t, tree(t, "patches/")), "patc")

	if row := plain(m.stateLine()); !strings.Contains(row, "to enter") {
		t.Errorf("a directory's state row does not offer to enter it: %q", row)
	}
	if hint := plain(m.hintLine()); !strings.Contains(hint, "open") {
		t.Errorf("the hint row still says attach over a directory: %q", hint)
	}
	if _, _, ok := m.Chosen(); ok {
		t.Error("a directory was offered as something to attach")
	}
}

// TestTheEmptyStatesAreDistinguishable.
//
// "Nothing matched what you typed" and "that directory is not there" are
// different problems with different fixes, and a picker that drew a blank
// listing for both would leave the reader retyping a name in a directory
// that does not exist.
func TestTheEmptyStatesAreDistinguishable(t *testing.T) {
	dir := tree(t, "a.txt")

	noMatch := typeText(t, open(t, dir), "zzzz")
	if row := plain(noMatch.stateLine()); !strings.Contains(row, "no match") {
		t.Errorf("an unmatched filter says %q", row)
	}

	noDir := typeText(t, open(t, dir), "nope/")
	if row := plain(noDir.stateLine()); !strings.Contains(row, "no such directory") {
		t.Errorf("a missing directory says %q", row)
	}
}

// TestADirectoryReadsQuieterThanAFile. Structure, not payload: the files
// are what the surface exists to attach, so they are what reads first.
func TestADirectoryReadsQuieterThanAFile(t *testing.T) {
	m := open(t, tree(t, "patches/", "a.txt"))

	entries := m.Matches()
	if len(entries) != 2 || !entries[0].Dir {
		t.Fatalf("precondition: listing is %v", names(entries))
	}
	folder, file := m.entryLine(entries[0], false), m.entryLine(entries[1], false)

	if folder == file {
		t.Fatal("a directory and a file are drawn identically")
	}
	r := roles()
	if !strings.Contains(folder, opensFg(r.Dim)) {
		t.Errorf("a directory's name is not drawn in Dim: %q", folder)
	}
	if !strings.Contains(file, opensFg(r.Fg)) {
		t.Errorf("a file's name is not drawn in Fg: %q", file)
	}
	if strings.Contains(folder, opensFg(r.Fg)) {
		t.Errorf("a directory is drawn as loud as a file: %q", folder)
	}
}

// TestTheSelectedRowIsMarked, and marked with a background as well as a bar
// — a row may hold a colour emoji, and a colour emoji ignores the
// foreground it is given.
func TestTheSelectedRowIsMarked(t *testing.T) {
	m := open(t, tree(t, "a.txt", "b.txt"))
	entries := m.Matches()

	selected, unselected := m.entryLine(entries[0], true), m.entryLine(entries[0], false)
	if selected == unselected {
		t.Fatal("the cursored row is drawn the same as any other")
	}
	if !strings.Contains(plain(selected), "▌") {
		t.Errorf("the cursored row has no selection bar: %q", plain(selected))
	}
	if strings.Contains(plain(unselected), "▌") {
		t.Errorf("an uncursored row has a selection bar: %q", plain(unselected))
	}
	if !strings.Contains(selected, opensBg(roles().Sel)) {
		t.Error("the cursored row is not painted — a bar alone is one cell of signal")
	}
}

// TestAFileLooksTheSameHereAsItWillInTheThread. The glyphs are the media
// card's, so a picked file is recognisable once it is sent.
func TestAFileLooksTheSameHereAsItWillInTheThread(t *testing.T) {
	for name, want := range map[string]string{
		"shot.png":   "▣",
		"notes.txt":  "▤",
		"song.mp3":   "▶",
		"clip.mp4":   "▶",
		"patches/":   "▸",
		"patch.diff": "▤",
	} {
		entry := Entry{Name: strings.TrimSuffix(name, "/"), Dir: strings.HasSuffix(name, "/")}
		if got := glyphFor(entry); got != want {
			t.Errorf("%s draws %q, want %q", name, got, want)
		}
	}
}

// TestTheHintRowNamesTheKeysThisSurfaceHonours — and only those. A hint
// listing a key the component does not get is the drift this repo has
// removed from the status bar, the README and the palette's own card.
func TestTheHintRowNamesTheKeysThisSurfaceHonours(t *testing.T) {
	hint := plain(open(t, tree(t, "a.txt")).hintLine())

	for _, want := range []string{"↵", "⇥", "←", "esc"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint row does not name %q: %q", want, hint)
		}
	}
	// ctrl+h is not bound, and neither is ctrl+p: one is backspace's own
	// byte and the other moves the selection one surface over.
	for _, forbidden := range []string{"^h", "^p", "j/k"} {
		if strings.Contains(hint, forbidden) {
			t.Errorf("the hint row advertises %q, which is not bound: %q", forbidden, hint)
		}
	}
}

// TestTheSizeAndTimeColumnsSayWhatTheyKnow.
func TestTheSizeAndTimeColumnsSayWhatTheyKnow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry Entry
		want  string
	}{
		{"bytes", Entry{Size: 900}, "900 B"},
		{"one decimal below ten units", Entry{Size: 2150}, "2.1 KB"},
		{"none above", Entry{Size: 184 * 1024}, "184 KB"},
		{"a directory nobody has counted says nothing", Entry{Dir: true}, ""},
		{"an unreadable directory says nothing", Entry{Dir: true, counted: true, Items: -1}, ""},
		{"one item is singular", Entry{Dir: true, counted: true, Items: 1}, "1 item"},
		{"more are plural", Entry{Dir: true, counted: true, Items: 12}, "12 items"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatSize(tc.entry); got != tc.want {
				t.Errorf("formatSize = %q, want %q", got, tc.want)
			}
		})
	}
}
