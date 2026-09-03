package telegram

import (
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gotd/td/tg"

	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// spanOf returns the substring an outgoing (UTF-16) entity selects, which
// is how Telegram itself will interpret it on the wire.
func spanOf(t *testing.T, text string, offset, length int) string {
	t.Helper()
	units := utf16.Encode([]rune(text))
	if offset < 0 || offset+length > len(units) {
		t.Fatalf("entity %d+%d out of range for %d UTF-16 units of %q",
			offset, length, len(units), text)
	}
	return string(utf16.Decode(units[offset : offset+length]))
}

func TestParseMarkdownFastPath(t *testing.T) {
	for _, in := range []string{"", "plain text", "no markers here", "a - b / c"} {
		got, entities := parseMarkdown(in)
		if got != in {
			t.Errorf("parseMarkdown(%q) text = %q, want unchanged", in, got)
		}
		if entities != nil {
			t.Errorf("parseMarkdown(%q) entities = %v, want nil", in, entities)
		}
	}
}

func TestParseMarkdownSpans(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantText string
		wantSpan string
		wantType any
	}{
		{"bold", "a **b c** d", "a b c d", "b c", &tg.MessageEntityBold{}},
		{"italic", "a __b__ d", "a b d", "b", &tg.MessageEntityItalic{}},
		{"strike", "a ~~b~~ d", "a b d", "b", &tg.MessageEntityStrike{}},
		{"spoiler", "a ||b|| d", "a b d", "b", &tg.MessageEntitySpoiler{}},
		{"code", "a `b` d", "a b d", "b", &tg.MessageEntityCode{}},
		{"link", "see [the docs](https://x.dev) now", "see the docs now", "the docs", &tg.MessageEntityTextURL{}},
		{"at start", "**b** rest", "b rest", "b", &tg.MessageEntityBold{}},
		{"at end", "rest **b**", "rest b", "b", &tg.MessageEntityBold{}},
		{"whole string", "**b**", "b", "b", &tg.MessageEntityBold{}},
	}
	for _, tt := range tests {
		text, entities := parseMarkdown(tt.in)
		if text != tt.wantText {
			t.Errorf("%s: text = %q, want %q", tt.name, text, tt.wantText)
			continue
		}
		if len(entities) != 1 {
			t.Errorf("%s: got %d entities, want 1", tt.name, len(entities))
			continue
		}
		e := entities[0]
		if got := spanOf(t, text, e.GetOffset(), e.GetLength()); got != tt.wantSpan {
			t.Errorf("%s: span = %q, want %q", tt.name, got, tt.wantSpan)
		}
		if gotT, wantT := typeName(e), typeName(tt.wantType.(tg.MessageEntityClass)); gotT != wantT {
			t.Errorf("%s: entity type = %s, want %s", tt.name, gotT, wantT)
		}
	}
}

func typeName(e tg.MessageEntityClass) string { return e.TypeName() }

func TestParseMarkdownLink(t *testing.T) {
	text, entities := parseMarkdown("go to [docs](https://example.com/a_b) ok")
	if text != "go to docs ok" {
		t.Fatalf("text = %q", text)
	}
	u, ok := entities[0].(*tg.MessageEntityTextURL)
	if !ok {
		t.Fatalf("entity = %T, want *tg.MessageEntityTextURL", entities[0])
	}
	if u.URL != "https://example.com/a_b" {
		t.Errorf("URL = %q, want %q", u.URL, "https://example.com/a_b")
	}
	if got := spanOf(t, text, u.Offset, u.Length); got != "docs" {
		t.Errorf("span = %q, want %q", got, "docs")
	}
}

func TestParseMarkdownFence(t *testing.T) {
	t.Run("with language", func(t *testing.T) {
		text, entities := parseMarkdown("see:\n```go\nfmt.Println()\n```")
		if text != "see:\nfmt.Println()\n" {
			t.Fatalf("text = %q", text)
		}
		pre, ok := entities[0].(*tg.MessageEntityPre)
		if !ok {
			t.Fatalf("entity = %T", entities[0])
		}
		if pre.Language != "go" {
			t.Errorf("language = %q, want %q", pre.Language, "go")
		}
		if got := spanOf(t, text, pre.Offset, pre.Length); got != "fmt.Println()\n" {
			t.Errorf("span = %q", got)
		}
	})

	t.Run("without language", func(t *testing.T) {
		text, entities := parseMarkdown("```plain code```")
		if text != "plain code" {
			t.Fatalf("text = %q", text)
		}
		pre := entities[0].(*tg.MessageEntityPre)
		if pre.Language != "" {
			t.Errorf("language = %q, want empty", pre.Language)
		}
	})
}

// TestParseMarkdownNeverEatsText is the safety property: anything that is
// not a complete, non-empty span must survive byte for byte.
func TestParseMarkdownNeverEatsText(t *testing.T) {
	literals := []string{
		"unclosed **bold",
		"unclosed `code",
		"stray ** markers **", // closes, but the span is " markers "
		"empty ****",
		"empty ``",
		"empty ||||",
		"a * b _ c ~ d | e",
		"[not a link]",
		"[label] (spaced)",
		"[label](",
		"[](https://x.dev)",
		"5 * 3 * 2",
		"snake_case_name",
		"```",
		"``````",
	}
	for _, in := range literals {
		text, entities := parseMarkdown(in)
		if len(entities) == 0 {
			if text != in {
				t.Errorf("parseMarkdown(%q) text = %q, want unchanged", in, text)
			}
			continue
		}
		// If something did match, no character may be lost beyond the
		// markers themselves.
		if len(text) > len(in) {
			t.Errorf("parseMarkdown(%q) grew to %q", in, text)
		}
	}
}

func TestParseMarkdownCodeIsOpaque(t *testing.T) {
	text, entities := parseMarkdown("use `**not bold**` here")
	if text != "use **not bold** here" {
		t.Fatalf("text = %q, want markers preserved inside code", text)
	}
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (code only)", len(entities))
	}
	if _, ok := entities[0].(*tg.MessageEntityCode); !ok {
		t.Errorf("entity = %T, want code", entities[0])
	}
}

func TestParseMarkdownMultipleSpans(t *testing.T) {
	text, entities := parseMarkdown("**a** and __b__ and `c`")
	if text != "a and b and c" {
		t.Fatalf("text = %q", text)
	}
	if len(entities) != 3 {
		t.Fatalf("got %d entities, want 3", len(entities))
	}
	want := []string{"a", "b", "c"}
	for i, e := range entities {
		if got := spanOf(t, text, e.GetOffset(), e.GetLength()); got != want[i] {
			t.Errorf("entity %d spans %q, want %q", i, got, want[i])
		}
	}
}

// TestMarkdownRoundTripThroughIncomingConverter is the critical test: the
// outgoing parser emits UTF-16 offsets, the incoming converter turns them
// back into rune indices, and the spans must still land on the intended
// substrings. Non-BMP runes are where a naive implementation breaks,
// because they are 1 rune but 2 UTF-16 units.
func TestMarkdownRoundTripThroughIncomingConverter(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantText string
		wantSpan string
	}{
		{"emoji before bold", "\U0001F600 **bold**", "\U0001F600 bold", "bold"},
		{"emoji inside bold", "**\U0001F600bold**", "\U0001F600bold", "\U0001F600bold"},
		{"emoji after bold", "**bold** \U0001F600", "bold \U0001F600", "bold"},
		{"emoji both sides", "\U0001F600 **b\U0001F600d** \U0001F600", "\U0001F600 b\U0001F600d \U0001F600", "b\U0001F600d"},
		{"two emoji before", "\U0001F600\U0001F601 __it__", "\U0001F600\U0001F601 it", "it"},
		{"emoji in link label", "[a\U0001F600b](https://x.dev)", "a\U0001F600b", "a\U0001F600b"},
		{"emoji before code", "\U0001F600 `x`", "\U0001F600 x", "x"},
		{"bmp only", "plain **bold** tail", "plain bold tail", "bold"},
	}

	for _, tt := range tests {
		// Outgoing: markdown -> wire text + UTF-16 entities.
		text, entities := parseMarkdown(tt.in)
		if text != tt.wantText {
			t.Errorf("%s: wire text = %q, want %q", tt.name, text, tt.wantText)
			continue
		}
		if len(entities) != 1 {
			t.Errorf("%s: got %d entities, want 1", tt.name, len(entities))
			continue
		}

		// The wire offsets must be right in UTF-16 terms.
		if got := spanOf(t, text, entities[0].GetOffset(), entities[0].GetLength()); got != tt.wantSpan {
			t.Errorf("%s: UTF-16 span = %q, want %q", tt.name, got, tt.wantSpan)
		}

		// Incoming: the same text+entities back through the converter the
		// receiving client uses, which yields rune indices.
		ft := formattedTextFromTG(text, entities)
		runes := []rune(ft.Text)
		e := ft.Entities[0]
		if int(e.Offset+e.Length) > len(runes) {
			t.Errorf("%s: rune span %d+%d out of range for %d runes",
				tt.name, e.Offset, e.Length, len(runes))
			continue
		}
		if got := string(runes[e.Offset : e.Offset+e.Length]); got != tt.wantSpan {
			t.Errorf("%s: rune span = %q, want %q", tt.name, got, tt.wantSpan)
		}
	}
}

// TestMarkdownRoundTripNonBMPActuallyDiffers proves the round trip is
// load-bearing rather than trivially true: for these inputs the UTF-16
// offset and the rune offset genuinely differ, so a missing conversion
// would land on the wrong substring.
func TestMarkdownRoundTripNonBMPActuallyDiffers(t *testing.T) {
	text, entities := parseMarkdown("\U0001F600 **bold**")

	wireOffset := entities[0].GetOffset()
	ft := formattedTextFromTG(text, entities)
	runeOffset := int(ft.Entities[0].Offset)

	if wireOffset == runeOffset {
		t.Fatalf("offsets agree (%d): the test no longer exercises the conversion", wireOffset)
	}
	if wireOffset != 3 || runeOffset != 2 {
		t.Errorf("wire offset %d / rune offset %d, want 3 / 2", wireOffset, runeOffset)
	}

	// Using the wire offset as a rune index would slice the wrong text.
	runes := []rune(ft.Text)
	if wireOffset+entities[0].GetLength() <= len(runes) {
		if wrong := string(runes[wireOffset : wireOffset+entities[0].GetLength()]); wrong == "bold" {
			t.Error("unconverted offset happens to be correct; test is not meaningful")
		}
	}
}

func TestFormatOutgoingRespectsToggle(t *testing.T) {
	var off Client // no config: parsing disabled
	text, entities := off.formatOutgoing("**b**")
	if text != "**b**" || entities != nil {
		t.Errorf("disabled: got (%q, %v), want unchanged text and nil entities", text, entities)
	}
}

func TestFormatOutgoingEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.UI.ParseMarkdown = true
	c := Client{config: cfg}

	text, entities := c.formatOutgoing("a **b** c")
	if text != "a b c" {
		t.Errorf("text = %q, want %q", text, "a b c")
	}
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	if _, ok := entities[0].(*tg.MessageEntityBold); !ok {
		t.Errorf("entity = %T, want bold", entities[0])
	}

	// Explicitly disabled sends exactly what was typed.
	cfg.UI.ParseMarkdown = false
	if text, entities := c.formatOutgoing("a **b** c"); text != "a **b** c" || entities != nil {
		t.Errorf("disabled: got (%q, %v), want the raw text and nil", text, entities)
	}
}

// --- Fix 1: balanced parentheses in link URLs ---

func TestParseMarkdownLinkBalancedParens(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantText string
		wantSpan string
		wantURL  string // empty means "no entity, literal passthrough"
	}{
		{
			name:     "wikipedia style",
			in:       "see [Go](https://en.wikipedia.org/wiki/Go_(language)) now",
			wantText: "see Go now",
			wantSpan: "Go",
			wantURL:  "https://en.wikipedia.org/wiki/Go_(language)",
		},
		{
			name:     "nested pairs",
			in:       "[a](https://x.dev/a(b(c)d)e)",
			wantText: "a",
			wantSpan: "a",
			wantURL:  "https://x.dev/a(b(c)d)e",
		},
		{
			name:     "trailing text after link",
			in:       "[a](https://x.dev/(q)) tail",
			wantText: "a tail",
			wantSpan: "a",
			wantURL:  "https://x.dev/(q)",
		},
		// Unbalanced: not a link at all, and nothing may be lost.
		{name: "unbalanced open", in: "[a](https://x.dev/(c)", wantText: "[a](https://x.dev/(c)"},
		{name: "unbalanced deep", in: "[a](https://x.dev/((c)", wantText: "[a](https://x.dev/((c)"},
	}

	for _, tt := range tests {
		text, entities := parseMarkdown(tt.in)
		if text != tt.wantText {
			t.Errorf("%s: text = %q, want %q", tt.name, text, tt.wantText)
			continue
		}
		if tt.wantURL == "" {
			if len(entities) != 0 {
				t.Errorf("%s: got %d entities, want none", tt.name, len(entities))
			}
			continue
		}
		if len(entities) != 1 {
			t.Errorf("%s: got %d entities, want 1", tt.name, len(entities))
			continue
		}
		u, ok := entities[0].(*tg.MessageEntityTextURL)
		if !ok {
			t.Errorf("%s: entity = %T, want text URL", tt.name, entities[0])
			continue
		}
		if u.URL != tt.wantURL {
			t.Errorf("%s: URL = %q, want %q", tt.name, u.URL, tt.wantURL)
		}
		if got := spanOf(t, text, u.Offset, u.Length); got != tt.wantSpan {
			t.Errorf("%s: span = %q, want %q", tt.name, got, tt.wantSpan)
		}
	}
}

// --- Fix 2: an empty fenced block never becomes inline code ---

func TestParseMarkdownEmptyFence(t *testing.T) {
	// Each of these is a complete fence with no body. The markers must
	// survive verbatim and must not be re-scanned as inline code.
	for _, in := range []string{
		"```x\n```",
		"```go\n```",
		"``````",
		"```\n```",
		"before ```x\n``` after",
	} {
		text, entities := parseMarkdown(in)
		if text != in {
			t.Errorf("parseMarkdown(%q) text = %q, want unchanged", in, text)
		}
		if len(entities) != 0 {
			t.Errorf("parseMarkdown(%q) produced %d entities (%T), want none",
				in, len(entities), entities[0])
		}
	}
}

func TestParseMarkdownFenceStillWorksAfterEmptyOne(t *testing.T) {
	// An empty fence must be consumed, not partially re-scanned, so a
	// real fence later in the message still parses.
	text, entities := parseMarkdown("```x\n```\n```go\nreal\n```")
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1: %#v", len(entities), entities)
	}
	pre, ok := entities[0].(*tg.MessageEntityPre)
	if !ok {
		t.Fatalf("entity = %T, want pre", entities[0])
	}
	if pre.Language != "go" {
		t.Errorf("language = %q, want %q", pre.Language, "go")
	}
	if got := spanOf(t, text, pre.Offset, pre.Length); got != "real\n" {
		t.Errorf("span = %q, want %q", got, "real\n")
	}
}

// --- Fix 3: URL scheme allowlist ---

func TestParseMarkdownURLSchemeAllowlist(t *testing.T) {
	allowed := []string{
		"https://x.dev",
		"http://x.dev",
		"tg://resolve?domain=telegram",
		"mailto:a@b.dev",
		"ftp://files.x.dev/f",
		"HTTPS://x.dev", // scheme match is case-insensitive
		"HtTp://x.dev",
	}
	for _, url := range allowed {
		in := "[a](" + url + ")"
		text, entities := parseMarkdown(in)
		if text != "a" {
			t.Errorf("%s: text = %q, want %q", url, text, "a")
			continue
		}
		if len(entities) != 1 {
			t.Errorf("%s: got %d entities, want 1", url, len(entities))
			continue
		}
		if u, ok := entities[0].(*tg.MessageEntityTextURL); !ok || u.URL != url {
			t.Errorf("%s: entity = %#v, want text URL with that URL", url, entities[0])
		}
	}

	rejected := []string{
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD4=",
		"file:///etc/passwd",
		"vbscript:msgbox",
		"example.com", // schemeless
		"//x.dev",     // protocol-relative
		"/relative/path",
		":nohost",
	}
	for _, url := range rejected {
		in := "[a](" + url + ")"
		text, entities := parseMarkdown(in)
		if len(entities) != 0 {
			t.Errorf("%s: produced an entity %#v, want none", url, entities[0])
		}
		if text != in {
			t.Errorf("%s: text = %q, want the construct verbatim %q", url, text, in)
		}
	}
}

func TestAllowedURL(t *testing.T) {
	for _, u := range []string{"http://a", "https://a", "tg://a", "mailto:a", "ftp://a", "HTTPS://a"} {
		if !allowedURL(u) {
			t.Errorf("allowedURL(%q) = false, want true", u)
		}
	}
	for _, u := range []string{"", "a", "javascript:x", "data:x", "file:/x", ":x", "http", "ht tp://a"} {
		if allowedURL(u) {
			t.Errorf("allowedURL(%q) = true, want false", u)
		}
	}
}

// --- Fuzz ---

// FuzzParseMarkdown asserts the parser's invariants on arbitrary input:
// it must not panic, must never grow the text (markers are only ever
// removed), must emit only in-range non-empty entities, and its output
// must survive the incoming converter with in-range rune offsets.
func FuzzParseMarkdown(f *testing.F) {
	seeds := []string{
		"", "plain", "**b**", "__i__", "~~s~~", "||sp||", "`c`",
		"```go\nx\n```", "```x\n```", "``````", "```",
		"[a](https://x.dev)", "[a](https://x.dev/(q))", "[a](https://x.dev/(q)",
		"[a](javascript:alert(1))", "[a](example.com)", "[](https://x.dev)",
		"unclosed **bold", "empty ****", "5 * 3 * 2", "snake_case",
		"\U0001F600 **b**", "**\U0001F600b**", "[a\U0001F600b](https://x.dev)",
		"**a** and __b__ and `c`", "use `**not bold**` here",
		"[a](b(c)d)", "***", "____", "~~~~", "||||", "[[]]", "]([",
		// Invalid UTF-8: normalised to U+FFFD, which grows the byte
		// length while preserving the rune count (found by fuzzing).
		"__0__\xf8\xf8\xf8", "\xff", "**\xf8**",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		text, entities := parseMarkdown(in)

		// Markers are only ever removed, never invented. The comparison
		// is in RUNES, not bytes: parsing round-trips through []rune, so
		// invalid UTF-8 is normalised to U+FFFD (1 byte in, 3 bytes out)
		// while the rune count is preserved.
		if got, want := utf8.RuneCountInString(text), utf8.RuneCountInString(in); got > want {
			t.Fatalf("text grew: %q (%d runes) -> %q (%d runes)", in, want, text, got)
		}

		units := len(utf16.Encode([]rune(text)))
		for i, e := range entities {
			off, length := e.GetOffset(), e.GetLength()
			if length <= 0 {
				t.Fatalf("entity %d has non-positive length %d for input %q", i, length, in)
			}
			if off < 0 || off+length > units {
				t.Fatalf("entity %d (%d+%d) out of range for %d UTF-16 units, input %q",
					i, off, length, units, in)
			}
		}

		// The incoming converter must accept our own wire format and
		// produce rune offsets that slice safely.
		ft := formattedTextFromTG(text, entities)
		runes := []rune(ft.Text)
		for i, e := range ft.Entities {
			if e.Offset < 0 || e.Length < 0 || int(e.Offset+e.Length) > len(runes) {
				t.Fatalf("converted entity %d (%d+%d) out of range for %d runes, input %q",
					i, e.Offset, e.Length, len(runes), in)
			}
			_ = string(runes[e.Offset : e.Offset+e.Length]) // must not panic
		}
	})
}
