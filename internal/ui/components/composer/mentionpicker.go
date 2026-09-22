package composer

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
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
}

// SetMentionsEnabled says whether a typed @ may open completion here. The
// host switches it on for basic groups and supergroups.
func (m *Model) SetMentionsEnabled(on bool) {
	m.mentionsEnabled = on
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

// queryMention makes q the completion's query, under a new generation, and
// asks the host about it.
func (m *Model) queryMention(q string) tea.Cmd {
	m.mention.query = q
	m.mention.gen++
	ask := MentionQueryMsg{
		ChatID: m.mention.chatID,
		Anchor: m.mention.anchor,
		Query:  q,
		Gen:    m.mention.gen,
	}
	return func() tea.Msg { return ask }
}
