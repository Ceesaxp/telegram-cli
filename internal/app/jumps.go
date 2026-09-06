package app

import (
	tea "charm.land/bubbletea/v2"
)

// The jump list, in vi's sense of the word.
//
// A jump is a move that teleports: following a t.me link into another chat,
// landing on a search hit, opening the discussion under a channel post.
// What they have in common is that the reader arrives somewhere they did
// not scroll to, and nothing on screen remembers where they came from —
// which is exactly the hole ctrl+o fills in vi.
//
// Deliberately NOT a jump: opening a chat from the chat list. That is
// navigation the reader is already driving, with the list still on screen
// holding the cursor they left; making ctrl+o a chat history as well would
// give one key two jobs and put the reader somewhere they can already get
// to with h and enter.

// jumpPoint is one place the reader was: a chat, and the message their
// cursor was on inside it.
//
// MessageID may be 0 — an empty or still-loading buffer has no cursor —
// and 0 is what openChatAt already means by "the newest message", so the
// zero value degrades to "back to that chat" rather than to a broken jump.
type jumpPoint struct {
	ChatID    int64
	MessageID int64
}

// maxJumps bounds the stack.
//
// It is a stack of positions inside a long-running TUI, so without a bound
// it grows for as long as the client is up. 32 is vi's own jumplist depth
// (:help jumplist), and a reader who has taken 32 jumps without going back
// is not going to want the 33rd-from-last one.
const maxJumps = 32

// pushJump records where the reader is, before something carries them off.
//
// A jump to the chat and message the reader is ALREADY on is not recorded:
// it would make ctrl+o a key that appears to do nothing, which is worse
// than a key that says the list is empty. A jump with no origin — nothing
// open yet — is not recorded either, for the same reason.
func (m *Model) pushJump() {
	from := jumpPoint{
		ChatID:    m.chatView.ChatId(),
		MessageID: m.chatView.CursorMessageId(),
	}
	if from.ChatID == 0 {
		return
	}
	if n := len(m.jumps); n > 0 && m.jumps[n-1] == from {
		return
	}
	m.jumps = append(m.jumps, from)
	if len(m.jumps) > maxJumps {
		// The oldest goes, not the newest: ctrl+o is asked "where was I
		// just now", and the answer that gets dropped has to be the one
		// furthest from that question.
		m.jumps = m.jumps[len(m.jumps)-maxJumps:]
	}
}

// popJump takes the most recent jump origin off the stack.
func (m *Model) popJump() (jumpPoint, bool) {
	n := len(m.jumps)
	if n == 0 {
		return jumpPoint{}, false
	}
	to := m.jumps[n-1]
	m.jumps = m.jumps[:n-1]
	return to, true
}

// jumpBack is ctrl+o: go back to where the last jump left from.
//
// An empty stack says so rather than doing nothing. The key is otherwise
// indistinguishable from an unbound one, and "nothing happened" is how a
// reader concludes a binding is broken — the same reason gx names the
// message with no links in it instead of staying silent.
func (m *Model) jumpBack() tea.Cmd {
	to, ok := m.popJump()
	if !ok {
		m.notify("no jump to go back from")
		return nil
	}
	return m.openChatAt(to.ChatID, to.MessageID)
}
