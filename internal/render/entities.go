package render

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// inlineStyle is the set of attributes active over one run of runes.
//
// A message's formatting is a set of overlapping ranges, not a sequence: a
// link inside a bold sentence, an italic word inside a blockquote, code
// inside a spoiler. Modelling it as "what is true of this rune" is what
// makes nesting fall out for free.
type inlineStyle struct {
	bold      bool
	italic    bool
	underline bool
	strike    bool
	code      bool
	spoiler   bool
	link      bool
	mention   bool
}

// inlineStyles builds a per-rune style table by layering every entity over
// the text.
//
// Layering, rather than walking the entities in offset order: the previous
// implementation emitted "the gap before this entity, then this entity",
// which double-printed every overlapped run. Telegram nests entities freely,
// so a link inside a bold sentence came out with its text twice.
//
// Offsets arrive already converted to rune indices and already clamped to
// the text (see telegram.formattedTextFromTG), but they are clamped again
// here: this function is also reachable from tests and from any future
// caller that builds a FormattedText by hand, and an out-of-range slice in a
// render path is a panic in the middle of a frame.
func inlineStyles(ft *telegram.FormattedText, n int) []inlineStyle {
	styles := make([]inlineStyle, n)
	if ft == nil {
		return styles
	}

	for _, e := range ft.Entities {
		if e == nil || e.Type == nil {
			continue
		}
		lo := max(int(e.Offset), 0)
		hi := min(int(e.Offset+e.Length), n)
		if lo >= hi {
			continue
		}

		var set func(*inlineStyle)
		switch e.Type.(type) {
		case *telegram.TextEntityTypeBold:
			set = func(s *inlineStyle) { s.bold = true }
		case *telegram.TextEntityTypeItalic:
			set = func(s *inlineStyle) { s.italic = true }
		case *telegram.TextEntityTypeUnderline:
			set = func(s *inlineStyle) { s.underline = true }
		case *telegram.TextEntityTypeStrikethrough:
			set = func(s *inlineStyle) { s.strike = true }
		case *telegram.TextEntityTypeCode:
			set = func(s *inlineStyle) { s.code = true }
		case *telegram.TextEntityTypeSpoiler:
			set = func(s *inlineStyle) { s.spoiler = true }
		case *telegram.TextEntityTypeTextURL, *telegram.TextEntityTypeURL,
			*telegram.TextEntityTypeEmailAddress:
			set = func(s *inlineStyle) { s.link = true }
		case *telegram.TextEntityTypeMention, *telegram.TextEntityTypeMentionName,
			*telegram.TextEntityTypeHashtag, *telegram.TextEntityTypeBotCommand:
			set = func(s *inlineStyle) { s.mention = true }
		default:
			// Pre, PreCode and BlockQuote are block-level and are taken out
			// by splitBlocks before this runs. Anything else is a
			// formatting kind this client does not render, and plain text
			// is the right answer for it rather than a guess.
			continue
		}
		for i := lo; i < hi; i++ {
			set(&styles[i])
		}
	}
	return styles
}

// style resolves one inline style to a lipgloss style.
//
// The colours are semantic (docs/tui-2.0.md, "Rich text and blocks"): bright
// bold, mauve italic, amber inline code, muted strike, underlined cyan
// links, blue mentions. The switch picks ONE foreground, most specific
// first — a bold link is a link, because where it goes matters more than
// that it is emphasised — while the attributes below it all accumulate.
//
// A hidden spoiler is drawn in its own background colour, so it occupies the
// right number of cells and reads as a deliberate block rather than as
// missing text. It is applied last, so no style above can leak the content
// through a foreground that is still legible against it.
func (s inlineStyle) style(roles theme.Roles, reveal bool) lipgloss.Style {
	out := lipgloss.NewStyle()

	switch {
	case s.code:
		out = out.Foreground(roles.Amber).Background(roles.Sel)
	case s.link:
		out = out.Foreground(roles.Cyan).Underline(true)
	case s.mention:
		out = out.Foreground(roles.Blue)
	case s.strike:
		out = out.Foreground(roles.Dim)
	case s.bold:
		out = out.Foreground(roles.Bright)
	case s.italic:
		out = out.Foreground(roles.Mauve)
	}

	if s.bold {
		out = out.Bold(true)
	}
	if s.italic {
		out = out.Italic(true)
	}
	if s.underline {
		out = out.Underline(true)
	}
	if s.strike {
		out = out.Strikethrough(true)
	}
	if s.spoiler && !reveal {
		out = out.Background(roles.Sel).Foreground(roles.Sel)
	}
	return out
}

// renderRunes styles a rune slice as a sequence of maximal constant-style
// runs. styles must be the table for exactly those runes.
func renderRunes(runes []rune, styles []inlineStyle, roles theme.Roles, reveal bool) string {
	if len(runes) == 0 {
		return ""
	}

	var b strings.Builder
	start := 0
	for i := 1; i <= len(runes); i++ {
		if i < len(runes) && styles[i] == styles[start] {
			continue
		}
		run := string(runes[start:i])
		if styles[start] == (inlineStyle{}) {
			b.WriteString(run)
		} else {
			b.WriteString(styles[start].style(roles, reveal).Render(run))
		}
		start = i
	}
	return b.String()
}

// RenderInline styles a whole formatted text with the semantic palette.
//
// reveal turns spoilers back into readable text; it is true only for the
// message under the cursor, and only after x.
//
// # No OSC 8 hyperlinks
//
// The design record asks for terminal hyperlinks on links "where supported",
// and this is where they would be emitted. They are deliberately absent.
//
// Width is not the obstacle: ansi.StringWidth measures an OSC 8 sequence as
// zero cells. ansi.Wrap is. It breaks a line in the middle of a hyperlink
// without closing and reopening it, so the opening sequence is left dangling
// on the first line and the closing one arrives on the second having opened
// nothing — and every terminal that supports OSC 8 then treats the rest of
// that first row, panel rule included, as part of the link. Emitting them
// correctly means wrapping before styling, which means owning a
// grapheme-aware wrapper instead of using the tested one. Recorded as a
// divergence; the cyan underline is the affordance in the meantime.
func RenderInline(ft *telegram.FormattedText, roles theme.Roles, reveal bool) string {
	if ft == nil || ft.Text == "" {
		return ""
	}
	runes := []rune(ft.Text)
	return renderRunes(runes, inlineStyles(ft, len(runes)), roles, reveal)
}
