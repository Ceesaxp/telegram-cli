package composer

import "github.com/Ceesaxp/telegram-cli/internal/telegram"

// MessageSubmittedMsg is emitted when the user submits a message.
type MessageSubmittedMsg struct {
	ChatId        int64
	Text          string
	ReplyToId     int64
	EditMessageId int64
	Attachment    string // local file path, empty if none
	AsPhoto       bool   // send the attachment as an inline photo, not a document

	// Mentions are the users mentioned by ID in Text, as rune ranges of it,
	// sorted by Start (issue #41). Only the mentions a username cannot carry
	// are here; "@username" typed out is already a mention on its own.
	// Converting to UTF-16 and merging with the Markdown entities is the
	// send path's business.
	Mentions []MentionSpan
}

// AttachRequestedMsg is emitted when the user asks to attach a file (Ctrl+T).
type AttachRequestedMsg struct{}

// PasteRequestedMsg is emitted when the user asks to attach whatever image is
// on the system clipboard (Ctrl+V).
type PasteRequestedMsg struct{}

// AttachmentDiscardedMsg is emitted when a pending attachment is dropped
// without being sent (Escape), so the owner can delete the spooled file.
type AttachmentDiscardedMsg struct {
	Path string
}

// ResizedMsg is emitted when the composer's row count changes, so the host
// can recompute the layout before the next paint.
//
// The composer cannot resize itself: the rows it takes come out of the
// thread's budget, and only the host knows what the rest of the screen is
// doing. Emitting rather than assuming is what keeps the two from disagreeing
// about where the composer starts.
type ResizedMsg struct{}

// MentionQueryMsg asks the host for the members an @ could mean (issue #41).
//
// The composer emits one when completion opens — with an empty Query, which a
// supergroup answers with its recent members — and again every time the
// query changes. Anchor is the rune offset of the @ in the draft, and Gen
// is this query's generation: no two queries share one, for the life of the
// composer.
//
// The answer is a MentionResultsMsg carrying all four back. A host that
// debounces may skip a query outright, since a newer one supersedes it.
type MentionQueryMsg struct {
	ChatID int64
	Anchor int
	Query  string
	Gen    uint64
}

// MentionResultsMsg answers a MentionQueryMsg: the members the search found,
// or why it failed (issue #41). ChatID, Anchor, Query and Gen are the
// query's, copied back.
//
// The composer applies it only while all four still describe the completion
// on screen, and drops it silently otherwise — so an answer that arrives
// late can neither overwrite a newer query's nor reopen a picker that has
// closed. An Err keeps whatever the picker already had to offer, and says
// the search failed.
type MentionResultsMsg struct {
	ChatID int64
	Anchor int
	Query  string
	Gen    uint64
	Users  []*telegram.User
	Err    error
}
