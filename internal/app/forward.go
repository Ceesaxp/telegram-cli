package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/render"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/forward"
	"github.com/Ceesaxp/telegram-cli/internal/ui/sigil"
)

// forwardSearchDebounce is how long a keystroke waits before the query
// reaches contacts.search. Long enough that typing a name is one request
// rather than eight, short enough that a pause reads as an answer coming.
const forwardSearchDebounce = 250 * time.Millisecond

// forwardSearchMsg fires after the debounce, carrying the query it was
// scheduled for and the generation that scheduled it.
type forwardSearchMsg struct {
	query string
	gen   int
}

// forwardResultsMsg carries what the server matched. err is reported
// without emptying the list — local matches stay usable.
type forwardResultsMsg struct {
	gen   int
	chats []*telegram.Chat
	err   error
}

// forwardDoneMsg is the outcome of the forward itself.
type forwardDoneMsg struct {
	destination string
	count       int
	err         error
}

// updateForwardPicker routes a keypress to the destination picker and acts
// on what it asks for.
func (m Model) updateForwardPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var action forward.Action
	m.forward, action = m.forward.Update(msg)

	switch action {
	case forward.ActionCancel:
		m.forward.Close()
		return m, nil

	case forward.ActionQueryChanged:
		// Every edit invalidates the search in flight. The generation is
		// what makes that true rather than hoped for: a reply to the old
		// query still arrives, and without the check it would repopulate
		// a list the reader has already typed past.
		m.forwardGen++
		query := strings.TrimSpace(m.forward.Query())
		if query == "" {
			m.forward.SetSearching(false)
			m.forward.SetResults(nil)
			return m, nil
		}
		m.forward.SetSearching(true)
		gen := m.forwardGen
		return m, tea.Tick(forwardSearchDebounce, func(time.Time) tea.Msg {
			return forwardSearchMsg{query: query, gen: gen}
		})

	case forward.ActionForward:
		src := m.forward.Source()
		dest, ok := m.forward.Destination()
		if !ok {
			return m, nil
		}
		m.forward.Close()
		return m, m.forwardMessage(src, dest)
	}
	return m, nil
}

// handleForwardSearch runs the debounced destination search, unless the
// query moved on while it was waiting.
func (m Model) handleForwardSearch(msg forwardSearchMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.forwardGen || !m.forward.IsVisible() {
		return m, nil
	}
	tg := m.tg
	query, gen := msg.query, msg.gen
	return m, func() tea.Msg {
		chats, err := tg.SearchChats(query, 20)
		return forwardResultsMsg{gen: gen, chats: chats, err: err}
	}
}

// handleForwardResults applies a search answer, if it is still the answer
// to the question on screen.
func (m Model) handleForwardResults(msg forwardResultsMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.forwardGen || !m.forward.IsVisible() {
		return m, nil
	}
	if msg.err != nil {
		// Not an empty list: the chats already loaded are still perfectly
		// good destinations, and clearing them because the network blinked
		// would take the feature away at the moment it is least able to
		// explain itself.
		m.forward.SetSearchFailed()
		return m, nil
	}
	out := make([]forward.Chat, 0, len(msg.chats))
	for _, c := range msg.chats {
		out = append(out, m.forwardChat(c, "not in your chats"))
	}
	m.forward.SetResults(out)
	return m, nil
}

// handleForwardDone reports the outcome in the notice row.
func (m Model) handleForwardDone(msg forwardDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.notify("⚠ " + forwardErrorText(msg.err))
		return m, nil
	}
	m.notify("forwarded to " + msg.destination)
	return m, nil
}

// forwardMessage performs the forward and reports what happened.
//
// Source and destination are both values captured before the command was
// built, so nothing the reader does while the request is in flight — moving
// the cursor, opening another chat — can change where this lands.
func (m Model) forwardMessage(src forward.Source, dest forward.Chat) tea.Cmd {
	tg := m.tg
	label := strings.TrimSpace(dest.Title)
	if label == "" {
		label = dest.Handle
	}
	return func() tea.Msg {
		msgs, err := tg.ForwardMessages(src.ChatID, dest.ID, []int64{src.MessageID})
		return forwardDoneMsg{destination: label, count: len(msgs), err: err}
	}
}

// forwardCandidates is the destination list the picker opens on: every chat
// already loaded, in the chat list's own order, so the chats a reader
// forwards to most are the ones already at the top.
func (m Model) forwardCandidates() []forward.Chat {
	entries := m.store.Chats.OrderedChats()
	out := make([]forward.Chat, 0, len(entries))
	for _, e := range entries {
		if e == nil || e.Chat == nil || e.Unresolved {
			// An invented entry has an id and nothing else — no name and
			// no type — so it cannot be shown as a destination, and
			// forwarding to an id nobody can read is not a choice.
			continue
		}
		out = append(out, m.forwardChat(e.Chat, ""))
	}
	return out
}

// forwardChat renders one chat as a picker row.
func (m Model) forwardChat(c *telegram.Chat, note string) forward.Chat {
	mark, _ := sigil.For(c.Type, c.ID == m.myUserId, m.roles)
	handle := ""
	if c.Username != "" {
		handle = "@" + c.Username
	}
	return forward.Chat{
		ID:     c.ID,
		Title:  c.Title,
		Sigil:  mark,
		Handle: handle,
		Note:   note,
	}
}

// messagePreview is the one-line rendering of a message shown at the
// confirmation step: who sent it, then what it says.
//
// The same shape as the reply row's quote, and for the same reason — the
// name is the half that identifies the message when the text is cut short.
func (m Model) messagePreview(chatID, messageID int64) string {
	for _, message := range m.store.Messages.Get(chatID) {
		if message.ID != messageID {
			continue
		}
		who := render.SenderName(message, m.store)
		if sender, ok := message.SenderID.(*telegram.MessageSenderUser); ok &&
			m.myUserId != 0 && sender.UserID == m.myUserId {
			who = "you"
		}
		body := "[media]"
		if text, ok := message.Content.(*telegram.MessageText); ok {
			body = text.Text.Text
		}
		return strings.TrimSpace(who + ": " + strings.ReplaceAll(body, "\n", " "))
	}
	return "this message"
}

// forwardErrorText turns Telegram's refusals into something a reader can
// act on. The rest are passed through: an unfamiliar error is more useful
// verbatim than flattened into "could not forward".
func forwardErrorText(err error) string {
	text := err.Error()
	switch {
	case strings.Contains(text, "CHAT_FORWARDS_RESTRICTED"):
		return "that chat does not allow forwarding out of it"
	case strings.Contains(text, "CHAT_SEND_MEDIA_FORBIDDEN"),
		strings.Contains(text, "CHAT_WRITE_FORBIDDEN"):
		return "you cannot post in that chat"
	case strings.Contains(text, "CHAT_ADMIN_REQUIRED"):
		return "only admins can post in that chat"
	case strings.Contains(text, "MESSAGE_ID_INVALID"):
		return "that message can no longer be forwarded"
	}
	return text
}
