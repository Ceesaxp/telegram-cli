// Package sigil holds the one-glyph chat-type marks of TUI 2.0.
//
// The sigil IS the identity in this design — avatars are an explicit
// non-goal — so the glyph and its colour carry the whole "what kind of
// thing is this" signal a picture used to. That makes agreement between the
// surfaces that draw it a correctness property rather than a nicety: a chat
// marked # in the list and @ in the thread header is two different chats as
// far as the reader is concerned.
//
// It lives in its own package because the chat list and the thread header
// are peers. Either one importing the other to borrow a glyph would make an
// arbitrary one of them the owner of a shared vocabulary.
package sigil

import (
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// For returns the chat-type mark and its semantic colour.
//
// Saved Messages is checked BEFORE the chat type, because Telegram models
// it as an ordinary private chat: the other order silently draws your own
// notes as a DM with yourself.
func For(t telegram.ChatType, saved bool, roles theme.Roles) (string, lipgloss.Color) {
	if saved {
		return "~", roles.Green
	}
	switch t {
	case telegram.ChatTypePrivate:
		return "@", roles.Blue
	case telegram.ChatTypeBasicGroup, telegram.ChatTypeSupergroup:
		return "#", roles.Mauve
	case telegram.ChatTypeChannel:
		return "!", roles.Amber
	default:
		return "@", roles.Blue
	}
}

// Kind is the word for a chat type, used where a sigil needs spelling out —
// the thread header's subtitle, for one, where there is room to be explicit
// and a reader who has not yet learned the glyphs.
func Kind(t telegram.ChatType, saved bool) string {
	if saved {
		return "saved"
	}
	switch t {
	case telegram.ChatTypePrivate:
		return "direct"
	case telegram.ChatTypeBasicGroup, telegram.ChatTypeSupergroup:
		return "group"
	case telegram.ChatTypeChannel:
		return "channel"
	default:
		return ""
	}
}
