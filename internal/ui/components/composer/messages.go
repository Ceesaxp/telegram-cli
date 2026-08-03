package composer

// MessageSubmittedMsg is emitted when the user submits a message.
type MessageSubmittedMsg struct {
	ChatId        int64
	Text          string
	ReplyToId     int64
	EditMessageId int64
	Attachment    string // local file path, empty if none
}

// AttachRequestedMsg is emitted when the user asks to attach a file (Ctrl+T).
type AttachRequestedMsg struct{}
