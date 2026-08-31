// Package help provides a dumb, reusable, lazygit-style help overlay: a
// centered, capped-size box listing key-binding sections. It holds no
// application state and knows nothing about actual keybindings beyond
// what SetSections is given — callers own deciding what to show, when to
// show it, and closing it (this component never consumes esc/? itself).
package help

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// Section is one titled group of key bindings shown in the overlay (e.g.
// "Navigation", "Search", "Folders"), rendered in the order given.
type Section struct {
	Title    string
	Bindings []Binding
}

// Binding is a single key-binding entry: the key(s) that trigger it and
// a short description of what they do.
type Binding struct {
	Keys string
	Desc string
}

// defaultFooter is shown when SetFooter has never been called (or was
// called with ""). It's a reasonable default for a caller that hasn't
// wired real keybinding text yet, but callers with configurable/rebound
// keys (e.g. keys.help) should call SetFooter with the real text — this
// component has no way to know what key actually closes it.
const defaultFooter = "↑↓/jk: scroll · Esc/?: close"

// Model is the help overlay component.
type Model struct {
	roles    theme.Roles
	width    int
	height   int
	visible  bool
	sections []Section
	offset   int    // scroll offset, in content lines
	footer   string // "" means defaultFooter
}

// New creates a help overlay drawn from the semantic palette.
func New(r theme.Roles) Model {
	return Model{roles: r}
}

// SetSections replaces the sections shown, in the order given. Resets
// scroll to the top, since a previous scroll offset is unlikely to still
// make sense against different content.
func (m *Model) SetSections(s []Section) {
	m.sections = s
	m.offset = 0
}

// SetFooter overrides the footer hint line (default: "↑↓/jk: scroll ·
// Esc/?: close"). Pass "" to fall back to the default. Exists because
// the actual keys that scroll/close the overlay are decided by the
// caller (e.g. a configurable/rebound keys.help), not by this component
// — a hardcoded footer here would silently drift out of sync with
// whatever the caller actually binds.
func (m *Model) SetFooter(text string) {
	m.footer = text
}

// SetVisible shows or hides the overlay. Resets scroll to the top on
// show, so re-opening the overlay always starts from the beginning.
func (m *Model) SetVisible(v bool) {
	m.visible = v
	if v {
		m.offset = 0
	}
}

// IsVisible reports whether the overlay is currently shown.
func (m Model) IsVisible() bool {
	return m.visible
}

// SetSize sets the window size the overlay is centered/clamped within.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Overlay geometry. The box is a centered, capped-size dialog rather than
// one stretched to the full window — the same geometry discipline the
// search overlay uses (see internal/ui/components/search/model.go's
// computeGeometry, which this mirrors): DialogBox contributes a 1-cell
// border and (1,2) padding per side.
const (
	// Width is capped because a keymap read across 200 columns is a keymap
	// nobody reads. HEIGHT is not: there is no such thing as a card that is
	// too tall to read, and a cap on it hides bindings behind a scroll
	// while leaving the screen half empty. A 28-row cap on a 60-row
	// terminal is how "} / {" came to be a binding this card knew about
	// and never showed.
	maxBoxWidth = 76
	minBoxWidth = 40

	// Two columns halve the scrolling on a terminal wide enough to hold
	// them. The threshold is what a second column needs to stay readable,
	// not a round number: two of minColumnWidth plus the gap between them,
	// plus the frame's own chrome.
	minColumnWidth = 46
	columnGap      = 3
	maxTwoColWidth = 2*maxBoxWidth + columnGap

	minBoxHeight = 12

	dialogChromeW = 6         // DialogBox: (border 1 + padding 2) * 2 sides
	dialogChromeH = 4         // DialogBox: (border 1 + padding 1) * 2 sides
	fixedRows     = 1 + 1 + 1 // title + blank separator + footer hint

	// minContentWidth mirrors search's own floor: below it DialogBox's
	// border+padding chrome no longer fits in boxWidth and silently
	// renders wider than asked instead of narrower.
	minContentWidth = 2

	// structuralMinWidth/structuralMinHeight are hard floors, distinct
	// from minBoxWidth/minBoxHeight: the smallest box DialogBox's fixed
	// chrome can actually render without itself overflowing past the
	// size it was asked for. computeGeometry prefers this floor over an
	// even smaller window-clamped size when the window can afford it —
	// but the window still wins below that (see computeGeometry's final
	// re-clamp), since exceeding the window entirely is worse than a box
	// that's merely cramped.
	structuralMinWidth  = dialogChromeW + minContentWidth
	structuralMinHeight = dialogChromeH + fixedRows + 1 // +1: at least one content row
)

// geometry is the overlay's computed layout.
type geometry struct {
	boxWidth   int
	boxHeight  int
	innerWidth int // content width for title/section/binding/footer rows
	contentH   int // rows available for the scrollable section content
}

// computeGeometry derives a centered overlay box from the full window
// size, capped so it never stretches to fill the screen and floored so
// it stays usable on a tiny terminal.
func computeGeometry(w, h int) geometry {
	boxW := w - 8
	// A wide window gets a wider box, but only because a SECOND COLUMN
	// fits in it — never a single column stretched across 200 cells, which
	// is a keymap nobody reads.
	cap := maxBoxWidth
	if boxW >= 2*minColumnWidth+columnGap+dialogChromeW {
		cap = maxTwoColWidth
	}
	if boxW > cap {
		boxW = cap
	}
	if boxW < minBoxWidth {
		boxW = minBoxWidth
	}
	if boxW > w {
		boxW = w
	}
	if boxW < structuralMinWidth {
		boxW = structuralMinWidth
	}
	// At truly pathological window sizes — narrower than the structural
	// floor itself — the floor above would win and the box would render
	// wider than the window entirely, not just cramped but overflowing.
	// The window is the one dimension this component can never
	// legitimately exceed (the caller places this box with
	// lipgloss.Place, which does not clip), so it gets the final say
	// even below the structural floor: better a box that's degraded past
	// "usable" than one that smears past the terminal edge. This is a
	// no-op whenever the window can actually afford the structural floor
	// (the common case), since boxW would already be <= w by then.
	if boxW > w {
		boxW = w
	}
	if boxW < 0 {
		boxW = 0
	}

	boxH := h - 6
	if boxH < minBoxHeight {
		boxH = minBoxHeight
	}
	if boxH > h {
		boxH = h
	}
	if boxH < structuralMinHeight {
		boxH = structuralMinHeight
	}
	// Same reasoning as boxW above.
	if boxH > h {
		boxH = h
	}
	if boxH < 0 {
		boxH = 0
	}

	innerW := boxW - dialogChromeW
	if innerW < 1 {
		innerW = 1
	}

	contentH := boxH - dialogChromeH - fixedRows
	if contentH < 1 {
		contentH = 1
	}

	return geometry{
		boxWidth:   boxW,
		boxHeight:  boxH,
		innerWidth: innerW,
		contentH:   contentH,
	}
}

// keysColumnWidth returns the display-cell width of the keys column: the
// widest Keys string across every section, capped so the description
// column always keeps a usable minimum regardless of how long some Keys
// string is.
func keysColumnWidth(sections []Section, innerWidth int) int {
	widest := 0
	for _, sec := range sections {
		for _, b := range sec.Bindings {
			if w := cell.Width(b.Keys); w > widest {
				widest = w
			}
		}
	}

	maxKeysW := innerWidth - 10 // leave >= 10 cells for "  "+desc when possible
	if maxKeysW < 4 {
		maxKeysW = 4
	}
	if widest > maxKeysW {
		widest = maxKeysW
	}
	if widest < 1 {
		widest = 1
	}
	return widest
}

// bindingLine renders one "keys  description" row, keys left-aligned and
// padded to keysW, followed by a 2-cell gap and the (possibly truncated)
// description. keys and desc are rendered through their own styles (no
// padding on either, so no lipgloss Width()-wrap risk — see
// cell.FitLine's doc comment) and concatenated; the combined,
// already-styled string is then passed through cell.FitLine with a
// bare style as the final ANSI-safe pad-to-width/truncate guarantee.
func bindingLine(b Binding, keysW, innerWidth int, keysStyle, descStyle lipgloss.Style) string {
	keys := cell.Pad(cell.Truncate(b.Keys, keysW), keysW)
	keysRendered := keysStyle.Render(keys)

	combined := keysRendered
	if descBudget := innerWidth - keysW - 2; descBudget > 0 && b.Desc != "" {
		combined += "  " + descStyle.Render(cell.Truncate(b.Desc, descBudget))
	}

	return cell.FitLine(lipgloss.NewStyle(), combined, innerWidth)
}

// bodyLines renders the full (unscrolled) section content as individual
// display lines, each already fit to innerWidth cells.
// bodyLines is the whole keymap as rows, laid out in one column or two
// depending on what the width affords.
//
// Two columns is not decoration: this keymap is 86 rows, so on any terminal
// under about 90 rows tall a single column puts most of it below the fold.
// A binding you have to scroll to find is one you look for in the source
// instead — which is what happened to "} / {".
func (m Model) bodyLines(innerWidth int) []string {
	mutedStyle := theme.OverlayMuted(m.roles)

	if len(m.sections) == 0 {
		return []string{cell.FitLine(mutedStyle, "No key bindings to show.", innerWidth)}
	}

	colW := innerWidth
	twoCol := innerWidth >= 2*minColumnWidth+columnGap
	if twoCol {
		colW = (innerWidth - columnGap) / 2
	}

	single := m.sectionLines(colW, mutedStyle)
	if !twoCol {
		return single
	}
	return joinColumns(single, colW, innerWidth, mutedStyle)
}

// joinColumns lays a flat body out side by side, splitting at the section
// boundary nearest the middle so a heading never ends up alone at the foot
// of the left column with its bindings in the right one.
func joinColumns(lines []string, colW, innerWidth int, blank lipgloss.Style) []string {
	split := splitPoint(lines)
	left, right := lines[:split], lines[split:]
	// The blank separator that WAS the boundary belongs to neither column.
	if len(right) > 0 && strings.TrimSpace(ansi.Strip(right[0])) == "" {
		right = right[1:]
	}

	rows := max(len(left), len(right))
	out := make([]string, rows)
	pad := strings.Repeat(" ", columnGap)
	empty := cell.Fit("", colW)

	for i := range rows {
		l, r := empty, ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = cell.Fit(cell.Fit(l, colW)+pad+r, innerWidth)
	}
	return out
}

// splitPoint is the blank line closest to the middle of the body, or the
// exact middle when there is none.
func splitPoint(lines []string) int {
	mid := (len(lines) + 1) / 2
	best, bestDist := mid, len(lines)
	for i, line := range lines {
		if strings.TrimSpace(ansi.Strip(line)) != "" {
			continue
		}
		if d := abs(i - mid); d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// sectionLines is the keymap as a single column of rows.
func (m Model) sectionLines(innerWidth int, mutedStyle lipgloss.Style) []string {
	keysW := keysColumnWidth(m.sections, innerWidth)
	sectionTitleStyle := theme.OverlayTitle(m.roles)
	keysStyle := theme.OverlayKey(m.roles).Bold(true)

	var lines []string
	for i, sec := range m.sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, cell.FitLine(sectionTitleStyle, sec.Title, innerWidth))
		for _, b := range sec.Bindings {
			lines = append(lines, bindingLine(b, keysW, innerWidth, keysStyle, mutedStyle))
		}
	}
	return lines
}

// hiddenRows describes what is off the top and bottom of the card, or ""
// when the whole keymap fits.
func (m Model) hiddenRows(g geometry) string {
	total := len(m.bodyLines(g.innerWidth))
	if total <= g.contentH {
		return ""
	}
	offset := clampOffset(m.offset, total, g.contentH)

	above, below := offset, total-offset-g.contentH
	switch {
	case above > 0 && below > 0:
		return "↑" + itoa(above) + " ↓" + itoa(below)
	case below > 0:
		return "↓" + itoa(below) + " more"
	default:
		return "↑" + itoa(above) + " above"
	}
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// clampOffset clamps a scroll offset into [0, max(0, total-visible)] —
// never negative, never past the point where the last line is at the
// bottom of the visible window.
func clampOffset(offset, total, visible int) int {
	maxOffset := total - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	return offset
}

// visibleBodyLines windows the full body content to at most contentH
// lines, honoring (clamped) scroll offset.
func (m Model) visibleBodyLines(innerWidth, contentH int) []string {
	all := m.bodyLines(innerWidth)
	if len(all) <= contentH {
		return all
	}
	offset := clampOffset(m.offset, len(all), contentH)
	end := offset + contentH
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end]
}

// Update handles j/k/up/down/pgup/pgdown/g/G scroll keys while the
// overlay is visible.
//
// This is NOT part of help's required contract (New/SetSections/
// SetVisible/IsVisible/SetSize/View): a caller that never invokes it
// still gets a fully working, correctly rendered overlay — it just can't
// scroll content that overflows the box height. A caller that wants
// scroll support should route key events here while IsVisible() is true;
// esc/? must stay handled by the caller outside Update (this component
// never closes itself), typically by checking those keys before ever
// reaching this method.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	g := computeGeometry(m.width, m.height)
	total := len(m.bodyLines(g.innerWidth))

	switch key.String() {
	case "up", "k":
		m.offset--
	case "down", "j":
		m.offset++
	case "pgup":
		m.offset -= g.contentH
	case "pgdown":
		m.offset += g.contentH
	case "g", "home":
		m.offset = 0
	case "G", "end":
		m.offset = total
	default:
		return m, nil
	}

	m.offset = clampOffset(m.offset, total, g.contentH)
	return m, nil
}

// View renders the overlay: a centered box with a title, the (possibly
// scrolled) section content, and a footer hint — or an empty string when
// not visible.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	g := computeGeometry(m.width, m.height)

	titleStyle := theme.OverlayTitle(m.roles)
	title := cell.FitLine(titleStyle, "Keyboard Shortcuts", g.innerWidth)

	body := m.visibleBodyLines(g.innerWidth, g.contentH)
	// Pad the body out to exactly contentH lines so the box height stays
	// constant regardless of how much content is currently showing.
	for len(body) < g.contentH {
		body = append(body, "")
	}

	footerText := m.footer
	if footerText == "" {
		footerText = defaultFooter
	}
	// A card that scrolls has to say it is scrolled. The footer already
	// advertises the keys; what it did not say is that there was anything
	// to use them on, so a binding below the fold was indistinguishable
	// from a binding that does not exist.
	if more := m.hiddenRows(g); more != "" {
		footerText = more + " · " + footerText
	}
	footerStyle := theme.OverlayMuted(m.roles)
	footer := cell.FitLine(footerStyle, footerText, g.innerWidth)

	rows := make([]string, 0, len(body)+3)
	rows = append(rows, title, "")
	rows = append(rows, body...)
	rows = append(rows, footer)
	content := strings.Join(rows, "\n")

	// g.boxWidth/g.boxHeight are the box's OUTER dimensions. DialogBox
	// adds its own 1-cell border on top of whatever is passed to
	// Width/Height (its (1,2) padding is baked into the style itself and
	// IS covered by the Width/Height argument) — passing g.boxWidth/
	// g.boxHeight directly would stretch the rendered box 2 cells past
	// what the geometry above computed. Mirrors search/model.go's View()
	// exactly (see its comment for the full accounting).
	return theme.OverlayFrame(m.roles).Padding(1, 2).
		Width(g.boxWidth - 2).
		Height(g.boxHeight - 2).
		Render(content)
}
