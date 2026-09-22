package app

import (
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// The @ picker's app half (issue #41).
//
// The composer owns completion: noticing the @, following the query, the
// keys, the ranking, the insertion. What it cannot know is the world outside
// the draft — what kind of chat is open, who has been talking in it, and
// what the server says about its members — and that is what lives here,
// along with painting the picker over the thread, which only the frame can
// do without moving anything.

// mentionPickerOpen reports whether the composer is showing the @ picker,
// which owns Up, Down, Enter, Tab and Esc while it is up. The app matches
// Tab before the focused panel sees it, so it is the one key that has to be
// let through here; the rest reach the composer anyway.
func (m Model) mentionPickerOpen() bool {
	return m.focus == PanelComposer && m.composer.MentionActive()
}

// mentionsAllowed reports whether an @ in chatID can name its members: a
// basic group or a supergroup. A private chat has one person to mention,
// who is already reading; a broadcast channel's members cannot be named at
// all. A chat the store has not described yet is neither, until it is.
func (m Model) mentionsAllowed(chatID int64) bool {
	entry, ok := m.store.Chats.Get(chatID)
	if !ok || entry.Chat == nil {
		return false
	}
	switch entry.Chat.Type {
	case telegram.ChatTypeBasicGroup, telegram.ChatTypeSupergroup:
		return true
	}
	return false
}
