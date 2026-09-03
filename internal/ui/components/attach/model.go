// Package attach is the Ctrl+T file picker: a path being typed, the
// directory it names, and what is in it.
//
// It is the command palette's twin rather than the dialog's. Same fixed
// width, same anchor, same selection marker, same key-hint footer, no
// buttons anywhere — the palette collects a command and this collects a
// path, and everything on screen is derived from what has been typed the
// way a shell derives completions. It replaced the last centred box with an
// OK button in the client; see docs/tui-2.0.md, divergence 49.
//
// The package does no sending. It reports a path and a send mode; the app
// stages them on the composer.
package attach

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// Width is the overlay's fixed width in cells, the palette's exactly. Fixed
// for the palette's reason: a 200-column terminal should not stretch a
// listing of short filenames across the whole screen.
const Width = 60

// maxRows caps the listing at six, where the palette caps its own at eight.
//
// Not an inconsistency: this overlay carries a divider and a state row the
// palette does not, so six holds the whole surface at the palette's eleven
// lines — which is what has to survive a 24-row terminal with the overlay
// anchored eight rows down.
const maxRows = 6

// Action is what a keypress asked the app to do.
type Action int

const (
	// ActionNone means the picker handled the key itself.
	ActionNone Action = iota
	// ActionAttach means Enter landed on a file: read Chosen.
	ActionAttach
	// ActionCancel means Escape was pressed: close, staging nothing.
	ActionCancel
)

// Model is the picker overlay.
type Model struct {
	roles   theme.Roles
	visible bool

	// typed is the whole path as entered, home-collapsed for display. It is
	// the only input state: the listing, the filter, the suggestion and the
	// state row are all derived from it.
	typed string

	// dir, entries and hidden are the cached listing. The cache key is the
	// directory AND whether dotfiles were included, because the tail's
	// leading dot is what decides the latter and it changes as you type.
	dir     string
	entries []Entry
	hidden  bool
	listErr bool

	filtered []int // indices into entries, in display order
	cursor   int   // index into filtered
	top      int   // first filtered index drawn; the window's own scroll

	// flipped inverts the send mode derived from the cursored entry. It is
	// cleared whenever the cursor moves, so the toggle applies to the file
	// it was pressed on and never silently to a later one.
	flipped bool
}

func New(r theme.Roles) Model {
	return Model{roles: r}
}

// SetRoles supplies the TUI 2.0 semantic palette.
func (m *Model) SetRoles(r theme.Roles) { m.roles = r }

// Open shows the picker.
//
// fallback is where to begin when the picker has nowhere else to be — the
// configured download directory, so the place the client saves to is the
// place it offers back. It is a FALLBACK and not a destination: once the
// picker has been somewhere, that is where it reopens, because attaching
// three files from one folder should not mean walking there three times.
// A caller wanting to move it should hand the path to Paste.
func (m *Model) Open(fallback string) {
	m.visible = true
	m.flipped = false
	if m.dir == "" {
		if fallback == "" {
			fallback = "~/"
		}
		m.dir = withSlash(display(fallback))
	}
	m.typed = m.dir
	m.reload()
}

// Close hides the picker, keeping the directory for next time.
//
// It resets nothing else, because Open resets everything else: the path, the
// cursor and the send mode are all set from m.dir on the way back in. Doing
// it here as well would be two places that have to agree about what a fresh
// picker looks like, and mutation testing could not tell them apart — which
// is the tell that one of them was not doing anything.
func (m *Model) Close() { m.visible = false }

func (m Model) IsVisible() bool { return m.visible }

// Typed returns the path as entered, for tests and for the app's notices.
func (m Model) Typed() string { return m.typed }

// Selected returns the cursored entry, if the filter matched anything.
func (m Model) Selected() (Entry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return Entry{}, false
	}
	return m.entries[m.filtered[m.cursor]], true
}

// Window is the slice of the match list currently drawn, and where in the
// match list it starts.
//
// The listing scrolls rather than being capped: a cursor that can walk past
// the last drawn row is a cursor the reader cannot see, on a file Enter
// would still attach.
func (m Model) Window() (rows []Entry, top int) {
	end := min(m.top+maxRows, len(m.filtered))
	for _, i := range m.filtered[min(m.top, end):end] {
		rows = append(rows, m.entries[i])
	}
	return rows, m.top
}

// Below is how many matches sit under the drawn window.
func (m Model) Below() int {
	return max(len(m.filtered)-m.top-maxRows, 0)
}

// Matches returns the filtered listing, for tests.
func (m Model) Matches() []Entry {
	out := make([]Entry, 0, len(m.filtered))
	for _, i := range m.filtered {
		out = append(out, m.entries[i])
	}
	return out
}

// AsPhoto reports how the cursored file would send.
//
// An image sends as a photo unless the reader said otherwise, which is the
// defect this component was built to fix: the prompt it replaces passed a
// hardcoded false, so Ctrl+T always attached as a document while Ctrl+V
// attached the same image as a photo. Anything that is not an image can
// only be a document, and the toggle says so rather than lying.
func (m Model) AsPhoto() bool {
	entry, ok := m.Selected()
	if !ok || entry.Dir || !entry.Image {
		return false
	}
	return !m.flipped
}

// Chosen is the absolute path of the cursored file and how to send it. ok is
// false unless the cursor is on a file — a directory is somewhere to go, not
// something to attach.
func (m Model) Chosen() (path string, asPhoto bool, ok bool) {
	entry, ok := m.Selected()
	if !ok || entry.Dir {
		return "", false, false
	}
	dir, _ := splitPath(m.typed)
	return filepath.Join(native(listable(dir)), entry.Name), m.AsPhoto(), true
}

// Update handles a keypress while the picker owns input.
//
// Movement is the arrows and nothing else, which is the palette's rule and
// the palette's reason: this is a text surface, so every printable key has
// to reach the path or a file called "jack" could not be typed. One
// spelling per action — the emacs chords were taken off the palette for
// being a second way to do one thing, and adding them back here would put
// the pair straight back out of step.
func (m Model) Update(msg tea.KeyPressMsg) (Model, Action) {
	if !m.visible {
		return m, ActionNone
	}

	switch msg.String() {
	case "esc":
		return m, ActionCancel

	case "enter":
		return m.enter()

	case "up":
		m.move(-1)
		return m, ActionNone

	case "down":
		m.move(1)
		return m, ActionNone

	case "pgup":
		m.move(-maxRows)
		return m, ActionNone

	case "pgdown":
		m.move(maxRows)
		return m, ActionNone

	case "tab":
		m.complete()
		return m, ActionNone

	// Backspace deletes a character, and on an empty tail it goes up a
	// directory instead — which is what a shell user's fingers already do,
	// and what removes the need for a ctrl+h binding. Ctrl+h is the same
	// byte as backspace outside the Kitty protocol, so binding the two to
	// different things would work on some terminals and quietly do the
	// wrong thing on the rest; here a terminal that conflates them lands
	// on the right behaviour either way.
	case "backspace":
		m.backspace()
		return m, ActionNone

	case "left":
		m.up()
		return m, ActionNone

	// The send-mode toggle, on the key that opened the surface. Not ctrl+p:
	// that is the composer's expand chord, and on a list overlay it reads
	// as "move up" to anyone who has used one.
	case "ctrl+t":
		// Flipped unconditionally: AsPhoto is the single place that knows
		// the toggle only means anything for an image, and a second guard
		// here would be one more thing to keep in step with it.
		m.flipped = !m.flipped
		return m, ActionNone

	case "ctrl+u":
		m.typed = ""
		m.reload()
		return m, ActionNone
	}

	// Everything else that produces text extends the path.
	//
	// msg.Text, not msg.String(): String() is the binding spelling, so a
	// space arrives as "space" and a filename with one in it could not be
	// typed at all.
	if msg.Text != "" {
		m.typed += msg.Text
		m.reload()
	}
	return m, ActionNone
}

// Paste replaces the path with a dropped or pasted one.
//
// Replaces rather than appends: a dropped path is absolute and complete, and
// appending it to whatever was already typed produces a path that exists
// nowhere. See [UnquotePath] for what a terminal actually delivers.
func (m Model) Paste(text string) Model {
	path := UnquotePath(text)
	if path == "" {
		return m
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = withSlash(path)
	}
	m.typed = display(path)
	m.reload()
	return m
}

// enter descends into a directory or attaches a file.
//
// The typed path wins over the cursor when it names something exactly:
// somebody who has pasted or typed a whole path has said which file they
// mean, and attaching the highlighted row instead would attach a different
// one.
//
// Only when there is a TAIL, though. With none, the typed path is the
// directory being browsed rather than a choice within it — it names a real
// directory on every single keystroke, so consulting it first would make
// Enter re-enter the current folder forever and the cursor would never be
// reachable at all.
func (m Model) enter() (Model, Action) {
	if _, tail := splitPath(m.typed); tail != "" {
		if info, err := os.Stat(native(m.typed)); err == nil {
			if info.IsDir() {
				m.typed = withSlash(m.typed)
				m.reload()
				return m, ActionNone
			}
			return m, ActionAttach
		}
	}

	entry, ok := m.Selected()
	if !ok {
		return m, ActionNone
	}
	if entry.Dir {
		dir, _ := splitPath(m.typed)
		m.typed = withSlash(dir + entry.Name)
		m.reload()
		return m, ActionNone
	}
	// Land the path on the file being attached, so what the prompt row says
	// and what gets staged are the same thing.
	dir, _ := splitPath(m.typed)
	m.typed = dir + entry.Name
	m.reload()
	return m, ActionAttach
}

// complete extends the path to the cursored entry, adding the separator on
// a directory so the next keystroke filters inside it.
func (m *Model) complete() {
	entry, ok := m.Selected()
	if !ok {
		return
	}
	dir, _ := splitPath(m.typed)
	m.typed = dir + entry.Name
	if entry.Dir {
		m.typed += "/"
	}
	m.reload()
}

// backspace deletes one character, or goes up a directory when there is no
// tail left to delete.
func (m *Model) backspace() {
	_, tail := splitPath(m.typed)
	if tail == "" && m.typed != "" {
		m.up()
		return
	}
	if m.typed == "" {
		return
	}
	runes := []rune(m.typed)
	m.typed = string(runes[:len(runes)-1])
	m.reload()
}

// up moves to the parent directory.
func (m *Model) up() {
	dir, tail := splitPath(m.typed)
	if tail != "" {
		// A half-typed tail is dropped first: the reader is standing in
		// this directory, and the tail is what they were doing in it.
		m.typed = dir
		m.reload()
		return
	}
	if parent := parentOf(dir); parent != dir {
		m.typed = parent
		m.reload()
	}
}

func (m *Model) move(delta int) {
	if len(m.filtered) == 0 {
		m.cursor, m.top = 0, 0
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.filtered)-1)
	m.scroll()
	// The send mode belongs to the file it was set on.
	m.flipped = false
}

// scroll brings the window to the cursor, and counts what the move brought
// into view.
func (m *Model) scroll() {
	switch {
	case m.cursor < m.top:
		m.top = m.cursor
	case m.cursor >= m.top+maxRows:
		m.top = m.cursor - maxRows + 1
	}
	m.top = max(m.top, 0)
	m.count()
}

// count fills in the item counts for the rows about to be drawn, and only
// those. See countInto: a count is a whole extra directory read, and doing
// them all up front made opening a home directory cost one read per child
// on Bubble Tea's update path.
func (m *Model) count() {
	dir, _ := splitPath(m.typed)
	end := min(m.top+maxRows, len(m.filtered))
	if m.top >= end {
		return
	}
	countInto(m.entries, listable(dir), m.filtered[m.top:end])
}

// reload re-lists the directory if the path now names a different one, then
// refilters. The listing is cached on (directory, dotfiles) so that typing
// a tail costs no disk at all — which is what makes a keystroke cheap in a
// directory of thousands of files.
func (m *Model) reload() {
	dir, tail := splitPath(m.typed)
	hidden := strings.HasPrefix(tail, ".")

	if dir != m.dir || hidden != m.hidden || m.entries == nil {
		entries, err := readDir(listable(dir), hidden)
		m.dir, m.hidden = dir, hidden
		m.entries, m.listErr = entries, err != nil
		if err != nil {
			m.entries = nil
		}
	}
	m.refilter()
}

// refilter recomputes the match list. The cursor resets to the top on every
// edit: after another character the previously highlighted row is usually
// gone, and a stale index would attach a file the reader is no longer
// looking at.
func (m *Model) refilter() {
	_, tail := splitPath(m.typed)
	m.filtered = m.filtered[:0]
	m.cursor, m.top = 0, 0

	for i, entry := range m.entries {
		if !matches(entry.Name, tail) {
			continue
		}
		// An EXACTLY typed name takes the cursor. The filter is
		// case-insensitive on purpose, so on a case-sensitive filesystem
		// holding both Foo and foo, typing "foo" matches both and Foo
		// sorts first — and everything downstream reads the cursor, so
		// the picker would show, describe and attach a different file
		// from the one that was typed. One mechanism rather than a second
		// rule inside Chosen: the row on screen is the row that acts.
		if entry.Name == tail {
			m.cursor = len(m.filtered)
		}
		m.filtered = append(m.filtered, i)
	}

	m.scroll()
	m.flipped = false
}

// listable is the directory to read for a typed path: an empty one means
// the working directory, which is what makes a bare relative prefix behave
// the way it does in a shell.
func listable(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}

// withSlash guarantees the trailing separator a directory carries in the
// prompt row, so the next keystroke filters inside it rather than renaming
// it.
func withSlash(path string) string {
	if path == "" || strings.HasSuffix(path, "/") {
		return path
	}
	return path + "/"
}
