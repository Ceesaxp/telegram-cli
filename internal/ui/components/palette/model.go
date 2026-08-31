// Package palette is the `:` command overlay: a filtered list of commands
// with a live query, driven entirely by a caller-supplied item list.
//
// It deliberately knows nothing about what a command does. The app owns the
// registry and executes; this package owns the query, the filtering, the
// selection, and the drawing. That split is what lets the registry stay the
// single source for the palette, the help card, and the keymap without this
// package importing any of them.
package palette

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// Width is the overlay's fixed width in cells (docs/tui-2.0.md, "Composer,
// modes, and palette"). Fixed rather than responsive because the palette is
// a reading surface: a 200-column terminal should not stretch a list of
// short command names across the whole screen.
const Width = 60

// maxRows caps how many commands are listed at once. The overlay sits about
// eight rows from the top, so a taller list would run off a short terminal.
const maxRows = 8

// Item is one command as the palette displays it. The app builds these from
// its registry; the palette never interprets them beyond matching Name.
type Item struct {
	// Name is the command word without the leading colon, e.g. "mark-read".
	Name string
	// Args is the argument shape shown after the name, e.g. "<query>".
	// Empty for commands that take none.
	Args string
	// Description is the one-line explanation shown beside the name.
	Description string
	// Key is the equivalent key binding, right-aligned so the palette
	// teaches the keymap. Empty when the command has no key of its own.
	Key string
}

// Action is what a keypress asked the app to do.
type Action int

const (
	// ActionNone means the palette handled the key itself.
	ActionNone Action = iota
	// ActionRun means Enter was pressed: execute Query().
	ActionRun
	// ActionCancel means Escape was pressed: close without running.
	ActionCancel
)

// Model is the palette overlay.
type Model struct {
	roles   theme.Roles
	visible bool

	// query is what the user has typed after the colon, command word and
	// arguments together, exactly as entered.
	query string

	items    []Item
	filtered []int // indices into items, in display order
	cursor   int   // index into filtered
}

func New(r theme.Roles) Model {
	return Model{roles: r}
}

// SetItems replaces the command list. The app calls this with its registry.
func (m *Model) SetItems(items []Item) {
	m.items = items
	m.refilter()
}

// Open shows the palette with an empty query.
func (m *Model) Open() {
	m.visible = true
	m.query = ""
	m.refilter()
}

// Close hides the palette and drops the query, so reopening never resurrects
// a half-typed command from last time.
func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.cursor = 0
}

func (m Model) IsVisible() bool { return m.visible }

// Query returns the raw typed text, command word and arguments together.
func (m Model) Query() string { return m.query }

// Selected returns the highlighted item, if the filter matched anything.
func (m Model) Selected() (Item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return Item{}, false
	}
	return m.items[m.filtered[m.cursor]], true
}

// Matches returns the currently filtered items, for tests and for callers
// that want to know whether anything matched.
func (m Model) Matches() []Item {
	out := make([]Item, 0, len(m.filtered))
	for _, i := range m.filtered {
		out = append(out, m.items[i])
	}
	return out
}

// Update handles a keypress while the palette owns input.
//
// Navigation is arrows and ctrl+n/ctrl+p only — NOT j/k. The handoff
// specified j/k, but the palette is a text surface: every printable key has
// to reach the query or commands like ":jump" and ":keymap" could not be
// typed at all. See docs/tui-2.0.md, "Divergences from the handoff prose".
func (m Model) Update(msg tea.KeyPressMsg) (Model, Action) {
	if !m.visible {
		return m, ActionNone
	}

	switch msg.String() {
	case "esc":
		return m, ActionCancel

	case "enter":
		return m, ActionRun

	// Arrows only. Divergence 9 already settled that this is a text
	// surface and so cannot take j/k; ctrl+p and ctrl+n were a second
	// spelling of the arrows, on a list that is never more than a few
	// entries tall.
	case "up":
		m.move(-1)
		return m, ActionNone

	case "down":
		m.move(1)
		return m, ActionNone

	case "tab":
		m.complete()
		return m, ActionNone

	case "backspace":
		if m.query != "" {
			r := []rune(m.query)
			m.query = string(r[:len(r)-1])
			m.refilter()
		}
		return m, ActionNone

	case "ctrl+u":
		m.query = ""
		m.refilter()
		return m, ActionNone
	}

	// Everything else that produces text goes into the query.
	//
	// This reads msg.Text, not msg.String(). String() is the binding
	// spelling — a space arrives as "space" — so typing an argument would
	// silently drop every separator and ":search foo bar" would become
	// ":searchfoobar". Text is what the key actually produces.
	if msg.Text != "" {
		m.query += msg.Text
		m.refilter()
	}
	return m, ActionNone
}

func (m *Model) move(delta int) {
	if len(m.filtered) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
}

// complete replaces the typed command word with the selected item's name,
// leaving any arguments already typed alone and adding a trailing space when
// the command takes arguments — so Tab lands the cursor where the argument
// goes.
func (m *Model) complete() {
	item, ok := m.Selected()
	if !ok {
		return
	}
	_, args := SplitQuery(m.query)
	switch {
	case args != "":
		m.query = item.Name + " " + args
	case item.Args != "":
		m.query = item.Name + " "
	default:
		m.query = item.Name
	}
	m.refilter()
}

// refilter recomputes the match list, keeping the cursor in range. The
// selection resets to the top on every edit: after typing another character
// the old highlighted row is usually gone, and holding a stale index would
// run a command the user is no longer looking at.
func (m *Model) refilter() {
	name, _ := SplitQuery(m.query)
	m.filtered = m.filtered[:0]

	// Two passes so prefix matches sort above looser subsequence matches,
	// which keeps an exactly-typed command at the top where Enter expects it.
	for i, it := range m.items {
		if strings.HasPrefix(it.Name, name) {
			m.filtered = append(m.filtered, i)
		}
	}
	for i, it := range m.items {
		if !strings.HasPrefix(it.Name, name) && subsequence(it.Name, name) {
			m.filtered = append(m.filtered, i)
		}
	}
	m.cursor = 0
}

// SplitQuery separates the command word from its arguments.
func SplitQuery(q string) (name, args string) {
	q = strings.TrimLeft(q, " ")
	if i := strings.IndexByte(q, ' '); i >= 0 {
		return q[:i], strings.TrimLeft(q[i+1:], " ")
	}
	return q, ""
}

// subsequence reports whether every rune of pattern appears in s in order.
// It is the "fuzzy" half of the match: ":mkrd" finds "mark-read".
func subsequence(s, pattern string) bool {
	if pattern == "" {
		return true
	}
	pr := []rune(pattern)
	pi := 0
	for _, r := range s {
		if r == pr[pi] {
			pi++
			if pi == len(pr) {
				return true
			}
		}
	}
	return false
}

// View renders the overlay. Every line is exactly [Width] cells, so the
// caller can place it without the frame shearing.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	// Amber for the prompt and the command names: the palette is where
	// commands are, and amber is the commands role.
	promptStyle := lipgloss.NewStyle().Foreground(m.roles.Amber).Background(m.roles.Panel).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(m.roles.Amber).Background(m.roles.Panel)
	descStyle := theme.OverlayMuted(m.roles)
	keyStyle := theme.OverlayKey(m.roles)

	var lines []string

	// Prompt row: ": <query>" with a block cursor.
	lines = append(lines, cell.Fit(promptStyle.Render(":")+" "+m.query+"█", Width))

	if len(m.filtered) == 0 {
		lines = append(lines, cell.Fit(descStyle.Render("  no matching command"), Width))
	}

	for row, idx := range m.filtered {
		if row >= maxRows {
			break
		}
		it := m.items[idx]
		lines = append(lines, m.itemLine(it, row == m.cursor, nameStyle, descStyle, keyStyle))
	}

	if n := len(m.filtered) - maxRows; n > 0 {
		lines = append(lines, cell.Fit(descStyle.Render("  +"+itoa(n)+" more"), Width))
	}

	lines = append(lines, cell.Fit(descStyle.Render("  enter run · tab complete · esc cancel"), Width))

	return theme.OverlayFrame(m.roles).Padding(0, 1).Render(strings.Join(lines, "\n"))
}

// itemLine renders one command row: marker, name, arguments, description,
// and the right-aligned key equivalent. The description absorbs all the
// shrink, since the name and key are what the row exists to show.
func (m Model) itemLine(it Item, selected bool, nameStyle, descStyle, keyStyle lipgloss.Style) string {
	marker := "  "
	if selected {
		marker = "▌ "
	}

	label := it.Name
	if it.Args != "" {
		label += " " + it.Args
	}

	// Budget: marker + label + gap + description + gap + key.
	used := cell.Width(marker) + cell.Width(label)
	keyW := cell.Width(it.Key)
	descBudget := Width - used - 2 - keyW
	if keyW > 0 {
		descBudget -= 2
	}

	line := marker + nameStyle.Render(label)
	if descBudget > 0 {
		desc := cell.Truncate(it.Description, descBudget)
		line += "  " + descStyle.Render(desc)
	}

	// Right-align the key equivalent against the fixed width.
	if keyW > 0 {
		pad := Width - cell.Width(line) - keyW
		if pad > 0 {
			line += strings.Repeat(" ", pad) + keyStyle.Render(it.Key)
		}
	}

	// Fit to the exact width FIRST, then colour. Rendering through a padded
	// style instead would spend part of the budget on the style's own frame
	// and truncate the tail — which is precisely the right-aligned key
	// equivalent, the one thing on the row that must not be cut.
	line = cell.Fit(line, Width)
	if selected {
		return theme.OverlaySelected(m.roles).Render(line)
	}
	return line
}

// itoa avoids pulling strconv in for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
