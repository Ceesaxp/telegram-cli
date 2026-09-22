package composer

import (
	"slices"
	"testing"
)

// nadia is the span the table below starts from: "Nadia" in "hi Nadia ok",
// runes [3, 8).
var nadia = MentionSpan{Start: 3, End: 8, UserID: 7, Label: "Nadia"}

func shifted(s MentionSpan, by int) MentionSpan {
	s.Start += by
	s.End += by
	return s
}

// TestAdjustMentions is the whole rule in one table. The draft is diffed
// before and after an edit; what the edit touched is the region between the
// common prefix and the common suffix, and every span is judged against that
// region alone.
func TestAdjustMentions(t *testing.T) {
	oleg := MentionSpan{Start: 10, End: 14, UserID: 8, Label: "Oleg"}

	cases := []struct {
		name  string
		spans []MentionSpan
		old   string
		new   string
		want  []MentionSpan
	}{
		// Insertions.
		{"insert before shifts", []MentionSpan{nadia},
			"hi Nadia ok", "oh, hi Nadia ok", []MentionSpan{shifted(nadia, 4)}},
		{"insert exactly at the start shifts", []MentionSpan{nadia},
			"hi Nadia ok", "hi XNadia ok", []MentionSpan{shifted(nadia, 1)}},
		{"insert inside drops", []MentionSpan{nadia},
			"hi Nadia ok", "hi NaXdia ok", nil},
		{"insert exactly at the end leaves it", []MentionSpan{nadia},
			"hi Nadia ok", "hi NadiaX ok", []MentionSpan{nadia}},
		{"insert after leaves it", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadia okay", []MentionSpan{nadia}},

		// Deletions.
		{"delete before shifts", []MentionSpan{nadia},
			"hi Nadia ok", "Nadia ok", []MentionSpan{shifted(nadia, -3)}},
		{"delete overlapping the start drops", []MentionSpan{nadia},
			"hi Nadia ok", "hiadia ok", nil},
		{"delete overlapping the end drops", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadiok", nil},
		{"delete covering drops", []MentionSpan{nadia},
			"hi Nadia ok", "hiok", nil},
		{"delete inside drops", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nia ok", nil},
		{"delete ending exactly at the start shifts", []MentionSpan{nadia},
			"hi Nadia ok", "hiNadia ok", []MentionSpan{shifted(nadia, -1)}},
		{"delete starting exactly at the end leaves it", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadiaok", []MentionSpan{nadia}},
		{"delete after leaves it", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadia", []MentionSpan{nadia}},

		// Replacements.
		{"replace before shifts by the length change", []MentionSpan{nadia},
			"hi Nadia ok", "hello Nadia ok", []MentionSpan{shifted(nadia, 3)}},
		{"replace inside drops", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadya ok", nil},
		{"replace after leaves it", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadia, bye", []MentionSpan{nadia}},

		// Offsets are runes, not bytes and not graphemes: the family emoji
		// is five runes joined by ZWJ, and the span moves by all five
		// plus the space.
		{"multi-rune emoji before shifts by its runes", []MentionSpan{nadia},
			"hi Nadia ok", "👨‍👩‍👧 hi Nadia ok", []MentionSpan{shifted(nadia, 6)}},
		{"CJK label", []MentionSpan{{Start: 3, End: 6, UserID: 9, Label: "李小龙"}},
			"你好 李小龙 ok", "嗨，你好 李小龙 ok",
			[]MentionSpan{{Start: 5, End: 8, UserID: 9, Label: "李小龙"}}},

		// Several spans are judged one at a time.
		{"two spans, edit between them", []MentionSpan{
			{Start: 0, End: 5, UserID: 7, Label: "Nadia"}, oleg},
			"Nadia and Oleg", "Nadia & Oleg", []MentionSpan{
				{Start: 0, End: 5, UserID: 7, Label: "Nadia"}, shifted(oleg, -2)}},
		{"two spans, edit inside the second", []MentionSpan{
			{Start: 0, End: 5, UserID: 7, Label: "Nadia"}, oleg},
			"Nadia and Oleg", "Nadia and Olga", []MentionSpan{
				{Start: 0, End: 5, UserID: 7, Label: "Nadia"}}},

		// Adjacent spans share a boundary: an insertion there is at the end
		// of the first and the start of the second, so the first stays and
		// the second moves. Neither is dropped.
		{"insert between adjacent spans", []MentionSpan{
			{Start: 0, End: 5, UserID: 7, Label: "Nadia"},
			{Start: 5, End: 9, UserID: 8, Label: "Oleg"}},
			"NadiaOleg", "Nadia, Oleg", []MentionSpan{
				{Start: 0, End: 5, UserID: 7, Label: "Nadia"},
				{Start: 7, End: 11, UserID: 8, Label: "Oleg"}}},
		{"delete the gap between two spans", []MentionSpan{
			{Start: 0, End: 5, UserID: 7, Label: "Nadia"},
			{Start: 6, End: 10, UserID: 8, Label: "Oleg"}},
			"Nadia Oleg", "NadiaOleg", []MentionSpan{
				{Start: 0, End: 5, UserID: 7, Label: "Nadia"},
				{Start: 5, End: 9, UserID: 8, Label: "Oleg"}}},

		// No change at all.
		{"identical text", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadia ok", []MentionSpan{nadia}},
		{"no spans", nil, "hi", "hello", nil},

		// The defensive check: whatever the diff concluded, a span only
		// survives if it still covers exactly its label.
		{"a span that no longer covers its label drops", []MentionSpan{
			{Start: 3, End: 8, UserID: 7, Label: "Nadia"}},
			"hi Oleg! ok", "hi Oleg! ok", nil},
		{"a span past the end of the text drops", []MentionSpan{
			{Start: 9, End: 14, UserID: 7, Label: "Nadia"}},
			"hi Nadia", "hi Nadia", nil},
		{"an empty span drops", []MentionSpan{
			{Start: 3, End: 3, UserID: 7, Label: ""}},
			"hi Nadia", "hi Nadia", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := slices.Clone(tc.spans)
			got := adjustMentions(tc.spans, tc.old, tc.new)
			if !slices.Equal(got, tc.want) {
				t.Errorf("adjustMentions(%q -> %q)\n got %+v\nwant %+v", tc.old, tc.new, got, tc.want)
			}
			if !slices.Equal(tc.spans, in) {
				t.Errorf("adjustMentions changed its input: %+v, was %+v", tc.spans, in)
			}
		})
	}
}

// The common prefix and suffix may not overlap. "aaa" -> "aa" has two runes
// in common at each end, and counting both would describe an edit region that
// ends before it starts. The prefix is measured first and the suffix gives
// way, so the deleted rune is the LAST one — the one the span covers — and the
// span goes, rather than sliding onto a rune it never marked.
func TestAdjustMentionsPrefixAndSuffixDoNotOverlap(t *testing.T) {
	span := MentionSpan{Start: 2, End: 3, UserID: 7, Label: "a"}
	if got := adjustMentions([]MentionSpan{span}, "aaa", "aa"); len(got) != 0 {
		t.Errorf("got %+v, want the span over the deleted rune dropped", got)
	}
}

// ---------------------------------------------------------------------------
// InsertMention
// ---------------------------------------------------------------------------

// A member without a username is mentioned by name, and the name only means
// them because the span says so.
func TestInsertMentionRecordsASpanOverTheLabel(t *testing.T) {
	m := typeInto(t, newFocused(), "hi @na")

	m.InsertMention(3, 6, "Nadia Petrova", 7)

	if got, want := m.Draft(), "hi Nadia Petrova "; got != want {
		t.Errorf("Draft = %q, want %q", got, want)
	}
	if got, want := m.textarea.Cursor, len([]rune("hi Nadia Petrova ")); got != want {
		t.Errorf("Cursor = %d, want %d (after the space)", got, want)
	}
	want := []MentionSpan{{Start: 3, End: 16, UserID: 7, Label: "Nadia Petrova"}}
	if !slices.Equal(m.mentions, want) {
		t.Errorf("mentions = %+v, want %+v (the label, not its trailing space)", m.mentions, want)
	}
}

// A member with a username is mentioned by typing it: "@nadia" is a mention
// on its own, so there is nothing to remember.
func TestInsertMentionWithoutAUserInsertsTextOnly(t *testing.T) {
	m := typeInto(t, newFocused(), "hi @na")

	m.InsertMention(3, 6, "@nadia", 0)

	if got, want := m.Draft(), "hi @nadia "; got != want {
		t.Errorf("Draft = %q, want %q", got, want)
	}
	if got, want := m.textarea.Cursor, len("hi @nadia "); got != want {
		t.Errorf("Cursor = %d, want %d", got, want)
	}
	if len(m.mentions) != 0 {
		t.Errorf("mentions = %+v, want none for a username", m.mentions)
	}
}

// The token being replaced can sit anywhere in the draft. What follows it
// moves along, and so do the mentions in it; what precedes it stays put.
func TestInsertMentionKeepsTheOtherSpansInStep(t *testing.T) {
	m := typeInto(t, newFocused(), "@na @ol")
	m.InsertMention(4, 7, "Oleg", 8)

	// Back to the first token and complete it too.
	m.textarea.Cursor = 3
	m.InsertMention(0, 3, "Nadia", 7)

	if got, want := m.Draft(), "Nadia  Oleg "; got != want {
		t.Fatalf("Draft = %q, want %q", got, want)
	}
	want := []MentionSpan{
		{Start: 0, End: 5, UserID: 7, Label: "Nadia"},
		{Start: 7, End: 11, UserID: 8, Label: "Oleg"},
	}
	if !slices.Equal(m.mentions, want) {
		t.Errorf("mentions = %+v, want %+v, in order", m.mentions, want)
	}
}
