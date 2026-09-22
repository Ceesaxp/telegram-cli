package telegram

import (
	"strings"

	"github.com/gotd/td/tg"
)

// Outgoing markdown: the Telegram Desktop subset.
//
// Grammar (flat, first-match, non-nesting — Desktop barely nests either):
//
//	**text**        bold
//	__text__        italic
//	~~text~~        strikethrough
//	||text||        spoiler
//	`text`          inline code
//	```lang         fenced pre, language optional; when the opening fence
//	text            is followed by a single word and a newline, that word
//	```             is the language and is not part of the content
//	[text](url)     text link; parens inside url may nest if balanced,
//	                and url must carry an allowed scheme
//
// Rules that matter:
//
//   - A marker only formats when its closer is found. An unmatched or
//     unclosed marker is emitted verbatim, so user text is never eaten.
//   - Empty spans ("****") are literal: Telegram rejects zero-length
//     entities anyway.
//   - `code` and ```pre``` are opaque — markers inside them stay literal,
//     which is what makes it possible to send markdown about markdown.
//   - Link URLs may contain balanced parentheses (Wikipedia-style), and
//     only an allowlisted scheme becomes an entity — see allowedURL.
//   - A well-formed but empty fence keeps its markers as literal text
//     rather than being re-scanned as inline code.
//   - Offsets and lengths are UTF-16 code units, the MTProto wire format
//     and the exact inverse of the incoming utf16RuneIndex conversion.
//
// Ordering versus sanitizeTerminal: outgoing text is deliberately never
// sanitized (sanitization is an inbound defense — see sanitizeTerminal),
// so the question does not arise on this path. Were it ever applied,
// either order would be correct: sanitizeTerminal replaces runes 1:1 with
// U+FFFD, preserving both the rune count and the UTF-16 length.
const markdownMarkers = "*_`~|["

// parseMarkdown converts markdown into the plain text to send plus the
// entities describing it. Text with no markers is returned untouched and
// allocates nothing beyond the fast-path scan.
//
// Parsing round-trips through []rune, so text that actually contains
// markup has any invalid UTF-8 normalised to U+FFFD. That preserves the
// rune count (and so every offset), and matches what a renderer would
// display anyway; text taking the fast path is returned byte-identical.
func parseMarkdown(text string) (string, []tg.MessageEntityClass) {
	if !strings.ContainsAny(text, markdownMarkers) {
		return text, nil
	}

	p := &mdParser{src: []rune(text)}
	return p.parse(text)
}

// parseMarkdownMapped is parseMarkdown that also reports where every source
// rune went, which is what lets a range marked in the draft be found again
// in the text that is sent.
//
// Entry i of the map is the UTF-16 offset in the output at which source
// rune i starts; entry len(runes) is the output's total length. A rune
// removed as markup maps to where the output stood when it was removed,
// which is the start of whatever is emitted next. The map therefore never
// decreases, and source runes [s, e) cover output units [at[s], at[e]).
func parseMarkdownMapped(text string) (string, []tg.MessageEntityClass, []int) {
	if !strings.ContainsAny(text, markdownMarkers) {
		return text, nil, utf16Offsets(text)
	}

	src := []rune(text)
	p := &mdParser{src: src, at: make([]int, len(src)+1)}
	out, entities := p.parse(text)
	return out, entities, p.at
}

// parse runs the parser over text, whose runes are already in p.src.
func (p *mdParser) parse(text string) (string, []tg.MessageEntityClass) {
	p.run()
	if len(p.entities) == 0 {
		// Markers were present but none of them formed a span, so the
		// text is unchanged. Return the original to keep the no-op case
		// allocation-free downstream.
		return text, nil
	}
	return p.out.String(), p.entities
}

// mdParser walks the source runes once, copying them to out and emitting
// an entity whenever a complete marker pair is consumed.
type mdParser struct {
	src      []rune
	pos      int
	out      strings.Builder
	utf16Len int32 // UTF-16 length of out so far: the next entity offset
	entities []tg.MessageEntityClass

	// at is the source-to-output offset map, filled only when asked for
	// (parseMarkdownMapped); mapped is the first source index not yet in it.
	at     []int
	mapped int
}

func (p *mdParser) run() {
	for p.pos < len(p.src) {
		if p.tryMarker() {
			continue
		}
		p.emitRange(p.pos, p.pos+1)
		p.pos++
	}
	p.mapUpTo(len(p.src) + 1)
}

// emitRange appends src[start:end] verbatim, advancing the UTF-16 cursor,
// and returns the UTF-16 length it added. Output is only ever copied from
// a source range, never from loose runes, so every output rune has a
// known source index.
func (p *mdParser) emitRange(start, end int) int32 {
	before := p.utf16Len
	for i := start; i < end; i++ {
		p.mapUpTo(i + 1)
		r := p.src[i]
		p.out.WriteRune(r)
		p.utf16Len += utf16RuneLen(r)
	}
	return p.utf16Len - before
}

// mapUpTo gives every source rune before end that has no entry in the
// offset map yet the current output offset. Called just before a rune is
// written, it records where that rune starts, and places any markup skipped
// since the previous one at the same spot, where the removed markers stood.
func (p *mdParser) mapUpTo(end int) {
	if p.at == nil {
		return
	}
	for ; p.mapped < end; p.mapped++ {
		p.at[p.mapped] = int(p.utf16Len)
	}
}

// emitLiteral copies src[p.pos:end] verbatim and consumes it. Used for
// constructs that are well-formed enough to delimit but must not format:
// consuming them is what stops their markers being re-scanned as some
// other, smaller construct.
func (p *mdParser) emitLiteral(end int) {
	p.emitRange(p.pos, end)
	p.pos = end
}

// utf16RuneLen is the number of UTF-16 code units a rune encodes to.
func utf16RuneLen(r rune) int32 {
	if r > 0xFFFF {
		return 2
	}
	return 1
}

// utf16Offsets maps text sent as typed from rune indices to UTF-16
// offsets: entry i is where rune i starts on the wire, and the final entry,
// at index len(runes), is the total length. It is the offset map of text
// that no Markdown touched.
func utf16Offsets(text string) []int {
	at := make([]int, 0, len(text)+1)
	n := 0
	for _, r := range text {
		at = append(at, n)
		n += int(utf16RuneLen(r))
	}
	return append(at, n)
}

// tryMarker attempts to consume a formatted span at the cursor. It
// reports whether one was consumed; on false the caller emits a literal
// rune and moves on, which is what makes unmatched markers harmless.
func (p *mdParser) tryMarker() bool {
	// Longest markers first: ``` must win over `.
	if p.hasPrefix("```") {
		return p.takeFence()
	}
	if p.hasPrefix("`") {
		return p.takeSimple("`", func() tg.MessageEntityClass { return &tg.MessageEntityCode{} })
	}
	if p.hasPrefix("**") {
		return p.takeSimple("**", func() tg.MessageEntityClass { return &tg.MessageEntityBold{} })
	}
	if p.hasPrefix("__") {
		return p.takeSimple("__", func() tg.MessageEntityClass { return &tg.MessageEntityItalic{} })
	}
	if p.hasPrefix("~~") {
		return p.takeSimple("~~", func() tg.MessageEntityClass { return &tg.MessageEntityStrike{} })
	}
	if p.hasPrefix("||") {
		return p.takeSimple("||", func() tg.MessageEntityClass { return &tg.MessageEntitySpoiler{} })
	}
	if p.hasPrefix("[") {
		return p.takeLink()
	}
	return false
}

// hasPrefix reports whether the source at the cursor starts with marker.
func (p *mdParser) hasPrefix(marker string) bool {
	m := []rune(marker)
	if p.pos+len(m) > len(p.src) {
		return false
	}
	for i, r := range m {
		if p.src[p.pos+i] != r {
			return false
		}
	}
	return true
}

// indexFrom finds the next occurrence of marker at or after start,
// returning -1 when there is none.
func (p *mdParser) indexFrom(start int, marker string) int {
	m := []rune(marker)
	for i := start; i+len(m) <= len(p.src); i++ {
		match := true
		for j, r := range m {
			if p.src[i+j] != r {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// takeSimple handles the symmetric markers, whose content is opaque:
// whatever sits between the two markers is emitted verbatim.
func (p *mdParser) takeSimple(marker string, mk func() tg.MessageEntityClass) bool {
	n := len([]rune(marker))
	contentStart := p.pos + n

	closer := p.indexFrom(contentStart, marker)
	if closer < 0 || closer == contentStart {
		// Unclosed, or an empty span like "****": literal.
		return false
	}

	offset := p.utf16Len
	length := p.emitRange(contentStart, closer)
	p.addEntity(mk(), offset, length)
	p.pos = closer + n
	return true
}

// takeFence handles ```…``` with an optional language on the fence line.
func (p *mdParser) takeFence() bool {
	const fence = "```"
	contentStart := p.pos + 3

	closer := p.indexFrom(contentStart, fence)
	if closer < 0 {
		// Unclosed: the caller emits a literal backtick and moves on.
		return false
	}

	// The opening fence may be followed by an info line terminated by a
	// newline. A bare newline just ends the fence line; a single word
	// before it names the language. A first line containing spaces is
	// not an info line at all, so it counts as content — as does a fence
	// with no newline before the closer ("```inline```").
	language := ""
	body := contentStart
	if nl := p.indexOfRune(contentStart, closer, '\n'); nl >= 0 {
		word := string(p.src[contentStart:nl])
		switch {
		case word == "":
			body = nl + 1
		case !strings.ContainsAny(word, " \t"):
			language = word
			body = nl + 1
		}
	}

	if body >= closer {
		// A complete fence with nothing in it ("```go\n```", "``````").
		// A zero-length entity would be rejected, and returning false
		// would let the caller re-scan these very backticks as inline
		// code — turning an empty block into mangled markup. Emit the
		// construct verbatim and consume it instead.
		p.emitLiteral(closer + 3)
		return true
	}

	offset := p.utf16Len
	length := p.emitRange(body, closer)
	if language != "" {
		p.addEntity(&tg.MessageEntityPre{Language: language}, offset, length)
	} else {
		p.addEntity(&tg.MessageEntityPre{}, offset, length)
	}
	p.pos = closer + 3
	return true
}

// takeLink handles [text](url).
func (p *mdParser) takeLink() bool {
	textStart := p.pos + 1

	closeBracket := p.indexOfRune(textStart, len(p.src), ']')
	if closeBracket < 0 || closeBracket == textStart {
		return false
	}
	// The URL must open immediately after the label.
	if closeBracket+1 >= len(p.src) || p.src[closeBracket+1] != '(' {
		return false
	}
	// URLs legitimately contain parentheses (Wikipedia article titles are
	// the classic case), so match the closer by depth rather than taking
	// the first ')' — otherwise the tail of the URL leaks into the
	// message text. An unbalanced URL is not a link at all.
	urlStart := closeBracket + 2
	closeParen, balanced := p.matchCloseParen(urlStart)
	if !balanced || closeParen == urlStart {
		return false
	}

	url := string(p.src[urlStart:closeParen])
	if !allowedURL(url) {
		// javascript:, data:, and anything schemeless never becomes a
		// link. The construct is well-formed, so consume it verbatim
		// rather than letting its markers be re-scanned.
		p.emitLiteral(closeParen + 1)
		return true
	}

	offset := p.utf16Len
	length := p.emitRange(textStart, closeBracket)
	p.addEntity(&tg.MessageEntityTextURL{URL: url}, offset, length)
	p.pos = closeParen + 1
	return true
}

// matchCloseParen finds the ')' closing the '(' that precedes start,
// allowing balanced pairs in between. Reports false when the parens never
// balance, in which case there is no link to make.
func (p *mdParser) matchCloseParen(start int) (int, bool) {
	depth := 1 // the '(' just before start is already open
	for i := start; i < len(p.src); i++ {
		switch p.src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return -1, false
}

// allowedURLSchemes are the only schemes that may become a link entity.
var allowedURLSchemes = [...]string{"http", "https", "tg", "mailto", "ftp"}

// allowedURL reports whether url carries an allowlisted scheme. Anything
// else — javascript:, data:, file:, or a schemeless string — is rejected,
// so a message can never smuggle an executable URL into a rendered link.
// The check is deliberately an allowlist: new dangerous schemes are added
// by browsers faster than a blocklist can track them.
func allowedURL(url string) bool {
	colon := strings.IndexByte(url, ':')
	if colon <= 0 {
		return false
	}
	scheme := strings.ToLower(url[:colon])
	for _, s := range allowedURLSchemes {
		if scheme == s {
			return true
		}
	}
	return false
}

// indexOfRune finds r in [start, end), or -1.
func (p *mdParser) indexOfRune(start, end int, r rune) int {
	for i := start; i < end && i < len(p.src); i++ {
		if p.src[i] == r {
			return i
		}
	}
	return -1
}

// addEntity records a span, skipping degenerate ones the server rejects.
func (p *mdParser) addEntity(e tg.MessageEntityClass, offset, length int32) {
	if length <= 0 {
		return
	}
	switch v := e.(type) {
	case *tg.MessageEntityBold:
		v.Offset, v.Length = int(offset), int(length)
	case *tg.MessageEntityItalic:
		v.Offset, v.Length = int(offset), int(length)
	case *tg.MessageEntityStrike:
		v.Offset, v.Length = int(offset), int(length)
	case *tg.MessageEntitySpoiler:
		v.Offset, v.Length = int(offset), int(length)
	case *tg.MessageEntityCode:
		v.Offset, v.Length = int(offset), int(length)
	case *tg.MessageEntityPre:
		v.Offset, v.Length = int(offset), int(length)
	case *tg.MessageEntityTextURL:
		v.Offset, v.Length = int(offset), int(length)
	default:
		return
	}
	p.entities = append(p.entities, e)
}

// PreviewMarkdown parses the outgoing markdown subset and returns the
// FormattedText that would go on the wire.
//
// It is the same parseMarkdown the send path uses, converted through the same
// entity mapping — so a composer preview built on it cannot drift from what
// is actually sent. A preview with its own parser is a preview that is right
// until the day it is not.
func PreviewMarkdown(text string) *FormattedText {
	plain, entities := parseMarkdown(text)
	return formattedTextFromTG(plain, entities)
}

// markdownEnabled reports whether outgoing markdown parsing is on. It is
// OFF by default and opted into via config (or the -migrate-config flag);
// a Client without config (tests) parses nothing.
func (c *Client) markdownEnabled() bool {
	return c.config != nil && c.config.UI.ParseMarkdown
}

// formatOutgoing prepares user-typed text for the wire, applying the
// markdown subset when enabled. Returns the text to send and its
// entities (nil when parsing is off or the text has no markup).
func (c *Client) formatOutgoing(text string) (string, []tg.MessageEntityClass) {
	if !c.markdownEnabled() {
		return text, nil
	}
	return parseMarkdown(text)
}
