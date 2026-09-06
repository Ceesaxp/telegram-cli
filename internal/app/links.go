package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatview"
)

// Following a t.me link, this side of it.
//
// chatview reads the link and says where it points (chatview.TelegramLinkMsg);
// everything below decides whether that destination can be reached and, if
// it can, goes there through openChatAt — the one function every way of
// opening a chat goes through. Nothing here duplicates it, and nothing in
// chatview does any of it.

// telegramLinkResolvedMsg is a username link's answer coming back from the
// server.
//
// It carries the whole chat rather than only its ID because a link may name
// a chat this account has never opened, and the store has to learn about it
// before the open — see [Model.openResolvedLink].
type telegramLinkResolvedMsg struct {
	chat      *telegram.Chat
	username  string
	messageID int64
	// gen is the navigation the resolution was started for. See
	// [Model.openResolvedLink] for what a mismatch means.
	gen int
}

// followTelegramLink navigates to where an armed t.me link pointed.
//
// The two forms it can carry need different things. A t.me/c/… link already
// holds the chat ID, so it is a jump and nothing more. A public @username is
// not a chat ID at all until the server says so, which is a round trip and
// therefore a command.
func (m *Model) followTelegramLink(msg chatview.TelegramLinkMsg) tea.Cmd {
	if msg.ChatID != 0 {
		// A private channel link names an ID and no username, so there is
		// nothing to resolve and nothing that could teach this client the
		// access hash for a channel it has never seen. The chat list is
		// the honest test of that: a chat in it has been resolved once
		// already, and a chat that is not can only produce a buffer that
		// fails to load with the reader sitting in it.
		//
		// Membership alone is not that test, though. The store INVENTS an
		// entry to hold a message from a chat nobody has described yet,
		// and for as long as that entry stands unresolved it is exactly
		// the title-less, may-fail-to-load buffer the paragraph above
		// promises to keep the reader out of — so the flag is read too,
		// and only a chat something has actually described counts as
		// reached once already.
		if entry, ok := m.store.Chats.Get(msg.ChatID); !ok || entry.Unresolved {
			m.notify("⚠ that chat is not in your chat list: " + msg.URI)
			return nil
		}
		m.pushJumpTo(msg.ChatID, msg.MessageID)
		return m.openChatAt(msg.ChatID, msg.MessageID)
	}

	// The generation is read HERE, when the reader asks, and travels with
	// the question: it is what tells the answer apart from an answer to a
	// question they have since replaced.
	tg, link, gen := m.tg, msg.TmeLink, m.navGen
	return func() tea.Msg {
		chat, err := tg.ResolveUsername(link.Username)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return telegramLinkResolvedMsg{
			chat: chat, username: link.Username, messageID: link.MessageID,
			gen: gen,
		}
	}
}

// openResolvedLink opens the chat a username resolved to.
//
// The store is taught the chat HERE, synchronously, rather than left to
// learn it from the ChatUpdateMsg the resolution also announces. That
// announcement travels the client's event channel while this travels a
// command's return value, and the two do not have an order — so relying on
// it would open a chat whose title the header does not have yet and draw
// the em dash placeholder instead of a name.
//
// The jump is pushed here rather than before the round trip, so a
// resolution that fails leaves the jump list exactly as it was: ctrl+o must
// not offer to return from somewhere the reader never went.
//
// An answer from an earlier navigation is dropped, and dropped SILENTLY. A
// round trip is long enough to follow a link, think better of it and open
// something else in, and a late reply that acted anyway would yank the
// reader off the destination they chose second and push a jump origin they
// never left — the arrival is where the push happens, so the wrong arrival
// records the wrong way back. Nothing is said about it because nothing went
// wrong: they asked for one thing and then asked for another, and the
// second ask is the one that wins.
func (m *Model) openResolvedLink(msg telegramLinkResolvedMsg) tea.Cmd {
	if msg.gen != m.navGen {
		return nil
	}
	if msg.chat == nil || msg.chat.ID == 0 {
		m.notify("⚠ @" + msg.username + " resolved to no chat")
		return nil
	}
	m.store.Chats.Merge(msg.chat)
	m.pushJumpTo(msg.chat.ID, msg.messageID)
	return m.openChatAt(msg.chat.ID, msg.messageID)
}
