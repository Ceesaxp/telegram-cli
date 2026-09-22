package composer

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
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
