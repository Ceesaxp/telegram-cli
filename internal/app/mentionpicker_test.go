package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
	"github.com/charmbracelet/x/ansi"
)

// The @ picker's app half (issue #41): which chats offer it, who it offers,
// the member search behind it, where it is painted, and what a send does
// with the mentions it inserts. The picker itself — the token, the keys,
// the ranking — is the composer's, and tested there.

// Chat IDs in TDLib's encoding, which is what SearchChatMembers reads the
// kind of chat from.
const (
	basicGroupID int64 = -4101
	supergroupID int64 = -1001234567890
	channelID    int64 = -1009876543210
	privateID    int64 = 4102
)

// openedChat opens chat the way the chat list does — through the real
// switch path — and focuses the composer, ready to type into.
func openedChat(t *testing.T, chat *telegram.Chat) Model {
	t.Helper()
	m := sizedMainModel(t)
	m.store.Chats.Set(chat)
	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: chat.ID})
	m.setFocus(PanelComposer)
	return m
}

// typeText types each rune of text into m as its own key press.
func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m = update(t, m, string(r))
	}
	return m
}

// A typed @ opens completion where there are members to choose between —
// a basic group, a supergroup — and nowhere else. A private chat has one
// person in it, and a broadcast channel's members cannot be mentioned.
func TestMentionCompletionOpensInGroupsOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		chat *telegram.Chat
		want bool
	}{
		{"basic group", &telegram.Chat{ID: basicGroupID, Type: telegram.ChatTypeBasicGroup, Title: "ops"}, true},
		{"supergroup", &telegram.Chat{ID: supergroupID, Type: telegram.ChatTypeSupergroup, Title: "infra"}, true},
		{"private chat", &telegram.Chat{ID: privateID, Type: telegram.ChatTypePrivate, Title: "Nadia"}, false},
		{"channel", &telegram.Chat{ID: channelID, Type: telegram.ChatTypeChannel, Title: "news"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := typeText(t, openedChat(t, tc.chat), "@")
			if got := m.composer.MentionActive(); got != tc.want {
				t.Fatalf("after @ the picker is open = %v, want %v", got, tc.want)
			}
		})
	}
}

// Completion is a property of the chat, so it follows the reader from a
// group into a private chat and back.
func TestMentionCompletionFollowsTheOpenChat(t *testing.T) {
	group := &telegram.Chat{ID: basicGroupID, Type: telegram.ChatTypeBasicGroup, Title: "ops"}
	dm := &telegram.Chat{ID: privateID, Type: telegram.ChatTypePrivate, Title: "Nadia"}

	m := openedChat(t, group)
	m.store.Chats.Set(dm)

	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: privateID})
	m.setFocus(PanelComposer)
	if m = typeText(t, m, "@"); m.composer.MentionActive() {
		t.Fatal("the private chat opened the picker the group had enabled")
	}

	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: basicGroupID})
	m.setFocus(PanelComposer)
	if m = typeText(t, m, "@"); !m.composer.MentionActive() {
		t.Fatal("back in the group, @ did not open the picker")
	}
}

// opsGroup is a basic group, the plainest chat completion works in.
func opsGroup() *telegram.Chat {
	return &telegram.Chat{ID: basicGroupID, Type: telegram.ChatTypeBasicGroup, Title: "ops"}
}

// nadia is a member with a username, so choosing her types it.
func nadia() *telegram.User {
	return &telegram.User{ID: 7, FirstName: "Nadia", LastName: "Feld", Username: "nadia"}
}

// Tab cycles panels from the composer, but while the picker is open it is
// one of the two keys that insert — the picker's hint says so, and the
// composer has to be the one to hear it.
func TestTabInsertsFromAnOpenPicker(t *testing.T) {
	m := openedChat(t, opsGroup())
	m.composer.SetMentionCandidates(basicGroupID, []*telegram.User{nadia()})
	m = typeText(t, m, "@na")
	if !m.composer.MentionActive() {
		t.Fatal("setup: the picker did not open")
	}

	m = update(t, m, "\t")
	if m.focus != PanelComposer {
		t.Fatalf("tab moved focus to %v; it belongs to the picker while it is open", m.focus)
	}
	if got := m.composer.Draft(); got != "@nadia " {
		t.Fatalf("draft = %q, want the chosen member inserted", got)
	}
}

// And with the picker closed Tab is panel cycling again — even with the
// half-typed @ still in the draft.
func TestTabCyclesPanelsOnceThePickerCloses(t *testing.T) {
	m := openedChat(t, opsGroup())
	m.composer.SetMentionCandidates(basicGroupID, []*telegram.User{nadia()})
	m = typeText(t, m, "@na")
	m = update(t, m, "\x1b") // esc closes the picker, and only that
	if m.composer.MentionActive() || m.focus != PanelComposer {
		t.Fatalf("setup: esc left picker open = %v, focus %v", m.composer.MentionActive(), m.focus)
	}

	m = update(t, m, "\t")
	if m.focus == PanelComposer {
		t.Fatal("tab with the picker closed did not cycle panels")
	}
	if got := m.composer.Draft(); got != "@na" {
		t.Fatalf("draft = %q, want it untouched", got)
	}
}

// Enter is the picker's too: it inserts, and nothing is sent. A half-typed
// "@na" going to a group is not something to do on the reader's behalf.
func TestEnterInsertsFromAnOpenPickerRatherThanSending(t *testing.T) {
	m := openedChat(t, opsGroup())
	m.composer.SetMentionCandidates(basicGroupID, []*telegram.User{nadia()})
	m = typeText(t, m, "@na")

	m, cmd := updateCmd(t, m, "\r")
	for _, msg := range flattenCmd(cmd) {
		if sent, ok := msg.(composer.MessageSubmittedMsg); ok {
			t.Fatalf("enter sent %q with the picker open", sent.Text)
		}
	}
	if got := m.composer.Draft(); got != "@nadia " {
		t.Fatalf("draft = %q, want the chosen member inserted", got)
	}
}

// said is a text message in chat from sender.
func said(chatID, id int64, sender telegram.MessageSender) *telegram.Message {
	return &telegram.Message{
		ID: id, ChatID: chatID, SenderID: sender,
		Content: &telegram.MessageText{Text: &telegram.FormattedText{Text: "hi"}},
	}
}

func byUser(id int64) telegram.MessageSender { return &telegram.MessageSenderUser{UserID: id} }

// userIDs lists who users are, for a readable failure.
func userIDs(users []*telegram.User) []int64 {
	out := make([]int64, 0, len(users))
	for _, u := range users {
		out = append(out, u.ID)
	}
	return out
}

// The members an @ offers before any search answers are the people who
// have been talking: newest first, each once, and only people — not the
// reader, whom mentioning notifies nobody, and not a channel posting in the
// group, which is not a member at all. A sender the store cannot name has
// nothing to insert, so is left for the server to find.
func TestMentionCandidatesAreTheRecentSenders(t *testing.T) {
	m := sizedMainModel(t)
	m.myUserId = 1
	for _, u := range []*telegram.User{
		{ID: 1, FirstName: "Me"},
		{ID: 2, FirstName: "Ivo"},
		{ID: 3, FirstName: "Sam"},
		{ID: 4, FirstName: "Mira"},
	} {
		m.store.Users.Set(u)
	}
	for i, sender := range []telegram.MessageSender{
		byUser(3),
		byUser(2),
		&telegram.MessageSenderChat{ChatID: channelID},
		byUser(3),
		byUser(1),
		byUser(9), // not in the store
		byUser(4),
	} {
		m.store.Messages.Append(basicGroupID, said(basicGroupID, int64(i+1), sender))
	}

	got := userIDs(m.mentionCandidates(basicGroupID))
	want := []int64{4, 3, 2}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
}

// pickerNames is what the open picker lists, top to bottom, as plain text.
func pickerNames(t *testing.T, m Model) string {
	t.Helper()
	rows, ok := m.composer.MentionPicker(60, 6)
	if !ok {
		t.Fatal("the picker drew nothing")
	}
	return ansi.Strip(strings.Join(rows, "\n"))
}

// The candidates are handed over when the chat opens, so the first @ has
// something to offer at once.
func TestOpeningAGroupOffersItsRecentSenders(t *testing.T) {
	m := sizedMainModel(t)
	m.store.Users.Set(nadia())
	m.store.Messages.Append(basicGroupID, said(basicGroupID, 1, byUser(nadia().ID)))
	m.store.Chats.Set(opsGroup())
	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: basicGroupID})
	m.setFocus(PanelComposer)

	m = typeText(t, m, "@")
	if names := pickerNames(t, m); !strings.Contains(names, "Nadia Feld") {
		t.Fatalf("picker =\n%s\nwant the recent sender offered", names)
	}
}

// Messages keep arriving after the chat opens, and the page that was
// loading when it opened lands later still. Each query hands the composer
// the senders as they are now.
func TestAMentionQueryRefreshesTheCandidates(t *testing.T) {
	m := openedChat(t, opsGroup())
	m.store.Users.Set(nadia())
	m.store.Messages.Append(basicGroupID, said(basicGroupID, 1, byUser(nadia().ID)))

	m, cmd := updateCmd(t, m, "@")
	var asked bool
	for _, msg := range flattenCmd(cmd) {
		if q, ok := msg.(composer.MentionQueryMsg); ok {
			m = send(t, m, q)
			asked = true
		}
	}
	if !asked {
		t.Fatal("setup: @ asked the host nothing")
	}
	if names := pickerNames(t, m); !strings.Contains(names, "Nadia Feld") {
		t.Fatalf("picker =\n%s\nwant the sender who spoke after the chat opened", names)
	}
}

// fakeMembers stands in for the client's member search and records what
// it was asked.
type fakeMembers struct {
	asked []memberSearch
	users []*telegram.User
	err   error
}

type memberSearch struct {
	chatID int64
	query  string
	limit  int
}

func (f *fakeMembers) SearchChatMembers(chatID int64, query string, limit int) ([]*telegram.User, error) {
	f.asked = append(f.asked, memberSearch{chatID, query, limit})
	return f.users, f.err
}

// searchingGroup is an open supergroup whose member search is members, with
// an @ typed: the picker is open, and a search has somebody waiting on it.
// The query the @ sent is not delivered; each test sends its own.
func searchingGroup(t *testing.T, members *fakeMembers) Model {
	t.Helper()
	m := openedChat(t, &telegram.Chat{ID: supergroupID, Type: telegram.ChatTypeSupergroup, Title: "infra"})
	m.members = members
	return pickerOpen(t, m)
}

// pickerOpen types an @ into m and checks that it opened the picker.
func pickerOpen(t *testing.T, m Model) Model {
	t.Helper()
	if m = typeText(t, m, "@"); !m.composer.MentionActive() {
		t.Fatal("setup: @ did not open the picker")
	}
	return m
}

// answersIn collects the answers to the picker a command produced, running
// it the way the runtime would and feeding what it returns back into m —
// the search's result comes back as a message of its own before it is an
// answer. Ticks are not run: a test fires one by sending mentionSearchMsg.
func answersIn(t *testing.T, m Model, cmd tea.Cmd) (Model, []composer.MentionResultsMsg) {
	t.Helper()
	var out []composer.MentionResultsMsg
	for _, msg := range flattenCmd(cmd) {
		switch msg := msg.(type) {
		case composer.MentionResultsMsg:
			out = append(out, msg)
		case mentionMembersMsg:
			next, more := m.Update(msg)
			m = next.(Model)
			var answers []composer.MentionResultsMsg
			m, answers = answersIn(t, m, more)
			out = append(out, answers...)
		}
	}
	return m, out
}

// fire lets q's debounce run out and returns the answers that follow.
func fire(t *testing.T, m Model, q composer.MentionQueryMsg) (Model, []composer.MentionResultsMsg) {
	t.Helper()
	next, cmd := m.Update(mentionSearchMsg{query: q})
	return answersIn(t, next.(Model), cmd)
}

// A query waits out the debounce before it reaches the server, and the tick
// that ends the wait carries the query it waited for.
func TestAMentionQueryIsSearchedAfterTheDebounce(t *testing.T) {
	members := &fakeMembers{}
	m := searchingGroup(t, members)
	q := composer.MentionQueryMsg{ChatID: supergroupID, Anchor: 0, Query: "na", Gen: 3}

	next, cmd := m.Update(q)
	m = next.(Model)
	if len(members.asked) != 0 {
		t.Fatalf("the query reached the server at once: %+v", members.asked)
	}
	var tick mentionSearchMsg
	for _, msg := range flattenCmd(cmd) {
		if msg, ok := msg.(mentionSearchMsg); ok {
			tick = msg
		}
	}
	if tick.query != q {
		t.Fatalf("the debounce fired for %+v, want %+v", tick.query, q)
	}
}

// A picker closed while its query waited out the debounce — Esc, a send, a
// chat switch — is waiting for nothing, and the server is not asked.
func TestAClosedPickersLastQueryIsNotSearched(t *testing.T) {
	members := &fakeMembers{}
	m := searchingGroup(t, members)
	q := composer.MentionQueryMsg{ChatID: supergroupID, Query: "na", Gen: 3}
	m = send(t, m, q)

	m = update(t, m, "\x1b") // esc closes the picker
	if m.composer.MentionActive() {
		t.Fatal("setup: esc did not close the picker")
	}
	if _, answers := fire(t, m, q); len(members.asked) != 0 || len(answers) != 0 {
		t.Fatalf("a closed picker searched %+v and answered %+v", members.asked, answers)
	}
}

// When the debounce runs out on the latest query, the server is asked and
// the composer gets an answer naming the query it answers.
func TestTheLatestMentionQueryIsAnswered(t *testing.T) {
	mira := &telegram.User{ID: 11, FirstName: "Mira"}
	members := &fakeMembers{users: []*telegram.User{mira}}
	m := searchingGroup(t, members)
	q := composer.MentionQueryMsg{ChatID: supergroupID, Anchor: 4, Query: "mi", Gen: 3}
	m = send(t, m, q)

	_, answers := fire(t, m, q)
	if len(members.asked) != 1 || members.asked[0] != (memberSearch{supergroupID, "mi", mentionSearchLimit}) {
		t.Fatalf("searched %+v, want one search for %q", members.asked, "mi")
	}
	if len(answers) != 1 {
		t.Fatalf("got %d answers, want 1", len(answers))
	}
	a := answers[0]
	if a.ChatID != q.ChatID || a.Anchor != q.Anchor || a.Query != q.Query || a.Gen != q.Gen {
		t.Fatalf("answer %+v does not name the query %+v", a, q)
	}
	if len(a.Users) != 1 || a.Users[0] != mira || a.Err != nil {
		t.Fatalf("answer carries %v / %v, want Mira and no error", userIDs(a.Users), a.Err)
	}
}

// A tick whose query has been typed past asks the server nothing: the newer
// query has a tick of its own coming, and that one is answered.
func TestASupersededMentionQueryIsNotSearched(t *testing.T) {
	members := &fakeMembers{}
	m := searchingGroup(t, members)
	older := composer.MentionQueryMsg{ChatID: supergroupID, Query: "n", Gen: 3}
	newer := composer.MentionQueryMsg{ChatID: supergroupID, Query: "na", Gen: 4}
	m = send(t, m, older)
	m = send(t, m, newer)

	m, answers := fire(t, m, older)
	if len(members.asked) != 0 || len(answers) != 0 {
		t.Fatalf("the superseded tick searched %+v and answered %+v", members.asked, answers)
	}
	if _, answers = fire(t, m, newer); len(answers) != 1 || answers[0].Gen != newer.Gen {
		t.Fatalf("the newest query got %+v, want its own answer", answers)
	}
}

// Nobody matching is an answer too. Without it the picker would say
// "searching…" for as long as it stayed open.
func TestAnEmptyMemberSearchIsStillAnswered(t *testing.T) {
	m := searchingGroup(t, &fakeMembers{})
	q := composer.MentionQueryMsg{ChatID: supergroupID, Query: "zz", Gen: 3}
	m = send(t, m, q)

	if _, answers := fire(t, m, q); len(answers) != 1 || len(answers[0].Users) != 0 || answers[0].Err != nil {
		t.Fatalf("answers = %+v, want one empty answer", answers)
	}
}

// A failed search says so, so the picker can say so under what it has.
func TestAFailedMemberSearchIsCarriedToThePicker(t *testing.T) {
	boom := errors.New("FLOOD_WAIT_7")
	m := searchingGroup(t, &fakeMembers{err: boom})
	q := composer.MentionQueryMsg{ChatID: supergroupID, Query: "na", Gen: 3}
	m = send(t, m, q)

	if _, answers := fire(t, m, q); len(answers) != 1 || !errors.Is(answers[0].Err, boom) {
		t.Fatalf("answers = %+v, want the error carried", answers)
	}
}

// With no client there is nobody to ask, and the question is still
// answered — empty — rather than left to spin.
func TestAMentionQueryWithoutAClientIsAnsweredEmpty(t *testing.T) {
	m := searchingGroup(t, nil)
	m.members = nil
	q := composer.MentionQueryMsg{ChatID: supergroupID, Query: "na", Gen: 3}
	m = send(t, m, q)

	if _, answers := fire(t, m, q); len(answers) != 1 || len(answers[0].Users) != 0 || answers[0].Err != nil {
		t.Fatalf("answers = %+v, want one empty answer", answers)
	}
}

// Whoever the search turns up is a user this client now knows, for the
// thread's sender names as much as for the picker — and a member the next
// completion in this chat can offer before its own search answers.
func TestSearchedMembersAreKeptAndOfferedAgain(t *testing.T) {
	mira := &telegram.User{ID: 11, FirstName: "Mira", Username: "mira"}
	m := searchingGroup(t, &fakeMembers{users: []*telegram.User{mira}})
	q := composer.MentionQueryMsg{ChatID: supergroupID, Query: "mi", Gen: 3}
	m = send(t, m, q)
	m, _ = fire(t, m, q)

	if u, ok := m.store.Users.Get(mira.ID); !ok || u.Username != "mira" {
		t.Fatalf("store has %+v, want the searched member", u)
	}
	if got := userIDs(m.mentionCandidates(supergroupID)); len(got) != 1 || got[0] != mira.ID {
		t.Fatalf("candidates = %v, want the searched member offered", got)
	}
}

// End to end: an @ typed in a supergroup, the debounce, the search, and the
// member it found on the picker.
func TestTheSearchedMemberReachesThePicker(t *testing.T) {
	mira := &telegram.User{ID: 11, FirstName: "Mira", LastName: "Okonkwo"}
	m := openedChat(t, &telegram.Chat{ID: supergroupID, Type: telegram.ChatTypeSupergroup, Title: "infra"})
	m.members = &fakeMembers{users: []*telegram.User{mira}}

	m, cmd := updateCmd(t, m, "@")
	for _, msg := range flattenCmd(cmd) {
		q, ok := msg.(composer.MentionQueryMsg)
		if !ok {
			continue
		}
		m = send(t, m, q)
		var answers []composer.MentionResultsMsg
		m, answers = fire(t, m, q)
		for _, a := range answers {
			m = send(t, m, a)
		}
	}
	if names := pickerNames(t, m); !strings.Contains(names, "Mira Okonkwo") {
		t.Fatalf("picker =\n%s\nwant the member the search found", names)
	}
}

// ivo is a member without a username, mentioned by name.
func ivo() *telegram.User { return &telegram.User{ID: 8, FirstName: "Ivo"} }

// basicGroup is the open ops group, its member list behind members, with the
// picker open as in searchingGroup.
func basicGroup(t *testing.T, members *fakeMembers) Model {
	t.Helper()
	m := openedChat(t, opsGroup())
	m.members = members
	return pickerOpen(t, m)
}

// A basic group hands over its whole member list, so it is fetched once, on
// the first query, and every query after it is answered from it at once —
// no debounce, no second request. The composer filters it.
func TestABasicGroupFetchesItsMembersOnce(t *testing.T) {
	members := &fakeMembers{users: []*telegram.User{nadia(), ivo()}}
	m := basicGroup(t, members)
	first := composer.MentionQueryMsg{ChatID: basicGroupID, Query: "", Gen: 1}
	m = send(t, m, first)
	m, answers := fire(t, m, first)
	if len(members.asked) != 1 || len(answers) != 1 || len(answers[0].Users) != 2 {
		t.Fatalf("first query: searched %+v, answered %+v; want one fetch answering with both", members.asked, answers)
	}

	next := composer.MentionQueryMsg{ChatID: basicGroupID, Query: "iv", Gen: 2}
	updated, cmd := m.Update(next)
	_, answers = answersIn(t, updated.(Model), cmd)
	if len(members.asked) != 1 {
		t.Fatalf("the list was fetched again: %+v", members.asked)
	}
	if len(answers) != 1 || answers[0].Gen != next.Gen || len(answers[0].Users) != 2 {
		t.Fatalf("answers = %+v, want the known list, at once, for the new query", answers)
	}
}

// Typing on while the list is on its way asks for it once, not once per
// letter — and the list, when it lands, answers what is being typed now
// rather than the question that sent for it.
func TestABasicGroupListOnItsWayAnswersTheNewestQuery(t *testing.T) {
	members := &fakeMembers{users: []*telegram.User{nadia(), ivo()}}
	m := basicGroup(t, members)
	first := composer.MentionQueryMsg{ChatID: basicGroupID, Query: "", Gen: 1}
	newer := composer.MentionQueryMsg{ChatID: basicGroupID, Query: "i", Gen: 2}

	m = send(t, m, first)
	updated, fetch := m.Update(mentionSearchMsg{query: first})
	m = send(t, updated.(Model), newer)
	m, early := fire(t, m, newer)
	if len(early) != 0 {
		t.Fatalf("answered %+v before the list landed", early)
	}

	_, answers := answersIn(t, m, fetch)
	if len(members.asked) != 1 {
		t.Fatalf("searched %+v, want the one fetch", members.asked)
	}
	if len(answers) != 1 || answers[0].Gen != newer.Gen || answers[0].Query != "i" {
		t.Fatalf("answers = %+v, want the list answering the newest query", answers)
	}
}

// A fetch that failed has fetched nothing: the next query asks again rather
// than waiting on a list that is not coming.
func TestAFailedBasicGroupFetchIsAskedAgain(t *testing.T) {
	members := &fakeMembers{err: errors.New("CHAT_ADMIN_REQUIRED")}
	m := basicGroup(t, members)
	first := composer.MentionQueryMsg{ChatID: basicGroupID, Query: "", Gen: 1}
	m = send(t, m, first)
	m, answers := fire(t, m, first)
	if len(answers) != 1 || answers[0].Err == nil {
		t.Fatalf("answers = %+v, want the failure carried", answers)
	}

	members.err, members.users = nil, []*telegram.User{nadia()}
	again := composer.MentionQueryMsg{ChatID: basicGroupID, Query: "n", Gen: 2}
	m = send(t, m, again)
	if _, answers = fire(t, m, again); len(members.asked) != 2 || len(answers) != 1 || len(answers[0].Users) != 1 {
		t.Fatalf("searched %+v, answered %+v; want a second fetch answering", members.asked, answers)
	}
}

// fiveMembers fill the picker's five rows.
func fiveMembers() []*telegram.User {
	return []*telegram.User{
		nadia(), ivo(),
		{ID: 9, FirstName: "Sam", Username: "sam"},
		{ID: 10, FirstName: "Mira"},
		{ID: 11, FirstName: "Jonas", Username: "jonas"},
	}
}

// pickerOpenAndClosed is ops open at w×h with an @ typed and the picker
// up, and the same model after Esc has closed it — the draft identical, so
// any difference between the two frames is the picker's.
func pickerOpenAndClosed(t *testing.T, w, h int) (open, closed Model) {
	t.Helper()
	m := openedChat(t, opsGroup())
	m = send(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	m.composer.SetMentionCandidates(basicGroupID, fiveMembers())
	open = typeText(t, m, "@")
	if !open.composer.MentionActive() {
		t.Fatal("setup: the picker did not open")
	}
	closed = update(t, open, "\x1b")
	if closed.composer.MentionActive() || closed.composer.Draft() != "@" {
		t.Fatal("setup: esc did not just close the picker")
	}
	return open, closed
}

// frameRows is a model's screen, one string per row.
func frameRows(m Model) []string {
	return strings.Split(strings.TrimSuffix(m.View().Content, "\n"), "\n")
}

// The picker is painted over the bottom of the thread, directly above the
// composer, and over nothing else: the thread above it, the composer under
// it and every other column are exactly the frame without it. That is what
// keeps opening and closing it from moving anything — it takes no rows of
// its own, so neither the composer's height nor the thread's lines change.
func TestTheMentionPickerIsPaintedOverTheThreadsFoot(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
		rows int // the picker's rows at this size
	}{
		{"two panels", 100, 40, 5},
		{"one panel", 60, 24, 5},
		// Five rows: no chrome, one for the composer, four for the thread
		// — its header and three the picker may have.
		{"short terminal", 100, 5, 3},
	} {
		t.Run(size.name, func(t *testing.T) {
			open, closed := pickerOpenAndClosed(t, size.w, size.h)
			l := open.layout
			if closed.layout != l {
				t.Fatalf("the layout moved when the picker closed:\n%+v\n%+v", l, closed.layout)
			}
			want, ok := open.composer.MentionPicker(l.ThreadWidth, mentionPickerRows)
			if !ok {
				t.Fatal("setup: the picker has no rows")
			}
			want = want[:size.rows]

			top := 0
			if l.TopBar {
				top = 1
			}
			first := top + l.ThreadHeight - len(want) // the picker's first screen row
			openRows, closedRows := frameRows(open), frameRows(closed)
			if len(openRows) != size.h || len(closedRows) != size.h {
				t.Fatalf("frames are %d and %d rows, want %d", len(openRows), len(closedRows), size.h)
			}
			for i := range openRows {
				if w := cell.Width(openRows[i]); w != size.w {
					t.Errorf("row %d is %d cells, want %d", i, w, size.w)
				}
				if p := cell.PaintedWidth(openRows[i]); p != size.w {
					t.Errorf("row %d: painted %d of %d cells", i, p, size.w)
				}
				if i >= first && i < first+len(want) {
					got, line := ansi.Strip(openRows[i]), ansi.Strip(want[i-first])
					if !strings.HasSuffix(got, line) {
						t.Errorf("row %d = %q, want the picker's row %q", i, got, line)
					}
					continue
				}
				if openRows[i] != closedRows[i] {
					t.Errorf("row %d changed under the picker:\nopen   %q\nclosed %q",
						i, ansi.Strip(openRows[i]), ansi.Strip(closedRows[i]))
				}
			}
		})
	}
}

// With no row to spare below the thread's header there is no picker, and
// nothing is painted over the header to make room for one.
func TestNoMentionPickerWithoutARowForIt(t *testing.T) {
	open, closed := pickerOpenAndClosed(t, 100, 2)
	if got, want := frameRows(open), frameRows(closed); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("a %d-row thread drew the picker:\n%s", open.layout.ThreadHeight,
			ansi.Strip(strings.Join(got, "\n")))
	}
}

// The picker's keys are on the help card, in both editing keymaps: while it
// is open Enter and Tab insert rather than send and cycle, and the card is
// where a reader goes to find out why.
func TestTheHelpCardDescribesMentionCompletion(t *testing.T) {
	for _, mode := range []composer.EditingMode{composer.ModeEmacs, composer.ModeVi} {
		m := mainModel(t, PanelComposer)
		m.composer.SetEditingMode(mode)
		rows := map[string]string{}
		for _, sec := range m.helpSections() {
			if strings.HasPrefix(sec.Title, "Composer") {
				for _, b := range sec.Bindings {
					rows[b.Keys] += b.Desc
				}
			}
		}
		for _, keys := range []string{"@", "up / down", "enter / tab", "esc"} {
			if !strings.Contains(strings.ToLower(rows[keys]), "complet") &&
				!strings.Contains(strings.ToLower(rows[keys]), "member") {
				t.Errorf("mode %v: no composer row for %q about completion (have %q)", mode, keys, rows[keys])
			}
		}
	}
}
