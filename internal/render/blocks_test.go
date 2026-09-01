package render

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

func testRoles() theme.Roles { return theme.DarkRoles(false) }

// span is a shorthand for one entity over a rune range of the text.
func span(offset, length int, kind telegram.TextEntityType) *telegram.TextEntity {
	return &telegram.TextEntity{Offset: int32(offset), Length: int32(length), Type: kind}
}

// formatted builds a FormattedText, taking entity ranges as rune offsets.
func formatted(text string, entities ...*telegram.TextEntity) *telegram.FormattedText {
	return &telegram.FormattedText{Text: text, Entities: entities}
}

// TestNestedEntitiesAreNotDuplicated is the bug the per-rune style table
// exists to fix.
//
// The previous renderer walked entities in offset order and emitted "the gap
// before this entity, then this entity", which printed every overlapped run
// once per entity that covered it. Telegram nests entities freely — a link
// inside a bold sentence is ordinary — so a message could arrive on screen
// with a phrase in it twice.
func TestNestedEntitiesAreNotDuplicated(t *testing.T) {
	ft := formatted("read the docs now",
		span(0, 17, &telegram.TextEntityTypeBold{}),
		span(9, 4, &telegram.TextEntityTypeTextURL{URL: "https://example.com"}),
	)

	got := ansi.Strip(RenderInline(ft, testRoles(), textOpts{}))
	if got != "read the docs now" {
		t.Fatalf("nested entities changed the text: %q", got)
	}
	if strings.Count(got, "docs") != 1 {
		t.Fatalf("the nested run was emitted more than once: %q", got)
	}
}

// TestOverlappingEntitiesCombine: a rune covered by two entities carries
// both, rather than one of them winning.
func TestOverlappingEntitiesCombine(t *testing.T) {
	ft := formatted("aaabbbccc",
		span(0, 6, &telegram.TextEntityTypeBold{}),
		span(3, 6, &telegram.TextEntityTypeItalic{}),
	)
	styles := inlineStyles(ft, 9)

	for i, want := range []inlineStyle{
		{bold: true}, {bold: true}, {bold: true},
		{bold: true, italic: true}, {bold: true, italic: true}, {bold: true, italic: true},
		{italic: true}, {italic: true}, {italic: true},
	} {
		if styles[i] != want {
			t.Fatalf("rune %d: %+v, want %+v", i, styles[i], want)
		}
	}
}

// TestEntityOffsetsOutOfRangeDoNotPanic: offsets arrive from the network.
// They are clamped at the boundary, but a render path must not be one bad
// number away from taking the program down.
func TestEntityOffsetsOutOfRangeDoNotPanic(t *testing.T) {
	ft := formatted("short",
		span(-5, 3, &telegram.TextEntityTypeBold{}),
		span(2, 9999, &telegram.TextEntityTypeItalic{}),
		span(4000, 10, &telegram.TextEntityTypeCode{}),
	)
	if got := ansi.Strip(RenderInline(ft, testRoles(), textOpts{})); got != "short" {
		t.Fatalf("got %q", got)
	}
}

// TestSplitBlocksSeparatesFencesFromProse.
func TestSplitBlocksSeparatesFencesFromProse(t *testing.T) {
	// "before\nCODE\nafter" — the fence covers runes 7..11.
	ft := formatted("before\nCODE\nafter",
		span(7, 4, &telegram.TextEntityTypePreCode{Language: "go"}))

	got := splitBlocks(ft)
	want := []block{
		{kind: blockText, start: 0, end: 7},
		{kind: blockCode, start: 7, end: 11, language: "go"},
		{kind: blockText, start: 11, end: 17},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d blocks, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("block %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestSplitBlocksTakesTheOutermost: a block entity nested inside another is
// dropped rather than producing two overlapping frames.
func TestSplitBlocksTakesTheOutermost(t *testing.T) {
	ft := formatted("0123456789",
		span(0, 10, &telegram.TextEntityTypeBlockQuote{}),
		span(2, 4, &telegram.TextEntityTypePre{}),
	)
	got := splitBlocks(ft)
	if len(got) != 1 || got[0].kind != blockQuote || got[0].start != 0 || got[0].end != 10 {
		t.Fatalf("expected one quote covering everything, got %+v", got)
	}
}

// TestCodeBlockIsFramedAndExactWidth sweeps the widths a thread pane
// actually takes. Every row of a frame has to be the same width as every
// other, or the frame is not a box.
func TestCodeBlockIsFramedAndExactWidth(t *testing.T) {
	code := "one\ntwo\nthree"

	for width := 12; width <= 130; width += 3 {
		lines := renderCodeBlock(code, "sql", testRoles(), width)
		if len(lines) != len(strings.Split(code, "\n"))+2 {
			t.Fatalf("width %d: %d lines for a 3-line block", width, len(lines))
		}

		want := min(width, maxBlockWidth)
		for i, line := range lines {
			if got := cell.Width(line); got != want {
				t.Fatalf("width %d: row %d is %d cells, want %d: %q",
					width, i, got, want, ansi.Strip(line))
			}
			if open := cell.OpenStyle(line); open != "" {
				t.Fatalf("width %d: row %d leaves %q open", width, i, open)
			}
		}
	}
}

// TestCodeBlockCapsAtEightyFour: a frame stretched the full width of a wide
// terminal stops reading as one object, and the eye has to travel the whole
// row to find the start of the next line.
func TestCodeBlockCapsAtEightyFour(t *testing.T) {
	lines := renderCodeBlock("x", "", testRoles(), 200)
	if got := cell.Width(lines[0]); got != maxBlockWidth {
		t.Fatalf("frame is %d cells at width 200, want %d", got, maxBlockWidth)
	}
}

// TestCodeBlockTruncatesRatherThanWraps. Code is a grid — its indentation
// carries the structure — and a wrapped line puts a fragment at column zero
// where a new statement belongs, which is exactly what the reader is
// scanning for.
func TestCodeBlockTruncatesRatherThanWraps(t *testing.T) {
	long := strings.Repeat("verylongtoken ", 20)
	lines := renderCodeBlock(long, "", testRoles(), 40)

	if len(lines) != 3 {
		t.Fatalf("expected a header, one code row and a footer, got %d rows", len(lines))
	}
	if !strings.Contains(ansi.Strip(lines[1]), cell.Ellipsis) {
		t.Fatalf("a truncated row must say so: %q", ansi.Strip(lines[1]))
	}
}

// TestCodeFrameGutterCompressesOnANarrowPane. Both forms are drawn in the
// goldens: at a 42-cell body (frame-137x29) the gutter is " 1   ", and at a
// 29-cell one (frame-120x40) it is "1  ". Four cells is a lot of gutter when
// the code column is under thirty, and code is the part that cannot be
// truncated without losing meaning.
func TestCodeFrameGutterCompressesOnANarrowPane(t *testing.T) {
	wide := ansi.Strip(renderCodeBlock("SELECT 1", "sql", testRoles(), 42)[1])
	if !strings.HasPrefix(wide, "│ 1   SELECT") {
		t.Errorf("wide frame gutter: %q", wide)
	}

	narrow := ansi.Strip(renderCodeBlock("SELECT 1", "sql", testRoles(), 29)[1])
	if !strings.HasPrefix(narrow, "│1  SELECT") {
		t.Errorf("narrow frame gutter: %q", narrow)
	}
}

// TestBlockBoundariesDoNotLeaveBlankRows. The newline between prose and a
// fence is the separator the sender typed, not content: kept, it draws a
// blank row above every code block, which reads as a rendering fault rather
// than as spacing.
func TestBlockBoundariesDoNotLeaveBlankRows(t *testing.T) {
	ft := formatted("intro:\nCODE\nafter",
		span(7, 4, &telegram.TextEntityTypePreCode{Language: "go"}))

	lines := renderBlocks(ft, testRoles(), 40, textOpts{})
	for i, line := range lines {
		if strings.TrimSpace(ansi.Strip(line)) == "" {
			t.Fatalf("blank row %d of %d at a block boundary:\n%s",
				i, len(lines), strings.Join(lines, "\n"))
		}
	}
}

// TestBlankLinesInsideProseSurvive: trimming the boundary must not eat the
// paragraph breaks a sender wrote on purpose.
func TestBlankLinesInsideProseSurvive(t *testing.T) {
	lines := renderBlocks(formatted("first\n\nsecond"), testRoles(), 40, textOpts{})
	if len(lines) != 3 {
		t.Fatalf("expected the blank line kept, got %d lines: %q", len(lines), lines)
	}
	if strings.TrimSpace(ansi.Strip(lines[1])) != "" {
		t.Fatalf("line 1 should be the blank one: %q", ansi.Strip(lines[1]))
	}
}

// TestCodeBlockNumbersEveryLine: the numbers are what make "line 3 of the
// error" mean something when the block is quoted back.
func TestCodeBlockNumbersEveryLine(t *testing.T) {
	var src []string
	for i := 1; i <= 12; i++ {
		src = append(src, "line")
	}
	lines := renderCodeBlock(strings.Join(src, "\n"), "", testRoles(), 60)

	for i := 1; i <= 12; i++ {
		row := ansi.Strip(lines[i])
		if !strings.Contains(row, strconv.Itoa(i)+" ") {
			t.Fatalf("row %d is not numbered %d: %q", i, i, row)
		}
	}
	// Two-digit numbers must not push the code left of the one-digit rows.
	first := strings.Index(ansi.Strip(lines[1]), "line")
	last := strings.Index(ansi.Strip(lines[12]), "line")
	if first != last {
		t.Fatalf("code starts at column %d on row 1 and %d on row 12", first, last)
	}
}

// TestCodeBlockColoursOnlyWhatItCanBeSureOf. Not syntax highlighting: a
// highlighter needs a grammar per language and gets the rest wrong. These
// three cases are recognisable from the first character in every language
// that has them.
func TestCodeBlockColoursOnlyWhatItCanBeSureOf(t *testing.T) {
	r := testRoles()
	cases := map[string]struct {
		line string
		want string
	}{
		"added":       {"+ added line", string(r.Green)},
		"removed":     {"- removed line", string(r.Red)},
		"diff header": {"--- a/file.go", string(r.Dim)},
		"comment":     {"// a comment", string(r.Dim)},
		"hash":        {"# a comment", string(r.Dim)},
		"code":        {"func main() {", string(r.Fg)},
		"indented":    {"    + still added", string(r.Green)},
	}
	for name, tc := range cases {
		if got := string(codeLineColour(tc.line, r)); got != tc.want {
			t.Errorf("%s: %q coloured %s, want %s", name, tc.line, got, tc.want)
		}
	}
}

// TestQuoteBlockRulesEveryLine: the rule is what says where the quotation
// ends. A rule on only the first line reads as a one-line quote followed by
// the quoter's own words.
func TestQuoteBlockRulesEveryLine(t *testing.T) {
	lines := renderQuoteBlock(strings.Repeat("quoted words ", 10), testRoles(), 30)
	if len(lines) < 3 {
		t.Fatalf("test needs a quote that wraps, got %d lines", len(lines))
	}
	for i, line := range lines {
		if !strings.HasPrefix(ansi.Strip(line), "│ ") {
			t.Errorf("line %d has no quote rule: %q", i, ansi.Strip(line))
		}
		if got := cell.Width(line); got > 30 {
			t.Errorf("line %d is %d cells, body is 30", i, got)
		}
	}
}

// TestListItemsGetAHangingIndent, so a wrapped bullet's continuation lines
// up under its own text instead of under the bullet, where it reads as a
// new item.
func TestListItemsGetAHangingIndent(t *testing.T) {
	ft := formatted("- " + strings.Repeat("word ", 12))
	lines := renderBlocks(ft, testRoles(), 24, textOpts{})

	if len(lines) < 2 {
		t.Fatalf("test needs a wrapped item, got %d lines", len(lines))
	}
	for i, line := range lines[1:] {
		if !strings.HasPrefix(ansi.Strip(line), "  ") {
			t.Errorf("continuation %d is not indented: %q", i, ansi.Strip(line))
		}
	}
	if strings.HasPrefix(ansi.Strip(lines[1]), "- ") {
		t.Error("a continuation line must not look like a new item")
	}
}

// TestOrdinalListsIndentByTheirOwnWidth: "10. " is two cells wider than
// "1. ", and a fixed indent would leave the tenth item misaligned.
func TestOrdinalListsIndentByTheirOwnWidth(t *testing.T) {
	for _, tc := range []struct{ marker string }{{"1. "}, {"10. "}, {"3) "}} {
		ft := formatted(tc.marker + strings.Repeat("word ", 12))
		lines := renderBlocks(ft, testRoles(), 24, textOpts{})
		if len(lines) < 2 {
			t.Fatalf("%q: test needs a wrapped item", tc.marker)
		}
		indent := len(tc.marker)
		got := ansi.Strip(lines[1])
		if len(got)-len(strings.TrimLeft(got, " ")) != indent {
			t.Errorf("%q: continuation indented %d, want %d: %q",
				tc.marker, len(got)-len(strings.TrimLeft(got, " ")), indent, got)
		}
	}
}

// TestListMarkerIsRecognisedOnlyAtALineStart. Telegram has no list entity,
// so this renderer is inferring; the inference has to be conservative, or a
// sentence with a dash in it becomes a bullet.
func TestListMarkerIsRecognisedOnlyAtALineStart(t *testing.T) {
	for _, line := range []string{
		"a sentence - with a dash in it",
		"2024 was a year",
		"-no space after the dash",
		"1.no space after the ordinal",
	} {
		if got := listMarker(line); got != 0 {
			t.Errorf("%q was read as a list item (marker %d)", line, got)
		}
	}
	for _, line := range []string{"- item", "* item", "· item", "1. item", "12) item"} {
		if listMarker(line) == 0 {
			t.Errorf("%q was not read as a list item", line)
		}
	}
}

// TestSpoilerIsHiddenUntilRevealed: a hidden spoiler is drawn in its own
// background, so it takes the right number of cells and reads as a
// deliberate block rather than as missing text — and nothing legible is left
// on screen for someone glancing at it.
func TestSpoilerIsHiddenUntilRevealed(t *testing.T) {
	r := testRoles()
	ft := formatted("the answer is 42", span(14, 2, &telegram.TextEntityTypeSpoiler{}))

	hidden := RenderInline(ft, r, textOpts{})
	if ansi.Strip(hidden) != "the answer is 42" {
		t.Fatalf("a spoiler must still occupy its cells: %q", ansi.Strip(hidden))
	}
	if !strings.Contains(hidden, "48;5;"+string(r.Sel)) {
		t.Fatalf("a hidden spoiler is not drawn on its own background: %q", hidden)
	}
	if !strings.Contains(hidden, "38;5;"+string(r.Sel)) {
		t.Fatalf("a hidden spoiler's foreground is legible: %q", hidden)
	}

	revealed := RenderInline(ft, r, textOpts{reveal: true})
	if strings.Contains(revealed, "38;5;"+string(r.Sel)) {
		t.Fatalf("a revealed spoiler is still hidden: %q", revealed)
	}
}

// TestMediaCardIsFramedWithRoomAndCollapsedWithout. Both forms are in the
// goldens; the threshold is where the framed one stops leaving space for a
// filename.
func TestMediaCardIsFramedWithRoomAndCollapsedWithout(t *testing.T) {
	r := testRoles()
	card := mediaCard{badge: "IMG", glyph: "▣", name: "auth-p95-2608.png",
		facts: []string{"1440×720", "184 KB", "png"}, actions: "enter open · s save"}

	framed := card.render(r, minCardWidth)
	if len(framed) != 3 {
		t.Fatalf("expected a three-row card at width %d, got %d rows", minCardWidth, len(framed))
	}
	if !strings.Contains(ansi.Strip(framed[1]), "IMG") {
		t.Fatalf("the badge is not in the box: %q", ansi.Strip(framed[1]))
	}
	if !strings.Contains(ansi.Strip(framed[2]), "enter open") {
		t.Fatalf("the framed card drops the actions: %q", ansi.Strip(framed[2]))
	}

	collapsed := card.render(r, minCardWidth-1)
	if len(collapsed) != 1 {
		t.Fatalf("expected one row below width %d, got %d", minCardWidth, len(collapsed))
	}
}

// TestCollapsedCardKeepsTheFactsAndDropsTheActions. A reader who cannot see
// "enter open" can press enter and find out; a reader who cannot see the
// size has no way to learn it from this screen at all.
func TestCollapsedCardKeepsTheFactsAndDropsTheActions(t *testing.T) {
	card := mediaCard{badge: "DOC", glyph: "▤", name: "a-rather-long-report-name.pdf",
		facts: []string{"2.4 MB", "pdf"}, actions: "enter open · s save"}

	got := ansi.Strip(card.collapsed(testRoles(), 30))
	if !strings.Contains(got, "2.4 MB") || !strings.Contains(got, "pdf") {
		t.Fatalf("the facts were dropped: %q", got)
	}
	if strings.Contains(got, "enter open") {
		t.Fatalf("the actions survived at the cost of the facts: %q", got)
	}
	if cell.Width(card.collapsed(testRoles(), 30)) > 30 {
		t.Fatalf("collapsed card overflows: %q", got)
	}
}

// TestMediaCardStatesOnlyKnownFacts. A card that fills in a plausible size
// is worse than one that omits it: the reader cannot tell which fields were
// measured and which were guessed.
func TestMediaCardStatesOnlyKnownFacts(t *testing.T) {
	card, ok := mediaCardFor(&telegram.MessageDocument{
		Document: &telegram.Document{FileName: "notes", File: &telegram.File{ID: "d"}},
	})
	if !ok {
		t.Fatal("a document must produce a card")
	}
	if len(card.facts) != 0 {
		t.Fatalf("a file with no size and no extension listed %v", card.facts)
	}
	if card.name != "notes" {
		t.Fatalf("name = %q", card.name)
	}

	withSize, _ := mediaCardFor(&telegram.MessageDocument{
		Document: &telegram.Document{
			FileName: "report.pdf",
			File:     &telegram.File{ID: "d", Size: 2_500_000},
		},
	})
	if got := strings.Join(withSize.facts, " · "); got != "2.4 MB · pdf" {
		t.Fatalf("facts = %q", got)
	}
}

// TestVoiceNoteWithoutAmplitudesDrawsNoWaveform. A bar drawn from nothing
// would be the one part of the card that looks like measurement and is not.
// The sender's client may never have computed one.
func TestVoiceNoteWithoutAmplitudesDrawsNoWaveform(t *testing.T) {
	card, ok := mediaCardFor(&telegram.MessageVoiceNote{
		VoiceNote: &telegram.VoiceNote{Duration: 47, File: &telegram.File{ID: "v"}},
	})
	if !ok {
		t.Fatal("a voice note must produce a card")
	}
	joined := ansi.Strip(strings.Join(card.render(testRoles(), 60), "\n"))
	for _, bar := range []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"} {
		if strings.Contains(joined, bar) {
			t.Fatalf("a waveform was drawn from no amplitude data: %q", joined)
		}
	}
	if !strings.Contains(joined, "0:47") {
		t.Fatalf("the duration, which is real, is missing: %q", joined)
	}
	if !strings.Contains(joined, "voice note") {
		t.Fatalf("a card with no waveform still needs a name: %q", joined)
	}
}

// TestEveryBodyLineFitsAndClosesItsStyles is the whole-body version of the
// two invariants each block is tested for on its own, across the content
// kinds a thread actually holds.
//
// From ONE cell, which is what the thread grid clamps its body column to
// rather than refusing to draw. Every block has a form it falls back to
// there — the code frame goes away, a ruled block loses its rule, a chip is
// cut — because a line wider than the column paints over whatever the grid
// put to the right of it.
func TestEveryBodyLineFitsAndClosesItsStyles(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	long := strings.Repeat("word ", 30)
	messages := map[string]telegram.MessageContent{
		"prose":  &telegram.MessageText{Text: formatted(long)},
		"code":   &telegram.MessageText{Text: formatted("x\n"+long, span(2, len([]rune(long)), &telegram.TextEntityTypePreCode{Language: "go"}))},
		"quote":  &telegram.MessageText{Text: formatted(long, span(0, len([]rune(long)), &telegram.TextEntityTypeBlockQuote{}))},
		"list":   &telegram.MessageText{Text: formatted("- " + long)},
		"cjk":    &telegram.MessageText{Text: formatted(strings.Repeat("你好世界🎉", 20))},
		"doc":    &telegram.MessageDocument{Document: &telegram.Document{FileName: "a.pdf", File: &telegram.File{ID: "d", Size: 1234}}},
		"voice":  &telegram.MessageVoiceNote{VoiceNote: &telegram.VoiceNote{Duration: 12, File: &telegram.File{ID: "v"}}},
		"poll":   &telegram.MessagePoll{Poll: &telegram.Poll{Question: long}},
		"joined": &telegram.MessageChatJoinByLink{},

		"voiced":  &telegram.MessageVoiceNote{VoiceNote: &telegram.VoiceNote{Duration: 47, File: &telegram.File{ID: "v"}, Waveform: speech(300)}},
		"tallied": &telegram.MessagePoll{Poll: designRecordPoll()},
		"preview": &telegram.MessageText{Text: formatted("Read later: " + long), WebPage: galleryPreview()},
	}

	for width := 1; width <= 120; width++ {
		for name, content := range messages {
			msg := &telegram.Message{ID: 1, Content: content, Reactions: galleryReactions()}
			for i, line := range r.RenderBody(msg, st, BodyOptions{Width: width}) {
				if got := cell.Width(line); got > width {
					t.Fatalf("%s at width %d: line %d is %d cells: %q",
						name, width, i, got, ansi.Strip(line))
				}
				if open := cell.OpenStyle(line); open != "" {
					t.Fatalf("%s at width %d: line %d leaves %q open", name, width, i, open)
				}
			}
		}
	}
}
