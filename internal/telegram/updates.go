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

// UploadProgressMsg reports how far an eager attachment upload has got, so
// the composer's attachment chip can show it. Path is the key the composer
// knows the file by — the same path it was attached from.
//
// Failed says the upload ended without producing anything. It carries no
// numbers: the chip drops the percentage rather than freezing it, because
// the send that follows will upload the file again itself and report its
// own failure if that fails too.
//
// Generation says which attempt is reporting. The same path can be uploaded
// twice — attach, discard, attach the same file again — and the abandoned
// attempt's callbacks are still arriving when the new one starts; the path
// alone cannot tell them apart. It rises with every upload started, so the
// consumer can drop anything older than what it is already showing.
type UploadProgressMsg struct {
	Path       string
	Generation uint64
	Uploaded   int64
	Total      int64
	Failed     bool
}

// MessageSendSucceededMsg is sent when a message is successfully sent.
type MessageSendSucceededMsg struct {
	Message      *Message
	OldMessageId int64
}

// MessageSendFailedMsg reports that an outgoing message never reached the
// server. It names the local placeholder the sender echoed into the thread
// so that row can be marked failed instead of sitting there pending
// forever, which would be the client telling a lie it never takes back.
//
// It lives here, next to the success message, because both the app (which
// surfaces the error text) and the thread (which marks the row) have to see
// it, and the telegram package is the one they both already import.
type MessageSendFailedMsg struct {
	ChatId       int64
	OldMessageId int64
	Err          error
}
