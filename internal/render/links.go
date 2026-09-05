package render

import (
	"sort"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
)

// Link is one followable destination in a message, as the reader sees it.
//
// Text is what is on screen and URI is where it actually goes, and the two
// are allowed to disagree — that is what a text_url entity IS. Keeping both
// is the point: a reader deciding whether to follow a link needs to see the
// destination, not the label somebody chose for it.
type Link struct {
	// Text is the visible run the entity covers.
	Text string
	// URI is the destination, resolved by [entityURI] — the same function
	// the renderer uses for OSC 8, so a link cannot be drawn pointing one
	// way and opened pointing another.
	URI string
	// InSpoiler marks a link that a spoiler covers. A hidden spoiler is
	// drawn foreground-on-background so it occupies the right cells and
	// reads as a deliberate block — which means a mark laid over it is
	// invisible too. The caller decides what to do about that; this only
	// reports it.
	InSpoiler bool
	// Lo and Hi are the rune offsets the entity covers in the message's own
	// text. The renderer marks exactly this range when the link is armed;
	// the block splitter preserves absolute offsets, so a link inside a
	// quote is still findable by them.
	Lo, Hi int
}

// Openable reports whether this destination can be handed to the platform
// opener: the scheme is one a message plausibly means, and the URI survives
// the same encoding and length rules an OSC 8 hyperlink is held to.
//
// Checked at the point of opening rather than when the list is built, so a
// link with a scheme this client refuses is still listed and still cycled
// past. A key that silently skips something the reader can plainly see
// underlined is a key that looks broken.
func (l Link) Openable() bool {
	_, ok := cell.SafeLinkURI(l.URI)
	return ok
}

// SafeURI is the destination in the form the platform opener should be given,
// or "" when it must not be opened at all.
func (l Link) SafeURI() string {
	uri, ok := cell.SafeLinkURI(l.URI)
	if !ok {
		return ""
	}
	return uri
}

// MessageLinks lists a message's links in reading order.
//
// Reading order rather than entity order: Telegram does not promise its
// entities are sorted, and "the second link" has to mean the second one down
// the screen or cycling through them is a lottery. Ties break on the wider
// span first, matching splitBlocks, so nesting is at least deterministic.
//
// Both a message's text and an attachment's caption are searched, because
// from the reader's side a link under a photo is a link.
func MessageLinks(msg *telegram.Message) []Link {
	if msg == nil {
		return nil
	}
	ft := textOf(msg.Content)
	if ft == nil || ft.Text == "" {
		return nil
	}

	runes := []rune(ft.Text)
	n := len(runes)

	var out []Link
	for _, e := range ft.Entities {
		if e == nil || e.Type == nil {
			continue
		}
		switch e.Type.(type) {
		case *telegram.TextEntityTypeTextURL, *telegram.TextEntityTypeURL,
			*telegram.TextEntityTypeEmailAddress:
		default:
			continue
		}
		lo := max(int(e.Offset), 0)
		hi := min(int(e.Offset+e.Length), n)
		if lo >= hi {
			continue
		}
		uri := entityURI(e, ft.Text, lo, hi)
		if uri == "" {
			// entityURI refuses to guess a destination it cannot read off
			// the entity or the covered text. A link with no destination is
			// not a link.
			continue
		}
		out = append(out, Link{
			Text: string(runes[lo:hi]), URI: uri, Lo: lo, Hi: hi,
			InSpoiler: overlapsSpoiler(ft, lo, hi, n),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Lo != out[j].Lo {
			return out[i].Lo < out[j].Lo
		}
		return out[i].Hi > out[j].Hi
	})
	return out
}

// textOf is the formatted text a message carries, whether that is its body
// or an attachment's caption.
func textOf(content telegram.MessageContent) *telegram.FormattedText {
	if t, ok := content.(*telegram.MessageText); ok {
		return t.Text
	}
	return captionOf(content)
}

// overlapsSpoiler reports whether any part of [lo,hi) is covered by a spoiler
// entity. Any overlap counts: half a link showing is still a link the reader
// cannot read the destination cue on.
func overlapsSpoiler(ft *telegram.FormattedText, lo, hi, n int) bool {
	for _, e := range ft.Entities {
		if e == nil || e.Type == nil {
			continue
		}
		if _, ok := e.Type.(*telegram.TextEntityTypeSpoiler); !ok {
			continue
		}
		slo := max(int(e.Offset), 0)
		shi := min(int(e.Offset+e.Length), n)
		if slo < hi && lo < shi {
			return true
		}
	}
	return false
}
