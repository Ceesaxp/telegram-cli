package composer

import (
	"os"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
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
// common prefix and the common suffix — placed by the cursor where the two
// overlap — and every span is judged against that region alone.
func TestAdjustMentions(t *testing.T) {
	oleg := MentionSpan{Start: 10, End: 14, UserID: 8, Label: "Oleg"}

	// before and after are the cursor on either side of the edit: where it
	// would naturally be for the key that made it. An insertion starts at
	// before and ends with the cursor after the new text; a deletion
	// backwards ends with the cursor at its start.
	cases := []struct {
		name          string
		spans         []MentionSpan
		old, new      string
		before, after int
		want          []MentionSpan
	}{
		// Insertions.
		{"insert before shifts", []MentionSpan{nadia},
			"hi Nadia ok", "oh, hi Nadia ok", 0, 4, []MentionSpan{shifted(nadia, 4)}},
		{"insert exactly at the start shifts", []MentionSpan{nadia},
			"hi Nadia ok", "hi XNadia ok", 3, 4, []MentionSpan{shifted(nadia, 1)}},
		{"insert inside drops", []MentionSpan{nadia},
			"hi Nadia ok", "hi NaXdia ok", 5, 6, nil},
		{"insert exactly at the end leaves it", []MentionSpan{nadia},
			"hi Nadia ok", "hi NadiaX ok", 8, 9, []MentionSpan{nadia}},
		{"insert after leaves it", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadia okay", 11, 13, []MentionSpan{nadia}},

		// Deletions.
		{"delete before shifts", []MentionSpan{nadia},
			"hi Nadia ok", "Nadia ok", 3, 0, []MentionSpan{shifted(nadia, -3)}},
		{"delete overlapping the start drops", []MentionSpan{nadia},
			"hi Nadia ok", "hiadia ok", 4, 2, nil},
		{"delete overlapping the end drops", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadiok", 9, 7, nil},
		{"delete covering drops", []MentionSpan{nadia},
			"hi Nadia ok", "hiok", 9, 2, nil},
		{"delete inside drops", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nia ok", 6, 4, nil},
		{"delete ending exactly at the start shifts", []MentionSpan{nadia},
			"hi Nadia ok", "hiNadia ok", 3, 2, []MentionSpan{shifted(nadia, -1)}},
		{"delete starting exactly at the end leaves it", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadiaok", 9, 8, []MentionSpan{nadia}},
		{"delete after leaves it", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadia", 11, 8, []MentionSpan{nadia}},

		// Replacements.
		{"replace before shifts by the length change", []MentionSpan{nadia},
			"hi Nadia ok", "hello Nadia ok", 2, 5, []MentionSpan{shifted(nadia, 3)}},
		{"replace inside drops", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadya ok", 7, 7, nil},
		{"replace after leaves it", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadia, bye", 11, 13, []MentionSpan{nadia}},

		// Offsets are runes, not bytes and not graphemes: the family emoji
		// is five runes joined by ZWJ, and the span moves by all five
		// plus the space.
		{"multi-rune emoji before shifts by its runes", []MentionSpan{nadia},
			"hi Nadia ok", "👨\u200d👩\u200d👧 hi Nadia ok", 0, 6, []MentionSpan{shifted(nadia, 6)}},
		{"CJK label", []MentionSpan{{Start: 3, End: 6, UserID: 9, Label: "李小龙"}},
			"你好 李小龙 ok", "嗨，你好 李小龙 ok", 0, 2,
			[]MentionSpan{{Start: 5, End: 8, UserID: 9, Label: "李小龙"}}},

		// Several spans are judged one at a time.
		{"two spans, edit between them", []MentionSpan{
			{Start: 0, End: 5, UserID: 7, Label: "Nadia"}, oleg},
			"Nadia and Oleg", "Nadia & Oleg", 9, 7, []MentionSpan{
				{Start: 0, End: 5, UserID: 7, Label: "Nadia"}, shifted(oleg, -2)}},
		{"two spans, edit inside the second", []MentionSpan{
			{Start: 0, End: 5, UserID: 7, Label: "Nadia"}, oleg},
			"Nadia and Oleg", "Nadia and Olga", 14, 14, []MentionSpan{
				{Start: 0, End: 5, UserID: 7, Label: "Nadia"}}},

		// Adjacent spans share a boundary: an insertion there is at the end
		// of the first and the start of the second, so the first stays and
		// the second moves. Neither is dropped.
		{"insert between adjacent spans", []MentionSpan{
			{Start: 0, End: 5, UserID: 7, Label: "Nadia"},
			{Start: 5, End: 9, UserID: 8, Label: "Oleg"}},
			"NadiaOleg", "Nadia, Oleg", 5, 7, []MentionSpan{
				{Start: 0, End: 5, UserID: 7, Label: "Nadia"},
				{Start: 7, End: 11, UserID: 8, Label: "Oleg"}}},
		{"delete the gap between two spans", []MentionSpan{
			{Start: 0, End: 5, UserID: 7, Label: "Nadia"},
			{Start: 6, End: 10, UserID: 8, Label: "Oleg"}},
			"Nadia Oleg", "NadiaOleg", 6, 5, []MentionSpan{
				{Start: 0, End: 5, UserID: 7, Label: "Nadia"},
				{Start: 5, End: 9, UserID: 8, Label: "Oleg"}}},

		// No change at all.
		{"identical text", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadia ok", 11, 11, []MentionSpan{nadia}},
		{"no spans", nil, "hi", "hello", 2, 5, nil},

		// The defensive check: whatever the diff concluded, a span only
		// survives if it still covers exactly its label.
		{"a span that no longer covers its label drops", []MentionSpan{
			{Start: 3, End: 8, UserID: 7, Label: "Nadia"}},
			"hi Oleg! ok", "hi Oleg! ok", 11, 11, nil},
		{"a span past the end of the text drops", []MentionSpan{
			{Start: 9, End: 14, UserID: 7, Label: "Nadia"}},
			"hi Nadia", "hi Nadia", 8, 8, nil},
		{"an empty span drops", []MentionSpan{
			{Start: 3, End: 3, UserID: 7, Label: ""}},
			"hi Nadia", "hi Nadia", 8, 8, nil},

		// Repeated text. "Alex, Alex" -> "Alex" could be either name going,
		// and the texts alone cannot say which — but the cursor can. Two
		// members called Alex are two different people, and the one left
		// must be the one the user kept.
		{"deleting the first of two same names backwards keeps the second user", []MentionSpan{
			{Start: 0, End: 4, UserID: 1, Label: "Alex"},
			{Start: 6, End: 10, UserID: 2, Label: "Alex"}},
			"Alex, Alex", "Alex", 6, 0, []MentionSpan{
				{Start: 0, End: 4, UserID: 2, Label: "Alex"}}},
		{"deleting the first of two same names forwards keeps the second user", []MentionSpan{
			{Start: 0, End: 4, UserID: 1, Label: "Alex"},
			{Start: 6, End: 10, UserID: 2, Label: "Alex"}},
			"Alex, Alex", "Alex", 0, 0, []MentionSpan{
				{Start: 0, End: 4, UserID: 2, Label: "Alex"}}},
		{"deleting the second of two same names backwards keeps the first user", []MentionSpan{
			{Start: 0, End: 4, UserID: 1, Label: "Alex"},
			{Start: 6, End: 10, UserID: 2, Label: "Alex"}},
			"Alex, Alex", "Alex", 10, 4, []MentionSpan{
				{Start: 0, End: 4, UserID: 1, Label: "Alex"}}},
		{"deleting the second of two same names forwards keeps the first user", []MentionSpan{
			{Start: 0, End: 4, UserID: 1, Label: "Alex"},
			{Start: 6, End: 10, UserID: 2, Label: "Alex"}},
			"Alex, Alex", "Alex", 4, 4, []MentionSpan{
				{Start: 0, End: 4, UserID: 1, Label: "Alex"}}},
		{"typing the same name in front of a mention shifts it", []MentionSpan{
			{Start: 0, End: 4, UserID: 1, Label: "Alex"}},
			"Alex", "Alex, Alex", 0, 6, []MentionSpan{
				{Start: 6, End: 10, UserID: 1, Label: "Alex"}}},

		// The cursor only settles what the texts leave open. Where they
		// pin the edit down, it is not consulted: vi's o opens a line past
		// the end of this one wherever the cursor sits on it, and a cursor
		// that merely moved changed nothing at all.
		{"a line opened past the cursor leaves the mention between", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadia ok\n", 0, 0, []MentionSpan{nadia}},
		{"a cursor moving into a mention leaves it", []MentionSpan{nadia},
			"hi Nadia ok", "hi Nadia ok", 11, 5, []MentionSpan{nadia}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := slices.Clone(tc.spans)
			got := adjustMentions(tc.spans, tc.old, tc.new, tc.before, tc.after)
			if !slices.Equal(got, tc.want) {
				t.Errorf("adjustMentions(%q -> %q, cursor %d -> %d)\n got %+v\nwant %+v",
					tc.old, tc.new, tc.before, tc.after, got, tc.want)
			}
			if !slices.Equal(tc.spans, in) {
				t.Errorf("adjustMentions changed its input: %+v, was %+v", tc.spans, in)
			}
		})
	}
}

// "aaa" -> "aa" has two runes in common at each end: any of the three could
// be the one that went. The cursor says which. Backspace at the end deleted
// the last — the one the span covers — so the span goes; a forward delete at
// the start deleted the first, so the span moves back onto its own rune.
func TestAdjustMentionsPlacesAnAmbiguousEditAtTheCursor(t *testing.T) {
	span := MentionSpan{Start: 2, End: 3, UserID: 7, Label: "a"}
	if got := adjustMentions([]MentionSpan{span}, "aaa", "aa", 3, 2); len(got) != 0 {
		t.Errorf("backspace at the end: got %+v, want the span over the deleted rune dropped", got)
	}
	if got, want := adjustMentions([]MentionSpan{span}, "aaa", "aa", 0, 0), []MentionSpan{shifted(span, -1)}; !slices.Equal(got, want) {
		t.Errorf("delete at the start: got %+v, want %+v", got, want)
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

// ---------------------------------------------------------------------------
// Spans follow the edits, whatever makes them
// ---------------------------------------------------------------------------

// withNadia is a composer holding "hi Nadia " with Nadia mentioned by ID,
// runes [3, 8), and the cursor after the trailing space — the state a
// completed mention leaves behind.
func withNadia(t *testing.T, m Model) Model {
	t.Helper()
	m = typeInto(t, m, "hi @na")
	m.InsertMention(3, 6, "Nadia", 7)
	if want := []MentionSpan{nadia}; !slices.Equal(m.mentions, want) {
		t.Fatalf("precondition: mentions = %+v, want %+v", m.mentions, want)
	}
	return m
}

func wantMentions(t *testing.T, m Model, want ...MentionSpan) {
	t.Helper()
	if !slices.Equal(m.mentions, want) {
		t.Errorf("draft %q: mentions = %+v, want %+v", m.Draft(), m.mentions, want)
	}
}

func TestTypingBeforeAMentionShiftsIt(t *testing.T) {
	m := withNadia(t, newFocused())
	m = typeSeq(t, m, "\x01") // ctrl+a
	m = chars(t, m, "oh ")

	wantMentions(t, m, shifted(nadia, 3))
}

func TestTypingAfterAMentionLeavesIt(t *testing.T) {
	m := withNadia(t, newFocused())
	m = chars(t, m, "how are you")

	wantMentions(t, m, nadia)
}

// The space after a mention is the composer's, so backspacing over it costs
// nothing. The next backspace eats into the name, and the name no longer
// says who it means.
func TestBackspacingIntoAMentionDropsIt(t *testing.T) {
	m := withNadia(t, newFocused())

	m = typeSeq(t, m, "\x7f")
	wantMentions(t, m, nadia)

	m = typeSeq(t, m, "\x7f")
	wantMentions(t, m)
}

// ctrl+w kills the word before the cursor — the whole mention.
func TestKillingAMentionDropsIt(t *testing.T) {
	m := withNadia(t, newFocused())
	m = typeSeq(t, m, "\x17") // ctrl+w

	if got := m.Draft(); got != "hi " {
		t.Fatalf("precondition: Draft = %q, want %q", got, "hi ")
	}
	wantMentions(t, m)
}

func TestANewlineBeforeAMentionShiftsIt(t *testing.T) {
	m := withNadia(t, newFocused())
	m = typeSeq(t, m, "\x01", "\n") // ctrl+a, ctrl+j

	wantMentions(t, m, shifted(nadia, 1))
}

func TestPastingBeforeAMentionShiftsIt(t *testing.T) {
	m := withNadia(t, newFocused())
	m = typeSeq(t, m, "\x01") // ctrl+a
	m, _ = m.Update(tea.PasteMsg{Content: "well,\n"})

	wantMentions(t, m, shifted(nadia, 6))
}

func TestPastingIntoAMentionDropsIt(t *testing.T) {
	m := withNadia(t, newFocused())
	m.textarea.Cursor = 5
	m, _ = m.Update(tea.PasteMsg{Content: "xx"})

	wantMentions(t, m)
}

// vi's operators go through the same door as typing. D from the start of the
// line takes the mention with it; x on its first letter breaks it.
func TestViOperatorsOverAMentionDropIt(t *testing.T) {
	for _, keys := range []string{"0D", "dd", "0lllx"} {
		t.Run(keys, func(t *testing.T) {
			m := withNadia(t, viComposer(t))
			m = typeSeq(t, m, "\x1b") // Esc: normal mode
			m = chars(t, m, keys)

			wantMentions(t, m)
		})
	}
}

// ... and dd on another line only moves it.
func TestViDeletingAnotherLineShiftsAMention(t *testing.T) {
	m := chars(t, viComposer(t), "first")
	m = typeSeq(t, m, "\n") // ctrl+j
	m = chars(t, m, "hi @na")
	m.InsertMention(9, 12, "Nadia", 7)
	wantMentions(t, m, shifted(nadia, 6))

	m = typeSeq(t, m, "\x1b") // Esc: normal mode
	m = chars(t, m, "kdd")

	if got := m.Draft(); got != "hi Nadia " {
		t.Fatalf("precondition: Draft = %q, want the first line gone", got)
	}
	wantMentions(t, m, nadia)
}

// vi's o opens the new line past the end of this one, wherever the cursor is
// on it. A mention between the two is not in the way.
func TestViOpeningALineKeepsAMentionBeforeIt(t *testing.T) {
	m := withNadia(t, viComposer(t))
	m = typeSeq(t, m, "\x1b") // Esc: normal mode
	m = chars(t, m, "0o")

	if got := m.Draft(); got != "hi Nadia \n" {
		t.Fatalf("precondition: Draft = %q", got)
	}
	wantMentions(t, m, nadia)
}

// ---------------------------------------------------------------------------
// Two members with the same name
// ---------------------------------------------------------------------------

var (
	alex1 = MentionSpan{Start: 0, End: 4, UserID: 1, Label: "Alex"}
	alex2 = MentionSpan{Start: 6, End: 10, UserID: 2, Label: "Alex"}
)

// mentionAlex completes "@a" at the cursor as a mention of Alex, the member
// with ID user, and takes back the space the completion adds.
func mentionAlex(t *testing.T, m Model, user int64) Model {
	t.Helper()
	m = typeInto(t, m, "@a")
	at := m.textarea.Cursor - 2
	m.InsertMention(at, at+2, "Alex", user)
	return typeSeq(t, m, "\x7f") // backspace
}

// alexAndAlex is "Alex, Alex": two different members called Alex, user 1
// mentioned first and user 2 second, with the cursor at the end.
func alexAndAlex(t *testing.T, m Model) Model {
	t.Helper()
	m = mentionAlex(t, m, 1)
	m = typeInto(t, m, ", ")
	m = mentionAlex(t, m, 2)
	if got := m.Draft(); got != "Alex, Alex" {
		t.Fatalf("precondition: Draft = %q", got)
	}
	wantMentions(t, m, alex1, alex2)
	return m
}

// Deleting the first Alex leaves text that reads exactly as if the second
// had been deleted. The mention left must be the one the user kept.
func TestDeletingTheFirstOfTwoSameNamesKeepsTheSecondUser(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
	}{
		{"ctrl+u from the second", append(slices.Repeat([]string{"\x02"}, 4), "\x15")},
		{"backspace from the second", append(slices.Repeat([]string{"\x02"}, 4), slices.Repeat([]string{"\x7f"}, 6)...)},
		{"ctrl+d from the start", append([]string{"\x01"}, slices.Repeat([]string{"\x04"}, 6)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := alexAndAlex(t, newFocused())
			m = typeSeq(t, m, tc.keys...)

			if got := m.Draft(); got != "Alex" {
				t.Fatalf("precondition: Draft = %q", got)
			}
			wantMentions(t, m, MentionSpan{Start: 0, End: 4, UserID: 2, Label: "Alex"})
		})
	}
}

func TestDeletingTheSecondOfTwoSameNamesKeepsTheFirstUser(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
	}{
		{"backspace from the end", slices.Repeat([]string{"\x7f"}, 6)},
		{"ctrl+k from after the first", append(slices.Repeat([]string{"\x02"}, 6), "\x0b")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := alexAndAlex(t, newFocused())
			m = typeSeq(t, m, tc.keys...)

			if got := m.Draft(); got != "Alex" {
				t.Fatalf("precondition: Draft = %q", got)
			}
			wantMentions(t, m, alex1)
		})
	}
}

// Typing or pasting the same name in front of a mention moves the mention;
// it does not hand it to the words that were just typed.
func TestTheSameNameTypedInFrontOfAMentionShiftsIt(t *testing.T) {
	moved := MentionSpan{Start: 6, End: 10, UserID: 1, Label: "Alex"}

	t.Run("typed", func(t *testing.T) {
		m := mentionAlex(t, newFocused(), 1)
		m = typeSeq(t, m, "\x01") // ctrl+a
		m = typeInto(t, m, "Alex, ")
		wantMentions(t, m, moved)
	})
	t.Run("pasted", func(t *testing.T) {
		m := mentionAlex(t, newFocused(), 1)
		m = typeSeq(t, m, "\x01") // ctrl+a
		m, _ = m.Update(tea.PasteMsg{Content: "Alex, "})
		wantMentions(t, m, moved)
	})
}

// vi's normal mode moves the cursor after an edit — dd lands it at the start
// of a line, which for the last line is the line above — so the cursor it
// ends on is no evidence of where the edit was.
func TestViDdBetweenTwoSameNamedLinesKeepsTheRightUser(t *testing.T) {
	build := func(t *testing.T) Model {
		t.Helper()
		m := mentionAlex(t, viComposer(t), 1)
		m = typeSeq(t, m, "\n") // ctrl+j
		m = mentionAlex(t, m, 2)
		if got := m.Draft(); got != "Alex\nAlex" {
			t.Fatalf("precondition: Draft = %q", got)
		}
		return typeSeq(t, m, "\x1b") // Esc: normal mode
	}

	t.Run("dd on the first line", func(t *testing.T) {
		m := chars(t, build(t), "k0dd")
		wantMentions(t, m, MentionSpan{Start: 0, End: 4, UserID: 2, Label: "Alex"})
	})
	t.Run("dd on the last line", func(t *testing.T) {
		m := chars(t, build(t), "dd")
		wantMentions(t, m, alex1)
	})
	// From the middle of the line the cursor says nothing about which Alex
	// went; dd deletes from the line's start, and that is what places it.
	t.Run("dd on the first line from its middle", func(t *testing.T) {
		m := chars(t, build(t), "k0lldd")
		wantMentions(t, m, MentionSpan{Start: 0, End: 4, UserID: 2, Label: "Alex"})
	})
}

// dd deletes a line from its start — from the break before it, on the last
// line — wherever the cursor sits on it. When the line after it starts with
// the same words the two texts read the same whichever line went, and an
// edit placed at the cursor handed the deleted line's mention to the line
// that took its place.
func TestViDdPlacesTheEditWhereTheLineStarts(t *testing.T) {
	// lines builds one line per user, each "<before>Alex<after>" with Alex
	// mentioning that user, and leaves vi in normal mode on the last line.
	lines := func(t *testing.T, before, after string, users ...int64) Model {
		t.Helper()
		m := viComposer(t)
		for i, user := range users {
			if i > 0 {
				m = typeSeq(t, m, "\n") // ctrl+j
			}
			m = typeInto(t, m, before)
			m = mentionAlex(t, m, user)
			m = typeInto(t, m, after)
		}
		return typeSeq(t, m, "\x1b") // Esc: normal mode
	}
	alex := func(start int, user int64) MentionSpan {
		return MentionSpan{Start: start, End: start + 4, UserID: user, Label: "Alex"}
	}

	for _, tc := range []struct {
		name          string
		before, after string
		users         []int64
		keys          string
		draft         string
		want          []MentionSpan
	}{
		{"the cursor past the mention", "", " x", []int64{1, 2}, "kdd",
			"Alex x", []MentionSpan{alex(0, 2)}},
		{"the cursor inside the mention", "", " x", []int64{1, 2}, "k0ldd",
			"Alex x", []MentionSpan{alex(0, 2)}},
		{"a mention mid-line, the cursor at the end", "hi ", "!", []int64{1, 2}, "k$dd",
			"hi Alex!", []MentionSpan{alex(3, 2)}},
		{"the middle of three lines", "", " x", []int64{1, 2, 3}, "kdd",
			"Alex x\nAlex x", []MentionSpan{alex(0, 1), alex(7, 3)}},
		{"the last line", "", " x", []int64{1, 2}, "dd",
			"Alex x", []MentionSpan{alex(0, 1)}},
		{"the last line from its start", "", " x", []int64{1, 2}, "0dd",
			"Alex x", []MentionSpan{alex(0, 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := chars(t, lines(t, tc.before, tc.after, tc.users...), tc.keys)
			if got := m.Draft(); got != tc.draft {
				t.Fatalf("precondition: Draft = %q, want %q", got, tc.draft)
			}
			wantMentions(t, m, tc.want...)
		})
	}
}

// ---------------------------------------------------------------------------
// Spans are part of the draft
// ---------------------------------------------------------------------------

// A parked draft is all of the work, and a mention is part of it. The words
// coming back without it would send a name that pings nobody.
func TestMentionsSurviveAChatSwitch(t *testing.T) {
	m := withNadia(t, newFocused())

	m.SetChatId(43)
	wantMentions(t, m)

	m.SetChatId(42)
	if got := m.Draft(); got != "hi Nadia " {
		t.Fatalf("precondition: Draft = %q", got)
	}
	wantMentions(t, m, nadia)
}

// The chat switched to must not inherit the spans of the one left behind,
// even when it has a draft of its own to restore.
func TestMentionsStayWithTheirOwnChat(t *testing.T) {
	m := newFocused()
	m.SetChatId(43)
	m = typeInto(t, m, "hi there")
	m.SetChatId(42)
	m = withNadia(t, m)

	m.SetChatId(43)
	if got := m.Draft(); got != "hi there" {
		t.Fatalf("precondition: Draft = %q", got)
	}
	wantMentions(t, m)
}

// Replying changes what the message answers, not what it says.
func TestReplyModeKeepsMentions(t *testing.T) {
	m := withNadia(t, newFocused())

	m.EnterReplyMode(5, "oleg: who's on call?")
	wantMentions(t, m, nadia)

	m, _ = press(t, m, "esc") // cancel the reply, keep the words
	wantMentions(t, m, nadia)
}

// With an attachment staged the text is its caption — the same text, so the
// same spans.
func TestAnAttachmentCaptionKeepsMentions(t *testing.T) {
	m := withNadia(t, newFocused())

	m.SetAttachment("/tmp/paste-1.png", true)
	wantMentions(t, m, nadia)

	m, _ = press(t, m, "esc") // unstage the file, keep the caption
	wantMentions(t, m, nadia)
}

// Editing a message parks the draft on screen, spans and all, and puts it
// back when the edit is cancelled.
func TestEditModeParksMentions(t *testing.T) {
	m := withNadia(t, newFocused())

	m.EnterEditMode(99, "the old message")
	wantMentions(t, m)

	m, _ = press(t, m, "esc")
	if got := m.Draft(); got != "hi Nadia " {
		t.Fatalf("precondition: Draft = %q", got)
	}
	wantMentions(t, m, nadia)
}

// A mention added while editing belongs to the edit, and travels with it
// through a chat switch; the draft the edit displaced keeps its own.
func TestAnEditKeepsItsMentionsThroughAChatSwitch(t *testing.T) {
	m := withNadia(t, newFocused())
	m.EnterEditMode(99, "ask ")
	m = chars(t, m, "@ol")
	m.InsertMention(4, 7, "Oleg", 8)
	oleg := MentionSpan{Start: 4, End: 8, UserID: 8, Label: "Oleg"}
	wantMentions(t, m, oleg)

	m.SetChatId(43)
	m.SetChatId(42)
	if !m.IsEditing() {
		t.Fatal("precondition: the edit did not come back with the chat")
	}
	wantMentions(t, m, oleg)

	m, _ = press(t, m, "esc")
	wantMentions(t, m, nadia)
}

// ---------------------------------------------------------------------------
// Submit
// ---------------------------------------------------------------------------

func submitted(t *testing.T, m Model) (Model, MessageSubmittedMsg) {
	t.Helper()
	m, msg := press(t, m, "enter")
	sub, ok := msg.(MessageSubmittedMsg)
	if !ok {
		t.Fatalf("got %T, want MessageSubmittedMsg", msg)
	}
	return m, sub
}

// The spans go out with the text, in order, and the next message starts
// with none.
func TestSubmitCarriesMentionsSortedAndClearsThem(t *testing.T) {
	m := newFocused()
	m.textarea.Value = "Nadia and Oleg"
	first := MentionSpan{Start: 0, End: 5, UserID: 7, Label: "Nadia"}
	second := MentionSpan{Start: 10, End: 14, UserID: 8, Label: "Oleg"}
	m.mentions = []MentionSpan{second, first}

	m, sub := submitted(t, m)
	if want := []MentionSpan{first, second}; !slices.Equal(sub.Mentions, want) {
		t.Errorf("Mentions = %+v, want %+v", sub.Mentions, want)
	}
	wantMentions(t, m)

	m = typeInto(t, m, "next")
	if _, sub = submitted(t, m); sub.Mentions != nil {
		t.Errorf("the next message carried %+v", sub.Mentions)
	}
}

func TestSubmitCarriesAMentionTypedTheUsualWay(t *testing.T) {
	m := withNadia(t, newFocused())
	m = chars(t, m, "ping")

	_, sub := submitted(t, m)
	if sub.Text != "hi Nadia ping" {
		t.Fatalf("precondition: Text = %q", sub.Text)
	}
	if want := []MentionSpan{nadia}; !slices.Equal(sub.Mentions, want) {
		t.Errorf("Mentions = %+v, want %+v", sub.Mentions, want)
	}
}

// The caption of an attachment is the text, and its mentions go with it.
func TestSubmitCarriesACaptionsMentions(t *testing.T) {
	m := withNadia(t, newFocused())
	m.SetAttachment("/tmp/paste-1.png", true)

	_, sub := submitted(t, m)
	if sub.Attachment != "/tmp/paste-1.png" {
		t.Fatalf("precondition: Attachment = %q", sub.Attachment)
	}
	if want := []MentionSpan{nadia}; !slices.Equal(sub.Mentions, want) {
		t.Errorf("Mentions = %+v, want %+v", sub.Mentions, want)
	}
}

// Sending an edit sends the edit's mentions, then the displaced draft comes
// back with its own.
func TestSubmittingAnEditCarriesItsMentions(t *testing.T) {
	m := withNadia(t, newFocused())
	m.EnterEditMode(99, "ask ")
	m = chars(t, m, "@ol")
	m.InsertMention(4, 7, "Oleg", 8)

	m, sub := submitted(t, m)
	if sub.EditMessageId != 99 {
		t.Fatalf("precondition: EditMessageId = %d", sub.EditMessageId)
	}
	oleg := MentionSpan{Start: 4, End: 8, UserID: 8, Label: "Oleg"}
	if want := []MentionSpan{oleg}; !slices.Equal(sub.Mentions, want) {
		t.Errorf("Mentions = %+v, want %+v", sub.Mentions, want)
	}
	wantMentions(t, m, nadia)
}

// The last line of defence: a span that has fallen out of step with the
// text — however it got there — is not sent. A user ID on the wrong words is
// worse than no mention at all.
func TestSubmitNeverSendsASpanOverTheWrongText(t *testing.T) {
	m := withNadia(t, newFocused())
	m.textarea.Value = "hi Oleg!! "

	_, sub := submitted(t, m)
	if len(sub.Mentions) != 0 {
		t.Errorf("Mentions = %+v, want none: the text no longer says Nadia", sub.Mentions)
	}
}

// ---------------------------------------------------------------------------
// External editor
// ---------------------------------------------------------------------------

// Opening the editor and closing it without a change costs nothing — the
// real round trip, through a stub that touches nothing.
func TestAnUnchangedEditorRoundTripKeepsMentions(t *testing.T) {
	t.Setenv("VISUAL", stubEditor(t, `:`))
	t.Setenv("EDITOR", "")
	m := withNadia(t, newFocused())

	var runErr error
	m, _ = runEditor(t, m, &runErr)
	if runErr != nil {
		t.Fatalf("stub editor failed: %v", runErr)
	}

	wantMentions(t, m, nadia)
	if strings.Contains(m.View(), noticeMentionsDropped) {
		t.Errorf("a notice about dropping mentions nothing dropped:\n%s", m.View())
	}
}

// The trailing newline an editor adds is trimmed on the way back in, so a
// file saved as-is still comes back as the same text.
func TestAnEditorsTrailingNewlineIsNotAChange(t *testing.T) {
	m := withNadia(t, newFocused())
	m, _ = m.Update(editorFinishedMsg{text: "hi Nadia \n", ok: true})

	wantMentions(t, m, nadia)
}

// Text changed outside the composer cannot be followed: no edit came through
// editDraft, only a new text. Guessing where the mention went could put a
// user ID on words that do not name them, so the mentions go — and the user
// is told, because the words still look the same.
func TestAChangedEditorResultDropsMentionsAndSaysSo(t *testing.T) {
	m := withNadia(t, newFocused())
	m, _ = m.Update(editorFinishedMsg{text: "hi Nadia, how are you?\n", ok: true})

	if got := m.Draft(); got != "hi Nadia, how are you?" {
		t.Fatalf("precondition: Draft = %q", got)
	}
	wantMentions(t, m)
	if !strings.Contains(m.View(), noticeMentionsDropped) {
		t.Errorf("no notice that the mentions were dropped:\n%s", m.View())
	}
}

// With no mentions to lose there is nothing to say.
func TestAChangedEditorResultWithoutMentionsSaysNothing(t *testing.T) {
	m := typeInto(t, newFocused(), "hi")
	m, _ = m.Update(editorFinishedMsg{text: "hi there\n", ok: true})

	if strings.Contains(m.View(), noticeMentionsDropped) {
		t.Errorf("a notice about mentions the draft never had:\n%s", m.View())
	}
}

// A failed editor keeps the draft, so it keeps the mentions in it.
func TestAFailedEditorKeepsMentions(t *testing.T) {
	m := withNadia(t, newFocused())
	m, _ = m.Update(editorFinishedMsg{err: os.ErrPermission})

	wantMentions(t, m, nadia)
}

// ---------------------------------------------------------------------------
// Editing a message that already mentions somebody
// ---------------------------------------------------------------------------

// Only mentions by ID become spans. "@oleg" typed out is a mention on its own
// and bold is not a mention at all. Offsets are already runes by the time
// they reach the domain type.
func TestMentionsInReadsTheMentionsByID(t *testing.T) {
	ft := &telegram.FormattedText{
		Text: "hi Nadia, @oleg 👍 Лев",
		Entities: []*telegram.TextEntity{
			{Offset: 0, Length: 2, Type: &telegram.TextEntityTypeBold{}},
			{Offset: 3, Length: 5, Type: &telegram.TextEntityTypeMentionName{UserID: 7}},
			{Offset: 10, Length: 5, Type: &telegram.TextEntityTypeMention{}},
			nil,
			{Offset: 18, Length: 3, Type: &telegram.TextEntityTypeMentionName{UserID: 9}},
			{Offset: 20, Length: 9, Type: &telegram.TextEntityTypeMentionName{UserID: 10}},
		},
	}

	want := []MentionSpan{
		nadia,
		{Start: 18, End: 21, UserID: 9, Label: "Лев"},
	}
	if got := MentionsIn(ft); !slices.Equal(got, want) {
		t.Errorf("MentionsIn = %+v, want %+v (the out-of-range one dropped)", got, want)
	}
	if got := MentionsIn(nil); got != nil {
		t.Errorf("MentionsIn(nil) = %+v, want nil", got)
	}
}

// The message being edited keeps the mentions it was sent with: sending the
// edit without them would turn a mention into a name that pings nobody.
func TestEditModeLoadsTheMessagesMentions(t *testing.T) {
	m := newFocused()
	m.EnterEditMode(99, "hi Nadia ok", nadia)
	wantMentions(t, m, nadia)

	_, sub := submitted(t, m)
	if want := []MentionSpan{nadia}; !slices.Equal(sub.Mentions, want) {
		t.Errorf("Mentions = %+v, want %+v", sub.Mentions, want)
	}
}

// What the caller hands over is checked against the text like any other span.
func TestEditModeDropsAMentionThatDoesNotFitTheText(t *testing.T) {
	m := newFocused()
	m.EnterEditMode(99, "hi Oleg ok", nadia)

	wantMentions(t, m)
}
