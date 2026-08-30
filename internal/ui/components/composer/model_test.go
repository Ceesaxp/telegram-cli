package composer

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

func newFocused() Model {
	m := New(theme.ForName("dark"))
	m.SetSize(60, 3)
	m.SetFocused(true)
	m.SetChatId(42)
	return m
}

// newFocusedNoChat is the state reachable with Tab before any chat has been
// selected: the composer has focus but no chat to send to.
func newFocusedNoChat() Model {
	m := New(theme.ForName("dark"))
	m.SetSize(60, 3)
	m.SetFocused(true)
	return m
}

func press(t *testing.T, m Model, key string) (Model, tea.Msg) {
	t.Helper()
	m, cmd := m.Update(tea.KeyPressMsg{Code: keyCode(key), Mod: keyMod(key)})
	if cmd == nil {
		return m, nil
	}
	return m, cmd()
}

func keyCode(key string) rune {
	switch key {
	case "ctrl+v":
		return 'v'
	case "enter":
		return tea.KeyEnter
	case "esc":
		return tea.KeyEscape
	}
	return rune(key[0])
}

func keyMod(key string) tea.KeyMod {
	if len(key) > 5 && key[:5] == "ctrl+" {
		return tea.ModCtrl
	}
	return 0
}

func TestCtrlVRequestsPaste(t *testing.T) {
	m := newFocused()
	_, msg := press(t, m, "ctrl+v")
	if _, ok := msg.(PasteRequestedMsg); !ok {
		t.Fatalf("got %T, want PasteRequestedMsg", msg)
	}
}

func TestSubmitCarriesPhotoAttachment(t *testing.T) {
	m := newFocused()
	m.SetAttachment("/tmp/paste-1.png", true)

	_, msg := press(t, m, "enter")
	submitted, ok := msg.(MessageSubmittedMsg)
	if !ok {
		t.Fatalf("got %T, want MessageSubmittedMsg", msg)
	}
	if submitted.Attachment != "/tmp/paste-1.png" {
		t.Errorf("Attachment = %q, want /tmp/paste-1.png", submitted.Attachment)
	}
	if !submitted.AsPhoto {
		t.Error("AsPhoto = false, want true")
	}
	if submitted.ChatId != 42 {
		t.Errorf("ChatId = %d, want 42", submitted.ChatId)
	}
}

// A pasted attachment with no text still sends, and the composer clears
// afterwards so the next message does not re-send it.
func TestSubmitResetsAttachment(t *testing.T) {
	m := newFocused()
	m.SetAttachment("/tmp/paste-1.png", true)

	m, msg := press(t, m, "enter")
	if _, ok := msg.(MessageSubmittedMsg); !ok {
		t.Fatalf("got %T, want MessageSubmittedMsg", msg)
	}

	_, msg = press(t, m, "enter")
	if msg != nil {
		t.Fatalf("second Enter produced %T, want nothing to send", msg)
	}
}

func TestSetNoticeReplacesHint(t *testing.T) {
	m := newFocused()
	m.SetNotice("⚠ no image in clipboard")
	if view := m.View(); !strings.Contains(view, "no image in clipboard") {
		t.Errorf("notice missing from view:\n%s", view)
	}
	// Attaching something clears the notice again.
	m.SetAttachment("/tmp/paste-1.png", true)
	if view := m.View(); strings.Contains(view, "no image in clipboard") {
		t.Errorf("notice survived SetAttachment:\n%s", view)
	}
}

// Bubble Tea v2 reports the Escape key as "esc"; the composer must match that
// name or every Escape handler below it is dead code.
func TestEscapeKeyName(t *testing.T) {
	if got := (tea.KeyPressMsg{Code: tea.KeyEscape}).String(); got != "esc" {
		t.Fatalf("Escape key renders as %q, want \"esc\"", got)
	}
}

func TestEscExitsReplyMode(t *testing.T) {
	m := newFocused()
	m.EnterReplyMode(7, "hello")

	m, msg := press(t, m, "esc")
	if msg != nil {
		t.Fatalf("got %T, want no message", msg)
	}
	if m.IsComposing() {
		t.Error("still composing after Esc, want reply mode cleared")
	}

	// A reply that was cancelled must not resurface on the next submit.
	m, _ = press(t, m, "x")
	_, msg = press(t, m, "enter")
	submitted, ok := msg.(MessageSubmittedMsg)
	if !ok {
		t.Fatalf("got %T, want MessageSubmittedMsg", msg)
	}
	if submitted.ReplyToId != 0 {
		t.Errorf("ReplyToId = %d, want 0", submitted.ReplyToId)
	}
}

func TestEscDiscardsAttachment(t *testing.T) {
	m := newFocused()
	m.SetAttachment("/tmp/paste-1.png", true)

	m, msg := press(t, m, "esc")
	discarded, ok := msg.(AttachmentDiscardedMsg)
	if !ok {
		t.Fatalf("got %T, want AttachmentDiscardedMsg", msg)
	}
	if discarded.Path != "/tmp/paste-1.png" {
		t.Errorf("Path = %q, want /tmp/paste-1.png", discarded.Path)
	}
	if m.Attachment() != "" {
		t.Errorf("Attachment = %q, want it cleared", m.Attachment())
	}
}

func TestSetAttachmentReturnsPrevious(t *testing.T) {
	m := newFocused()

	if prev := m.SetAttachment("/tmp/paste-1.png", true); prev != "" {
		t.Errorf("first SetAttachment returned %q, want empty", prev)
	}
	prev := m.SetAttachment("/tmp/paste-2.png", true)
	if prev != "/tmp/paste-1.png" {
		t.Errorf("SetAttachment returned %q, want /tmp/paste-1.png", prev)
	}
	if m.Attachment() != "/tmp/paste-2.png" {
		t.Errorf("Attachment = %q, want /tmp/paste-2.png", m.Attachment())
	}
}

// Ctrl+V before a chat is open has nothing to attach to: it must warn instead
// of spooling a file that can never be sent.
func TestCtrlVWithoutChatIsGuarded(t *testing.T) {
	m := newFocusedNoChat()

	m, msg := press(t, m, "ctrl+v")
	if msg != nil {
		t.Fatalf("got %T, want no message", msg)
	}
	if view := m.View(); !strings.Contains(view, "open a chat first") {
		t.Errorf("guard notice missing from view:\n%s", view)
	}
}

// Enter before a chat is open would submit ChatId 0, which fails deep inside
// the Telegram client instead of at the keystroke.
func TestSubmitWithoutChatIsGuarded(t *testing.T) {
	m := newFocusedNoChat()
	m.SetAttachment("/tmp/paste-1.png", true)
	m, _ = press(t, m, "x")

	m, msg := press(t, m, "enter")
	if msg != nil {
		t.Fatalf("got %T, want no message", msg)
	}
	if view := m.View(); !strings.Contains(view, "open a chat first") {
		t.Errorf("guard notice missing from view:\n%s", view)
	}
	if m.Attachment() != "/tmp/paste-1.png" {
		t.Errorf("Attachment = %q, want it kept for when a chat opens", m.Attachment())
	}
}

func TestIsEditing(t *testing.T) {
	m := newFocused()
	if m.IsEditing() {
		t.Error("IsEditing = true for a fresh composer")
	}
	m.EnterReplyMode(7, "hello")
	if m.IsEditing() {
		t.Error("IsEditing = true in reply mode")
	}
	m.EnterEditMode(7, "hello")
	if !m.IsEditing() {
		t.Error("IsEditing = false in edit mode")
	}
}

// An edit carries text only: entering edit mode with an attachment pending
// must hand the path back so the spool file is deleted rather than orphaned.
func TestEnterEditModeDiscardsAttachment(t *testing.T) {
	m := newFocused()
	m.SetAttachment("/tmp/paste-1.png", true)

	discarded := m.EnterEditMode(7, "hello")
	if discarded != "/tmp/paste-1.png" {
		t.Errorf("EnterEditMode returned %q, want /tmp/paste-1.png", discarded)
	}
	if m.Attachment() != "" {
		t.Errorf("Attachment = %q, want it cleared", m.Attachment())
	}
	if !m.IsEditing() {
		t.Error("IsEditing = false after EnterEditMode")
	}
	if view := m.View(); !strings.Contains(view, "attachment discarded") {
		t.Errorf("discard notice missing from view:\n%s", view)
	}

	// The discarded file must not ride along on the edit submit.
	_, msg := press(t, m, "enter")
	submitted, ok := msg.(MessageSubmittedMsg)
	if !ok {
		t.Fatalf("got %T, want MessageSubmittedMsg", msg)
	}
	if submitted.Attachment != "" {
		t.Errorf("Attachment = %q, want empty on an edit", submitted.Attachment)
	}
}

// Entering edit mode with nothing pending has nothing to discard.
func TestEnterEditModeWithoutAttachment(t *testing.T) {
	m := newFocused()
	if discarded := m.EnterEditMode(7, "hello"); discarded != "" {
		t.Errorf("EnterEditMode returned %q, want empty", discarded)
	}
}

// Switching chats parks the draft rather than discarding it (decision 13).
//
// This reverses the old behaviour, which threw the draft and any staged
// attachment away and handed the path back for cleanup. It made the chat
// list unusable half-way through a message: checking who else had written
// cost you what you had typed.
func TestSetChatIdParksTheDraft(t *testing.T) {
	m := newFocused()
	m.SetChatId(42)
	m.textarea.Value = "half a thought"
	m.textarea.Cursor = m.textarea.Len()
	m.SetAttachment("/tmp/paste-1.png", true)

	if discarded := m.SetChatId(43); discarded != "" {
		t.Errorf("SetChatId discarded %q; drafts are parked now", discarded)
	}
	if m.ChatId() != 43 {
		t.Errorf("ChatId = %d, want 43", m.ChatId())
	}
	if m.textarea.Value != "" || m.Attachment() != "" {
		t.Errorf("the new chat inherited the old chat's draft: %q / %q",
			m.textarea.Value, m.Attachment())
	}

	m.SetChatId(42)
	if m.textarea.Value != "half a thought" {
		t.Errorf("draft came back as %q", m.textarea.Value)
	}
	if m.Attachment() != "/tmp/paste-1.png" {
		t.Errorf("attachment came back as %q", m.Attachment())
	}
	if m.textarea.Cursor != len("half a thought") {
		t.Errorf("cursor came back at %d, want %d", m.textarea.Cursor, len("half a thought"))
	}
}

// A reply target is part of the draft. Restoring the words without it would
// send them to the right chat and the wrong message.
func TestParkedDraftKeepsItsReplyTarget(t *testing.T) {
	m := newFocused()
	m.SetChatId(42)
	m.EnterReplyMode(7, "nadia: rebased")
	m.textarea.Value = "on it"

	m.SetChatId(43)
	if m.mode != ModeNormal || m.replyToID != 0 {
		t.Fatalf("reply state leaked into the new chat: mode=%v replyTo=%d", m.mode, m.replyToID)
	}

	m.SetChatId(42)
	if m.mode != ModeReply || m.replyToID != 7 {
		t.Errorf("reply target lost: mode=%v replyTo=%d", m.mode, m.replyToID)
	}
	if m.replyText != "nadia: rebased" {
		t.Errorf("reply preview = %q", m.replyText)
	}
}

// A sent or cleared draft is gone: parking it again on the next chat switch
// would resurrect what the user just got rid of.
func TestResetForgetsTheParkedDraft(t *testing.T) {
	m := newFocused()
	m.SetChatId(42)
	m.textarea.Value = "typed"
	m.SetChatId(43)
	m.SetChatId(42)
	if m.textarea.Value != "typed" {
		t.Fatalf("precondition: draft was not parked, got %q", m.textarea.Value)
	}

	m.Reset()
	m.SetChatId(43)
	m.SetChatId(42)
	if m.textarea.Value != "" {
		t.Errorf("a cleared draft came back as %q", m.textarea.Value)
	}
}

// HasDraftFor is what the chat list reads. The OPEN chat never reports one:
// its draft is already on screen in the composer, and saying "draft: saved
// locally" about the thing you are looking at costs the message preview and
// tells the reader nothing.
func TestHasDraftForExcludesTheOpenChat(t *testing.T) {
	m := newFocused()
	m.SetChatId(42)
	m.textarea.Value = "parked"
	m.SetChatId(43)

	if !m.HasDraftFor(42) {
		t.Error("chat 42 holds a draft and does not report one")
	}
	if m.HasDraftFor(43) {
		t.Error("the open chat reports a draft")
	}
	if m.HasDraftFor(99) {
		t.Error("a chat with no draft reports one")
	}

	m.SetChatId(42)
	if m.HasDraftFor(42) {
		t.Error("the now-open chat still reports a parked draft")
	}
	if got := m.DraftChats(); len(got) != 0 {
		t.Errorf("DraftChats = %v, want empty once the only draft is open", got)
	}
}

// Whitespace is not work, and neither is an empty composer: neither parks.
func TestEmptyDraftsAreNotParked(t *testing.T) {
	m := newFocused()
	m.SetChatId(42)
	m.textarea.Value = "   \n  "
	m.SetChatId(43)
	if m.HasDraftFor(42) {
		t.Error("whitespace was parked as a draft")
	}
}

// TestHasDraft covers the accessor app.go's quit-confirm rule reads: only
// actual typed text counts as work to lose. Whitespace does not, and neither
// does a mode change on its own — which is why IsComposing cannot stand in
// for it.
func TestHasDraft(t *testing.T) {
	m := newFocused()
	if m.HasDraft() {
		t.Error("a fresh composer reports a draft")
	}

	m, _ = press(t, m, "a")
	if !m.HasDraft() {
		t.Error("a typed character is not reported as a draft")
	}

	m.Reset()
	if m.HasDraft() {
		t.Error("Reset left a draft behind")
	}

	// Whitespace alone is not work: it must not raise a confirm dialog.
	m.textarea.Value = "   \n\t "
	if m.HasDraft() {
		t.Error("whitespace-only text reports a draft")
	}

	// Reply mode and a pending attachment both make IsComposing true while
	// the text stays empty. HasDraft answers only for the text.
	m.textarea.Value = ""
	m.EnterReplyMode(7, "preview")
	if m.HasDraft() {
		t.Error("reply mode alone reports a draft")
	}
	if !m.IsComposing() {
		t.Error("precondition: reply mode should be composing")
	}

	m.Reset()
	m.SetAttachment("/tmp/x.png", true)
	if m.HasDraft() {
		t.Error("a pending attachment alone reports a draft")
	}
}
