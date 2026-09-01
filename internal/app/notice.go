package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// A notification about a chat this client cannot yet describe is held until
// it can.
//
// The listener emits NewMessageMsg and ChatLastMessageMsg for the same
// message, in that order. The first is where the decision to notify is made,
// and it arrives while the chat is still absent from the store — so a
// message from a chat below the first dialog page was decided on with no
// mute flag to read. It failed open and rang, and only the SECOND message
// from that chat was silent, by which time the fetch had landed.
//
// "Muted chats never notify" then held for every chat except the ones a
// reader is least likely to have thought about, which is not a rule worth
// stating. So the notification waits for the answer instead of guessing at
// it. It still fails open — but on a resolution that did not arrive, rather
// than on one that had not arrived yet.
type pendingNotice struct {
	chatID int64
	body   string
}

// noticeGrace is how long a held notification waits before it is posted
// anyway.
//
// The fetch it is waiting for is one round trip and normally lands in well
// under a second; this bound is for the case where it does not come back at
// all — no connection, a peer that cannot be resolved, a rate limit. A
// message announced late is worse than one announced on time and much
// better than one never announced, so the backstop is short enough to still
// be about the message that arrived.
//
// A var rather than a const so tests can shorten it: a test that asserts
// what happens after the grace period would otherwise have to wait out the
// real one, and there are enough of them to notice.
var noticeGrace = 4 * time.Second

// maxPendingNotices caps what can be held at once. Reaching it means
// resolution has stopped working rather than that this many chats are
// talking; the oldest is released rather than dropped.
const maxPendingNotices = 32

type noticeGraceMsg struct{}

func noticeGraceCmd() tea.Cmd {
	return tea.Tick(noticeGrace, func(time.Time) tea.Msg { return noticeGraceMsg{} })
}

// holdNotice parks a notification until the chat behind it is known.
func (m *Model) holdNotice(chatID int64, body string) tea.Cmd {
	m.pendingNotices = append(m.pendingNotices, pendingNotice{chatID: chatID, body: body})

	if len(m.pendingNotices) > maxPendingNotices {
		// Released with the fallback title rather than discarded: the
		// reader is owed the message, and being unable to name the chat is
		// not a reason to withhold it.
		oldest := m.pendingNotices[0]
		m.pendingNotices = m.pendingNotices[1:]
		return tea.Batch(m.postNotice("New Message", oldest.body), noticeGraceCmd())
	}
	return noticeGraceCmd()
}

// releaseNotices posts, or drops, everything held for one chat.
//
// The mute flag and the title come from the message rather than from the
// store: this runs in the app's own switch, which is ahead of the chat list
// that does the storing, so the store still holds the unresolved stub.
func (m *Model) releaseNotices(chat *telegram.Chat) tea.Cmd {
	if chat == nil {
		return nil
	}

	var (
		cmds []tea.Cmd
		kept []pendingNotice
	)
	for _, notice := range m.pendingNotices {
		if notice.chatID != chat.ID {
			kept = append(kept, notice)
			continue
		}
		if chat.Muted {
			continue // the answer arrived, and it is no
		}
		cmds = append(cmds, m.postNotice(chat.Title, notice.body))
	}
	m.pendingNotices = kept

	return tea.Batch(cmds...)
}

// releaseAllNotices is the backstop: whatever is still held has not been
// resolved in time, so it goes out unnamed rather than silently.
func (m *Model) releaseAllNotices() tea.Cmd {
	if len(m.pendingNotices) == 0 {
		return nil
	}

	cmds := make([]tea.Cmd, 0, len(m.pendingNotices))
	for _, notice := range m.pendingNotices {
		cmds = append(cmds, m.postNotice("New Message", notice.body))
	}
	m.pendingNotices = nil

	return tea.Batch(cmds...)
}

// postNotice hands one notification to the terminal or the system.
//
// A terminal-posted notification comes back as a sequence to write rather
// than as something already sent: this process does not own the terminal,
// and tea.Raw is what puts bytes in the renderer's buffer instead of racing
// it mid-frame.
func (m *Model) postNotice(title, body string) tea.Cmd {
	m.sound.Play()
	if seq := m.notifier.Notify(title, body); seq != "" {
		return tea.Raw(seq)
	}
	return nil
}
