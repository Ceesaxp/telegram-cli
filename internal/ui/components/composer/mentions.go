package composer

import (
	"cmp"
	"slices"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// MentionSpan marks the runes of the draft that mention a user: the label the
// picker inserted, and who it means.
//
// Start and End are RUNE offsets into the composer's text, End exclusive, the
// same unit the textarea's cursor uses. UTF-16 is the wire's business and is
// converted there.
//
// The span is recorded when the mention is inserted and carried with the
// draft from then on (issue #41). It is never rediscovered from the visible
// text: two members can share a display name, and a name typed by hand is not
// a mention of anybody.
type MentionSpan struct {
	Start, End int
	UserID     int64
	// Label is the text the span covers. It is what the span is checked
	// against after every edit, and a span that no longer covers it goes.
	Label string
}

// InsertMention completes a mention: it replaces the runes [anchor, end) —
// the "@query" being typed — with label and a space, and leaves the cursor
// after the space, ready for the rest of the message.
//
// userID says who label means. A member with a username is mentioned by
// typing it, so for them the label is "@username", userID is 0, and only the
// text goes in. A member without one can only be mentioned by an entity
// carrying their ID, so the label — their name — gets a span. The span covers
// the label and not the space: the space is the composer's, not theirs, and
// deleting it must not cost the mention.
//
// Mentions already in the draft stay in step with the replacement: those
// before the token stay, those after it move along, and one inside it is
// replaced with it.
func (m *Model) InsertMention(anchor, end int, label string, userID int64) {
	n := m.textarea.Len()
	anchor = min(max(anchor, 0), n)
	end = min(max(end, anchor), n)

	m.textarea.DeleteRange(anchor, end)
	m.textarea.Cursor = anchor
	m.textarea.InsertString(label + " ")

	// Unlike an edit coming through editDraft, this one is known exactly —
	// [anchor, end) became the label and a space — so the spans follow it
	// by position rather than by a diff of the two texts. A diff reads a
	// label that begins with what it replaced as untouched: "@alex" picked
	// over "@" and a mention of somebody named "alex" would keep that
	// mention inside the username, and send it.
	delta := m.textarea.Len() - n
	var spans []MentionSpan
	for _, span := range m.mentions {
		switch {
		case span.End <= anchor:
		case span.Start >= end:
			span.Start += delta
			span.End += delta
		default:
			continue
		}
		spans = append(spans, span)
	}
	if userID != 0 && label != "" {
		spans = append(spans, MentionSpan{
			Start:  anchor,
			End:    anchor + len([]rune(label)),
			UserID: userID,
			Label:  label,
		})
		slices.SortFunc(spans, byStart)
	}
	m.mentions = spans
}

// validMentions returns the spans that cover their labels in text, sorted by
// Start, as a new slice.
//
// Submit runs it on the way out. Every path that changes the text already
// keeps the spans in step, so it should find nothing to drop there; it runs
// anyway because that is the last point before a user ID goes on the wire,
// and a mention over words that do not name its user is worse than no
// mention at all.
func validMentions(spans []MentionSpan, text string) []MentionSpan {
	out := coveringLabels(spans, []rune(text))
	slices.SortFunc(out, byStart)
	return out
}

// coveringLabels returns, as a new slice, the spans that cover exactly their
// labels in text.
func coveringLabels(spans []MentionSpan, text []rune) []MentionSpan {
	var out []MentionSpan
	for _, span := range spans {
		if coversLabel(text, span) {
			out = append(out, span)
		}
	}
	return out
}

func byStart(a, b MentionSpan) int { return cmp.Compare(a.Start, b.Start) }

// MentionsIn returns the mentions by ID in a message's text, as spans of it,
// for EnterEditMode.
//
// Only TextEntityTypeMentionName counts. A plain @username mention is its own
// text and needs no span, and nothing else names a user. An entity that does
// not fit inside the text is skipped rather than clamped: a shortened span
// would cover part of a name, and that is not a mention of anybody.
func MentionsIn(ft *telegram.FormattedText) []MentionSpan {
	if ft == nil {
		return nil
	}
	text := []rune(ft.Text)
	var out []MentionSpan
	for _, e := range ft.Entities {
		if e == nil {
			continue
		}
		mention, ok := e.Type.(*telegram.TextEntityTypeMentionName)
		if !ok {
			continue
		}
		span := MentionSpan{
			Start:  int(e.Offset),
			End:    int(e.Offset) + int(e.Length),
			UserID: mention.UserID,
		}
		if span.Start < 0 || span.Start >= span.End || span.End > len(text) {
			continue
		}
		span.Label = string(text[span.Start:span.End])
		out = append(out, span)
	}
	return out
}

// adjustMentions carries spans across one change to the draft, from oldText to
// newText. It returns a new slice and never touches spans: the model is
// copied by value, and a parked draft shares its spans with the copy that
// parked it.
//
// It does not need to know what the change was. Every editing primitive —
// typing, the emacs chords, vi's operators, a paste, the whole draft swapped
// at once — comes down to "these runes were replaced by those", and the two
// texts say which, with the cursor settling what they leave open. See
// changedRegion.
//
// Each span is then judged against that region alone:
//
//   - entirely before it: unchanged;
//   - entirely at or after its end: shifted by however much the text grew
//     or shrank;
//   - touching it at all: dropped. A mention whose label was edited no
//     longer says who it means, and guessing would put a user ID on text
//     that does not name them.
//
// The boundaries follow from that. An insertion exactly at a span's start is
// "at or after" the region, so the span moves along with the text in front
// of it; an insertion exactly at its end is "before", so the span stays and
// the new text follows it. Typing straight after a mention does not swallow
// the mention, and typing straight in front of one does not either.
//
// Finally, and whatever the diff concluded, a span survives only if it still
// covers exactly its label. The diff cannot be wrong about which runes
// changed, but a span that arrived out of step with the text can — and a
// mention must never be sent over text that does not name its user.
func adjustMentions(spans []MentionSpan, oldText, newText string, cursorBefore, cursorAfter int) []MentionSpan {
	if len(spans) == 0 {
		return nil
	}
	oldRunes, newRunes := []rune(oldText), []rune(newText)
	if oldText == newText {
		// Nothing was edited — the key only moved the cursor — so there
		// is no region to judge by, and an empty one placed at the cursor
		// would cut every span the cursor sits inside.
		return coveringLabels(spans, newRunes)
	}
	start, oldEnd := changedRegion(oldRunes, newRunes, min(cursorBefore, cursorAfter))
	delta := len(newRunes) - len(oldRunes)

	var out []MentionSpan
	for _, span := range spans {
		switch {
		case span.End <= start:
		case span.Start >= oldEnd:
			span.Start += delta
			span.End += delta
		default:
			continue
		}
		if coversLabel(newRunes, span) {
			out = append(out, span)
		}
	}
	return out
}

// changedRegion returns the runes of a, [start, oldEnd), that were replaced to
// make b, which must differ from it. at is where the cursor says the edit
// was.
//
// Whatever a and b share at the start and at the end was not touched. When
// those two leave a gap between them, the gap is the edit and the cursor is
// not consulted: vi's o opens a line past the end of this one wherever the
// cursor sits on it.
//
// When they meet or overlap, the texts cannot say where the edit was. It was
// an insertion or deletion of text that repeats what is next to it, and it
// could have been made anywhere from where the shared suffix begins to where
// the shared prefix ends: "Alex, Alex" -> "Alex" reads the same whichever
// name went. Only the cursor knows, so the edit goes where it says, held
// inside that range — and when two members share a name, that is the
// difference between keeping the mention the user kept and handing it to
// the other one.
func changedRegion(a, b []rune, at int) (start, oldEnd int) {
	short := min(len(a), len(b))
	prefix := 0
	for prefix < short && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < short && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	if prefix+suffix < short {
		return prefix, len(a) - suffix
	}
	start = min(max(at, short-suffix), prefix)
	return start, start + len(a) - short
}

// coversLabel reports whether span lies inside text and covers exactly its
// label.
func coversLabel(text []rune, span MentionSpan) bool {
	if span.Start < 0 || span.Start >= span.End || span.End > len(text) {
		return false
	}
	return string(text[span.Start:span.End]) == span.Label
}
