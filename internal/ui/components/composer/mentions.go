package composer

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

// adjustMentions carries spans across one change to the draft, from oldText to
// newText. It returns a new slice and never touches spans: the model is
// copied by value, and a parked draft shares its spans with the copy that
// parked it.
//
// It does not need to know what the change was. Every editing primitive —
// typing, the emacs chords, vi's operators, a paste, the whole draft swapped
// at once — comes down to "these runes were replaced by those", and the two
// texts say which: whatever they share at the start and at the end was not
// touched, and what lies between was. The shared prefix is measured first,
// and the shared suffix is not allowed to reach back into it, so the region
// never ends before it starts.
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
func adjustMentions(spans []MentionSpan, oldText, newText string) []MentionSpan {
	if len(spans) == 0 {
		return nil
	}
	oldRunes, newRunes := []rune(oldText), []rune(newText)
	prefix, suffix := commonAffixes(oldRunes, newRunes)
	oldEnd := len(oldRunes) - suffix
	delta := len(newRunes) - len(oldRunes)

	var out []MentionSpan
	for _, span := range spans {
		switch {
		case span.End <= prefix:
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

// commonAffixes returns how many runes a and b share at the start and, of what
// is left after that, at the end.
func commonAffixes(a, b []rune) (prefix, suffix int) {
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	for suffix < len(a)-prefix && suffix < len(b)-prefix &&
		a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	return prefix, suffix
}

// coversLabel reports whether span lies inside text and covers exactly its
// label.
func coversLabel(text []rune, span MentionSpan) bool {
	if span.Start < 0 || span.Start >= span.End || span.End > len(text) {
		return false
	}
	return string(text[span.Start:span.End]) == span.Label
}
