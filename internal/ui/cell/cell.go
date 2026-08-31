// Package cell is the single source of terminal geometry: measuring text in
// display cells and cutting or padding it to an exact width.
//
// Every layout decision in the UI must go through this package. The rule it
// enforces is that a terminal's unit is the *cell*, which is neither a byte,
// nor a rune, nor a grapheme:
//
//   - "四字" is 2 runes and 4 cells.
//   - A ZWJ family emoji is 7 runes, 1 grapheme, and 2 cells — but summing
//     runewidth.RuneWidth over its runes says 8. That mistake is what
//     produced the one defect found when the TUI 2.0 goldens were first
//     reviewed (docs/tui-2.0.md, decision 11).
//   - A styled string carries escape sequences that occupy no cells at all.
//
// [Width] handles all three. Measuring with len(), len([]rune(s)), or a
// per-rune width sum does not, and each failure mode shears a panel in a way
// that is invisible in ASCII-only testing.
//
// Rune indexing is still correct for things that are not geometry — cursor
// positions in a text buffer, Telegram's rune-indexed entity offsets, a
// parser's cursor. This package is about how wide something *draws*, not
// where it sits in a string.
package cell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Ellipsis is the marker appended by [Truncate] when it had to cut.
const Ellipsis = "…"

// Width returns the display width of a single line in terminal cells.
// ANSI escape sequences are ignored and grapheme clusters are measured as
// the terminal draws them.
//
// "As the terminal draws them" is a claim the Unicode tables cannot make on
// their own for composed emoji, which is what [EmojiMode] is about; in
// [EmojiSeparate] this measures the pieces instead. For a row being laid out
// against a budget rather than measured after the fact, use [Reserve].
//
// For multi-line text use [MaxWidth]; this counts a newline as nothing and
// so would under-report a block.
func Width(s string) int {
	if CurrentEmojiMode() == EmojiSeparate {
		return separateWidth(s)
	}
	return ansi.StringWidth(s)
}

// MaxWidth returns the width of the widest line in possibly-multi-line
// text — the width of the box that would contain it.
func MaxWidth(s string) int {
	widest := 0
	for _, line := range strings.Split(s, "\n") {
		if w := Width(line); w > widest {
			widest = w
		}
	}
	return widest
}

// Truncate clamps s to at most maxWidth display cells, appending an
// ellipsis when it actually had to cut something. It is ANSI-safe, so an
// already-styled string is not corrupted mid-escape.
//
// This is the primary truncation used for user-supplied text — chat titles,
// message previews, folder labels — where the reader benefits from seeing
// that something was elided.
func Truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if Width(s) <= maxWidth {
		return s
	}
	return ansi.Truncate(s, maxWidth, Ellipsis)
}

// Clamp is [Truncate] without the ellipsis: a hard cap for cases where the
// cut is a safety net rather than something to show the reader, or where
// the caller has already added its own marker.
func Clamp(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if Width(s) <= maxWidth {
		return s
	}
	return ansi.Truncate(s, maxWidth, "")
}

// ClampLeft removes width cells from the *start* of s, keeping the tail.
//
// This is what a single-line input field needs when the cursor has run past
// the right edge: the visible window slides right by dropping leading cells,
// so the cursor stays in view.
func ClampLeft(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.TruncateLeft(s, width, "")
}

// Wrap breaks s onto lines of at most width cells, preferring word
// boundaries. An unbroken token longer than width is hard-broken rather
// than allowed to overflow, since a row wider than its column shears the
// frame.
//
// Wrapping is ANSI-aware in the sense that escape sequences are not counted
// as cells and are not cut in half. It does NOT make each line
// independently styled — see [WrapLines], which is what a caller composing
// rows out of the result almost always wants.
func Wrap(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Wrap(s, width, "")
}

// WrapLines wraps s to width and returns lines that are each self-contained:
// a style spanning a break is closed at the end of one line and reopened at
// the start of the next.
//
// [Wrap] alone is not enough for anything drawn into a column. It leaves the
// opening sequence on the first line and the reset on the last, so the lines
// between carry no styling of their own — and a terminal does not reset at a
// newline. In a multi-column frame the rows of one panel are not adjacent on
// screen: whatever a body line leaves open bleeds through its own trailing
// padding, across the panel rule, and into the next column, for as many rows
// as it takes to reach the reset.
//
// This is the ONLY correct way to wrap styled text into rows. There is no
// version of the bug that shows up in a single-column layout, which is why
// it survives review so easily.
func WrapLines(s string, width int) []string {
	if width <= 0 {
		return nil
	}
	lines := strings.Split(Wrap(s, width), "\n")

	open, link := "", ""
	for i, line := range lines {
		if link != "" {
			line = link + line
		}
		if open != "" {
			line = open + line
		}
		open, link = OpenStyle(line), OpenLink(line)
		if open != "" {
			line += "\x1b[0m"
		}
		if link != "" {
			line += LinkClose
		}
		lines[i] = line
	}
	return lines
}

// LinkClose ends an OSC 8 hyperlink: the same sequence with an empty URI.
const LinkClose = "\x1b]8;;\x1b\\"

// OpenLink returns the OSC 8 opening sequence left active at the end of s,
// verbatim so it can be re-emitted, or "" when s closes what it opened.
//
// It is [OpenStyle]'s counterpart for hyperlinks, and it exists for the same
// reason. `ansi.Wrap` breaks a line between a link's opening and closing
// sequences without repairing either, so the first line ends with the link
// still open and the second carries a close that opens nothing. Every
// terminal that understands OSC 8 then treats the rest of that first row —
// its trailing padding, the panel rule, and the column beside it — as part
// of the link.
//
// The design record concluded this could not be fixed the way the SGR leak
// was, because reopening a hyperlink means knowing which runes belong to it
// after wrapping. That is true of a wrapper that has to infer the URI, and
// not true here: the URI is carried in the opening sequence, so the sequence
// is its own answer. See docs/tui-2.0.md, divergence 14.
//
// Unlike OpenStyle this does not accumulate. A hyperlink is not a mode that
// composes — a second OSC 8 replaces the first, and an empty URI ends it.
func OpenLink(s string) string {
	open := ""
	for i := 0; i+1 < len(s); {
		if s[i] != 0x1b || s[i+1] != ']' {
			i++
			continue
		}
		n, _ := escapeAt(s, i)
		if n == 0 {
			break // unterminated: nothing after it can close it either
		}
		seq := s[i : i+n]
		if isLinkOpen(seq) {
			open = seq
		} else if isLinkClose(seq) {
			open = ""
		}
		i += n
	}
	return open
}

// isLinkOpen reports whether seq is an OSC 8 with a non-empty URI. The
// payload is "8;params;URI", so the URI is whatever follows the second
// semicolon — params may itself be empty, which is the common spelling.
func isLinkOpen(seq string) bool {
	uri, ok := linkURI(seq)
	return ok && uri != ""
}

func isLinkClose(seq string) bool {
	uri, ok := linkURI(seq)
	return ok && uri == ""
}

func linkURI(seq string) (string, bool) {
	body := strings.TrimPrefix(seq, "\x1b]")
	body = strings.TrimSuffix(strings.TrimSuffix(body, "\x1b\\"), "\a")
	rest, ok := strings.CutPrefix(body, "8;")
	if !ok {
		return "", false
	}
	_, uri, ok := strings.Cut(rest, ";")
	if !ok {
		return "", false
	}
	return uri, true
}

// Link wraps text in an OSC 8 hyperlink. A caller that has no URI, or is
// drawing into a terminal that was not asked for hyperlinks, should not call
// it rather than passing "" — an empty URI is the CLOSING sequence, and
// emitting one that closes nothing is how a link leaks in the other
// direction.
func Link(uri, text string) string {
	if uri == "" {
		return text
	}
	return "\x1b]8;;" + uri + "\x1b\\" + text + LinkClose
}

// OpenStyle returns the SGR state left active at the end of s: the
// concatenation of the sequences that have not been cancelled by a reset,
// and "" when s closes everything it opens.
//
// [WrapLines] uses it to reopen a run on the next line. It is exported
// because "this row leaves no style open" is an invariant every component
// that draws into a column has to hold, and asserting it is the only way to
// catch a leak that is invisible in a single-column dump.
//
// Accumulating rather than keeping only the last sequence, because a styled
// run can be opened in pieces — lipgloss emits one combined sequence, but
// nested renders produce several — and dropping the earlier ones would
// reopen a continuation line in half its original style.
func OpenStyle(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return ""
	}

	var open strings.Builder
	for i := 0; i < len(s); {
		if s[i] != 0x1b || i+1 >= len(s) || s[i+1] != '[' {
			i++
			continue
		}
		j := i + 2
		for j < len(s) && s[j] != 'm' && s[j] != 0x1b {
			j++
		}
		if j >= len(s) || s[j] != 'm' {
			// Not an SGR sequence (a cursor move, say). Nothing here
			// styles text, so it cannot be part of the open state.
			i++
			continue
		}
		seq := s[i : j+1]
		if seq == "\x1b[0m" || seq == "\x1b[m" {
			open.Reset()
		} else {
			open.WriteString(seq)
		}
		i = j + 1
	}
	return open.String()
}

// Pad right-pads s with spaces to width cells. A string already at or over
// width is returned unchanged — callers wanting a hard cap should [Clamp]
// first, or use [Fit], which does both.
func Pad(s string, width int) string {
	w := Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// PadLeft left-pads s with spaces to width cells, i.e. right-aligns it in
// a field of that width. A string already at or over width is returned
// unchanged — [Truncate] first if it must also be capped.
func PadLeft(s string, width int) string {
	w := Width(s)
	if w >= width {
		return s
	}
	return strings.Repeat(" ", width-w) + s
}

// Fit clamps s to exactly width cells: truncated without an ellipsis if
// wider, space-padded if narrower.
//
// This is the final guarantee for an assembled row whose individual field
// budgets are each best-effort — it is what makes "every line is exactly
// the frame width" true rather than aspirational.
func Fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return Pad(Clamp(s, width), width)
}

// Fill fits s to exactly width cells and paints colour behind every one of
// them — including the cells covered by s's own styled spans.
//
// This exists because the obvious spelling silently does not work:
//
//	lipgloss.NewStyle().Background(r.Panel).Render(cell.Fit(line, width))
//
// emits the background once, at the front. Every styled span inside the line
// closes itself with ESC[0m, and a reset clears the background along with
// the foreground, so the panel colour survives only as far as the first span
// and the rest of the row is drawn on whatever the terminal's default
// happens to be. In a two-line chat row that shows up as a title line with
// no fill at all and a preview line whose fill stops where the text does.
//
// It is the same family of defect as [WrapLines]: SGR is a mode, and a reset
// is not a scope. Fill therefore re-opens the background after every reset in
// s, which also means a span may set its own colours — a selection bar, an
// unread badge on cyan — and the line still returns to the surface rather
// than to nothing. Because the surface is re-opened BEFORE each span's own
// sequences, a nested Fill wins over an outer one: a row that painted itself
// sel keeps sel when the frame fills its column with panel.
func Fill(colour lipgloss.Color, s string, width int) string {
	if width <= 0 {
		return ""
	}
	line := Fit(s, width)
	style := lipgloss.NewStyle().Background(colour)

	var b strings.Builder
	start := 0
	for i := 0; i+1 < len(line); i++ {
		if line[i] != 0x1b || line[i+1] != '[' {
			continue
		}
		n := 0
		switch {
		case strings.HasPrefix(line[i:], "\x1b[0m"):
			n = 4
		case strings.HasPrefix(line[i:], "\x1b[m"):
			n = 3
		default:
			continue
		}
		// The reset itself is dropped: the style's own trailing reset
		// closes the segment, so keeping both would emit it twice.
		if seg := line[start:i]; seg != "" {
			b.WriteString(style.Render(seg))
		}
		start = i + n
		i += n - 1
	}
	if seg := line[start:]; seg != "" {
		b.WriteString(style.Render(seg))
	}
	return b.String()
}

// FillRows fits each row to width, paints colour behind every cell of it,
// and joins them.
//
// It exists because [Fill] kept being applied one row at a time and then
// forgotten on the next surface. Every overlay assembles a row out of styled
// spans and hands it to a background style, and every styled span closes
// itself with ESC[0m — which clears the background along with the
// foreground. So the surface survives as far as the first span and the rest
// of the row shows the terminal through it. That is divergence 19, and it
// arrived a second time in the overlays, which were migrated to the palette
// after the panels were fixed and did not get the fix that went with it.
//
// One function rather than a call site per surface, so the next one written
// gets it by using it.
func FillRows(colour lipgloss.Color, rows []string, width int) string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = Fill(colour, row, width)
	}
	return strings.Join(out, "\n")
}

// PaintedWidth returns how many leading display cells of s are drawn with
// some background colour in effect, stopping at the first cell that is not.
//
// It is the assertion that makes [Fill] testable, and it is a count rather
// than a boolean because the interesting failure is a row whose fill dies two
// thirds of the way along — the column it died at is the thing worth putting
// in the message. It deliberately does not care WHICH background: a row is
// correct when no cell falls through to the terminal's default, and the
// badge, the selection bar and the surface are all legitimate answers.
//
// A colour profile with no colour renders no sequences at all, so this
// returns 0 for every string under the default `go test` profile. That is
// the point: a package asserting on it must pin a profile in TestMain, which
// turns a vacuous styling test into a failing one.
func PaintedWidth(s string) int {
	painted, bg := 0, false
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			// An SGR sequence changes the background; anything else — an OSC
			// 8 hyperlink, a cursor move — does not, and is stepped over.
			// The step is why this branch handles every escape rather than
			// only SGR: falling through to the text scan below with i still
			// on the ESC would measure an empty run and never advance.
			if n, sgr := escapeAt(s, i); n > 0 {
				if sgr != "" || isReset(s, i) {
					bg = applySGR(bg, sgr)
				}
				i += n
				continue
			}
			// A lone trailing ESC: nothing follows it to draw.
			return painted
		}
		j := i
		for j < len(s) && s[j] != 0x1b {
			j++
		}
		if !bg {
			return painted
		}
		painted += Width(s[i:j])
		i = j
	}
	return painted
}

// escapeAt measures the escape sequence starting at s[i] and returns its
// byte length, plus its parameter list when it is an SGR sequence.
//
// A zero length means s[i] does not begin a sequence this understands, which
// callers must treat as "stop" rather than "skip one byte" — see the loop in
// [PaintedWidth], where advancing by anything less than the whole sequence
// would measure its bytes as drawable text.
func escapeAt(s string, i int) (n int, sgrParams string) {
	if i+1 >= len(s) {
		return 0, ""
	}
	switch s[i+1] {
	case '[': // CSI: ends at the first byte in @-~
		for j := i + 2; j < len(s); j++ {
			if c := s[j]; c >= '@' && c <= '~' {
				if c == 'm' {
					return j + 1 - i, s[i+2 : j]
				}
				return j + 1 - i, ""
			}
		}
		return 0, ""
	case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC: end at ST or BEL
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1 - i, ""
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2 - i, ""
			}
		}
		return 0, ""
	default: // a two-byte escape
		return 2, ""
	}
}

// isReset reports whether s[i] begins ESC[0m or ESC[m, whose empty parameter
// list [escapeAt] cannot tell apart from a non-SGR sequence.
func isReset(s string, i int) bool {
	return strings.HasPrefix(s[i:], "\x1b[0m") || strings.HasPrefix(s[i:], "\x1b[m")
}

// applySGR folds one SGR parameter list into "is a background set".
//
// The extended-colour forms have to be parsed rather than scanned for,
// because their arguments look like other codes: a true-colour foreground of
// ESC[38;2;16;100;7m contains the token "100", which read on its own is the
// bright-black background. Skipping each 38/48/58 introducer's own arguments
// is what keeps that from reading as a painted cell.
func applySGR(bg bool, params string) bool {
	if params == "" {
		return false // ESC[m is ESC[0m
	}
	fields := strings.Split(params, ";")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case f == "0" || f == "":
			bg = false
		case f == "49":
			bg = false
		case f == "38" || f == "48" || f == "58":
			if f == "48" {
				bg = true
			}
			if i+1 < len(fields) {
				switch fields[i+1] {
				case "5":
					i += 2
				case "2":
					i += 4
				}
			}
		case len(f) == 2 && f[0] == '4' && f[1] >= '0' && f[1] <= '7':
			bg = true
		case len(f) == 3 && f[0] == '1' && f[1] == '0' && f[2] >= '0' && f[2] <= '7':
			bg = true
		}
	}
	return bg
}

// FitLine renders s through style, clamped to exactly one line and at most
// totalWidth display cells — padded with the style's own
// background/whitespace if s is shorter.
//
// This exists to close a whole class of bug: lipgloss's Style.Width(w)
// treats w as the TOTAL rendered width, INCLUDING the style's own padding,
// and when content-plus-padding would exceed w, lipgloss WORD-WRAPS the
// content onto additional lines rather than truncating or overflowing (see
// lipgloss Style.Render: "if !inline && width > 0 { wrapAt := width -
// leftPadding - rightPadding; str = ansi.Wrap(str, wrapAt, "") }", applied
// before the padding is even added). Style.MaxWidth doesn't help either —
// it truncates each already-wrapped line individually, so it produces more
// short lines instead of fewer.
//
// Concretely: content built to be exactly totalWidth cells wide and then
// handed to a *padded* style's .Width(totalWidth) silently wraps instead of
// rendering as one line. That single misunderstanding produced four
// independent-looking bugs across this codebase: chat list rows growing past
// their fixed 2-line stride (desyncing row-index math), folder tab
// backgrounds wrapping so a "visible" tab was never actually painted, and
// the status bar hints line splitting mid-word. FitLine is the fix for all
// of them: it truncates s to totalWidth - style.GetHorizontalFrameSize()
// display cells BEFORE handing it to style.Width, so the internal word-wrap
// never has anything to wrap.
//
// s is assumed to be single-line content (no embedded "\n"); render each
// line of a multi-line row through FitLine separately and join with "\n"
// rather than passing multi-line input in.
//
// The style is expected to be a SINGLE-ROW one: padding, colours, bold. A
// border or a vertical margin adds rows of its own, which no amount of
// horizontal budgeting can collapse back to one, and a horizontal margin is
// laid outside the width lipgloss is given rather than inside it. No row
// style in this app has any of the three; a caller that wants a box wants
// theme.OverlayFrame, which is not fitted to a line.
//
// Note: if totalWidth is smaller than style.GetHorizontalFrameSize(),
// lipgloss cannot shrink the style's padding below its natural size, so the
// result can exceed totalWidth in that degenerate case. No panel in this app
// is ever that narrow.
func FitLine(style lipgloss.Style, s string, totalWidth int) string {
	if totalWidth <= 0 {
		return ""
	}
	budget := totalWidth - style.GetHorizontalFrameSize()
	if budget < 0 {
		budget = 0
	}
	return style.Width(totalWidth).Render(Clamp(s, budget))
}
