package composer

import "strings"

// draft is one chat's unsent work: what was typed, where the cursor was, and
// what the message was going to be — a reply, an edit, or a new message with
// something attached.
//
// All of it, not just the text. A draft that came back without its reply
// target would send the words to the right chat and the wrong message, which
// is worse than losing them.
type draft struct {
	text       string
	cursor     int
	mode       Mode
	replyToID  int64
	editMsgID  int64
	replyText  string
	attachment string
	asPhoto    bool
}

func (d draft) empty() bool {
	return strings.TrimSpace(d.text) == "" && d.attachment == "" && d.mode == ModeNormal
}

// SetChatId switches the composer to another chat, parking the current
// chat's draft and restoring that chat's own (decision 13).
//
// It used to discard: the draft and any staged attachment went in the bin and
// the path came back for cleanup. That made the chat list unusable while
// half-way through a message — checking who else had written cost you what
// you had typed — so switching now costs nothing. Drafts live for the
// session only and are never synced to Telegram, so nothing here can
// surprise another client.
//
// The return value is the attachment path the caller should delete. It is
// now always empty, since nothing is displaced; the signature is unchanged
// so callers keep cleaning up after the paths that ARE dropped, by Escape
// and by entering edit mode.
func (m *Model) SetChatId(chatID int64) string {
	if chatID == m.chatID {
		return ""
	}
	m.parkDraft()
	m.chatID = chatID
	m.restoreDraft(chatID)
	return ""
}

// parkDraft stores the current chat's work, or forgets it when there is none
// left to store.
func (m *Model) parkDraft() {
	if m.chatID == 0 {
		return
	}
	d := draft{
		text:       m.textarea.Value,
		cursor:     m.textarea.Cursor,
		mode:       m.mode,
		replyToID:  m.replyToID,
		editMsgID:  m.editMsgID,
		replyText:  m.replyText,
		attachment: m.attachment,
		asPhoto:    m.asPhoto,
	}
	if d.empty() {
		delete(m.drafts, m.chatID)
		return
	}
	m.drafts[m.chatID] = d
}

// restoreDraft loads a chat's parked work, or clears the composer when it has
// none.
//
// The entry is CONSUMED, not copied: the map means "unsent work in chats that
// are not open", and the restored draft is now the live one in the composer.
// Leaving a copy behind would make the map disagree with itself the moment
// the draft was sent — and the chat list reads the map.
func (m *Model) restoreDraft(chatID int64) {
	d, ok := m.drafts[chatID]
	if !ok {
		m.Reset()
		return
	}
	delete(m.drafts, chatID)

	m.textarea.Reset()
	m.textarea.Value = d.text
	m.textarea.Cursor = min(max(d.cursor, 0), len([]rune(d.text)))
	m.mode = d.mode
	m.replyToID = d.replyToID
	m.editMsgID = d.editMsgID
	m.replyText = d.replyText
	m.attachment = d.attachment
	m.asPhoto = d.asPhoto
	m.notice = ""
	// A restored draft is ready to be typed into, the same as a cleared one.
	m.vi = viInsert
	m.viPending = 0
}

// HasDraftFor reports whether a chat has unsent work parked. The chat list
// shows it in the preview row.
//
// The open chat never has one: restoreDraft consumed its entry on the way in,
// because its draft is on screen in the composer rather than parked. Saying
// "draft: saved locally" about the thing you are looking at would tell the
// reader nothing and cost them the message preview.
func (m Model) HasDraftFor(chatID int64) bool {
	_, ok := m.drafts[chatID]
	return ok
}

// DraftChats is the set of chats holding parked drafts, for the chat list.
func (m Model) DraftChats() map[int64]bool {
	if len(m.drafts) == 0 {
		return nil
	}
	out := make(map[int64]bool, len(m.drafts))
	for id := range m.drafts {
		out[id] = true
	}
	return out
}
