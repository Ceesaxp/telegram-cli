package composer

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/render"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// AppMode is the interaction mode the badge reports.
//
// It is a copy of the app's InteractionMode rather than a shared type
// because the app imports this package and not the other way round. The
// mapping is one switch in internal/app with a test that walks every value,
// which is a cheaper coupling than a third package existing to hold four
// constants.
//
// The badge REPORTS the mode; it never decides one. Decision 3: a badge that
// altered key routing would be a second source of truth for something
// Model.Mode() already derives.
type AppMode int

const (
	// AppNormal: printable keys navigate. A browsing panel, or an overlay
	// that navigates rather than collects text.
	AppNormal AppMode = iota
	// AppInsert: printable keys type.
	AppInsert
	// AppVi: this composer is in vi editing and has returned to its
	// command state, so the next letter runs a vi command on the draft
	// (decision I-12). It shared NORMAL with the browsing panels for a
	// release while sharing none of their keys.
	AppVi
	// AppCommand: the palette owns the keyboard.
	AppCommand
)

func (a AppMode) String() string {
	switch a {
	case AppInsert:
		return "INSERT"
	case AppVi:
		// Short because the badge column is seven cells, and because the
		// colour is a second channel rather than the only one.
		return "VI"
	case AppCommand:
		return "COMMAND"
	default:
		return "NORMAL"
	}
}

// badgeWidth is what the badge column is padded to.
//
// Six, not seven: NORMAL and INSERT are six cells and COMMAND is seven, so
// six is the width the badge already had wherever it can change under the
// reader's eyes. VI is two, and left unpadded it would pull the prompt two
// cells left every time a vi user pressed Esc.
//
// Padding to seven instead would fix the column at COMMAND's width and shift
// every existing frame by one cell — a geometry change, which the golden
// fixtures exist to refuse (TUI 2.0 decision 11). Recorded as an amendment
// to I-12 in docs/interaction-model.md.
const badgeWidth = 6

// badgeLabel is String() padded to the badge column, so the prompt after it
// does not move as the mode changes.
func (a AppMode) badgeLabel() string {
	label := a.String()
	for cell.Width(label) < badgeWidth {
		label += " "
	}
	return label
}

func (a AppMode) colour(r theme.Roles) lipgloss.Color {
	switch a {
	case AppInsert:
		return r.Green
	case AppVi:
		return r.Mauve
	case AppCommand:
		return r.Amber
	default:
		return r.Cyan
	}
}

// Layout constants for the inline row (docs/tui-2.0.md, "Composer, modes,
// and palette", and the composer row in every frame golden):
//
//	" NORMAL › draft or hint                          md "
const (
	// composerLead is the blank column before the badge.
	composerLead = 1
	// composerTrail is the blank column after the right-hand label.
	composerTrail = 1
	// expandedRows is the eight-row split source-and-preview form.
	expandedRows = 8
)

// SetRoles supplies the TUI 2.0 semantic palette.
func (m *Model) SetRoles(r theme.Roles) { m.roles = r }

// SetMode tells the composer which interaction mode to report for the states
// it cannot see itself: COMMAND, and the NORMAL of another panel holding
// focus. The app resolves it; see internal/app/mode.go.
func (m *Model) SetMode(mode AppMode) { m.appMode = mode }

// badge is the mode this row reports.
//
// Derived where it can be. A focused composer knows whether the next
// printable key will be inserted — that is precisely its vi state — so it
// says so without being told, and a Model constructed directly by a test
// reports the truth rather than whatever the host last set. COMMAND is the
// exception: the palette owns the keyboard over everything, including a
// focused composer, and only the host knows the palette is up.
//
// This must agree with app.Model.Mode() by construction, not by coincidence;
// TestComposerBadgeAgreesWithTheResolver pins that it does.
func (m Model) badge() AppMode {
	if m.appMode == AppCommand {
		return AppCommand
	}
	if !m.focused {
		return m.appMode
	}
	if m.editing == ModeVi && m.vi == viNormal {
		return AppVi
	}
	return AppInsert
}

// SetParseMarkdown tells the composer whether outgoing markdown parsing is
// on, which decides both the "md" label and whether the expanded form's
// preview shows parsed text or the text verbatim.
//
// The label is only honest when it reflects the setting: showing "md" with
// parsing off would promise a transformation that will not happen.
func (m *Model) SetParseMarkdown(on bool) { m.parseMarkdown = on }

// SetExpanded switches between the inline row and the eight-row form.
func (m *Model) SetExpanded(on bool) { m.expanded = on }

// Expanded reports whether the eight-row form is showing.
func (m Model) Expanded() bool { return m.expanded }

// Rows is how many terminal rows the composer needs.
//
// The host asks before computing the layout, so the thread gives up exactly
// the rows the composer is about to use — a composer that rendered more rows
// than it asked for would push the bottom of the history off screen, and one
// that rendered fewer would leave a hole.
// Rows is how many rows the composer WANTS: one to type into, and one each
// for the context above it.
//
// What it wants, not what it was given — the layout budgets from this, and
// a Rows that reported its own budget back would be a loop that settles on
// one row and never grows. What it actually DRAWS is bounded separately;
// see View.
func (m Model) Rows() int {
	if m.expanded {
		return expandedRows
	}

	rows := 1
	if m.mode != ModeNormal {
		rows++ // the reply or edit bar
	}
	if m.attachment != "" {
		rows++ // the attachment chip
	}
	return rows
}

// View renders the composer as exactly Rows() lines, each exactly the
// panel width.
func (m Model) View() string {
	if m.width < 1 {
		return ""
	}
	if m.expanded {
		return strings.Join(m.expandedView(), "\n")
	}

	// Context first, the prompt last, and then cut from the FRONT to the
	// budget. The prompt is the row a client is for — a composer showing
	// what you are replying to and no way to reply is worse than one that
	// shows only the reply — so the context rows are what give way, newest
	// first, the same order they would be read in.
	var rows []string
	if bar := m.contextBar(); bar != "" {
		rows = append(rows, bar)
	}
	if chip := m.attachmentChip(); chip != "" {
		rows = append(rows, chip)
	}
	rows = append(rows, m.promptRow(m.width))

	// Cut from the FRONT to the budget the layout granted. The frame
	// reserves that many rows and clips whatever comes past them — and the
	// row it clips is the last one, which is the row you type into. That is
	// how a reply on a nineteen-row terminal came out as a quote bar with
	// nowhere to put the answer.
	if m.height > 0 && len(rows) > m.height {
		rows = rows[len(rows)-m.height:]
	}
	return strings.Join(rows, "\n")
}

// promptRow is the composer's own row: mode badge, prompt, what you are
// typing (or how to start), and a right-hand label.
func (m Model) promptRow(width int) string {
	r := m.roles

	mode := m.badge()
	badge := mode.badgeLabel()
	badgeStyle := lipgloss.NewStyle().Foreground(mode.colour(r)).Bold(true)

	right := m.rightLabel()
	rightW := cell.Width(right)
	if rightW > 0 {
		rightW++ // a gap before it
	}

	// lead + badge + gap + prompt + gap
	gutter := composerLead + cell.Width(badge) + 1 + 1 + 1
	contentW := width - gutter - rightW - composerTrail
	if contentW < 1 {
		contentW = 1
	}

	content, style := m.promptContent(contentW)

	line := strings.Repeat(" ", composerLead) +
		badgeStyle.Render(badge) + " " +
		lipgloss.NewStyle().Foreground(mode.colour(r)).Render(m.promptGlyph()) + " " +
		style.Render(cell.Fit(content, contentW))

	if right != "" {
		line += " " + lipgloss.NewStyle().Foreground(r.Faint).Render(right)
	}
	return cell.Fill(r.Panel, line, width)
}

// promptGlyph is the prompt mark. It becomes a downward chevron when the
// draft has more rows than this one, so a multi-line draft cannot look like
// a one-line one that has lost its tail.
func (m Model) promptGlyph() string {
	if strings.Contains(m.textarea.Value, "\n") {
		return "⌄"
	}
	return "›"
}

// promptContent is what sits after the prompt: a notice if there is one,
// else the draft, else how to start typing.
//
// A notice outranks the draft because it is the thing that just happened and
// the draft is still there underneath it. It is the one row this component
// has, so there is nowhere else to put it.
func (m Model) promptContent(width int) (string, lipgloss.Style) {
	r := m.roles
	switch {
	case m.expanded && m.notice == "":
		// The draft is in the source pane beside this row, so the row says
		// what the keys do instead. Divergence 4: the chords advertised are
		// the ones that already exist, plus ctrl+p — the handoff's footer
		// claimed four that emacs line editing owns.
		return m.expandedHint(), lipgloss.NewStyle().Foreground(r.Dim)
	case m.notice != "":
		return m.notice, lipgloss.NewStyle().Foreground(r.Amber)
	case m.textarea.Value != "":
		return m.draftLine(width), lipgloss.NewStyle().Foreground(r.Fg)
	case m.focused:
		// The cursor comes first, before the placeholder, and it is the
		// whole point of this branch. Without it, pressing i changed the
		// badge's colour and nothing else — every motion key then moved an
		// invisible cursor, which is indistinguishable from a motion key
		// that does nothing. It is why the line editing was reported as
		// missing when ctrl+a and $ had worked all along.
		return cursorBlock + " type a message · enter sends",
			lipgloss.NewStyle().Foreground(r.Dim)
	default:
		return "i to compose · : for commands", lipgloss.NewStyle().Foreground(r.Dim)
	}
}

// drawCursor puts the caret into a line, in whichever of the two
// conventions the current mode uses.
//
// vi has two cursors, and drawing both the same way is what made normal mode
// read as broken. In NORMAL the cursor sits ON a character — that is why
// Escape appears to step back one, why x deletes "the character under the
// cursor", and why i inserts before it — so the caret is that character in
// reverse video. In INSERT the caret is a GAP between characters, because
// typing puts text there.
//
// A gap is a position, not a cell, and a terminal has only cells. Drawing
// the gap as an inserted block was the mistake: "123" with the caret before
// the 3 came out as "12█3", four cells for three characters, and the 3
// visibly stepped right the moment you pressed i. No terminal editor does
// that — a block cursor in vim covers the next character, it does not push
// it — and the movement reads as though i had typed a space.
//
// So both modes now draw ON the character and neither changes the line's
// width. They stay apart by HOW: reverse video for normal, an underline for
// insert, which is the same pair of shapes vim asks the terminal for and
// leaves the character legible in the mode where you are adding to it.
//
// At the end of a line there is no character to draw on — an empty line, or
// a line whose last character was just deleted — so both fall back to the
// block, which is what a terminal does there too.
func (m Model) drawCursor(runes []rune, start, cursor, end int) string {
	head, tail := string(runes[start:cursor]), string(runes[cursor:end])
	if cursor >= end {
		return head + cursorBlock + tail
	}

	caret := lipgloss.NewStyle().Underline(true)
	if m.IsViNormalMode() {
		caret = lipgloss.NewStyle().Reverse(true)
	}

	on := string(runes[cursor])
	rest := string(runes[cursor+1 : end])
	return head + caret.Render(on) + rest
}

// cursorBlock is what the composer draws where the caret is.
//
// A block the app draws itself rather than the terminal's own caret: Bubble
// Tea's alternate screen hides the hardware cursor, and a caret that is only
// visible when the terminal feels like showing it is not a caret you can
// build an editing model on.
const cursorBlock = "█"

// draftLine is the row of the draft the cursor is on, windowed so the cursor
// stays visible.
//
// The cursor's row, not the first: a composer that showed the top of a draft
// while the user typed at the bottom would be showing them somebody else's
// text.
func (m Model) draftLine(width int) string {
	runes := []rune(m.textarea.Value)
	cursor := min(max(m.textarea.Cursor, 0), len(runes))

	start := 0
	for i := cursor - 1; i >= 0; i-- {
		if runes[i] == '\n' {
			start = i + 1
			break
		}
	}
	end := len(runes)
	for i := cursor; i < len(runes); i++ {
		if runes[i] == '\n' {
			end = i
			break
		}
	}

	line := string(runes[start:end])
	if !m.focused {
		return cell.Truncate(line, width)
	}

	col := cursor - start
	withCursor := m.drawCursor(runes, start, cursor, end)
	if cell.Width(withCursor) <= width {
		return withCursor
	}
	// Slide the window right so the cursor stays on screen, the same rule
	// the single-line textarea uses.
	over := cell.Width(string(runes[start:start+col])) - width + 1
	if over < 0 {
		over = 0
	}
	return cell.Clamp(cell.ClampLeft(withCursor, over), width)
}

// rightLabel is the right-hand cell of the composer row: how long the draft
// is, or — while it is empty — whether markdown will be applied to it.
func (m Model) rightLabel() string {
	if n := len([]rune(m.textarea.Value)); n > 0 {
		return strconv.Itoa(n)
	}
	if m.parseMarkdown {
		return "md"
	}
	return ""
}

// contextBar is the one-row reply or edit bar above the composer.
//
// No-wrap by construction: it is assembled from budgeted fields and cut to
// the width, so a long quote costs the reader nothing but the tail of the
// quote.
func (m Model) contextBar() string {
	r := m.roles

	var label, glyph, text string
	switch m.mode {
	case ModeReply:
		label, glyph, text = "reply", "↳", m.replyText
	case ModeEdit:
		label, glyph, text = "edit", "✎", m.textarea.Value
	default:
		return ""
	}
	return m.barRow(label, glyph, text, r.Amber)
}

// attachmentChip is the one-row staged-attachment bar.
func (m Model) attachmentChip() string {
	if m.attachment == "" {
		return ""
	}
	glyph := "▤"
	if m.asPhoto {
		glyph = "▣"
	}
	return m.barRow("attach", glyph, filepath.Base(m.attachment), m.roles.Amber)
}

// barRow draws a labelled context row with a right-hand way out.
func (m Model) barRow(label, glyph, text string, colour lipgloss.Color) string {
	r := m.roles
	const escape = "esc to drop"

	head := strings.Repeat(" ", composerLead) +
		lipgloss.NewStyle().Foreground(colour).Render(label) + " " +
		lipgloss.NewStyle().Foreground(r.Ghost).Render(glyph) + " "
	headW := composerLead + cell.Width(label) + 1 + cell.Width(glyph) + 1

	textW := m.width - headW - cell.Width(escape) - 1 - composerTrail
	if textW < 1 {
		textW = 1
	}

	// A quote is one row, so newlines in it are collapsed rather than
	// obeyed: the bar says which message, not what all of it said.
	flat := strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")

	line := head + lipgloss.NewStyle().Foreground(r.Dim).
		Render(cell.Fit(cell.Truncate(flat, textW), textW)) +
		" " + lipgloss.NewStyle().Foreground(r.Faint).Render(escape)

	return cell.Fill(r.Panel, line, m.width)
}

// expandedView is the eight-row form: the draft as source on the left with
// line numbers, what it will actually send on the right, any staged
// attachment, and the chords that work here.
//
// Source and preview side by side rather than one above the other, because
// the question the preview answers is "did that asterisk do what I meant",
// and answering it by scrolling between two views is how a message goes out
// wrong.
func (m Model) expandedView() []string {
	r := m.roles

	sourceW := (m.width - 3) / 2
	if sourceW < 4 {
		sourceW = 4
	}
	previewW := m.width - sourceW - 3
	if previewW < 1 {
		previewW = 1
	}

	bodyRows := expandedRows - 2 // the header rule and the footer row
	chip := m.attachmentChip()
	if chip != "" {
		bodyRows--
	}

	rows := []string{m.expandedHeader(sourceW, previewW)}

	source := m.sourceLines(sourceW, bodyRows)
	preview := m.previewLines(previewW, bodyRows)
	rule := lipgloss.NewStyle().Foreground(r.Rule).Render("│")
	for i := range bodyRows {
		rows = append(rows, cell.Fill(r.Panel,
			" "+source[i]+" "+rule+" "+preview[i], m.width))
	}

	if chip != "" {
		rows = append(rows, chip)
	}
	rows = append(rows, m.promptRow(m.width))
	return rows
}

// expandedHeader labels the two columns, so it is never a guess which side
// is what you typed and which is what will be sent.
func (m Model) expandedHeader(sourceW, previewW int) string {
	r := m.roles
	rule := lipgloss.NewStyle().Foreground(r.RuleSoft)
	label := lipgloss.NewStyle().Foreground(r.Faint)

	sends := "sends as"
	if !m.parseMarkdown {
		// With parsing off the right-hand side is the text verbatim, and
		// calling it a preview of formatting would be a promise the setting
		// has switched off.
		sends = "sends as (markdown off)"
	}

	left := " compose "
	right := " " + sends + " "
	head := label.Render(left) +
		rule.Render(strings.Repeat("─", max(sourceW-cell.Width(left)+1, 0))) +
		rule.Render("┬") +
		label.Render(right) +
		rule.Render(strings.Repeat("─", max(previewW-cell.Width(right)+1, 0)))

	return cell.Fill(r.Panel, head, m.width)
}

// sourceLines is the draft with line numbers, windowed on the cursor.
func (m Model) sourceLines(width, rows int) []string {
	r := m.roles
	lines := strings.Split(m.textarea.Value, "\n")

	cursorLine := strings.Count(string([]rune(m.textarea.Value)[:min(max(m.textarea.Cursor, 0), len([]rune(m.textarea.Value)))]), "\n")
	start := max(cursorLine-rows+1, 0)
	if start+rows > len(lines) {
		start = max(len(lines)-rows, 0)
	}

	numW := len(strconv.Itoa(max(len(lines), 1)))
	num := lipgloss.NewStyle().Foreground(r.Ghost)
	body := lipgloss.NewStyle().Foreground(r.Fg)

	out := make([]string, rows)
	for i := range rows {
		n := start + i
		if n >= len(lines) {
			out[i] = strings.Repeat(" ", width)
			continue
		}
		text := lines[n]
		if m.focused && n == cursorLine {
			text = m.draftLine(width - numW - 1)
		}
		out[i] = num.Render(cell.PadLeft(strconv.Itoa(n+1), numW)) + " " +
			body.Render(cell.Fit(cell.Clamp(text, width-numW-1), width-numW-1))
	}
	return out
}

// previewLines is what the draft will actually send, rendered by the same
// entity renderer that draws received messages.
//
// The same renderer, not a second one that agrees today: a preview drawn by
// different code is a preview that can be wrong, and the whole point of it is
// to be trusted.
func (m Model) previewLines(width, rows int) []string {
	text := m.textarea.Value
	var lines []string
	if strings.TrimSpace(text) != "" {
		ft := &telegram.FormattedText{Text: text}
		if m.parseMarkdown {
			ft = telegram.PreviewMarkdown(text)
		}
		lines = render.RenderText(ft, m.roles, width)
	}

	out := make([]string, rows)
	for i := range rows {
		if i < len(lines) {
			out[i] = cell.Fit(lines[i], width)
			continue
		}
		out[i] = strings.Repeat(" ", width)
	}
	return out
}
