package render

import (
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// maxBlockWidth caps how wide a framed block is allowed to grow
// (docs/tui-2.0.md, "Rich text and blocks"). A code frame stretched across a
// 200-column terminal is a worse read than the same frame at 84: the eye has
// to travel the whole width to find the start of the next line, and the
// frame stops reading as one object.
const maxBlockWidth = 84

// minCardWidth is the body width a three-row media card needs. Below it the
// card collapses to a single line — both forms are in the goldens
// (frame-137x29 draws the card, frame-120x40 the collapsed line).
const minCardWidth = 40

// blockKind classifies a run of a message's text.
type blockKind int

const (
	blockText  blockKind = iota // ordinary prose, inline entities applied
	blockCode                   // pre / pre-with-language
	blockQuote                  // blockquote
)

// block is one run of a message's text, in rune offsets into ft.Text.
type block struct {
	kind     blockKind
	start    int32
	end      int32
	language string
}

// splitBlocks divides formatted text into block-level runs.
//
// Pre, PreCode and BlockQuote are the three entities that are not inline:
// they own whole lines and get a frame or a rule of their own, so they have
// to come out of the text flow before anything is styled. Everything between
// them is prose.
//
// Nested block entities are ignored — the outermost wins. Telegram does not
// produce a quote inside a code fence, and treating one as both would mean
// deciding which frame goes outside; the outer one is the one the sender
// drew.
func splitBlocks(ft *telegram.FormattedText) []block {
	if ft == nil || ft.Text == "" {
		return nil
	}
	n := int32(len([]rune(ft.Text)))

	var marks []block
	for _, e := range ft.Entities {
		if e == nil || e.Type == nil {
			continue
		}
		lo := max(e.Offset, 0)
		hi := min(e.Offset+e.Length, n)
		if lo >= hi {
			continue
		}
		switch t := e.Type.(type) {
		case *telegram.TextEntityTypePre:
			marks = append(marks, block{kind: blockCode, start: lo, end: hi})
		case *telegram.TextEntityTypePreCode:
			marks = append(marks, block{kind: blockCode, start: lo, end: hi, language: t.Language})
		case *telegram.TextEntityTypeBlockQuote:
			marks = append(marks, block{kind: blockQuote, start: lo, end: hi})
		}
	}

	if len(marks) == 0 {
		return []block{{kind: blockText, start: 0, end: n}}
	}

	// Earliest first, and widest first on a tie, so the outermost of a
	// nested pair is the one that survives the overlap check below.
	slices.SortFunc(marks, func(a, b block) int {
		if a.start != b.start {
			return int(a.start - b.start)
		}
		return int(b.end - a.end)
	})

	var out []block
	pos := int32(0)
	for _, m := range marks {
		if m.start < pos {
			continue // nested in, or overlapping, a block already taken
		}
		if m.start > pos {
			out = append(out, block{kind: blockText, start: pos, end: m.start})
		}
		out = append(out, m)
		pos = m.end
	}
	if pos < n {
		out = append(out, block{kind: blockText, start: pos, end: n})
	}
	return out
}

// RenderText lays formatted text out as body lines, exactly as a message
// body is drawn: blocks split out, entities styled, everything wrapped to
// width with each line closing its own styles.
//
// It is exported for the composer's preview, which has to be drawn by the
// same code that draws received messages. A preview rendered by a second
// implementation is a preview that can disagree with what arrives, and being
// trusted is the entire purpose of it.
//
// No hyperlinks and no revealed spoilers: this is what the text WILL be, and
// a clickable link in a draft is an invitation to click a thing that has not
// been sent yet.
func RenderText(ft *telegram.FormattedText, roles theme.Roles, width int) []string {
	return renderBlocks(ft, roles, width, textOpts{})
}

// renderBlocks lays a message's text out as body lines.
func renderBlocks(ft *telegram.FormattedText, roles theme.Roles, width int, o textOpts) []string {
	if ft == nil || ft.Text == "" || width < 1 {
		return nil
	}

	runes := []rune(ft.Text)
	styles := inlineStyles(ft, len(runes))

	var out []string
	for _, b := range splitBlocks(ft) {
		switch b.kind {
		case blockCode:
			out = append(out, renderCodeBlock(string(runes[b.start:b.end]), b.language, roles, width)...)
		case blockQuote:
			out = append(out, renderQuoteBlock(string(runes[b.start:b.end]), roles, width)...)
		default:
			// Trim the newlines at a block's edges. They are the separator
			// the sender typed between prose and a fence, not content: kept,
			// they draw a blank row above every code block and below every
			// quote, which reads as a rendering fault rather than as
			// spacing. Blank lines INSIDE prose survive, because those are
			// the ones a sender meant.
			lo, hi := b.start, b.end
			for lo < hi && runes[lo] == '\n' {
				lo++
			}
			for hi > lo && runes[hi-1] == '\n' {
				hi--
			}
			if lo == hi {
				continue
			}
			styled := renderRunes(runes[lo:hi], styles[lo:hi], roles, o)
			out = append(out, wrapProse(styled, roles, width)...)
		}
	}
	return out
}

// wrapProse wraps prose to width, giving list items a hanging indent so a
// wrapped bullet's continuation lines up under its own text rather than
// under the bullet.
//
// The list marker is recognised, never rewritten: a line that starts with
// "- ", "* ", "· " or "1. " keeps exactly the characters the sender typed
// and gains only an indent and a coloured marker. Telegram has no list
// entity, so anything more would be this client deciding what someone meant.
func wrapProse(styled string, roles theme.Roles, width int) []string {
	var out []string
	for _, line := range strings.Split(styled, "\n") {
		marker := listMarker(line)
		if marker == 0 || width <= marker+2 {
			for _, w := range cell.WrapLines(line, width) {
				out = append(out, cell.Clamp(w, width))
			}
			continue
		}

		// Wrap the whole line to the narrower body, then indent every line
		// after the first by the marker's width. Wrapping the remainder
		// separately would lose the marker's own cells from the first line's
		// budget and let it run one word long.
		indent := strings.Repeat(" ", marker)
		wrapped := cell.WrapLines(line, width-marker)
		for i, w := range wrapped {
			if i == 0 {
				out = append(out, markListMarker(w, marker, roles))
				continue
			}
			out = append(out, indent+w)
		}
	}
	return out
}

// listMarker returns the display width of a leading list marker, including
// the space after it, or 0 when the line does not start with one.
func listMarker(line string) int {
	plain := stripSGRLocal(line)
	trimmed := strings.TrimLeft(plain, " ")
	lead := len(plain) - len(trimmed)

	for _, m := range []string{"- ", "* ", "· ", "• "} {
		if strings.HasPrefix(trimmed, m) {
			return lead + cell.Width(m)
		}
	}

	// An ordinal: digits then "." or ")" then a space.
	i := 0
	for i < len(trimmed) && trimmed[i] >= '0' && trimmed[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(trimmed) && (trimmed[i] == '.' || trimmed[i] == ')') && trimmed[i+1] == ' ' {
		return lead + i + 2
	}
	return 0
}

// markListMarker recolours the first marker cells of a line.
//
// It only acts on a line whose marker survived styling as plain text. A
// marker the sender put inside bold or a link keeps its own styling — the
// entity the sender chose outranks a decoration this renderer inferred.
func markListMarker(line string, marker int, roles theme.Roles) string {
	if len(line) < marker || stripSGRLocal(line[:marker]) != line[:marker] {
		return line
	}
	return lipgloss.NewStyle().Foreground(roles.Cyan).Render(line[:marker]) + line[marker:]
}

// renderQuoteBlock draws a blockquote: a ghost left rule and dim italic
// text, so it reads as somebody else's words without competing with the
// message that quotes them.
func renderQuoteBlock(text string, roles theme.Roles, width int) []string {
	const rule = "│ "
	inner := width - cell.Width(rule)
	if inner < 1 {
		inner = 1
	}

	prefix := lipgloss.NewStyle().Foreground(roles.Ghost).Render(rule)
	body := lipgloss.NewStyle().Foreground(roles.Dim).Italic(true)

	var out []string
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		for _, w := range cell.WrapLines(line, inner) {
			out = append(out, ruledLine(prefix, w, body, width))
		}
	}
	return out
}

// ruledLine draws one line of a ruled block — a quote, a link preview — and
// drops the rule when the pane cannot hold it and the line both.
//
// The rule is this renderer's own mark and the line is somebody's words, so
// when only one of them fits it is the mark that goes. Two things reach
// here: a body column narrower than the two-cell rule plus one cell of
// content, and a wide rune that cell.WrapLines emitted whole rather than
// dropping the sender's character. Both would otherwise paint over whatever
// the grid put to the right of the body.
func ruledLine(prefix, text string, style lipgloss.Style, width int) string {
	if row := prefix + style.Render(text); cell.Width(row) <= width {
		return row
	}
	return style.Render(cell.Truncate(text, width))
}

// renderCodeBlock draws a framed code block: a language tag in the top rule,
// line numbers, and colour on the lines where colour means something.
//
// Lines are TRUNCATED horizontally, never wrapped. Code is a grid — its
// indentation carries the structure — and a wrapped line puts a fragment at
// column zero where a new statement belongs, which is exactly the thing the
// reader is scanning for.
func renderCodeBlock(text, language string, roles theme.Roles, width int) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")

	frameW := min(width, maxBlockWidth)
	numW := len(strconv.Itoa(len(lines)))

	// The cheapest frame there is: two borders, the line number, the two
	// spaces of the narrow gutter, and one cell of code. Below it the box
	// would not fit around the thing it is a box for — and drawn anyway it
	// would run past the body column and over whatever the grid put to the
	// right of it. The code is still the message, so show it bare.
	if frameW < numW+5 {
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			out = append(out, lipgloss.NewStyle().Foreground(roles.Fg).Render(cell.Truncate(line, width)))
		}
		return out
	}

	// A comfortable gutter is " N   "; a cramped one is "N  ". The wide
	// form is what the goldens draw at 137 columns and the narrow one what
	// they draw at 120, so both are real rather than one being a fallback.
	lead, trail := 1, 3
	if frameW-2-numW-lead-trail < 24 {
		lead, trail = 0, 2
	}
	codeW := frameW - 2 - numW - lead - trail
	if codeW < 1 {
		codeW = 1
	}

	border := lipgloss.NewStyle().Foreground(roles.Border)
	tag := lipgloss.NewStyle().Foreground(roles.Amber)
	meta := lipgloss.NewStyle().Foreground(roles.Faint)
	num := lipgloss.NewStyle().Foreground(roles.Ghost)

	out := []string{codeHeader(language, len(lines), frameW, border, tag, meta)}
	for i, line := range lines {
		body := lipgloss.NewStyle().Foreground(codeLineColour(line, roles)).
			Render(cell.Fit(cell.Truncate(line, codeW), codeW))
		out = append(out,
			border.Render("│")+
				strings.Repeat(" ", lead)+
				num.Render(cell.PadLeft(strconv.Itoa(i+1), numW))+
				strings.Repeat(" ", trail)+
				body+
				border.Render("│"))
	}
	out = append(out, border.Render("└"+strings.Repeat("─", frameW-2)+"┘"))
	return out
}

// codeHeader is the frame's top rule: the language on the left, the size on
// the right, and a rule between them.
//
// The size says how much is here without the reader counting, which matters
// because the block does not scroll independently — a "4 lines" block is
// read in place and a "180 lines" one is a reason to open the message
// somewhere else.
func codeHeader(language string, n, frameW int, border, tag, meta lipgloss.Style) string {
	right := strconv.Itoa(n) + " lines"
	if n == 1 {
		right = "1 line"
	}

	left := "┌"
	if language != "" {
		left = "┌ " + cell.Truncate(language, 16) + " "
	}
	head := left
	tail := " " + right + " ┐"

	ruleW := frameW - cell.Width(head) - cell.Width(tail)
	if ruleW < 0 {
		// No room for the language tag: drop it rather than the size, which
		// is the part that cannot be recovered by reading the code.
		head = "┌"
		ruleW = max(frameW-cell.Width(head)-cell.Width(tail), 0)
		language = ""
	}

	styled := border.Render("┌")
	if language != "" {
		styled += " " + tag.Render(cell.Truncate(language, 16)) + " "
	}
	styled += border.Render(strings.Repeat("─", ruleW)) +
		" " + meta.Render(right) + " " + border.Render("┐")
	return cell.Fit(styled, frameW)
}

// codeLineColour colours only the lines where colour carries information:
// the two halves of a diff, and comments. Everything else is body text.
//
// Deliberately not syntax highlighting. A highlighter needs a grammar per
// language and gets the rest wrong; these three cases are recognisable from
// the first character in every language that has them, and they are the ones
// a reader is scanning a pasted block for.
func codeLineColour(line string, roles theme.Roles) lipgloss.Color {
	trimmed := strings.TrimLeft(line, " \t")
	switch {
	case strings.HasPrefix(trimmed, "+++") || strings.HasPrefix(trimmed, "---"):
		// A unified-diff file header, not an added or removed line.
		return roles.Dim
	case strings.HasPrefix(trimmed, "+"):
		return roles.Green
	case strings.HasPrefix(trimmed, "-"):
		return roles.Red
	case strings.HasPrefix(trimmed, "#"), strings.HasPrefix(trimmed, "//"),
		strings.HasPrefix(trimmed, "--"), strings.HasPrefix(trimmed, ";"):
		return roles.Dim
	}
	return roles.Fg
}

// stripSGRLocal removes SGR escape sequences. render needs its own copy
// because chatview's lives in another package and neither is worth an
// exported helper of its own.
func stripSGRLocal(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' && s[j] != 0x1b {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
