package app

// FocusChangedMsg signals a focus panel change.
type FocusChangedMsg struct {
	Panel FocusPanel
}

// ErrorMsg carries an error to display.
type ErrorMsg struct {
	Err error
}

// AuthStateChangedMsg is sent from the authorizer callback.
type AuthStateChangedMsg struct {
	State int
	Hint  string
}

// AuthErrorMsg is sent when the Telegram client fails during authentication.
type AuthErrorMsg struct {
	Err error
}

// AuthenticatedMsg signals that authentication is complete.
type AuthenticatedMsg struct {
	UserId    int64
	FirstName string
	LastName  string
}

// ClipboardPastedMsg carries a clipboard image that has been spooled to disk
// and is ready to attach.
type ClipboardPastedMsg struct {
	// ChatId is the chat that was active when the paste was requested. If
	// the active chat has changed by the time this message arrives, the
	// paste is discarded rather than installed into the wrong chat.
	ChatId  int64
	Path    string
	IsImage bool
}

// ClipboardPasteFailedMsg reports why a clipboard paste produced nothing.
type ClipboardPasteFailedMsg struct {
	Err error
}

// SendFailedMsg reports a send that failed after the composer was already
// reset. It carries the attachment back so it can be restored for a retry
// instead of being lost with the spool file.
type SendFailedMsg struct {
	Err        error
	ChatId     int64 // chat the send was for; restore only into that composer
	Attachment string
	AsPhoto    bool
}
