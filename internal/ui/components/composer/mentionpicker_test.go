package composer

import (
	"errors"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// mentionComposer is a focused emacs composer in a chat where the host has
// switched completion on — a group, in the app.
func mentionComposer(t *testing.T) Model {
	t.Helper()
	m := newFocused()
	m.SetMentionsEnabled(true)
	return m
}

// queryIn returns the MentionQueryMsg among what a key produced, if any.
func queryIn(msg tea.Msg) (MentionQueryMsg, bool) {
	switch msg := msg.(type) {
	case MentionQueryMsg:
		return msg, true
	case tea.BatchMsg:
		for _, cmd := range msg {
			if cmd == nil {
				continue
			}
			if q, ok := queryIn(cmd()); ok {
				return q, true
			}
		}
	}
	return MentionQueryMsg{}, false
}

// ---------------------------------------------------------------------------
// Opening
// ---------------------------------------------------------------------------

// A typed @ where a mention can start opens completion, and asks the host
// for the chat's members straight away — with nothing typed yet, the query
// is empty, and a supergroup answers that with its recent members.
func TestATypedAtOpensCompletion(t *testing.T) {
	m, msg := send(t, mentionComposer(t), "@")

	if !m.MentionActive() {
		t.Fatal("MentionActive = false after typing @ into an empty draft")
	}
	q, ok := queryIn(msg)
	if !ok {
		t.Fatalf("typing @ produced %T, want a MentionQueryMsg", msg)
	}
	if q.ChatID != 42 || q.Anchor != 0 || q.Query != "" || q.Gen == 0 {
		t.Errorf("MentionQueryMsg = %+v, want chat 42, anchor 0, empty query, a generation", q)
	}
	if m.Draft() != "@" {
		t.Errorf("Draft = %q, want the @ itself typed", m.Draft())
	}
}

// Where a mention can start: at the start of the draft or of a line, and
// after whitespace or the punctuation that opens or separates a token. Never
// inside a word — an email address is the case that matters.
func TestAnAtOpensOnlyWhereAMentionCanStart(t *testing.T) {
	cases := []struct {
		typed string
		opens bool
	}{
		{"@", true},
		{"hi @", true},
		{"(@", true},
		{"«@", true},
		{`"@`, true},
		{"yes,@", true},
		{"name@", false},
		{"name@example.com", false},
		{"a.@", false},
		{"@@", true}, // the first opened it; the second is inside its token
	}
	for _, tc := range cases {
		m := chars(t, mentionComposer(t), tc.typed)
		if got := m.MentionActive(); got != tc.opens {
			t.Errorf("typing %q: MentionActive = %v, want %v", tc.typed, got, tc.opens)
		}
	}
}

// After a line break is the start of a line, which is as good a place to
// start a mention as the start of the draft.
func TestAnAtOpensAtTheStartOfALine(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi")
	m = typeSeq(t, m, "\n") // ctrl+j
	m = chars(t, m, "@")
	if !m.MentionActive() {
		t.Error("an @ at the start of the second line did not open completion")
	}
}

// Off is the default, and what a private chat or a channel gets: there is
// nobody to choose between.
func TestAnAtDoesNotOpenWhereMentionsAreOff(t *testing.T) {
	m, msg := send(t, newFocused(), "@")
	if m.MentionActive() {
		t.Error("completion opened with mentions switched off")
	}
	if _, ok := queryIn(msg); ok {
		t.Error("asked the host for members with mentions switched off")
	}
}

// Nor before a chat is open: there are no members to ask about.
func TestAnAtDoesNotOpenWithoutAChat(t *testing.T) {
	m := newFocusedNoChat()
	m.SetMentionsEnabled(true)
	if m, _ = send(t, m, "@"); m.MentionActive() {
		t.Error("completion opened with no chat open")
	}
}

// In vi's normal mode a key is a command, not text. @ is not one of the
// commands, but it must not become a way into completion either.
func TestAnAtInViNormalModeDoesNotOpen(t *testing.T) {
	m := viComposer(t)
	m.SetMentionsEnabled(true)
	m = chars(t, m, "hi ")
	m, _ = send(t, m, "\x1b")
	m = chars(t, m, "A") // back to insert, at the end...
	m, _ = send(t, m, "\x1b")
	if !m.IsViNormalMode() {
		t.Fatal("precondition: not in normal mode")
	}

	if m, _ = send(t, m, "@"); m.MentionActive() {
		t.Error("an @ pressed in normal mode opened completion")
	}
}

// Only a typed @ opens completion. A pasted one is text somebody already
// wrote — an address, a quote, a whole message — and a picker springing up
// from the middle of it would take the next Enter for itself.
func TestAPastedAtDoesNotOpen(t *testing.T) {
	for _, paste := range []string{"@", "ask @nadia", "hi @"} {
		m, msg := mentionComposer(t).Update(tea.PasteMsg{Content: paste})
		if m.MentionActive() {
			t.Errorf("pasting %q opened completion", paste)
		}
		if msg != nil {
			if _, ok := queryIn(msg()); ok {
				t.Errorf("pasting %q asked the host for members", paste)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// The query
// ---------------------------------------------------------------------------

// Every key that changes the query asks again, under a newer generation, so
// the host can tell the answer to "na" from the answer to "n".
func TestTypingAfterTheAtFollowsTheQuery(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi ")

	var gens []uint64
	for _, r := range "@na" {
		var msg tea.Msg
		m, msg = send(t, m, string(r))
		q, ok := queryIn(msg)
		if !ok {
			t.Fatalf("typing %q asked nothing", r)
		}
		gens = append(gens, q.Gen)
		if r == 'a' {
			want := MentionQueryMsg{ChatID: 42, Anchor: 3, Query: "na", Gen: q.Gen}
			if q != want {
				t.Errorf("MentionQueryMsg = %+v, want %+v", q, want)
			}
		}
	}
	if !(gens[0] < gens[1] && gens[1] < gens[2]) {
		t.Errorf("generations %v, want each newer than the last", gens)
	}
	if got := m.mention.query; got != "na" {
		t.Errorf("query = %q, want %q", got, "na")
	}
}

// Raw terminal sequences for the keys the dismissal cases press.
const (
	keyBackspace = "\x7f"
	keyLeft      = "\x1b[D"
	keyRight     = "\x1b[C"
	keyUp        = "\x1b[A"
	keyDown      = "\x1b[B"
	keyTab       = "\t"
	keyEsc       = "\x1b"
	keyEnter     = "\r"
	keyCtrlA     = "\x01"
	keyCtrlJ     = "\n"
	keyCtrlW     = "\x17"
)

// Leaving the token closes completion, however it is left, and leaves the
// draft exactly as the keys made it: nothing is completed on the way out.
func TestLeavingTheTokenClosesCompletion(t *testing.T) {
	cases := []struct {
		name  string
		typed string   // typed first, a character at a time
		keys  []string // then these
		draft string
	}{
		{"a space right after the @ keeps the literal @", "hi @", []string{" "}, "hi @ "},
		{"a space after the query", "hi @na", []string{" "}, "hi @na "},
		{"a line break", "hi @na", []string{keyCtrlJ}, "hi @na\n"},
		{"a comma", "hi @na", []string{","}, "hi @na,"},
		{"a full stop", "hi @na", []string{"."}, "hi @na."},
		{"a closing bracket", "(@na", []string{")"}, "(@na)"},
		{"backspacing the @ away", "hi @na", []string{keyBackspace, keyBackspace, keyBackspace}, "hi "},
		{"killing the word", "hi @na", []string{keyCtrlW}, "hi "},
		{"the cursor moving left of the @", "hi @na", []string{keyLeft, keyLeft, keyLeft}, "hi @na"},
		{"the cursor jumping to the line start", "hi @na", []string{keyCtrlA}, "hi @na"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := chars(t, mentionComposer(t), tc.typed)
			if !m.MentionActive() {
				t.Fatalf("precondition: typing %q did not open completion", tc.typed)
			}
			m = typeSeq(t, m, tc.keys...)
			if m.MentionActive() {
				t.Errorf("still open with the draft at %q", m.Draft())
			}
			if m.Draft() != tc.draft {
				t.Errorf("Draft = %q, want %q", m.Draft(), tc.draft)
			}
		})
	}
}

// The cursor can also leave to the right, when there is text after the
// token: an @ typed in front of a word, then the cursor walked on past it.
func TestTheCursorLeavingRightClosesCompletion(t *testing.T) {
	m := chars(t, mentionComposer(t), " rest")
	m = typeSeq(t, m, keyCtrlA)
	m = chars(t, m, "@na")
	if !m.MentionActive() {
		t.Fatal("precondition: completion not open")
	}

	m = typeSeq(t, m, keyRight)
	if m.MentionActive() {
		t.Errorf("still open with the cursor past the token in %q", m.Draft())
	}
}

// Inside the token the cursor may move, and the query follows it: the
// query is what lies between the @ and the cursor.
func TestTheCursorInsideTheTokenNarrowsTheQuery(t *testing.T) {
	m := chars(t, mentionComposer(t), "@nad")
	m, msg := send(t, m, keyLeft)

	if !m.MentionActive() {
		t.Fatal("moving left inside the token closed completion")
	}
	if q, ok := queryIn(msg); !ok || q.Query != "na" {
		t.Errorf("moving left asked %+v, want the query %q", q, "na")
	}
}

// Once closed, it stays closed: typing on in the same word is typing, and
// nothing but another @ opens completion again.
func TestAClosedTokenDoesNotReopen(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")
	m = typeSeq(t, m, keyLeft, keyLeft, keyLeft) // out, to the left
	m = typeSeq(t, m, keyRight, keyRight, keyRight)
	m = chars(t, m, "dia")
	if m.MentionActive() {
		t.Error("walking back into a closed token reopened it")
	}
}

// ---------------------------------------------------------------------------
// Closing from outside
// ---------------------------------------------------------------------------

// A chat switch closes completion, and coming back brings the draft back
// without it: the members it was offering were the other chat's.
func TestAChatSwitchClosesCompletion(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")

	m.SetChatId(43)
	if m.MentionActive() {
		t.Error("completion survived the switch to another chat")
	}
	m.SetChatId(42)
	m.SetMentionsEnabled(true)
	if m.MentionActive() {
		t.Error("coming back reopened completion")
	}
	if m.Draft() != "hi @na" {
		t.Errorf("Draft = %q, want the parked draft back", m.Draft())
	}
}

// Whether completion applies is a property of the chat, so it does not
// follow the composer into the next one. The host says so again for each.
func TestAChatSwitchSwitchesMentionsOff(t *testing.T) {
	m := mentionComposer(t)
	m.SetChatId(43)
	if m, _ = send(t, m, "@"); m.MentionActive() {
		t.Error("an @ opened completion in a chat the host never enabled it for")
	}
}

// Losing focus closes it, and getting focus back does not reopen it.
func TestLosingFocusClosesCompletion(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")

	m.SetFocused(false)
	if m.MentionActive() {
		t.Error("completion survived losing focus")
	}
	m.SetFocused(true)
	if m.MentionActive() {
		t.Error("getting focus back reopened completion")
	}
}

func TestSwitchingMentionsOffClosesCompletion(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")
	m.SetMentionsEnabled(false)
	if m.MentionActive() {
		t.Error("completion survived mentions being switched off")
	}
}

// Anything that replaces the draft wholesale closes it: the token it was
// completing is gone with the text.
func TestReplacingTheDraftClosesCompletion(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")
	m.EnterEditMode(9, "@na")
	if m.MentionActive() {
		t.Error("completion survived entering edit mode")
	}

	m = chars(t, mentionComposer(t), "hi @na")
	m, _ = m.Update(editorFinishedMsg{text: "hi @na", ok: true})
	if m.MentionActive() {
		t.Error("completion survived the external editor")
	}
}

// ---------------------------------------------------------------------------
// Esc
// ---------------------------------------------------------------------------

// Esc closes the picker and nothing else: the text stays as typed, and the
// next Enter is an ordinary send.
func TestEscClosesCompletionAndKeepsTheText(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")

	m, _ = send(t, m, keyEsc)
	if m.MentionActive() {
		t.Fatal("Esc did not close completion")
	}
	if m.Draft() != "hi @na" {
		t.Errorf("Draft = %q, want the text untouched", m.Draft())
	}

	_, msg := send(t, m, keyEnter)
	sub, ok := msg.(MessageSubmittedMsg)
	if !ok || sub.Text != "hi @na" {
		t.Errorf("Enter after Esc produced %#v, want the draft sent as typed", msg)
	}
}

// The first Esc belongs to the picker. The composer's own Escape — cancel the
// reply, leave vi's insert mode — waits for the next one.
func TestTheFirstEscOnlyClosesThePicker(t *testing.T) {
	m := mentionComposer(t)
	m.EnterReplyMode(5, "the message")
	m = chars(t, m, "@na")
	m, _ = send(t, m, keyEsc)
	if m.mode != ModeReply {
		t.Error("the Esc that closed the picker also cancelled the reply")
	}

	v := viComposer(t)
	v.SetMentionsEnabled(true)
	v = chars(t, v, "@na")
	v, _ = send(t, v, keyEsc)
	if v.MentionActive() || v.IsViNormalMode() {
		t.Errorf("vi: after one Esc, open = %v and normal = %v, want the picker closed and still inserting",
			v.MentionActive(), v.IsViNormalMode())
	}
}

// The app gives Escape to the composer only while IsComposing says it is
// the composer's. An open picker is.
func TestAnOpenPickerIsComposing(t *testing.T) {
	m := mentionComposer(t)
	if m.IsComposing() {
		t.Fatal("precondition: an idle emacs composer reports composing")
	}
	if m = chars(t, m, "@"); !m.IsComposing() {
		t.Error("IsComposing = false with the picker open; the app would take Esc for itself")
	}
}

// After Esc the token stays closed however much more of it is typed. Another
// @ is another token, and opens.
func TestAfterEscOnlyANewAtReopens(t *testing.T) {
	m := chars(t, mentionComposer(t), "hi @na")
	m, _ = send(t, m, keyEsc)

	if m = chars(t, m, "dia"); m.MentionActive() {
		t.Fatal("typing on in the dismissed token reopened it")
	}
	if m = chars(t, m, " @"); !m.MentionActive() {
		t.Error("a new @ after a dismissed one did not open")
	}
}

// ---------------------------------------------------------------------------
// Candidates and results
// ---------------------------------------------------------------------------

var (
	errFlood = errors.New("FLOOD_WAIT_5")

	nadiaUser = user(1, "nadia", "Nadia", "Petrova")
	nadiaS    = user(2, "", "Nadia", "S.")
	olegUser  = user(3, "", "Oleg", "")
)

// shown is the IDs the picker is offering, best first.
func shown(m Model) []int64 { return ids(m.mention.results) }

// The local candidates are offered the moment the @ is typed, before any
// answer from the server — and narrowed as the query grows.
func TestLocalCandidatesShowImmediately(t *testing.T) {
	m := mentionComposer(t)
	m.SetMentionCandidates(42, []*telegram.User{olegUser, nadiaS, nadiaUser})

	if m = chars(t, m, "@"); !slices.Equal(shown(m), []int64{3, 2, 1}) {
		t.Errorf("after @: showing %v, want every candidate in recency order", shown(m))
	}
	if m = chars(t, m, "na"); !slices.Equal(shown(m), []int64{1, 2}) {
		t.Errorf("after @na: showing %v, want the username prefix first", shown(m))
	}
}

// Candidates arriving while the picker is open are offered at once.
func TestCandidatesArrivingWhileOpenAreShown(t *testing.T) {
	m := chars(t, mentionComposer(t), "@na")
	m.SetMentionCandidates(42, []*telegram.User{nadiaUser})
	if !slices.Equal(shown(m), []int64{1}) {
		t.Errorf("showing %v, want the candidate that just arrived", shown(m))
	}
}

// Candidates are per chat. Another chat's are not these members, and a chat
// switch forgets the old chat's.
func TestCandidatesBelongToTheirChat(t *testing.T) {
	m := mentionComposer(t)
	m.SetMentionCandidates(43, []*telegram.User{nadiaUser})
	if m = chars(t, m, "@"); len(shown(m)) != 0 {
		t.Errorf("showing %v, candidates for another chat", shown(m))
	}

	m = mentionComposer(t)
	m.SetMentionCandidates(42, []*telegram.User{nadiaUser})
	m.SetChatId(43)
	m.SetChatId(42)
	m.SetMentionsEnabled(true)
	if m = chars(t, m, " @"); len(shown(m)) != 0 {
		t.Errorf("showing %v after switching away and back, want the host to supply them again", shown(m))
	}
}

// answer is the MentionResultsMsg the host would send back for q.
func answer(q MentionQueryMsg, users ...*telegram.User) MentionResultsMsg {
	return MentionResultsMsg{ChatID: q.ChatID, Anchor: q.Anchor, Query: q.Query, Gen: q.Gen, Users: users}
}

// openAt types s and returns the composer with the last query it asked.
func openAt(t *testing.T, m Model, s string) (Model, MentionQueryMsg) {
	t.Helper()
	var last MentionQueryMsg
	for _, r := range s {
		var msg tea.Msg
		m, msg = send(t, m, string(r))
		if q, ok := queryIn(msg); ok {
			last = q
		}
	}
	if !m.MentionActive() {
		t.Fatalf("precondition: typing %q did not leave completion open", s)
	}
	return m, last
}

// The answer to the current query is merged with the local candidates.
func TestTheAnswerToTheCurrentQueryIsShown(t *testing.T) {
	m := mentionComposer(t)
	m.SetMentionCandidates(42, []*telegram.User{nadiaS})
	m, q := openAt(t, m, "@na")
	if !m.mention.loading {
		t.Error("loading = false while the query is unanswered")
	}

	m, _ = m.Update(answer(q, nadiaUser, nadiaS))
	if !slices.Equal(shown(m), []int64{1, 2}) {
		t.Errorf("showing %v, want the server's @nadia merged in first", shown(m))
	}
	if m.mention.loading {
		t.Error("loading = true after the answer arrived")
	}
}

// An answer to anything but the current query is dropped, whatever it
// disagrees about: the generation, the query, the @ or the chat.
func TestAStaleAnswerIsDropped(t *testing.T) {
	stale := map[string]func(*MentionResultsMsg){
		"an older generation": func(r *MentionResultsMsg) { r.Gen-- },
		"another query":       func(r *MentionResultsMsg) { r.Query = "n" },
		"another @":           func(r *MentionResultsMsg) { r.Anchor = 7 },
		"another chat":        func(r *MentionResultsMsg) { r.ChatID = 43 },
	}
	for name, spoil := range stale {
		t.Run(name, func(t *testing.T) {
			m, q := openAt(t, mentionComposer(t), "@na")
			res := answer(q, nadiaUser)
			spoil(&res)

			m, _ = m.Update(res)
			if len(shown(m)) != 0 {
				t.Errorf("showing %v from a stale answer", shown(m))
			}
			if !m.mention.loading {
				t.Error("a stale answer ended the wait for the real one")
			}
		})
	}
}

// Nor can an answer reopen a picker that closed while it was on its way —
// after "@ ", after the cursor left, after a chat switch.
func TestALateAnswerDoesNotReopen(t *testing.T) {
	closers := map[string]func(Model) Model{
		"a space":        func(m Model) Model { return chars(t, m, " ") },
		"the cursor out": func(m Model) Model { return typeSeq(t, m, keyLeft, keyLeft, keyLeft) },
		"Esc":            func(m Model) Model { return typeSeq(t, m, keyEsc) },
		"a chat switch": func(m Model) Model {
			m.SetChatId(43)
			m.SetChatId(42)
			m.SetMentionsEnabled(true)
			return m
		},
	}
	for name, leave := range closers {
		t.Run(name, func(t *testing.T) {
			m, q := openAt(t, mentionComposer(t), "@na")
			m = leave(m)

			m, _ = m.Update(answer(q, nadiaUser))
			if m.MentionActive() || len(shown(m)) != 0 {
				t.Errorf("the late answer reopened the picker: open = %v, showing %v",
					m.MentionActive(), shown(m))
			}
		})
	}
}

// A failed search leaves the local candidates usable and says it failed.
func TestAFailedSearchKeepsTheLocalCandidates(t *testing.T) {
	m := mentionComposer(t)
	m.SetMentionCandidates(42, []*telegram.User{nadiaUser})
	m, q := openAt(t, m, "@na")

	res := answer(q)
	res.Err = errFlood
	m, _ = m.Update(res)

	if !slices.Equal(shown(m), []int64{1}) {
		t.Errorf("showing %v, want the local @nadia still offered", shown(m))
	}
	if !m.mention.failed || m.mention.loading {
		t.Errorf("failed = %v, loading = %v; want failed and no longer loading",
			m.mention.failed, m.mention.loading)
	}

	// The next query is a new search, and has not failed yet.
	if m, _ = send(t, m, "d"); m.mention.failed {
		t.Error("the failure outlived the query it was about")
	}
}

// ---------------------------------------------------------------------------
// Choosing and inserting
// ---------------------------------------------------------------------------

// selectedID is the member Enter would insert, 0 when there is none.
func selectedID(m Model) int64 {
	if s := m.mention; s.selected < len(s.results) {
		return s.results[s.selected].ID
	}
	return 0
}

// threeNadias is a composer on a second line, completing "@nad" with three
// members to choose between: @nadia, then Nadia S., then @nadz.
func threeNadias(t *testing.T) Model {
	t.Helper()
	m := mentionComposer(t)
	m.SetMentionCandidates(42, []*telegram.User{nadiaUser, nadiaS, user(4, "nadz", "Zed", "")})
	m = chars(t, m, "first")
	m = typeSeq(t, m, keyCtrlJ)
	m = chars(t, m, "@nad")
	if got := shown(m); !slices.Equal(got, []int64{1, 4, 2}) {
		t.Fatalf("precondition: showing %v", got)
	}
	return m
}

// Up and Down move the selection and stop at either end rather than
// wrapping: on a list of five, one press too many landing on the worst match
// is a worse surprise than a press that does nothing. The text and the
// cursor stay where they are, though Up would otherwise go to the line above.
func TestUpAndDownMoveTheSelection(t *testing.T) {
	m := threeNadias(t)
	draft, cursor := m.Draft(), m.textarea.Cursor

	steps := []struct {
		key  string
		want int64
	}{
		{keyUp, 1}, // already at the top: stays
		{keyDown, 4},
		{keyDown, 2},
		{keyDown, 2}, // at the bottom: stays
		{keyUp, 4},
	}
	for i, st := range steps {
		m, _ = send(t, m, st.key)
		if got := selectedID(m); got != st.want {
			t.Errorf("step %d: selected %d, want %d", i, got, st.want)
		}
	}
	if m.Draft() != draft || m.textarea.Cursor != cursor {
		t.Errorf("the arrows moved the text: %q at %d, want %q at %d",
			m.Draft(), m.textarea.Cursor, draft, cursor)
	}
}

// A new query is a new list, so the selection goes back to its top — the
// row that was selected is usually not on it any more.
func TestANewQueryResetsTheSelection(t *testing.T) {
	m := threeNadias(t)
	m = typeSeq(t, m, keyDown)
	if m = chars(t, m, "i"); selectedID(m) != 1 {
		t.Errorf("selected %d after typing on, want the top row", selectedID(m))
	}
}

// An answer arriving under the reader's selection does not move it to
// somebody else: the member selected stays selected wherever they now sit.
func TestAnAnswerKeepsTheSelectedMember(t *testing.T) {
	m := mentionComposer(t)
	m.SetMentionCandidates(42, []*telegram.User{nadiaS, olegUser})
	m, q := openAt(t, m, "@")
	m = typeSeq(t, m, keyDown)
	if selectedID(m) != 3 {
		t.Fatalf("precondition: selected %d", selectedID(m))
	}

	m, _ = m.Update(answer(q, user(9, "", "Anna", "")))
	if selectedID(m) != 3 {
		t.Errorf("selected %d after the answer, want Oleg still", selectedID(m))
	}
}

// Enter and Tab insert the selected member. One with a username is
// mentioned by it: "@nadia " goes in, and nothing needs remembering.
func TestEnterAndTabInsertAUsername(t *testing.T) {
	for _, key := range []string{keyEnter, keyTab} {
		m := mentionComposer(t)
		m.SetMentionCandidates(42, []*telegram.User{nadiaUser})
		m = chars(t, m, "hi @na")

		m, msg := send(t, m, key)
		if _, sent := msg.(MessageSubmittedMsg); sent {
			t.Fatalf("%q sent the draft instead of completing", key)
		}
		if m.Draft() != "hi @nadia " {
			t.Errorf("%q: Draft = %q, want %q", key, m.Draft(), "hi @nadia ")
		}
		if len(m.mentions) != 0 {
			t.Errorf("%q: mentions = %+v, want none for a username", key, m.mentions)
		}
		if m.MentionActive() {
			t.Errorf("%q: completion still open after inserting", key)
		}
	}
}

// One without a username can only be mentioned by an entity carrying their
// ID: their name goes in, with a span saying who it means — and the span
// goes out with the message.
func TestEnterInsertsANameWithItsMention(t *testing.T) {
	m := mentionComposer(t)
	m.SetMentionCandidates(42, []*telegram.User{nadiaS})
	m = chars(t, m, "hi @na")

	m, _ = send(t, m, keyEnter)
	if m.Draft() != "hi Nadia S. " {
		t.Fatalf("Draft = %q, want %q", m.Draft(), "hi Nadia S. ")
	}
	span := MentionSpan{Start: 3, End: 11, UserID: 2, Label: "Nadia S."}
	wantMentions(t, m, span)

	m = chars(t, m, "ok")
	if _, sub := submitted(t, m); !slices.Equal(sub.Mentions, []MentionSpan{span}) {
		t.Errorf("sent Mentions = %+v, want %+v", sub.Mentions, []MentionSpan{span})
	}
}

// Enter with nothing to insert does not send either. It closes the picker
// and leaves the draft be: sending a half-typed "@nad" to a group is the
// kind of thing nobody wants done for them, and one more Enter sends it.
func TestEnterWithNothingToInsertDoesNotSend(t *testing.T) {
	for _, key := range []string{keyEnter, keyTab} {
		m := chars(t, mentionComposer(t), "hi @zz")

		m, msg := send(t, m, key)
		if msg != nil {
			t.Errorf("%q with no match produced %#v, want nothing", key, msg)
		}
		if m.MentionActive() || m.Draft() != "hi @zz" {
			t.Errorf("%q: open = %v, Draft = %q; want closed and the draft as typed",
				key, m.MentionActive(), m.Draft())
		}
	}

	m := chars(t, mentionComposer(t), "hi @zz")
	m, _ = send(t, m, keyEnter)
	if _, msg := send(t, m, keyEnter); msg == nil {
		t.Error("the Enter after that did not send")
	}
}

// Every other key still reaches the text while the picker is open —
// letters that are bindings elsewhere included.
func TestPrintableKeysReachTheTextWhileOpen(t *testing.T) {
	m := chars(t, mentionComposer(t), "@jkq/`")
	if m.Draft() != "@jkq/`" || !m.MentionActive() {
		t.Errorf("Draft = %q, open = %v; want every key typed, the picker open",
			m.Draft(), m.MentionActive())
	}
	if m.mention.query != "jkq/`" {
		t.Errorf("query = %q, want what was typed after the @", m.mention.query)
	}
}
