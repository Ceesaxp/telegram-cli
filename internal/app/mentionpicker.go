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

// offerMentionCandidates hands the composer the members of chatID already
// known here. The composer ignores them for any chat but its own.
func (m *Model) offerMentionCandidates(chatID int64) {
	m.composer.SetMentionCandidates(chatID, m.mentionCandidates(chatID))
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

// mentionMembersMsg is what the member search said about query. whole is
// whether it was asked for a basic group, which answers with every member it
// has whatever the query.
type mentionMembersMsg struct {
	query composer.MentionQueryMsg
	users []*telegram.User
	err   error
	whole bool
}

// chatMembers is what the member search has said about one chat.
type chatMembers struct {
	// users are the members it turned up, newest answer first, each once,
	// no more than mentionMembersKept of them.
	users []*telegram.User
	// whole is whether users is a basic group's entire member list, which
	// answers every query in the chat from here on.
	whole bool
	// fetching is whether that list has been asked for and not come back.
	fetching bool
}

// learn adds an answer's users to what is known, ahead of the rest. A whole
// member list replaces what was known instead: it is all of it.
func (c chatMembers) learn(users []*telegram.User, whole bool) chatMembers {
	if whole {
		c = chatMembers{whole: true}
	}
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
// the server's — or at once, in a basic group whose whole list is here.
//
// The candidates are handed over again first. They were handed over when
// the chat opened, but the page of history that was loading then has
// usually landed since, and every message after it may be from somebody
// new.
func (m *Model) handleMentionQuery(q composer.MentionQueryMsg) tea.Cmd {
	m.mentionQuery = q
	m.offerMentionCandidates(q.ChatID)
	if known := m.mentionMembers[q.ChatID]; known.whole {
		return answerMention(q, known.users, nil)
	}
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
//
// A basic group is asked once for all its members, whatever the query —
// that is what the search gives back for one — and the list answers every
// query after it. A query arriving while the list is on its way asks
// nothing: the list answers it when it lands.
func (m *Model) handleMentionSearch(msg mentionSearchMsg) tea.Cmd {
	q := msg.query
	if q != m.mentionQuery {
		return nil
	}
	if m.members == nil {
		return answerMention(q, nil, nil)
	}
	kind, _ := m.chatType(q.ChatID)
	whole := kind == telegram.ChatTypeBasicGroup
	if whole {
		known := m.mentionMembers[q.ChatID]
		switch {
		case known.whole:
			return answerMention(q, known.users, nil)
		case known.fetching:
			return nil
		}
		known.fetching = true
		m.keepMembers(q.ChatID, known)
	}
	members := m.members
	return func() tea.Msg {
		users, err := members.SearchChatMembers(q.ChatID, q.Query, mentionSearchLimit)
		return mentionMembersMsg{query: q, users: users, err: err, whole: whole}
	}
}

// handleMentionMembers takes the member search's answer: the users it named
// become users this client knows — so a sender name in the thread can use
// them as much as the picker — and members of the chat the next completion
// can offer at once. Then the composer is told, whether or not it is still
// asking; it discards an answer to a question it has moved past.
//
// A basic group's list answers the newest query in the chat rather than the
// one that sent for it: every query typed while it was on its way was left
// for it to answer. A failed fetch leaves nothing on its way, so the next
// query asks again.
func (m *Model) handleMentionMembers(msg mentionMembersMsg) tea.Cmd {
	q, chatID := msg.query, msg.query.ChatID
	known := m.mentionMembers[chatID]
	if msg.whole {
		known.fetching = false
		if m.mentionQuery.ChatID == chatID {
			q = m.mentionQuery
		}
	}
	if msg.err == nil {
		for _, u := range msg.users {
			if u != nil {
				m.store.Users.Set(u)
			}
		}
		known = known.learn(msg.users, msg.whole)
	}
	m.keepMembers(chatID, known)
	return answerMention(q, msg.users, msg.err)
}

// keepMembers records what is known of chatID's members.
func (m *Model) keepMembers(chatID int64, known chatMembers) {
	if m.mentionMembers == nil {
		m.mentionMembers = map[int64]chatMembers{}
	}
	m.mentionMembers[chatID] = known
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

// mentionPickerRows is the most rows the picker is given: five members and
// a line about the search, which is all MentionPicker ever draws.
const mentionPickerRows = 6

// threadHeaderRows is the chat view's header, which the picker never
// covers: it names the chat the mention is going to.
const threadHeaderRows = 1

// paintMentionPicker paints the open @ picker over the foot of thread — the
// thread column's lines, exactly ThreadHeight of them — so its last row sits
// directly above the composer.
//
// Over the thread, not between it and the composer. The picker takes no rows
// of its own: the composer keeps its height, the thread keeps its lines and
// its scroll, and opening or closing the picker repaints the rows under it
// and moves nothing. Which is also why a closed picker changes nothing at
// all — thread comes back as it went in.
//
// It is the thread column's width, and never more than mentionPickerRows
// tall. On a short terminal it gets what is left under the header, and the
// composer draws fewer members rather than spill over it; with no row to
// spare there is no picker.
func (m Model) paintMentionPicker(thread []string) []string {
	rows := min(mentionPickerRows, len(thread)-threadHeaderRows)
	picker, ok := m.composer.MentionPicker(m.layout.ThreadWidth, rows)
	if !ok {
		return thread
	}
	picker = picker[:min(len(picker), rows)]
	copy(thread[len(thread)-len(picker):], picker)
	return thread
}
