package render

import (
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
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

	// armed marks the one link the reader is about to follow. It is part of
	// the style for the same reason uri is: renderRunes cuts a run wherever
	// the style changes, so marking a range here is all it takes to make
	// that range its own span.
	armed bool

	// uri is where a link goes, for the OSC 8 sequence. It is part of the
	// style rather than a parallel table because renderRunes cuts runs
	// wherever the style changes, and two adjacent links to different
	// places must not be merged into one — which is exactly what a bool
	// would have let happen.
	uri string
}

// textOpts are the per-render choices that reach the inline styler.
//
// A struct rather than a growing tail of bools: reveal and links are both
// "how, not what", they both have to travel from RenderBody down to
// renderRunes through three intermediaries, and a call ending in
// `roles, 40, false, true` says nothing about which is which.
type textOpts struct {
	// reveal turns spoilers back into readable text. True only for the
	// message under the cursor, and only after x.
	reveal bool

	// links emits OSC 8 hyperlinks on link runs. Off unless the terminal
	// was found to support them (theme.SupportsHyperlinks) or the user
	// asked for them outright.
	links bool

	// armedLo/armedHi are the rune offsets of the link the reader has
	// armed with gx, in the message's OWN text. Absolute rather than
	// block-relative because splitBlocks preserves absolute offsets, so a
	// link inside a quote is reachable by the same number.
	//
	// armedHi <= armedLo means nothing is armed, which is the zero value.
	armedLo, armedHi int
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
			uri := entityURI(e, ft.Text, lo, hi)
			set = func(s *inlineStyle) { s.link, s.uri = true, uri }
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

// entityURI is where a link entity points.
//
// Three spellings, because Telegram carries the destination in three
// different places: a text_url has it as a field, a bare url IS its own text,
// and an email address is a mailto: that the client has to build. Getting
// this wrong is worse than not linking at all — a hyperlink whose target
// disagrees with its visible text is the shape of a phishing link, so the
// only sources allowed here are the entity's own field and the covered text.
func entityURI(e *telegram.TextEntity, text string, lo, hi int) string {
	if t, ok := e.Type.(*telegram.TextEntityTypeTextURL); ok {
		return t.URL
	}
	runes := []rune(text)
	if lo < 0 || hi > len(runes) || lo >= hi {
		return ""
	}
	covered := strings.TrimSpace(string(runes[lo:hi]))
	if covered == "" {
		return ""
	}
	if _, isEmail := e.Type.(*telegram.TextEntityTypeEmailAddress); isEmail {
		return "mailto:" + covered
	}
	// A bare URL entity may cover text with no scheme ("example.com").
	// Terminals need an absolute URI, and guessing anything but http is
	// guessing about someone else's link.
	if !strings.Contains(covered, "://") {
		return "https://" + covered
	}
	return covered
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
	// Last, so it survives every rule above. An armed link that a bold or a
	// spoiler could overpaint would be a cursor you cannot find, which is
	// the whole job of this one.
	if s.armed {
		out = out.Background(roles.Sel).Bold(true)
	}
	return out
}

// renderRunes styles a rune slice as a sequence of maximal constant-style
// runs. styles must be the table for exactly those runes.
func renderRunes(runes []rune, styles []inlineStyle, roles theme.Roles, o textOpts) string {
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
			start = i
			continue
		}
		styled := styles[start].style(roles, o.reveal).Render(run)
		if o.links {
			// Inside the styling, not around it: the SGR reset that closes
			// the run would otherwise land between the link's open and its
			// close, and cell.WrapLines repairs the two modes separately.
			styled = cell.Link(styles[start].uri, styled)
		}
		b.WriteString(styled)
		start = i
	}
	return b.String()
}

// RenderInline styles a whole formatted text with the semantic palette, and
// with OSC 8 hyperlinks when o.links says the terminal has them.
//
// The hyperlinks were absent through phases 4 to 8 for a reason that turned
// out to be smaller than it looked: ansi.Wrap breaks a line between a link's
// opening and closing sequences and repairs neither, so the rest of that row
// — its padding and the panel rule beside it — becomes part of the link. The
// design record concluded that fixing it meant owning a grapheme-aware
// wrapper, because reopening a link means knowing which runes belong to it
// after wrapping. It does not: the URI is carried in the opening sequence, so
// cell.OpenLink can recover it from the wrapped line the same way OpenStyle
// recovers an SGR run. See docs/tui-2.0.md, divergence 14.
func RenderInline(ft *telegram.FormattedText, roles theme.Roles, o textOpts) string {
	if ft == nil || ft.Text == "" {
		return ""
	}
	runes := []rune(ft.Text)
	return renderRunes(runes, inlineStyles(ft, len(runes)), roles, o)
}
