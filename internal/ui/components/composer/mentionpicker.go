package composer

import (
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// @-completion (issue #41).
//
// The composer owns the whole of it except the looking up: noticing an @
// being typed, following the query as the text and the cursor move, the keys
// while the picker is up, the ranking, and the insertion. The host is asked
// for members with a MentionQueryMsg and answers with a MentionResultsMsg,
// and paints the picker over the thread from MentionPicker.

// mentionState is the completion in progress.
type mentionState struct {
	active bool
	// chatID is the chat the completion was opened in.
	chatID int64
	// anchor is the rune offset of the @ in the draft.
	anchor int
	// query is the runes between the @ and the cursor.
	query string
	// gen is the generation of the latest query. It is never lowered and
	// never reused — closing keeps it — so an answer to any earlier query,
	// in this completion or another, cannot pass for an answer to this one.
	gen uint64

	// results are what the picker offers, best first: the local candidates
	// and server merged and ranked against query. See rankMentions.
	results []*telegram.User
	// selected is the index in results of the member Enter inserts.
	selected int
	// server is the member search's latest answer. It is kept when the
	// query changes: its members are still this chat's, and ranking
	// against the new query sorts out which of them still match.
	server []*telegram.User
	// loading is whether the current query is still unanswered, and failed
	// whether its search failed.
	loading, failed bool
}

// SetMentionsEnabled says whether a typed @ may open completion here. The
// host switches it on for basic groups and supergroups.
//
// It is a property of the open chat: SetChatId switches it off again, so the
// host calls this after every switch. Forgetting to leaves a group without
// completion — never a private chat with it.
func (m *Model) SetMentionsEnabled(on bool) {
	m.mentionsEnabled = on
	if !on {
		m.closeMention()
	}
}

// SetMentionCandidates supplies the members of chatID the host already
// knows — recent senders, a basic group's member list — most recent first.
// The picker offers them the moment an @ is typed, ahead of the member
// search's answer, and goes on offering them if the search fails.
//
// They are kept until the chat changes, so the host can supply them once
// when the chat opens and again whenever it learns more. Candidates for any
// chat but the open one are ignored.
func (m *Model) SetMentionCandidates(chatID int64, users []*telegram.User) {
	if chatID != m.chatID {
		return
	}
	m.mentionCandidates = users
	if m.mention.active {
		m.rankMention()
	}
}

// MentionActive reports whether the picker is open. While it is, the
// composer owns Up, Down, Enter, Tab and Esc — so a host that takes any of
// those before the composer sees them has to let them through.
func (m Model) MentionActive() bool { return m.mention.active }

// trackMention updates the completion after an edit: before and cursor are
// the text and the cursor the edit started from.
func (m *Model) trackMention(msg tea.Msg, before string, cursor int) tea.Cmd {
	if m.typedMentionTrigger(msg, before, cursor) {
		return m.openMention(cursor)
	}
	if m.mention.active {
		return m.followMention()
	}
	return nil
}

// typedMentionTrigger reports whether the edit just made was an @ typed at
// the cursor.
//
// Read from what the edit did rather than from the key: one @ more in the
// text, where the cursor was, with the cursor now after it, is typing and
// nothing else — whatever the terminal called the key. Which is also why vi's
// normal mode needs no rule of its own: no command there types an @.
//
// A paste can leave the same trace, so it is ruled out by what it is.
func (m Model) typedMentionTrigger(msg tea.Msg, before string, cursor int) bool {
	if _, key := msg.(tea.KeyPressMsg); !key || !m.mentionsEnabled || m.chatID == 0 {
		return false
	}
	after := []rune(m.textarea.Value)
	typed := len(after) == len([]rune(before))+1 &&
		m.textarea.Cursor == cursor+1 && after[cursor] == '@'
	return typed && (cursor == 0 || canStartMention(after[cursor-1]))
}

// mentionOpeners are the punctuation an @ may follow and still start a
// mention: what opens a bracket or a quote, and what separates the parts of
// a sentence. A full stop is not among them: "a.@b" is likelier an address
// than a mention.
const mentionOpeners = `([{"'«,;:!?`

// canStartMention reports whether an @ typed after prev starts a mention:
// after whitespace — a line break included — or one of mentionOpeners.
// Anything else puts the @ inside a word, and name@example.com must stay an
// address.
func canStartMention(prev rune) bool {
	return unicode.IsSpace(prev) || strings.ContainsRune(mentionOpeners, prev)
}

// openMention starts a completion at the @ at anchor.
func (m *Model) openMention(anchor int) tea.Cmd {
	m.mention = mentionState{
		active: true,
		chatID: m.chatID,
		anchor: anchor,
		gen:    m.mention.gen,
	}
	return m.queryMention("")
}

// followMention re-reads the query from the text and the cursor after an
// edit or a motion: asks again if it changed, and closes the completion if
// the cursor has left the token.
//
// Every way out comes down to that one test, so none of them needs a rule of
// its own — a space or a line break typed, a comma, the @ backspaced or
// killed, the cursor walked or jumped off either end, a paste with a space
// in it. The draft is left exactly as the key made it: "@ " is an @ and a
// space.
func (m *Model) followMention() tea.Cmd {
	q, ok := m.mentionQuery()
	if !ok {
		m.closeMention()
		return nil
	}
	if q == m.mention.query {
		return nil
	}
	return m.queryMention(q)
}

// mentionEnders end the token being completed: whitespace, what can start
// one, and what closes a bracket, a quote or a sentence. None of them is in
// a username, and a name is found by its words without them.
const mentionEnders = mentionOpeners + `)]}».`

func endsMention(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune(mentionEnders, r)
}

// mentionQuery returns the query the cursor is completing — the runes from
// the @ to the cursor — and false once the cursor is no longer in the token:
// at or before the @, past something that ends the token, or the @ itself
// gone.
func (m Model) mentionQuery() (string, bool) {
	runes := []rune(m.textarea.Value)
	anchor, cursor := m.mention.anchor, m.textarea.Cursor
	if anchor >= len(runes) || runes[anchor] != '@' || cursor <= anchor || cursor > len(runes) {
		return "", false
	}
	q := runes[anchor+1 : cursor]
	if slices.ContainsFunc(q, endsMention) {
		return "", false
	}
	return string(q), true
}

// queryMention makes q the completion's query, under a new generation, and
// asks the host about it.
func (m *Model) queryMention(q string) tea.Cmd {
	m.mention.query = q
	m.mention.gen++
	m.mention.loading = true
	m.mention.failed = false
	m.rankMention()
	// A new list: whoever was selected is usually not on it any more.
	m.mention.selected = 0
	ask := MentionQueryMsg{
		ChatID: m.mention.chatID,
		Anchor: m.mention.anchor,
		Query:  q,
		Gen:    m.mention.gen,
	}
	return func() tea.Msg { return ask }
}

// mentionKey runs a key the open picker owns, and reports whether stroke was
// one. Matched on Keystroke(), like every chord in handleKey. Anything else
// goes on to the composer, which is how every printable key still reaches
// the text.
//
// Up and Down move the selection, and stop at either end rather than wrap:
// on a list of five, a press too many landing on the worst match is a worse
// surprise than a press that does nothing. While the picker is open they are
// its, not the multi-line cursor's.
//
// Enter and Tab insert the selected member. With nobody to insert they close
// the picker and do nothing else — Enter does NOT send. A half-typed "@nad"
// going to a group is not something to do on somebody's behalf, and the next
// Enter sends it.
//
// Esc closes the picker and does nothing else: the text stays as typed, a
// reply stays a reply, and vi stays in insert mode. The composer's own
// Escape is the next press. Closing forgets the token for good — only a
// typed @ opens completion, so nothing can bring this one back.
func (m Model) mentionKey(stroke string) (Model, bool) {
	switch stroke {
	case "up":
		m.mention.selected = max(m.mention.selected-1, 0)
	case "down":
		m.mention.selected = max(min(m.mention.selected+1, len(m.mention.results)-1), 0)
	case "enter", "tab":
		m.acceptMention()
	case "esc":
		m.closeMention()
	default:
		return m, false
	}
	return m, true
}

// acceptMention puts the selected member in place of the token and closes
// the completion.
//
// A member with a username is mentioned by it, and "@username" is a mention
// all by itself. One without is mentioned by name, which only means them
// because of the span InsertMention records with it.
func (m *Model) acceptMention() {
	s := m.mention
	m.closeMention()
	if s.selected >= len(s.results) {
		return
	}
	u := s.results[s.selected]
	if u.Username != "" {
		m.InsertMention(s.anchor, m.textarea.Cursor, "@"+u.Username, 0)
		return
	}
	m.InsertMention(s.anchor, m.textarea.Cursor, displayName(u), u.ID)
}

// applyMentionResults takes the member search's answer, if it is the answer
// to the query on screen. See MentionResultsMsg.
func (m *Model) applyMentionResults(res MentionResultsMsg) {
	s := m.mention
	if !s.active || res.ChatID != s.chatID || res.Anchor != s.anchor ||
		res.Query != s.query || res.Gen != s.gen {
		return
	}
	m.mention.loading = false
	if res.Err != nil {
		m.mention.failed = true
	} else {
		m.mention.server = res.Users
	}
	m.rankMention()
}

// rankMention recomputes what the picker offers. The member selected stays
// selected if they are still on offer, wherever they now sit: an answer
// arriving under the reader's cursor must not move it onto somebody else.
func (m *Model) rankMention() {
	var keep int64
	if s := m.mention; s.selected < len(s.results) {
		keep = s.results[s.selected].ID
	}
	m.mention.results = rankMentions(m.mention.query, m.mentionCandidates, m.mention.server)
	m.mention.selected = max(slices.IndexFunc(m.mention.results,
		func(u *telegram.User) bool { return u.ID == keep }), 0)
}

// closeMention ends the completion, keeping only the generation: an answer
// still on its way must not match whatever opens next.
func (m *Model) closeMention() {
	m.mention = mentionState{gen: m.mention.gen}
}
