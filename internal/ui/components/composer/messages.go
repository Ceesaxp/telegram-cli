package composer

// MessageSubmittedMsg is emitted when the user submits a message.
type MessageSubmittedMsg struct {
	ChatId        int64
	Text          string
	ReplyToId     int64
	EditMessageId int64
	Attachment    string // local file path, empty if none
	AsPhoto       bool   // send the attachment as an inline photo, not a document
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
