package app

import (
	"strings"
	"testing"

	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// newTestModel builds an app.Model with no live Telegram client, suitable
// for exercising Update() paths that don't touch the network. It stays in
// package app (rather than app_test) because the assertions below need to
// read composer state through unexported fields.
func newTestModel(t *testing.T) Model {
	t.Helper()
	cfg := &config.Config{}
	s := store.NewStore()
	authorizer := telegram.NewTUIAuthorizer(cfg)
	var tg *telegram.Client // never dereferenced by the paths under test
	return New(cfg, tg, s, authorizer)
}

// TestClipboardPastedMsg_WrongChatDiscarded covers review finding M-1: a
// clipboard paste that lands after the user has switched to a different
// chat must be discarded, not silently installed into whatever chat is now
// open.
func TestClipboardPastedMsg_WrongChatDiscarded(t *testing.T) {
	m := newTestModel(t)
	m.composer.SetChatId(1) // chat active when the (simulated) paste landed

	// Simulate switching chats before the async paste completes.
	m.composer.SetChatId(2)

	updated, _ := m.Update(ClipboardPastedMsg{ChatId: 1, Path: "/tmp/does-not-matter.png", IsImage: true})
	got := updated.(Model)

	if got.pasteInFlight {
		t.Error("pasteInFlight should be cleared once the paste message is handled")
	}
	if attachment := got.composer.Attachment(); attachment != "" {
		t.Errorf("attachment should not be installed after a chat change, got %q", attachment)
	}
	// composer.Model doesn't expose its notice field directly, so check it
	// through the rendered view instead.
	if view := got.composer.View(); !strings.Contains(view, "paste discarded") {
		t.Errorf("composer view = %q, want it to contain the paste-discarded warning", view)
	}
}

// TestClipboardPastedMsg_SameChatInstalled is the control case: a paste that
// lands while the same chat is still open must be installed as the pending
// attachment.
func TestClipboardPastedMsg_SameChatInstalled(t *testing.T) {
	m := newTestModel(t)
	m.composer.SetChatId(1)

	updated, _ := m.Update(ClipboardPastedMsg{ChatId: 1, Path: "/tmp/does-not-matter.png", IsImage: true})
	got := updated.(Model)

	if got.pasteInFlight {
		t.Error("pasteInFlight should be cleared once the paste message is handled")
	}
	if attachment := got.composer.Attachment(); attachment != "/tmp/does-not-matter.png" {
		t.Errorf("attachment = %q, want the pasted path installed", attachment)
	}
}
