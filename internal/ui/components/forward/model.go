// Package forward is the destination picker raised by the forward action:
// a filtered list of chats with a live query, then a confirmation naming
// what is going where.
//
// It is the palette's twin, like the attach picker before it — same 60-cell
// fixed width, same anchor, same `▌` marker, same key-hint footer, no
// buttons. The palette collects a command and the attach picker collects a
// path; this collects a chat.
//
// It knows nothing about Telegram. The app supplies candidates, runs the
// search behind the query, and does the forwarding; this package owns the
// query, the filtering, the selection, the confirmation step, and the
// drawing. That is the same split that keeps the palette from importing the
// command registry.
package forward

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
)

// Width is the overlay's fixed width in cells, matching the palette and the
// attach picker. Fixed rather than responsive for the same reason: this is
// a reading surface, and a 200-column terminal should not stretch a list of
// chat titles across the whole screen.
const Width = 60

// maxRows caps how many destinations are listed at once. The overlay sits
// about eight rows from the top, so a taller list would run off a short
// terminal.
const maxRows = 8

// Chat is one destination as the picker displays it. The app builds these
// from its store and from search results; the picker never interprets them
// beyond matching Title and Handle.
type Chat struct {
	ID int64
	// Title is the chat's display name.
	Title string
	// Sigil is the one-cell type mark drawn before the title, so a group
	// and a person are told apart the way they are in the chat list.
	Sigil string
	// Handle is the @username where there is one, shown right-aligned. It
	// is also matched against, which is what lets a stranger be reached by
	// the name they are searchable under.
	Handle string
	// Note is a short right-aligned qualifier for rows that are not from
	// the open dialog list, e.g. "not in your chats".
	Note string
}

// Source is the message being forwarded, captured when the picker opens.
//
// It is captured rather than re-read because the picker is modal but the
// world is not: an update can arrive, the cursor can be moved by a
// mouse-driven scroll, and a source re-read at confirmation time would be
// whatever the cursor had drifted to. Forwarding the wrong message to a
// deliberately chosen destination is the worst failure this surface has.
type Source struct {
	ChatID    int64
	MessageID int64
	// Preview is a one-line rendering of the message, shown at the
	// confirmation step so the reader can see what they are about to send.
	Preview string
}

// Step is which of the picker's two screens is showing.
type Step int

const (
	// StepPick is the searchable destination list.
	StepPick Step = iota
	// StepConfirm names the destination and previews the message.
	StepConfirm
)

// Action is what a keypress asked the app to do.
type Action int

const (
	// ActionNone means the picker handled the key itself.
	ActionNone Action = iota
	// ActionQueryChanged means the query was edited: the app should run
	// its search and call SetResults.
	ActionQueryChanged
	// ActionForward means the confirmation was accepted. Read Source and
	// Destination.
	ActionForward
	// ActionCancel means the picker closed without forwarding.
	ActionCancel
)

// Model is the destination picker overlay.
type Model struct {
	roles   theme.Roles
	visible bool
	step    Step

	query  string
	source Source

	// local is the candidate list the app supplied when the picker opened:
	// the chats already loaded. It is filtered as the query is typed and
	// is what makes the common case instant.
	local []Chat
	// remote is what the server matched for the current query. It is
	// appended after the local matches rather than merged into them, so a
	// result arriving mid-type never reorders the rows under the cursor.
	remote []Chat

	filtered []Chat
	cursor   int
	// offset is the first row drawn, so the cursor can never sit on a
	// destination the reader cannot see.
	offset int

	// chosen is the destination frozen when the confirmation opened.
	//
	// The source has always been captured; the destination was still read
	// off the cursor at the moment Enter was pressed the second time. A
	// search answer landing between the two presses refilters the list and
	// resets the cursor, so the chat named on the confirmation and the
	// chat actually forwarded to could differ — the same failure the
	// captured source exists to prevent, on the other half of the
	// sentence.
	chosen Chat

	// searching and searchFailed drive the status line. A failure must not
	// replace usable local matches — it is a note on the row list, not
	// instead of one.
	searching    bool
	searchFailed bool
}

func New(r theme.Roles) Model {
	return Model{roles: r}
}

// Open shows the picker for one message, with an empty query and the
// caller's candidate list.
func (m *Model) Open(src Source, candidates []Chat) {
	m.visible = true
	m.step = StepPick
	m.query = ""
	m.source = src
	m.local = candidates
	m.remote = nil
	m.searching = false
	m.searchFailed = false
	m.chosen = Chat{}
	m.refilter()
}

// Close hides the picker and drops everything it was holding, so reopening
// never resurrects last time's query, results, or target.
func (m *Model) Close() {
	m.visible = false
	m.step = StepPick
	m.query = ""
	m.source = Source{}
	m.local = nil
	m.remote = nil
	m.filtered = nil
	m.cursor = 0
	m.offset = 0
	m.chosen = Chat{}
	m.searching = false
	m.searchFailed = false
}

func (m Model) IsVisible() bool { return m.visible }
func (m Model) Step() Step      { return m.step }
func (m Model) Query() string   { return m.query }

// Source returns the message the picker was opened on. It cannot change
// while the picker is open.
func (m Model) Source() Source { return m.source }

// Destination returns the chat a forward would go to: the frozen choice
// once the confirmation is open, the highlighted row before that.
func (m Model) Destination() (Chat, bool) {
	if m.step == StepConfirm {
		return m.chosen, m.chosen.ID != 0
	}
	return m.highlighted()
}

// highlighted is the row under the cursor, which is the destination only
// until the confirmation freezes one.
func (m Model) highlighted() (Chat, bool) {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return Chat{}, false
	}
	return m.filtered[m.cursor], true
}

// Matches returns the currently displayed rows, for tests and for callers
// that want to know whether anything matched.
func (m Model) Matches() []Chat { return m.filtered }

// SetSearching marks a server search in flight, so the status line can say
// so without the rows changing.
func (m *Model) SetSearching(searching bool) {
	m.searching = searching
	if searching {
		m.searchFailed = false
	}
}

// SetResults replaces the server-side matches. The app is responsible for
// dropping results whose query no longer matches [Model.Query] — a stale
// answer must never repopulate a list the reader has typed past.
//
// Rows already in the local candidate list are skipped: contacts.search
// returns your own peers alongside global ones, and a chat listed twice
// looks like two different destinations.
func (m *Model) SetResults(chats []Chat) {
	if m.step != StepPick {
		// The confirmation is open. Its destination is frozen, but
		// refiltering underneath it would still change what the list
		// shows when Escape steps back, and it costs nothing to wait.
		return
	}
	seen := make(map[int64]bool, len(m.local))
	for _, c := range m.local {
		seen[c.ID] = true
	}
	m.remote = m.remote[:0]
	for _, c := range chats {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		m.remote = append(m.remote, c)
	}
	m.searching = false
	m.searchFailed = false
	m.refilter()
}

// SetSearchFailed notes that the server search did not answer. Local
// matches stay on screen: a picker that empties itself because the network
// blinked is worse than one that quietly lists less.
func (m *Model) SetSearchFailed() {
	if m.step != StepPick {
		return
	}
	m.searching = false
	m.searchFailed = true
}

// Update handles a keypress while the picker owns input.
//
// Navigation is arrows only, for divergence 9's reason: this is a text
// surface, so j and k have to reach the query or a chat called "jack"
// could not be typed.
func (m Model) Update(msg tea.KeyPressMsg) (Model, Action) {
	if !m.visible {
		return m, ActionNone
	}
	if m.step == StepConfirm {
		return m.updateConfirm(msg)
	}

	switch msg.String() {
	case "esc":
		return m, ActionCancel

	case "enter":
		// Enter on an empty list is not a cancel and not a forward. Doing
		// nothing is the only honest answer: there is no destination to
		// confirm.
		chosen, ok := m.highlighted()
		if !ok {
			return m, ActionNone
		}
		m.chosen = chosen
		m.step = StepConfirm
		return m, ActionNone

	case "up":
		m.move(-1)
		return m, ActionNone

	case "down":
		m.move(1)
		return m, ActionNone

	case "backspace":
		if m.query == "" {
			return m, ActionNone
		}
		r := []rune(m.query)
		m.query = string(r[:len(r)-1])
		m.editedQuery()
		return m, ActionQueryChanged

	case "ctrl+u":
		if m.query == "" {
			return m, ActionNone
		}
		m.query = ""
		m.editedQuery()
		return m, ActionQueryChanged
	}

	// Everything else that produces text goes into the query. msg.Text,
	// not msg.String(), so a space arrives as a space rather than as the
	// word "space" — a chat title has spaces in it.
	if msg.Text != "" {
		m.query += msg.Text
		m.editedQuery()
		return m, ActionQueryChanged
	}
	return m, ActionNone
}

// editedQuery re-filters after a change to the query, dropping the server
// matches with it.
//
// They answered the PREVIOUS query. Keeping them until a replacement
// arrives leaves rows on screen that do not match what is typed — and if
// the replacement search fails, or the query is emptied so none is run,
// they stay indefinitely and can be confirmed. The generation guard in the
// app rejects a late answer; it cannot retract one already accepted.
func (m *Model) editedQuery() {
	m.remote = nil
	m.refilter()
}

// updateConfirm handles the second screen, where the only questions are yes
// and no.
func (m Model) updateConfirm(msg tea.KeyPressMsg) (Model, Action) {
	switch msg.String() {
	case "enter":
		return m, ActionForward
	case "esc":
		// Back to the list, not out of the picker. Escape means "undo the
		// last step" everywhere else in this client, and a reader who
		// picked the wrong chat wants the list again, not to start over.
		m.step = StepPick
		m.chosen = Chat{}
		return m, ActionNone
	}
	return m, ActionNone
}

func (m *Model) move(delta int) {
	if len(m.filtered) == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	m.scrollToCursor()
}

// scrollToCursor keeps the selected row inside the rendered window.
//
// Without it the cursor walked past row eight while the view kept drawing
// rows one to eight: the selection marker vanished and Enter confirmed a
// chat that was not on screen. A picker that can send to something it is
// not showing is worse than one that cannot reach it at all.
func (m *Model) scrollToCursor() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+maxRows {
		m.offset = m.cursor - maxRows + 1
	}
	if max := len(m.filtered) - maxRows; m.offset > max {
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// refilter recomputes the row list: local candidates matching the query
// first, then the server's, each in the order it was given.
//
// The cursor resets to the top on every edit, as in the palette: after
// another character the highlighted row is usually gone, and a stale index
// would confirm a chat the reader is no longer looking at.
func (m *Model) refilter() {
	q := strings.ToLower(strings.TrimSpace(m.query))
	m.filtered = m.filtered[:0]
	for _, c := range m.local {
		if matches(c, q) {
			m.filtered = append(m.filtered, c)
		}
	}
	// Server results are not re-filtered: the server matched them against
	// this query, and re-applying a substring test here would drop the
	// transliterated and prefix matches it is better at than we are.
	m.filtered = append(m.filtered, m.remote...)
	m.cursor = 0
	m.offset = 0
}

// matches reports whether a chat answers the query, by title or by handle.
// An empty query matches everything, which is what makes the picker open on
// the recent chats rather than on nothing.
func matches(c Chat, q string) bool {
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(c.Title), q) {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimPrefix(c.Handle, "@")), strings.TrimPrefix(q, "@"))
}

// View renders the overlay. Every line is exactly [Width] cells, so the
// caller can place it without the frame shearing.
func (m Model) View() string {
	if !m.visible {
		return ""
	}
	if m.step == StepConfirm {
		return m.confirmView()
	}
	return m.pickView()
}

func (m Model) pickView() string {
	promptStyle := lipgloss.NewStyle().Foreground(m.roles.Amber).Background(m.roles.Panel).Bold(true)
	muted := theme.OverlayMuted(m.roles)

	var lines []string
	lines = append(lines, m.promptRow(promptStyle, muted))

	switch {
	case len(m.filtered) == 0 && m.searching:
		lines = append(lines, cell.Fit(muted.Render("  searching…"), Width))
	case len(m.filtered) == 0:
		lines = append(lines, cell.Fit(muted.Render("  no chat matches"), Width))
	}

	// The window, not the first maxRows: the cursor can be anywhere in a
	// long list, and it has to be on screen wherever it is.
	end := m.offset + maxRows
	if end > len(m.filtered) {
		end = len(m.filtered)
	}
	for i := m.offset; i < end; i++ {
		lines = append(lines, m.chatLine(m.filtered[i], i == m.cursor))
	}

	// The status line is additive: it never replaces rows, so a failed
	// search still leaves the local matches usable.
	if m.searching && len(m.filtered) > 0 {
		lines = append(lines, cell.Fit(muted.Render("  searching…"), Width))
	}
	if m.searchFailed {
		lines = append(lines, cell.Fit(theme.OverlayError(m.roles).Render("  search unavailable — showing your chats only"), Width))
	}

	lines = append(lines, cell.Fit(muted.Render("  ↵ choose · ↑↓ move · esc cancel"), Width))
	return theme.OverlayFrame(m.roles).Padding(0, 1).Render(strings.Join(lines, "\n"))
}

// promptRow is the typed query, with the position in the list right-
// aligned.
//
// The counter replaces the "+N more" line the first draft had, which
// counted only what was below the fold and so said nothing once the view
// scrolled. "3 of 12" is the shape the attach picker's prompt row already
// uses and the shape the chat list's filter header agrees on.
func (m Model) promptRow(promptStyle, muted lipgloss.Style) string {
	left := promptStyle.Render("→") + " " + m.query + "█"
	if len(m.filtered) == 0 {
		return cell.Fit(left, Width)
	}
	right := itoa(m.cursor+1) + " of " + itoa(len(m.filtered))
	if pad := Width - cell.Width(left) - cell.Width(right); pad > 0 {
		return cell.Fit(left+strings.Repeat(" ", pad)+muted.Render(right), Width)
	}
	return cell.Fit(left, Width)
}

func (m Model) confirmView() string {
	muted := theme.OverlayMuted(m.roles)
	body := theme.OverlayBody(m.roles)
	title := theme.OverlayTitle(m.roles)

	dest, _ := m.Destination()
	label := strings.TrimSpace(dest.Sigil + " " + dest.Title)

	var lines []string
	lines = append(lines, cell.Fit(title.Render("Forward to "+cell.Truncate(label, Width-13)), Width))
	lines = append(lines, cell.Fit("", Width))
	lines = append(lines, cell.Fit("  "+body.Render(cell.Truncate(m.source.Preview, Width-4)), Width))
	lines = append(lines, cell.Fit("", Width))
	lines = append(lines, cell.Fit(muted.Render("  ↵ forward · esc back · the original sender is kept"), Width))
	return theme.OverlayFrame(m.roles).Padding(0, 1).Render(strings.Join(lines, "\n"))
}

// chatLine renders one destination row: marker, sigil, title, and the
// right-aligned handle or note. The title absorbs the shrink, since the
// handle is what disambiguates two chats with the same name.
func (m Model) chatLine(c Chat, selected bool) string {
	marker := "  "
	if selected {
		marker = "▌ "
	}

	right := c.Handle
	if right == "" {
		right = c.Note
	}
	rightW := cell.Width(right)

	titleBudget := Width - cell.Width(marker) - cell.Width(c.Sigil) - 1 - rightW - 2
	line := marker + c.Sigil + " " + cell.Truncate(c.Title, max(titleBudget, 1))

	if rightW > 0 {
		if pad := Width - cell.Width(line) - rightW; pad > 0 {
			line += strings.Repeat(" ", pad) + theme.OverlayMuted(m.roles).Render(right)
		}
	}

	line = cell.Fit(line, Width)
	if selected {
		return theme.OverlaySelected(m.roles).Render(line)
	}
	return line
}

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
