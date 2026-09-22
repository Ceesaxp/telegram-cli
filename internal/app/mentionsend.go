package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
)

// Sending what the @ picker inserted (issue #41).
//
// A member with a username was inserted as "@username", which is a mention
// all by itself and needs nothing here. A member without one was inserted
// by name, and the draft carries a span saying who the name means. The
// span travels with the send, and the client turns it into an entity.

// mentionSpans converts the composer's spans to the send path's. Both
// count runes of the text as typed; the wire's UTF-16 is the client's
// business. No spans is nil, which every send takes as "no mentions".
func mentionSpans(spans []composer.MentionSpan) []telegram.MentionSpan {
	if len(spans) == 0 {
		return nil
	}
	out := make([]telegram.MentionSpan, 0, len(spans))
	for _, s := range spans {
		out = append(out, telegram.MentionSpan{Offset: s.Start, Length: s.End - s.Start, UserID: s.UserID})
	}
	return out
}

// mentionsSentPlainMsg reports a send that went out with that many of its
// mentions as plain text: the member could not be resolved, so the name
// was sent without saying who it means.
type mentionsSentPlainMsg int

// sentPlain is what a successful send reports back: nothing, unless some of
// its mentions went out as plain text.
func sentPlain(n int) tea.Msg {
	if n == 0 {
		return nil
	}
	return mentionsSentPlainMsg(n)
}

// mentionsSentPlainNotice says so. A warning, not an error: the message was
// sent, and reads as the reader wrote it — it just notifies nobody.
func mentionsSentPlainNotice(n mentionsSentPlainMsg) string {
	if n == 1 {
		return "⚠ 1 mention sent as plain text"
	}
	return fmt.Sprintf("⚠ %d mentions sent as plain text", n)
}
