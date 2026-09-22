package app

import (
	"time"

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

// mentionSearchDebounce is how long a query waits before it reaches the
// member search: the forward picker's wait, for the forward picker's
// reason. Typing a name is one request rather than one per letter, and a
// pause still reads as an answer coming.
const mentionSearchDebounce = 250 * time.Millisecond

// mentionSearchLimit is how many members one search asks for. Four times
// what the picker lists: the composer ranks them against the query with
// the local candidates, and the best five of twenty are likelier to be the
// right five than the best five of five.
const mentionSearchLimit = 20

// mentionMembersKept bounds what is remembered of one chat's members, most
// recent answer first. A basic group has at most 200 members, so its whole
// list fits; a supergroup's searches are a window onto the members people
// have been looking for, not a copy of the member list.
const mentionMembersKept = 200

// memberSearcher is what the app needs of the Telegram client to find the
// members an @ could mean. See [telegram.Client.SearchChatMembers].
type memberSearcher interface {
	SearchChatMembers(chatID int64, query string, limit int) ([]*telegram.User, error)
}

// mentionSearchMsg is a query's debounce running out.
type mentionSearchMsg struct {
	query composer.MentionQueryMsg
}

// mentionMembersMsg is what the member search said about query.
type mentionMembersMsg struct {
	query composer.MentionQueryMsg
	users []*telegram.User
	err   error
}

// chatMembers is what the member search has said about one chat.
type chatMembers struct {
	// users are the members it turned up, newest answer first, each once,
	// no more than mentionMembersKept of them.
	users []*telegram.User
}

// learn adds an answer's users to what is known, ahead of the rest.
func (c chatMembers) learn(users []*telegram.User) chatMembers {
	seen := map[int64]bool{}
	var out []*telegram.User
	for _, group := range [][]*telegram.User{users, c.users} {
		for _, u := range group {
			if u == nil || seen[u.ID] || len(out) == mentionMembersKept {
				continue
			}
			seen[u.ID] = true
			out = append(out, u)
		}
	}
	c.users = out
	return c
}

// handleMentionQuery answers the composer's question about who an @ could
// mean: now with the members already known here, and after the debounce with
// the server's.
//
// The candidates are handed over again first. They were handed over when
// the chat opened, but the page of history that was loading then has
// usually landed since, and every message after it may be from somebody
// new.
func (m *Model) handleMentionQuery(q composer.MentionQueryMsg) tea.Cmd {
	m.mentionQuery = q
	m.offerMentionCandidates(q.ChatID)
	return tea.Tick(mentionSearchDebounce, func(time.Time) tea.Msg {
		return mentionSearchMsg{query: q}
	})
}

// handleMentionSearch asks the server about a query whose debounce has run
// out — unless another query has arrived since, in which case this one has
// been typed past and its answer would be thrown away on arrival. The newer
// query has a debounce of its own running.
//
// Every query that is not typed past gets an answer, even one with nobody
// in it, and even with no client to ask: the picker says "searching…" until
// it hears back, and would go on saying it.
func (m *Model) handleMentionSearch(msg mentionSearchMsg) tea.Cmd {
	q := msg.query
	if q != m.mentionQuery {
		return nil
	}
	if m.members == nil {
		return answerMention(q, nil, nil)
	}
	members := m.members
	return func() tea.Msg {
		users, err := members.SearchChatMembers(q.ChatID, q.Query, mentionSearchLimit)
		return mentionMembersMsg{query: q, users: users, err: err}
	}
}

// handleMentionMembers takes the member search's answer: the users it named
// become users this client knows — so a sender name in the thread can use
// them as much as the picker — and members of the chat the next completion
// can offer at once. Then the composer is told, whether or not it is still
// asking; it discards an answer to a question it has moved past.
func (m *Model) handleMentionMembers(msg mentionMembersMsg) tea.Cmd {
	q := msg.query
	if msg.err == nil {
		for _, u := range msg.users {
			if u != nil {
				m.store.Users.Set(u)
			}
		}
		if m.mentionMembers == nil {
			m.mentionMembers = map[int64]chatMembers{}
		}
		m.mentionMembers[q.ChatID] = m.mentionMembers[q.ChatID].learn(msg.users)
	}
	return answerMention(q, msg.users, msg.err)
}

// answerMention is the composer's answer to q.
func answerMention(q composer.MentionQueryMsg, users []*telegram.User, err error) tea.Cmd {
	return func() tea.Msg {
		return composer.MentionResultsMsg{
			ChatID: q.ChatID,
			Anchor: q.Anchor,
			Query:  q.Query,
			Gen:    q.Gen,
			Users:  users,
			Err:    err,
		}
	}
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
// first, and then the members earlier searches turned up — each once.
//
// Only people. The reader is left out — a mention of yourself notifies
// nobody — and so is a channel or a group posting in the chat, which is not
// a member and cannot be mentioned. A sender the store has no user for is
// left to the search: there is no name to show or insert for them.
func (m Model) mentionCandidates(chatID int64) []*telegram.User {
	seen := map[int64]bool{m.myUserId: true}
	var out []*telegram.User
	offer := func(u *telegram.User) {
		if !seen[u.ID] {
			seen[u.ID] = true
			out = append(out, u)
		}
	}
	msgs := m.store.Messages.Get(chatID)
	for i := len(msgs) - 1; i >= 0; i-- {
		sender, ok := msgs[i].SenderID.(*telegram.MessageSenderUser)
		if !ok {
			continue
		}
		if u, ok := m.store.Users.Get(sender.UserID); ok {
			offer(u)
		}
	}
	for _, u := range m.mentionMembers[chatID].users {
		offer(u)
	}
	return out
}
