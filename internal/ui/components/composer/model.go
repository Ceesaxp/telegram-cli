package composer

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/Ceesaxp/telegram-cli/internal/ui/widgets"
	"github.com/charmbracelet/lipgloss"
)

// Mode represents the composer's current mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeReply
	ModeEdit
)

// Notices shown on the composer's hint line.
const (
	// noticeNoChat is shown when the composer is used before a chat is open.
	noticeNoChat = "⚠ open a chat first"
	// noticeEditDiscard is shown when entering edit mode drops an attachment.
	noticeEditDiscard = "⚠ attachment discarded — editing"
	// noticeNoEditor is shown when ctrl+o has no editor to launch.
	noticeNoEditor = "⚠ no $EDITOR set"
)

// Model is the message composer component.
type Model struct {
	textarea   widgets.TextArea
	width      int
	height     int
	focused    bool
	mode       Mode
	chatID     int64
	replyToID  int64
	editMsgID  int64
	replyText  string
	attachment string
	asPhoto    bool
	notice     string

	// editing selects the line-editing keymap (emacs or vi); vi/viPending
	// hold the modal state that only ModeVi uses. See editing.go.
	editing   EditingMode
	vi        viState
	viPending rune

	// roles is the TUI 2.0 semantic palette. New installs a default, so a
	// Model that never has SetRoles called still renders — this component's
	// own tests construct it directly.
	roles theme.Roles

	// appMode is what the badge reports, set by the host from its own
	// resolver. It is display only: nothing in this package reads it to
	// decide what a key does.
	appMode AppMode

	// parseMarkdown mirrors ui.parse_markdown. It decides the "md" label
	// and whether the expanded preview parses or shows the text verbatim.
	parseMarkdown bool

	// expanded is the eight-row split source-and-preview form, toggled with
	// ctrl+p.
	expanded bool

	// drafts holds each other chat's unsent work (decision 13). Held as a
	// map so the value copies bubbletea makes of this model all share one,
	// the same reason chatview holds its render cache by pointer.
	drafts map[int64]draft

	// editParked holds the draft that entering edit mode displaced, per
	// chat (decision I-3). A map for the same two reasons drafts is one:
	// the value copies bubbletea makes all share it, and switching chats
	// mid-edit parks the edit itself into drafts, so what it displaced has
	// to travel with it rather than be dropped.
	//
	// Presence is what matters, not emptiness. An entry recording "there
	// was nothing here" is how cancelling an edit clears the message text
	// the edit loaded, instead of leaving it in the composer as a draft
	// nobody wrote.
	editParked map[int64]draft
}

// New creates a new composer model.
func New(r theme.Roles) Model {
	ta := widgets.NewTextArea()
	ta.Placeholder = "Type a message..."
	ta.Style = lipgloss.NewStyle().Foreground(r.Fg).Background(r.Panel)
	ta.StylePlaceholder = lipgloss.NewStyle().Foreground(r.Dim).Background(r.Panel)
	// A message can hold line breaks (ctrl+j / shift+enter, vi o/O). This is
	// declared here rather than inferred from Height so it holds before the
	// first WindowSizeMsg and on a terminal too short to give the composer
	// more than one row — a draft must never lose its newlines to a
	// transient layout number.
	ta.MultiLine = true

	return Model{
		textarea: ta,
		height:   3,
		// A placeholder width until the first WindowSizeMsg. Every row this
		// component draws is cut to its width, and cutting to zero would
		// make a composer that has not been laid out yet render nothing at
		// all — including the draft somebody has already pasted into it.
		width:      40,
		roles:      r,
		drafts:     make(map[int64]draft),
		editParked: make(map[int64]draft),
	}
}

// SetSize sets the component dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.textarea.Width = width - 2
	// Height is the composer pane's row budget minus the hint line. It only
	// sizes the textarea's scroll window (MultiLine, set in New, is what
	// makes the buffer multi-line), but a window of zero rows renders
	// nothing at all, so keep at least one.
	if h := height - 1; h > 1 {
		m.textarea.Height = h
	} else {
		m.textarea.Height = 1
	}
}

// SetFocused sets focus state.
// SetFocused moves the keyboard into or out of the composer.
//
// Arriving in the composer puts vi mode back into INSERT. The reason is the
// one SetEditingMode already gives for starting there: typing a message is
// the common case, and a normal-mode landing swallows the first word — but
// the state was only initialised once, so the composer remembered whatever
// mode it was left in.
//
// A vi user leaves the composer with two escapes: the first goes to normal
// mode, the second gives the panel back. So the composer was ALWAYS in
// normal mode by the time it was next entered, and r, e and i each landed
// there. Pressing r and typing "abc" produced "bc" — the "a" was vi's
// append. ctrl+j did not insert a newline for the same reason, deliberately
// (see isNewlineChord); ctrl+o worked because it is handled before the modal
// dispatch, which is why the external editor looked like the only way in.
//
// Only on the transition, so a setFocus while already focused cannot pull a
// vi user out of normal mode mid-command.
func (m *Model) SetFocused(focused bool) {
	if focused && !m.focused {
		m.vi = viInsert
		m.viPending = 0
	}
	m.focused = focused
	m.textarea.Focused = focused
}

// EnterReplyMode starts replying to a message.
func (m *Model) EnterReplyMode(messageID int64, previewText string) {
	m.mode = ModeReply
	m.replyToID = messageID
	m.replyText = previewText
}

// EnterEditMode starts editing a message. An edit cannot carry media, so any
// pending attachment is discarded and its path returned for the caller to
// delete.
//
// The draft on screen is PARKED first (decision I-3): loading the message
// text over it used to destroy whatever was half-written, without a confirm
// and without a way back. Cancelling the edit or sending it puts the draft
// back — see unparkEdit.
func (m *Model) EnterEditMode(messageID int64, currentText string) string {
	// Only the first e parks. A second one, pressed while already editing,
	// would otherwise park the message text of the first edit as if it
	// were the user's own draft.
	if m.mode != ModeEdit {
		m.parkEdit()
	}
	discarded := m.attachment
	m.attachment = ""
	m.asPhoto = false
	m.mode = ModeEdit
	m.editMsgID = messageID
	m.textarea.Value = currentText
	m.textarea.Cursor = len([]rune(currentText))
	if discarded != "" {
		m.notice = noticeEditDiscard
	}
	return discarded
}

// parkEdit stores what an edit is about to displace: the text, where the
// cursor was in it, and the reply target it was going to answer.
//
// Unconditionally, even when there is nothing to store, because presence is
// what unparkEdit reads. The attachment is deliberately not carried: an edit
// discards it and hands the spool path back to be deleted, so there is
// nothing left to restore.
func (m *Model) parkEdit() {
	if m.chatID == 0 {
		return
	}
	m.editParked[m.chatID] = draft{
		text:      m.textarea.Value,
		cursor:    m.textarea.Cursor,
		mode:      m.mode,
		replyToID: m.replyToID,
		replyText: m.replyText,
	}
}

// unparkEdit puts back the draft an edit displaced, and reports whether it
// found one. The entry is consumed: the draft is live in the composer again.
//
// A parked draft that was empty still restores — as an empty composer. That
// is the point: without it, cancelling an edit would leave the message's own
// text sitting there looking like something the user typed.
func (m *Model) unparkEdit() bool {
	d, ok := m.editParked[m.chatID]
	if !ok {
		return false
	}
	delete(m.editParked, m.chatID)

	m.textarea.Reset()
	m.textarea.Value = d.text
	m.textarea.Cursor = min(max(d.cursor, 0), len([]rune(d.text)))
	m.mode = d.mode
	m.replyToID = d.replyToID
	m.editMsgID = 0
	m.replyText = d.replyText
	m.attachment = ""
	m.asPhoto = false
	m.notice = ""
	return true
}

// clearContext drops everything ABOUT the message being written — the mode,
// the reply and edit targets, the pending attachment, the notice — and keeps
// the text itself.
//
// It is the Escape rung's primitive (decision I-3). Escape steps back one
// rung per press and never discards: cancelling a reply keeps the words that
// were going to be the reply, because they are just as usable as a message
// of their own. Reset is this plus the text, and belongs to submit, where
// the text has been sent and is gone on purpose.
func (m *Model) clearContext() {
	m.mode = ModeNormal
	m.replyToID = 0
	m.editMsgID = 0
	m.replyText = ""
	m.attachment = ""
	m.asPhoto = false
	m.notice = ""
}

// Reset clears the composer state, text included.
func (m *Model) Reset() {
	m.textarea.Reset()
	m.clearContext()
	// A cleared composer is ready to be typed into; vi's normal mode is
	// restored explicitly by the Escape-cancel path, which is the only
	// place where staying in normal mode is the right answer.
	m.vi = viInsert
	m.viPending = 0
}

// Update handles messages.
//
// Key dispatch matches on tea.KeyPressMsg.Keystroke() for every chord, and on
// String() only for the unmodified printables that drive vi's normal mode.
// String() returns Key.Text whenever the terminal attached any: a
// Kitty-protocol shift+enter arrives as CSI 13;2;13u, so String() is "\r"
// while Keystroke() is "shift+enter". Matching the newline chord on String()
// would have made it invisible in exactly the terminals that can report it.
// See the keyPress doc comment in internal/app/keymap.go.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	// The external editor's result is handled before the focus guard: it
	// arrives after the program resumed from suspension, and dropping it
	// would lose the user's edits and leak the temp file.
	if fin, ok := msg.(editorFinishedMsg); ok {
		m.applyEditorResult(fin)
		return m, nil
	}

	if !m.focused {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		m.textarea.Update(msg)
	}

	return m, nil
}

// handleKey routes one key press. The chords handled here are the ones that
// mean the same thing in both editing keymaps; everything else goes to the vi
// normal-mode handler or straight to the textarea's emacs/insert handling.
func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	stroke := msg.Keystroke()

	switch stroke {
	case "esc":
		return m.handleEsc()
	case "ctrl+t":
		m.notice = ""
		return m, func() tea.Msg { return AttachRequestedMsg{} }
	case "ctrl+v":
		if m.chatID == 0 {
			m.notice = noticeNoChat
			return m, nil
		}
		m.notice = "pasting from clipboard..."
		return m, func() tea.Msg { return PasteRequestedMsg{} }
	case "ctrl+o":
		return m, m.editInEditor()
	case "ctrl+p":
		// The inline/expanded toggle. Non-destructive by construction: it
		// changes how the same draft is drawn and touches nothing else, so
		// there is no state to lose by looking and no reason to hesitate
		// before pressing it.
		m.notice = ""
		m.expanded = !m.expanded
		return m, func() tea.Msg { return ResizedMsg{} }
	case "enter":
		return m.submit()
	}

	if m.isNewlineChord(stroke) {
		m.notice = ""
		m.textarea.InsertNewline()
		return m, nil
	}

	if m.editing == ModeVi && m.vi == viNormal {
		return m.handleViNormal(msg)
	}

	m.notice = ""
	m.textarea.Update(msg)
	return m, nil
}

// handleEsc implements the composer's Escape semantics.
//
// In emacs mode (and in vi's normal mode) Escape clears reply/edit state and
// any pending attachment, handing the spool path back to the app so the file
// is deleted; with nothing to clear it falls through and app.go moves focus
// out of the panel.
//
// What it never does is take the text (decision I-3). One press steps back
// one rung, and the words survive every rung — work is only lost through a
// key that says so.
//
// In vi mode the first Escape only leaves insert mode. The cancel path above
// is then reached by pressing Escape *again* from normal mode, so every
// invariant built around cancel — attachment discard included — survives, it
// just costs one extra keystroke. Model.IsComposing reports true while vi is
// in insert mode so that app.go forwards that first Escape here instead of
// consuming it to move focus.
func (m Model) handleEsc() (Model, tea.Cmd) {
	if m.editing == ModeVi && m.vi == viInsert {
		m.vi = viNormal
		m.viPending = 0
		m.notice = ""
		// Insert mode leaves the cursor in the gap after the character it
		// just typed; normal mode sits on a character. See viClampCursor.
		m.viClampCursor()
		return m, nil
	}

	if m.mode != ModeNormal || m.attachment != "" {
		discarded := m.attachment
		// The text is kept, whatever was cancelled (decision I-3). Reset
		// used to be the only cancel primitive, so a reply target and a
		// half-written reply went together; they are two things, and only
		// one of them was asked about.
		//
		// An edit is the exception in shape but not in rule: what is on
		// screen is the message being edited, not something the user
		// typed, so cancelling puts back the draft the edit displaced —
		// with its own reply target, if it had one.
		if m.mode != ModeEdit || !m.unparkEdit() {
			m.clearContext()
		}
		if discarded != "" {
			// The app owns the spool file — tell it the
			// attachment is gone so it can delete it.
			return m, func() tea.Msg {
				return AttachmentDiscardedMsg{Path: discarded}
			}
		}
		return m, nil
	}
	return m, nil
}

// submit sends the draft. Enter means "send" in every editing mode, including
// vi's normal mode — a line break is ctrl+j / shift+enter (or vi o/O).
func (m Model) submit() (Model, tea.Cmd) {
	if m.chatID == 0 {
		m.notice = noticeNoChat
		return m, nil
	}
	if m.textarea.Value == "" && m.attachment == "" {
		return m, nil
	}

	submitted := MessageSubmittedMsg{
		ChatId:     m.chatID,
		Text:       m.textarea.Value,
		Attachment: m.attachment,
		AsPhoto:    m.asPhoto,
	}
	wasEdit := m.mode == ModeEdit
	switch m.mode {
	case ModeReply:
		submitted.ReplyToId = m.replyToID
	case ModeEdit:
		submitted.EditMessageId = m.editMsgID
	}

	m.Reset()
	// Sending the edit finishes it, so the draft it displaced comes back
	// (decision I-3) — the same restoration cancelling would have done. An
	// edit is an interruption of the message someone was writing, not a
	// replacement for it.
	if wasEdit {
		m.unparkEdit()
	}
	return m, func() tea.Msg { return submitted }
}

// SetAttachment sets the pending attachment path shown above the composer.
// asPhoto requests that an image be sent inline rather than as a document.
// It returns the attachment it replaced (empty when there was none) so the
// caller can clean up a spool file that is no longer referenced.
func (m *Model) SetAttachment(path string, asPhoto bool) string {
	previous := m.attachment
	m.attachment = path
	m.asPhoto = asPhoto
	m.notice = ""
	return previous
}

// Attachment returns the pending attachment path, empty when there is none.
func (m Model) Attachment() string { return m.attachment }

// ChatId returns the chat the composer is currently sending to, 0 when none.
func (m Model) ChatId() int64 { return m.chatID }

// Draft is the text currently in the composer, for a host that needs to see
// what was typed rather than only whether anything was.
func (m Model) Draft() string { return m.textarea.Value }

// HasDraft reports whether the composer holds unsent text.
//
// It exists for app.go's quit-confirm rule: "q" from a browsing panel quits
// outright, unless there is work in the composer to lose, in which case it
// asks first. Whitespace alone is not work, so it is trimmed.
//
// Not replaceable by IsComposing: that answers a different question (does
// Escape belong to the composer) and is true for reply/edit mode and, in vi
// mode, for insert mode — regardless of whether anything has been typed.
func (m Model) HasDraft() bool { return strings.TrimSpace(m.textarea.Value) != "" }

// IsEditing reports whether the composer is editing an existing message.
// Attachments cannot be added to an edit.
func (m Model) IsEditing() bool { return m.mode == ModeEdit }

// IsComposing reports whether Escape belongs to the composer rather than to
// app.go's focus-back handler: reply/edit mode or a pending attachment needs
// clearing first, and in vi mode an Escape pressed in insert mode has to
// reach the composer so it can switch to normal mode.
func (m Model) IsComposing() bool {
	if m.editing == ModeVi && m.vi == viInsert {
		return true
	}
	return m.mode != ModeNormal || m.attachment != ""
}

// SetNotice shows a transient message on the composer's hint line.
func (m *Model) SetNotice(notice string) {
	m.notice = notice
}
