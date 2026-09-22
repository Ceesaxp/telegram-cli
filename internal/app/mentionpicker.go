package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
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

// prepareMentions sets completion up for chatID, which the composer has just
// been switched to: on in a group, with the members already known offered
// at once, and off anywhere else.
func (m *Model) prepareMentions(chatID int64) {
	on := m.mentionsAllowed(chatID)
	m.composer.SetMentionsEnabled(on)
	if on {
		m.offerMentionCandidates(chatID)
	}
}

// handleMentionQuery answers the composer's question about who an @ could
// mean.
//
// The candidates are handed over again first. They were handed over when
// the chat opened, but the page of history that was loading then has
// usually landed since, and every message after it may be from somebody
// new.
func (m *Model) handleMentionQuery(q composer.MentionQueryMsg) tea.Cmd {
	m.offerMentionCandidates(q.ChatID)
	return nil
}

// offerMentionCandidates hands the composer the members of chatID already
// known here. The composer ignores them for any chat but its own.
func (m *Model) offerMentionCandidates(chatID int64) {
	m.composer.SetMentionCandidates(chatID, m.mentionCandidates(chatID))
}

// mentionsAllowed reports whether an @ in chatID can name its members: a
// basic group or a supergroup. A private chat has one person to mention,
// who is already reading; a broadcast channel's members cannot be named at
// all. A chat the store has not described yet is neither, until it is.
func (m Model) mentionsAllowed(chatID int64) bool {
	kind, ok := m.chatType(chatID)
	return ok && (kind == telegram.ChatTypeBasicGroup || kind == telegram.ChatTypeSupergroup)
}

// chatType is what kind of chat the store says chatID is, and false when it
// has not said.
func (m Model) chatType(chatID int64) (telegram.ChatType, bool) {
	entry, ok := m.store.Chats.Get(chatID)
	if !ok || entry.Chat == nil {
		return 0, false
	}
	return entry.Chat.Type, true
}

// mentionCandidates are the members of chatID an @ can offer before any
// search answers: the people who have been talking in it, most recent
// first, each once.
//
// Only people. The reader is left out — a mention of yourself notifies
// nobody — and so is a channel or a group posting in the chat, which is not
// a member and cannot be mentioned. A sender the store has no user for is
// left to the search: there is no name to show or insert for them.
func (m Model) mentionCandidates(chatID int64) []*telegram.User {
	seen := map[int64]bool{m.myUserId: true}
	var out []*telegram.User
	msgs := m.store.Messages.Get(chatID)
	for i := len(msgs) - 1; i >= 0; i-- {
		sender, ok := msgs[i].SenderID.(*telegram.MessageSenderUser)
		if !ok || seen[sender.UserID] {
			continue
		}
		if u, ok := m.store.Users.Get(sender.UserID); ok {
			seen[u.ID] = true
			out = append(out, u)
		}
	}
	return out
}
