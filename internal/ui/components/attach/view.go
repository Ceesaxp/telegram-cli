package attach

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/render"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// The row's column budget, in cells, summing to [Width].
//
//	▌ ▤ backoff.patch                              2.1 KB      14:22
//	│ └ glyph and its space                        └ size      └ mtime
//	└ selection bar
const (
	markW = 1 // the selection bar
	iconW = 2 // type glyph and the space after it
	sizeW = 9 // right-aligned
	timeW = 8 // right-aligned
	nameW = Width - markW - iconW - sizeW - timeW
)

// Type glyphs are the media card's, so a file looks the same here as it will
// in the thread once it is sent.
//
// Audio and video share ▶ because the media card does: render/media.go draws
// it for video, animation, voice and audio alike, and a picker that
// distinguished them would be teaching a distinction the thread does not
// make. A directory's ▸ is the one glyph that is this component's own — a
// directory is never a message.
func glyphFor(e Entry) string {
	switch kindOf(e) {
	case "directory":
		return "▸"
	case "image":
		return "▣"
	case "audio", "video":
		return "▶"
	}
	return "▤"
}

// View renders the overlay. Every content line is exactly [Width] cells, so
// the caller can place it without the frame shearing.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	lines := []string{m.promptLine()}

	rows, top := m.Window()
	for i, entry := range rows {
		lines = append(lines, m.entryLine(entry, top+i == m.cursor))
	}
	if n := m.Below(); n > 0 {
		lines = append(lines, cell.Fit(
			theme.OverlayMuted(m.roles).Render(" +"+strconv.Itoa(n)+" more"), Width))
	}

	lines = append(lines,
		lipgloss.NewStyle().Foreground(m.roles.RuleSoft).Background(m.roles.Panel).
			Render(strings.Repeat("─", Width)),
		m.stateLine(),
		m.hintLine(),
	)

	return theme.OverlayFrame(m.roles).Padding(0, 1).Render(strings.Join(lines, "\n"))
}

// promptLine is the path being typed, with the cursored entry's remainder
// offered after it and the match position on the right.
//
// The block cursor sits immediately after the typed text, BEFORE the ghost
// suggestion, because a cursor states where the next character goes and the
// suggestion is not text that has been entered. Drawing it past the ghost
// would put it where typing does not happen — the same defect divergences 28
// and 46 were about, in the surface that is nothing but a caret and a path.
// See divergence 50.
func (m Model) promptLine() string {
	r := m.roles
	dir, tail := splitPath(m.typed)

	dirStyle := lipgloss.NewStyle().Foreground(r.Dim).Background(r.Panel)
	tailStyle := lipgloss.NewStyle().Foreground(r.Bright).Background(r.Panel)
	ghostStyle := lipgloss.NewStyle().Foreground(r.Ghost).Background(r.Panel)
	cursorStyle := lipgloss.NewStyle().Foreground(r.Cyan).Background(r.Panel)
	glyphStyle := lipgloss.NewStyle().Foreground(r.Amber).Background(r.Panel)

	position := ""
	if len(m.filtered) > 0 {
		position = strconv.Itoa(m.cursor+1) + " of " + strconv.Itoa(len(m.filtered))
	}

	suggestion := ""
	if entry, ok := m.Selected(); ok && len(entry.Name) > len(tail) {
		suggestion = entry.Name[len(tail):]
		if entry.Dir {
			suggestion += "/"
		}
	}

	// The position is reserved BEFORE the path is laid out, not fitted
	// around it afterwards. It is a fixed handful of cells and the path is
	// unbounded, so a path long enough to need cutting is exactly when
	// "which of these am I on" is worth most — leaving it to whatever space
	// was left over drops it precisely then.
	reserved := 0
	if position != "" {
		reserved = cell.Width(position) + 2
	}

	// The path shrinks from the FRONT: the tail is what is being typed and
	// what the suggestion continues, so a long path scrolls its leading
	// directories out of sight rather than its end.
	budget := Width - markW - iconW - reserved
	if over := cell.Width(dir+tail+"█"+suggestion) - budget; over > 0 {
		dir = cell.ClampLeft(dir, over)
		if rest := budget - cell.Width(dir+tail+"█"); cell.Width(suggestion) > max(rest, 0) {
			suggestion = cell.Clamp(suggestion, max(rest, 0))
		}
	}

	line := " " + glyphStyle.Render("▤") + " " +
		dirStyle.Render(dir) + tailStyle.Render(tail) +
		cursorStyle.Render("█") + ghostStyle.Render(suggestion)

	if position != "" {
		line = cell.Pad(line, Width-cell.Width(position)-1) +
			ghostStyle.Render(position) + " "
	}
	return cell.Fill(r.Panel, line, Width)
}

// entryLine renders one listing row.
func (m Model) entryLine(e Entry, selected bool) string {
	r := m.roles

	// A directory is structure rather than payload: its glyph is inert and
	// its name is quieter than a file's, which is what makes the files —
	// the things this surface exists to attach — the things that read first.
	glyphColour, nameColour := r.Amber, r.Fg
	if e.Dir {
		glyphColour, nameColour = r.Ghost, r.Dim
	}

	mark := " "
	if selected {
		mark = lipgloss.NewStyle().Foreground(r.Cyan).Background(r.Panel).Render("▌")
	}

	name := e.Name
	if e.Dir {
		name += "/"
	}

	line := mark +
		lipgloss.NewStyle().Foreground(glyphColour).Background(r.Panel).Render(glyphFor(e)) + " " +
		lipgloss.NewStyle().Foreground(nameColour).Background(r.Panel).
			Render(cell.Pad(cell.Truncate(name, nameW), nameW)) +
		lipgloss.NewStyle().Foreground(r.Faint).Background(r.Panel).
			Render(cell.PadLeft(cell.Clamp(formatSize(e), sizeW), sizeW)) +
		lipgloss.NewStyle().Foreground(r.Ghost).Background(r.Panel).
			Render(cell.PadLeft(cell.Clamp(formatTime(e.ModTime, render.Now()), timeW), timeW))

	if selected {
		return cell.Fill(r.Sel, line, Width)
	}
	return cell.Fill(r.Panel, line, Width)
}

// stateLine says what the cursored thing is and, for a file, how it would
// send — which is the answer the prompt this replaced could never give until
// after it had already failed.
func (m Model) stateLine() string {
	r := m.roles
	body := lipgloss.NewStyle().Foreground(r.Fg).Background(r.Panel)
	note := lipgloss.NewStyle().Foreground(r.Faint).Background(r.Panel)

	if m.listErr {
		return cell.Fill(r.Panel, " "+
			lipgloss.NewStyle().Foreground(r.Red).Background(r.Panel).
				Render("no such directory"), Width)
	}

	entry, ok := m.Selected()
	if !ok {
		dir, _ := splitPath(m.typed)
		return cell.Fill(r.Panel, " "+
			lipgloss.NewStyle().Foreground(r.Amber).Background(r.Panel).
				Render(cell.Truncate("no match in "+listable(dir), Width-2)), Width)
	}

	left, right := entry.Name+" · directory", "↵ to enter"
	if !entry.Dir {
		left = entry.Name + " · " + formatSize(entry)
		right = "document · original bytes"
		if m.AsPhoto() {
			right = "photo · recompressed"
		}
	}

	left = cell.Truncate(left, max(Width-cell.Width(right)-3, 1))
	line := " " + body.Render(left)
	if pad := Width - cell.Width(line) - cell.Width(right) - 1; pad > 0 {
		line += strings.Repeat(" ", pad) + note.Render(right) + " "
	}
	return cell.Fill(r.Panel, line, Width)
}

// hintLine names the keys this surface honours, and names them by what they
// would do to the thing under the cursor rather than in the abstract: a
// toggle that cannot apply says so instead of appearing to be available.
func (m Model) hintLine() string {
	entry, ok := m.Selected()

	open := "attach"
	if ok && entry.Dir {
		open = "open"
	}

	hints := [][2]string{{"↵", open}, {"⇥", "complete"}}
	switch {
	case !ok || entry.Dir:
		// Nothing to send: the toggle is left off the row entirely rather
		// than shown inert.
	case !entry.Image:
		hints = append(hints, [2]string{"^t", "document only"})
	case m.AsPhoto():
		hints = append(hints, [2]string{"^t", "as document"})
	default:
		hints = append(hints, [2]string{"^t", "as photo"})
	}
	hints = append(hints, [2]string{"←", "up"}, [2]string{"esc", "cancel"})

	key := theme.OverlayKey(m.roles)
	label := theme.OverlayMuted(m.roles)

	line := ""
	for _, hint := range hints {
		line += "  " + key.Render(hint[0]) + " " + label.Render(hint[1])
	}
	return cell.Fill(m.roles.Panel, strings.TrimPrefix(line, " "), Width)
}
