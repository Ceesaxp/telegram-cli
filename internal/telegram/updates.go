package telegram

// Tea messages produced from Telegram updates.
// These are sent into the bubbletea program via p.Send().

// AuthStateMsg carries authorization state changes.
type AuthStateMsg struct {
	State AuthState
}

// NewMessageMsg is sent when a new message arrives.
type NewMessageMsg struct {
	Message *Message
}

// MessageEditedMsg is sent when a message is edited.
type MessageEditedMsg struct {
	ChatId    int64
	MessageId int64
}

// MessageDeletedMsg is sent when messages are deleted.
// ChatId is 0 for non-channel deletions (the update carries no peer).
type MessageDeletedMsg struct {
	ChatId     int64
	MessageIds []int64
}

// ChatUpdateMsg is sent when chat metadata changes (title, photo, etc)
// or when a chat is loaded from the dialog list.
type ChatUpdateMsg struct {
	Chat *Chat

	// FromPeer says this chat was built by resolving a PEER rather than by
	// reading a dialog, and so is a partial view: it knows who the chat is
	// and whether it is muted, and nothing about unread counts, pinning or
	// the last message.
	//
	// The store has to be told, because it cannot tell from the value: a
	// chat that is not pinned and a chat whose pin nobody asked about are
	// the same struct. Storing one of these as though it were complete is
	// what unmuted every chat the reader opened.
	FromPeer bool
}

// ChatLastMessageMsg is sent when a chat's last message changes.
type ChatLastMessageMsg struct {
	ChatId      int64
	LastMessage *Message
}

// ChatReadInboxMsg is sent when the read inbox state changes.
type ChatReadInboxMsg struct {
	ChatId                 int64
	LastReadInboxMessageId int64
	UnreadCount            int32
}

// ChatReadOutboxMsg is sent when the read outbox state changes.
type ChatReadOutboxMsg struct {
	ChatId                  int64
	LastReadOutboxMessageId int64
}

// ChatMuteChangedMsg is sent when a chat's notification settings change.
type ChatMuteChangedMsg struct {
	ChatId int64
	Muted  bool
}

// ChatFoldersMsg carries the current chat folder list, in server order.
type ChatFoldersMsg struct {
	Folders []*ChatFolder
}

// ClientErrorMsg reports that the Telegram client itself failed, as
// opposed to a single RPC. Terminal is true when the run loop has exited
// for good, meaning nothing will arrive until the app reconnects — the
// session being terminated from another device looks like this.
type ClientErrorMsg struct {
	Err      error
	Terminal bool
}

// ClientWarningMsg reports a permanent, non-fatal degradation of the
// current run. The client keeps working, but with less than its usual
// capability, and the user may want to know why.
type ClientWarningMsg struct {
	Text string
}

// FileUpdateMsg is sent when a file download completes.
type FileUpdateMsg struct {
	File *File
}

// ChatActionMsg is sent when someone is typing or performing an action.
type ChatActionMsg struct {
	ChatId int64
	UserId int64
	Action ChatAction
}

// ConnectionStateMsg is sent when the network connection state changes.
type ConnectionStateMsg struct {
	State ConnectionState
}

// MessageSendSucceededMsg is sent when a message is successfully sent.
type MessageSendSucceededMsg struct {
	Message      *Message
	OldMessageId int64
}

// MessageSendFailedMsg is sent when a message fails to send.
type MessageSendFailedMsg struct {
	Message      *Message
	OldMessageId int64
	ErrorCode    int32
	ErrorMessage string
}
