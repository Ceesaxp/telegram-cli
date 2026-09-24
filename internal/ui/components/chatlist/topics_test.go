package chatlist

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/render"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/sigil"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// topicRowOf draws the list and returns the topic's title row, ANSI
// stripped, as the reader would see it. topicPreviewRowOf is the second
// line of the same cell.
func topicRowOf(t *testing.T, m Model, topicID int64) string {
	t.Helper()
	return topicRow(t, m, topicID, 0)
}

func topicPreviewRowOf(t *testing.T, m Model, topicID int64) string {
	t.Helper()
	return topicRow(t, m, topicID, 1)
}

func topicRow(t *testing.T, m Model, topicID int64, line int) string {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i, it := range m.list.Items {
		if it.ID == fmt.Sprint(topicID) {
			// One header row, then two rows per topic.
			return lines[1+2*i+line]
		}
	}
	t.Fatalf("topic %d has no row", topicID)
	return ""
}

// forumTopics is the little forum every test in this file drills into:
// General, a Jobs topic with something waiting in it, and a closed one.
func forumTopics() []*telegram.Topic {
	return []*telegram.Topic{
		{ID: 1, ChatID: 2, Title: "General", IconColor: topicIconBlue},
		{ID: 7, ChatID: 2, Title: "Jobs", IconColor: topicIconGreen, UnreadCount: 3},
		{ID: 9, ChatID: 2, Title: "Archive", IconColor: topicIconRed, Closed: true},
	}
}

// drilledIn is a loaded chat list that has opened the forum "Go Serbia",
// which is chat 2, and been handed its topics.
func drilledIn(t *testing.T) Model {
	t.Helper()
	m := newLoadedModel(t, "Alice", "Go Serbia")
	m.EnterForum(2, "Go Serbia")
	m.SetTopics(2, forumTopics())
	return m
}

func TestEnteringAForumReplacesTheChatRows(t *testing.T) {
	m := drilledIn(t)

	if got := m.ForumChatID(); got != 2 {
		t.Errorf("ForumChatID() = %d while drilled into chat 2", got)
	}
	want := []string{"General", "Jobs", "Archive"}
	if got := listTitles(m); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want the forum's topics %v", got, want)
	}
}

// The list is empty between the key press and the answer: the fetch is
// asynchronous, and drawing the chats under a forum header would say the
// forum holds them.
func TestEnteringAForumEmptiesTheListUntilTheTopicsArrive(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Go Serbia")

	m.EnterForum(2, "Go Serbia")

	if got := listTitles(m); len(got) != 0 {
		t.Errorf("rows = %v before the topics arrived, want none", got)
	}
}

// The answer to a fetch the reader has already walked away from redraws a
// list they are no longer looking at. It is dropped instead.
func TestTopicsForAnotherForumAreIgnored(t *testing.T) {
	m := drilledIn(t)

	m.SetTopics(3, []*telegram.Topic{{ID: 4, ChatID: 3, Title: "Elsewhere"}})

	want := []string{"General", "Jobs", "Archive"}
	if got := listTitles(m); !slices.Equal(got, want) {
		t.Errorf("rows = %v after topics for another forum, want %v", got, want)
	}
}

func TestTopicsArrivingAfterLeavingAreIgnored(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Go Serbia")
	m.EnterForum(2, "Go Serbia")
	m.LeaveForum()

	m.SetTopics(2, forumTopics())

	want := []string{"Alice", "Go Serbia"}
	if got := listTitles(m); !slices.Equal(got, want) {
		t.Errorf("rows = %v after a late answer, want the chats %v", got, want)
	}
}

func TestLeavingAForumRestoresTheChatRows(t *testing.T) {
	m := drilledIn(t)

	m.LeaveForum()

	if got := m.ForumChatID(); got != 0 {
		t.Errorf("ForumChatID() = %d after leaving, want 0", got)
	}
	want := []string{"Alice", "Go Serbia"}
	if got := listTitles(m); !slices.Equal(got, want) {
		t.Errorf("rows = %v after leaving, want the chats %v", got, want)
	}
}

// The order the server gave is the order the reader sees: it already puts
// pinned topics first, and sorting again here would be a second opinion
// about an ordering that already has one.
func TestTheTopicOrderIsTheOneTheServerGave(t *testing.T) {
	m := newLoadedModel(t, "Go Serbia")
	m.EnterForum(1, "Go Serbia")
	m.SetTopics(1, []*telegram.Topic{
		{ID: 9, ChatID: 1, Title: "Rules", Pinned: true},
		{ID: 1, ChatID: 1, Title: "General"},
		{ID: 7, ChatID: 1, Title: "Jobs"},
	})

	want := []string{"Rules", "General", "Jobs"}
	if got := listTitles(m); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want them in the server's order %v", got, want)
	}
}

// Hidden is only ever set on General, and a hidden General is one the forum
// has chosen not to show. It is absent, not dimmed.
func TestAHiddenTopicIsNotListed(t *testing.T) {
	m := newLoadedModel(t, "Go Serbia")
	m.EnterForum(1, "Go Serbia")
	m.SetTopics(1, []*telegram.Topic{
		{ID: 1, ChatID: 1, Title: "General", Hidden: true},
		{ID: 7, ChatID: 1, Title: "Jobs"},
	})

	want := []string{"Jobs"}
	if got := listTitles(m); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want the hidden General gone %v", got, want)
	}
	if got := m.TotalCount(); got != 1 {
		t.Errorf("TotalCount() = %d, want 1 — a hidden topic is not part of the total either", got)
	}
}

// A topic nobody has posted to yet is a real topic. A list that dropped it
// would make a new topic impossible to reach from the one place topics are
// listed.
func TestATopicWithNoLastMessageStillDrawsARow(t *testing.T) {
	m := drilledIn(t)

	title := topicRowOf(t, m, 1)
	if !strings.Contains(title, "General") {
		t.Errorf("the title row of a topic with no last message = %q", title)
	}
	// Blank but for the selection bar, which runs down both rows of the
	// cursored cell whatever the cell holds.
	if got := strings.TrimSpace(strings.TrimPrefix(topicPreviewRowOf(t, m, 1), "▌")); got != "" {
		t.Errorf("the preview row of a topic with no last message = %q, want it blank", got)
	}
}

// ---------------------------------------------------------------------------
// The topic row
// ---------------------------------------------------------------------------

// A topic's mark is the group's — a topic is part of one, and the sigils
// are a shared vocabulary rather than this component's to extend.
func TestATopicRowIsMarkedWithTheGroupSigil(t *testing.T) {
	m := drilledIn(t)

	row := []rune(topicRowOf(t, m, 7))
	if got := string(row[rowSigilCol]); got != "#" {
		t.Errorf("col %d = %q on a topic row, want the # sigil: %q", rowSigilCol, got, string(row))
	}
}

// Telegram already chose a colour for the topic. Hashing the title instead
// would be the same topic in two colours depending on the client.
func TestATopicSigilTakesItsColourFromTheIconColour(t *testing.T) {
	r := theme.DarkRoles(false)
	m := Model{roles: r}

	for _, tc := range []struct {
		name string
		icon int32
		want lipgloss.Color
	}{
		{"blue", topicIconBlue, r.Blue},
		{"yellow", topicIconYellow, r.Amber},
		{"purple", topicIconPurple, r.Mauve},
		{"green", topicIconGreen, r.Green},
		{"pink", topicIconPink, r.Red},
		{"red", topicIconRed, r.Red},
	} {
		mark, colour := m.topicSigil(&telegram.Topic{IconColor: tc.icon})
		if mark != "#" {
			t.Errorf("%s topic sigil = %q, want #", tc.name, mark)
		}
		if colour != tc.want {
			t.Errorf("%s topic (icon %#x) is colour %q, want %q", tc.name, tc.icon, colour, tc.want)
		}
	}
}

// An icon_color the palette has no answer for is not guessed at: the topic
// takes the colour of the group it lives in.
func TestAnUnknownIconColourFallsBackToTheGroupColour(t *testing.T) {
	r := theme.DarkRoles(false)
	m := Model{roles: r}

	_, group := sigil.For(telegram.ChatTypeSupergroup, false, r)
	if _, colour := m.topicSigil(&telegram.Topic{IconColor: 0x123456}); colour != group {
		t.Errorf("an unmapped icon colour drew %q, want the group's %q", colour, group)
	}
}

// A closed topic cannot be posted to, and says so in the same quiet word a
// muted chat uses — readable on a terminal with no colour at all.
func TestAClosedTopicIsMarkedOnItsTitle(t *testing.T) {
	m := drilledIn(t)

	if got := topicRowOf(t, m, 9); !strings.Contains(got, "Archive closed") {
		t.Errorf("the closed topic's title row = %q, want it marked closed", got)
	}
	if got := topicRowOf(t, m, 7); strings.Contains(got, "closed") {
		t.Errorf("an open topic's title row = %q, want no closed marker", got)
	}
}

// The badge and the @ chip come off the topic's own counts: per-topic read
// state is the whole reason a topic needs a chat of its own.
func TestTheTopicTrailIsDrawnFromTheTopicsCounts(t *testing.T) {
	m := newLoadedModel(t, "Go Serbia")
	m.EnterForum(1, "Go Serbia")
	m.SetTopics(1, []*telegram.Topic{
		{ID: 7, ChatID: 1, Title: "Jobs", UnreadCount: 4, UnreadMentionsCount: 1},
		{ID: 8, ChatID: 1, Title: "Quiet"},
	})

	if got := strings.TrimRight(topicPreviewRowOf(t, m, 7), " "); !strings.HasSuffix(got, "@ [4]") {
		t.Errorf("row two of an unread topic = %q, want it to end in the @ chip and the badge", got)
	}
	if got := topicPreviewRowOf(t, m, 8); strings.ContainsAny(got, "@[") {
		t.Errorf("row two of a read topic = %q, want no chips", got)
	}
}

// The preview says who spoke, as a group's row does: a forum is a
// supergroup, so every topic has several speakers.
func TestATopicPreviewNamesTheSpeaker(t *testing.T) {
	m := newLoadedModel(t, "Go Serbia")
	m.store.Users.Set(&telegram.User{ID: 42, FirstName: "milos"})
	m.EnterForum(1, "Go Serbia")
	m.SetTopics(1, []*telegram.Topic{{
		ID:    7,
		Title: "Jobs",
		LastMessage: &telegram.Message{
			Date:     1,
			SenderID: &telegram.MessageSenderUser{UserID: 42},
			Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "remote Go role"}},
		},
	}})

	if got := topicPreviewRowOf(t, m, 7); !strings.Contains(got, "milos: remote Go role") {
		t.Errorf("row two = %q, want the speaker's name before the text", got)
	}
}

// ---------------------------------------------------------------------------
// The header row
// ---------------------------------------------------------------------------

// headerRowOf draws the list and returns row 0, ANSI stripped.
func headerRowOf(m Model) string {
	return strings.Split(ansi.Strip(m.View()), "\n")[0]
}

// Drilled in, row 0 stops being the filter line and says where the reader
// is, with the way back in front of it.
func TestTheHeaderNamesTheForumWhileDrilledIn(t *testing.T) {
	m := drilledIn(t)

	header := headerRowOf(m)
	if !strings.HasPrefix(header, " ‹ Go Serbia") {
		t.Errorf("header = %q, want the back glyph and the forum's name", header)
	}
	if !strings.Contains(header, "3/3") {
		t.Errorf("header = %q, want the shown/total count kept", header)
	}
}

// Still one row. The list's height budget and ClickAt's arithmetic are both
// derived from it, so a header that grew by a row while drilled in would
// slide every row under the cursor by one.
func TestTheForumHeaderIsStillOneRow(t *testing.T) {
	m := drilledIn(t)

	if got := m.headerHeight(); got != 1 {
		t.Errorf("headerHeight() = %d while drilled in, want 1", got)
	}
}

// The filter still works, over topic titles, and the row carries both: you
// cannot type into a field you cannot see, and a query with no forum name
// beside it loses the only thing saying which list is being narrowed.
func TestTheForumHeaderShowsTheForumAndTheQueryTogether(t *testing.T) {
	m := drilledIn(t)

	m.OpenFilter()
	m = typeFilter(m, "job")

	header := headerRowOf(m)
	if !strings.Contains(header, "Go Serbia") {
		t.Errorf("header = %q while filtering, want the forum's name still there", header)
	}
	if !strings.Contains(header, "/ job") {
		t.Errorf("header = %q while filtering, want the query visible", header)
	}
	if got := listTitles(m); !slices.Equal(got, []string{"Jobs"}) {
		t.Errorf("rows = %v with the query %q, want only the matching topic", got, "job")
	}
	if !strings.Contains(header, "1/3") {
		t.Errorf("header = %q, want 1 of 3 topics shown", header)
	}
}

// Under pressure the NAME gives way, not the query: the reader pressed
// enter on that forum a moment ago, and the query is live text they are
// still editing.
func TestTheForumNameGivesWayToTheQuery(t *testing.T) {
	m := newLoadedModel(t, "Alice")
	m.EnterForum(2, "A forum with a very long name indeed")
	m.SetTopics(2, forumTopics())
	m.OpenFilter()
	m = typeFilter(m, "general")

	header := headerRowOf(m)
	if !strings.Contains(header, "/ general") {
		t.Errorf("header = %q, want the whole query kept", header)
	}
	if got := cell.Width(header); got != 40 {
		t.Errorf("header is %d cells, want the panel's 40: %q", got, header)
	}
}

// ---------------------------------------------------------------------------
// The keys
// ---------------------------------------------------------------------------

// press runs one key through Update and collects the messages the command
// it returned produced, so a test can assert on what the component SAID
// rather than on a tea.Cmd it cannot read.
func press(t *testing.T, m Model, k tea.KeyPressMsg) (Model, []tea.Msg) {
	t.Helper()
	m, cmd := m.Update(k)

	var msgs []tea.Msg
	walk(cmd, func(msg tea.Msg) { msgs = append(msgs, msg) })
	return m, msgs
}

// selectedTopic is the message the given key produced, or nil.
func selectedTopic(msgs []tea.Msg) *telegram.Topic {
	for _, msg := range msgs {
		if sel, ok := msg.(TopicSelectedMsg); ok {
			return sel.Topic
		}
	}
	return nil
}

// Enter on a topic row hands back the topic itself. It deliberately does
// NOT name a chat: the ID a topic is opened under is the app's to work out.
func TestEnterOnATopicEmitsTheTopic(t *testing.T) {
	m := drilledIn(t)
	m, _ = m.Update(key('j')) // off General, onto Jobs

	m, msgs := press(t, m, specialKey(tea.KeyEnter))

	topic := selectedTopic(msgs)
	if topic == nil {
		t.Fatalf("enter on a topic row produced %v, want a TopicSelectedMsg", msgs)
	}
	if topic.ID != 7 || topic.Title != "Jobs" {
		t.Errorf("TopicSelectedMsg carried topic %d %q, want 7 \"Jobs\"", topic.ID, topic.Title)
	}
	for _, msg := range msgs {
		if _, ok := msg.(ChatSelectedMsg); ok {
			t.Error("enter on a topic row also emitted a ChatSelectedMsg")
		}
	}
}

// The chats are untouched: a topic row is the only row that answers with a
// topic.
func TestEnterOnAChatRowStillEmitsTheChat(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Bob")

	m, msgs := press(t, m, specialKey(tea.KeyEnter))

	var got []int64
	for _, msg := range msgs {
		if sel, ok := msg.(ChatSelectedMsg); ok {
			got = append(got, sel.ChatId)
		}
	}
	if !slices.Equal(got, []int64{1}) {
		t.Errorf("enter on a chat row emitted %v, want a ChatSelectedMsg for chat 1", msgs)
	}
}

// Esc stacks (docs/topics.md, "Resolved"): the filter first, the forum
// second. One key, two things to back out of, in the order they were
// entered.
func TestEscWithAFilterUpClearsItAndStaysInTheForum(t *testing.T) {
	m := drilledIn(t)
	m.OpenFilter()
	m = typeFilter(m, "job")
	m, _ = m.Update(specialKey(tea.KeyEnter)) // close the input, keep the filter

	m, _ = m.Update(specialKey(tea.KeyEscape))

	if m.FilterQuery() != "" {
		t.Errorf("FilterQuery() = %q after esc, want it cleared", m.FilterQuery())
	}
	if m.ForumChatID() != 2 {
		t.Error("the first esc left the forum as well as clearing the filter")
	}
	want := []string{"General", "Jobs", "Archive"}
	if got := listTitles(m); !slices.Equal(got, want) {
		t.Errorf("rows = %v after esc, want the unfiltered topics %v", got, want)
	}
}

func TestEscWithNoFilterLeavesTheForum(t *testing.T) {
	m := drilledIn(t)

	m, _ = m.Update(specialKey(tea.KeyEscape))

	if m.ForumChatID() != 0 {
		t.Error("esc with no filter did not leave the forum")
	}
	if got := listTitles(m); !slices.Equal(got, []string{"Alice", "Go Serbia"}) {
		t.Errorf("rows = %v after esc, want the chats back", got)
	}
}

// Backspace is bound to nothing in the chat list, so it is always "up" and
// never has to wait its turn behind the filter.
func TestBackspaceLeavesTheForum(t *testing.T) {
	m := drilledIn(t)
	m.OpenFilter()
	m = typeFilter(m, "job")
	m, _ = m.Update(specialKey(tea.KeyEnter))

	m, _ = m.Update(specialKey(tea.KeyBackspace))

	if m.ForumChatID() != 0 {
		t.Error("backspace did not leave the forum")
	}
}

// ---------------------------------------------------------------------------
// A topic's read state
// ---------------------------------------------------------------------------

// jobsChatID is the synthetic chat ID the client minted for the Jobs topic.
// Any value from the reserved band will do here; what matters is that it is
// the ID a read mark for that topic arrives under.
const jobsChatID int64 = 1<<52 + 1

// unreadJobs is the forum with one topic that has something waiting in it,
// far enough along that a read mark has somewhere to move to.
func unreadJobs(t *testing.T) Model {
	t.Helper()
	m := newLoadedModel(t, "Alice", "Go Serbia")
	m.EnterForum(2, "Go Serbia")
	m.SetTopics(2, []*telegram.Topic{{
		ID: 7, ChatID: 2, Title: "Jobs", TopicChatID: jobsChatID,
		UnreadCount: 3, TopMessageID: 40, ReadInboxMaxID: 37,
	}})
	return m
}

// Reading a topic clears its badge and moves its read pointer, the way
// [telegram.ChatMarkedReadMsg] does both for a chat in the store. Nothing
// used to take a topic's count down at all: a topic read to the end kept
// every message that had ever arrived in it on the badge, and three more
// arriving while the reader read them made it six.
func TestReadingATopicClearsItsBadgeAndMovesItsPointer(t *testing.T) {
	m := unreadJobs(t)

	m.TopicRead(jobsChatID, 40)

	if got := badgeOf(t, m, 7); got != "" {
		t.Errorf("badge = %q after the topic was read to its newest message, want none", got)
	}
	if got := m.topicByChat(jobsChatID).ReadInboxMaxID; got != 40 {
		t.Errorf("ReadInboxMaxID = %d after the read, want 40", got)
	}
}

// Never backwards. Reads and receipts arrive out of order — a phone-side
// read of three old messages lands after this session's read of the newest
// — and an older mark that moved the pointer back would make messages the
// reader has read count as unread again.
func TestAnOlderReadDoesNotMoveATopicsPointerBack(t *testing.T) {
	m := unreadJobs(t)
	m.TopicRead(jobsChatID, 40)

	m.TopicRead(jobsChatID, 20)

	if got := m.topicByChat(jobsChatID).ReadInboxMaxID; got != 40 {
		t.Errorf("ReadInboxMaxID = %d after an older read, want it left at 40", got)
	}
	if got := badgeOf(t, m, 7); got != "" {
		t.Errorf("badge = %q after an older read, want it left cleared", got)
	}
}

// A read that stops short of the newest message is a partial read: the
// pointer moves, the count stays. It is the chat store's own rule
// (store.MarkReadUpTo), applied to the topic's numbers.
func TestAReadShortOfTheNewestMessageLeavesTheTopicsBadge(t *testing.T) {
	m := unreadJobs(t)

	m.TopicRead(jobsChatID, 38)

	if got := badgeOf(t, m, 7); got != "[3]" {
		t.Errorf("badge = %q after a partial read, want [3]", got)
	}
	if got := m.topicByChat(jobsChatID).ReadInboxMaxID; got != 38 {
		t.Errorf("ReadInboxMaxID = %d after a partial read, want 38", got)
	}
}

// A mark for a chat that is not one of the open forum's topics — an
// ordinary chat being read while the reader stands in a forum — changes
// nothing here.
func TestAReadForAnotherChatLeavesTheTopicsAlone(t *testing.T) {
	m := unreadJobs(t)

	m.TopicRead(1, 40)

	if got := badgeOf(t, m, 7); got != "[3]" {
		t.Errorf("badge = %q after a read of another chat, want [3]", got)
	}
}

// ---------------------------------------------------------------------------
// A topic's last word
// ---------------------------------------------------------------------------

// arrived is a message in a topic as the listener publishes it, under the
// topic's synthetic chat ID.
func arrived(id int64, text string) *telegram.Message {
	return &telegram.Message{
		ID: id, ChatID: jobsChatID, Date: int32(time.Now().Unix()),
		SenderID: &telegram.MessageSenderUser{UserID: 43},
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: text}},
	}
}

// emptyJobs is the forum with one topic nothing has arrived in yet.
func emptyJobs(t *testing.T) Model {
	t.Helper()
	m := newLoadedModel(t, "Alice", "Go Serbia")
	m.EnterForum(2, "Go Serbia")
	m.SetTopics(2, []*telegram.Topic{{
		ID: 7, ChatID: 2, Title: "Jobs", TopicChatID: jobsChatID,
	}})
	m.store.Users.Set(&telegram.User{ID: 43, FirstName: "milos"})
	return m
}

// The preview follows the row's pointer rather than the arrival. A
// reconnect replays messages the client already has, and an older one
// overwriting the preview said the topic's last word was something said
// before the word the row was already showing — while TopMessageID, right
// beside it, correctly refused to move.
func TestAReplayedOlderMessageLeavesATopicsPreview(t *testing.T) {
	m := emptyJobs(t)
	m.TopicMessage(jobsChatID, arrived(40, "remote Go role"))

	m.TopicMessage(jobsChatID, arrived(20, "last month's thread"))

	if got := topicPreviewRowOf(t, m, 7); !strings.Contains(got, "remote Go role") {
		t.Errorf("the Jobs preview = %q after an older message was replayed, "+
			"want the newest message still", got)
	}
}

// And a newer one still takes the row, or the guard above bought its
// steadiness by freezing the preview at the first thing ever said.
func TestANewerMessageTakesATopicsPreview(t *testing.T) {
	m := emptyJobs(t)
	m.TopicMessage(jobsChatID, arrived(40, "remote Go role"))

	m.TopicMessage(jobsChatID, arrived(41, "still open?"))

	if got := topicPreviewRowOf(t, m, 7); !strings.Contains(got, "still open?") {
		t.Errorf("the Jobs preview = %q after a newer message, want that message", got)
	}
}

// Folders are a property of the chat list. A forum's topics are in none of
// them, so the keys that switch tabs do nothing rather than moving a
// selection the reader cannot see.
func TestFolderKeysAreInertInAForum(t *testing.T) {
	m := drilledIn(t)
	m.SetFoldersForTest([]string{"All", "Work", "Family"})
	m.EnterForum(2, "Go Serbia")
	m.SetTopics(2, forumTopics())

	for _, k := range []tea.KeyPressMsg{specialKey(tea.KeyRight), specialKey(tea.KeyLeft), key('3')} {
		before := m.ActiveFolderIndex()
		m, _ = m.Update(k)
		if got := m.ActiveFolderIndex(); got != before {
			t.Errorf("%s moved the folder tab from %d to %d while drilled in", k, before, got)
		}
		if got := listTitles(m); len(got) != 3 {
			t.Errorf("%s changed the rows to %v while drilled in", k, got)
		}
	}
}

// ---------------------------------------------------------------------------
// The panel, whole
// ---------------------------------------------------------------------------

// The drilled-in panel as the reader sees it, against the mock in
// docs/topics.md, "The interaction".
//
// Whole rather than field by field: the pieces are asserted above, and what
// this catches is the thing none of them can — a header, a row and a badge
// that are each correct and do not add up to a column.
func TestTheDrilledInPanelDrawsLikeTheMock(t *testing.T) {
	at := time.Date(2026, 9, 24, 14, 2, 0, 0, time.UTC)
	defer render.PinClock(at)()

	m := newLoadedModel(t, "Alice", "Go Serbia")
	m.SetSize(38, 7)
	m.EnterForum(2, "Go Serbia")
	m.SetTopics(2, []*telegram.Topic{
		{
			ID: 1, ChatID: 2, Title: "General", IconColor: topicIconBlue,
			LastMessage: said(42, "meetup Thursday?", at.Add(-2*time.Minute)),
		},
		{
			ID: 7, ChatID: 2, Title: "Jobs", IconColor: topicIconGreen,
			UnreadCount:         3,
			UnreadMentionsCount: 1,
			LastMessage:         said(43, "remote Go role", at.Add(-22*time.Minute)),
		},
		{ID: 9, ChatID: 2, Title: "Archive", IconColor: topicIconRed, Closed: true},
	})
	m.store.Users.Set(&telegram.User{ID: 42, FirstName: "ana"})
	m.store.Users.Set(&telegram.User{ID: 43, FirstName: "milos"})
	m.refreshList()

	want := []string{
		" ‹ Go Serbia                      3/3 ",
		"▌# General                      2m    ",
		"▌  ana: meetup Thursday?              ",
		" # Jobs                         22m   ",
		"   milos: remote Go role        @ [3] ",
		" # Archive closed                     ",
		"                                      ",
	}

	// The header and the three topics. Anything after them is the list
	// widget padding its column out to the panel's height, which is the
	// same whatever the column holds.
	got := strings.Split(ansi.Strip(m.View()), "\n")[:len(want)]
	if !slices.Equal(got, want) {
		t.Errorf("the drilled-in panel draws\n%s\nwant\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The row renderer is a method VALUE, bound to a copy of the model at
// restyle time, so a topic recorded into the model by value would be
// invisible to the renderer that has to draw it. This is the test that
// would catch that: both a repaint and a topic list installed after the
// binding have to reach the rows.
func TestTheRowsFollowTheForumAcrossARepaint(t *testing.T) {
	m := drilledIn(t)
	m.SetRoles(theme.LightRoles(false))

	m.SetTopics(2, []*telegram.Topic{{ID: 4, ChatID: 2, Title: "Meetups"}})

	if got := topicRowOf(t, m, 4); !strings.Contains(got, "# Meetups") {
		t.Errorf("the row after a repaint = %q, want the topic drawn with its sigil", got)
	}
}

// said is a message from one user at one instant, for the render example.
func said(userID int64, text string, at time.Time) *telegram.Message {
	return &telegram.Message{
		Date:     int32(at.Unix()),
		SenderID: &telegram.MessageSenderUser{UserID: userID},
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: text}},
	}
}

// ---------------------------------------------------------------------------
// A topic ID is not a chat ID
// ---------------------------------------------------------------------------

// Every one of these answers a chat, and a topic's row ID is a MESSAGE ID
// in the forum — topic 7 and chat 7 are unrelated. Answering "chat 7" would
// send a caller somewhere the reader never pointed at, so they answer
// nothing and CursorTopic is the question to ask instead.
func TestNothingAnswersATopicIDAsAChatID(t *testing.T) {
	m := drilledIn(t)
	m.SetSize(40, 20)

	if got := m.CursorChatId(); got != 0 {
		t.Errorf("CursorChatId() = %d over a topic row, want 0", got)
	}
	if got, ok := m.SelectDelta(1); ok || got != 0 {
		t.Errorf("SelectDelta(1) = (%d, %v) in a forum, want (0, false)", got, ok)
	}
	if got, ok := m.ClickAt(1); ok || got != 0 {
		t.Errorf("ClickAt(1) = (%d, %v) in a forum, want (0, false)", got, ok)
	}
	if got, ok := m.OpenCursor(); ok || got != 0 {
		t.Errorf("OpenCursor() = (%d, %v) in a forum, want (0, false)", got, ok)
	}
	if got := m.BufferIndex(7); got != 0 {
		t.Errorf("BufferIndex(7) = %d in a forum, want 0 — topic 7 is not chat 7", got)
	}
	if got := m.ActiveChatId(); got != 0 {
		t.Errorf("ActiveChatId() = %d after moving over topics, want the chat still unopened", got)
	}
}

// The cursor still MOVES; it is only the answer that is withheld. A click
// that selected nothing and left the highlight behind would be a panel that
// ignored the mouse.
func TestAClickInAForumStillMovesTheCursor(t *testing.T) {
	m := drilledIn(t)
	m.SetSize(40, 20)

	m.ClickAt(3) // header, then two rows per topic: the second topic

	topic := m.CursorTopic()
	if topic == nil || topic.ID != 7 {
		t.Errorf("CursorTopic() = %v after clicking the second row, want topic 7", topic)
	}
}

// Paging asks the server for more DIALOGS. There are none to ask for while
// the list is showing topics, and the request would arrive as a chat list
// growing under a forum header.
func TestAForumDoesNotPageTheDialogList(t *testing.T) {
	m := drilledIn(t)

	if m.shouldPageAhead() {
		t.Error("the list asked for another page of dialogs while drilled into a forum")
	}
}

// ---------------------------------------------------------------------------
// What LeaveForum puts back
// ---------------------------------------------------------------------------

// The drill-in is one level of one panel, so coming back up puts the reader
// where they were: the query they had typed over the CHATS, and the chat
// that was under the cursor.
//
// The topic query does not come up with them. It was typed against topic
// titles, and a chat list narrowed by a word about topics would hide rows
// for a reason nothing on screen could explain.
func TestLeavingAForumRestoresTheFilterItInterrupted(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Go Serbia", "Bob")
	m.OpenFilter()
	m = typeFilter(m, "o") // Go Serbia and Bob
	m, _ = m.Update(specialKey(tea.KeyEnter))
	m, _ = m.Update(key('j')) // onto Bob
	m.EnterForum(2, "Go Serbia")
	m.SetTopics(2, forumTopics())

	if got := m.FilterQuery(); got != "" {
		t.Errorf("FilterQuery() = %q inside the forum, want the chat query left behind", got)
	}

	m.LeaveForum()

	if got := m.FilterQuery(); got != "o" {
		t.Errorf("FilterQuery() = %q after leaving, want the chat query %q back", got, "o")
	}
	if got := listTitles(m); !slices.Equal(got, []string{"Go Serbia", "Bob"}) {
		t.Errorf("rows = %v after leaving, want the filtered chats back", got)
	}
	if got := m.CursorChatId(); got != 3 {
		t.Errorf("CursorChatId() = %d after leaving, want Bob (3) still under the cursor", got)
	}
}

// A query typed over the topics is dropped on the way up, not carried into
// the chats.
func TestTheTopicQueryDoesNotFollowTheReaderOut(t *testing.T) {
	m := drilledIn(t)
	m.OpenFilter()
	m = typeFilter(m, "job")
	m, _ = m.Update(specialKey(tea.KeyEnter))

	m.LeaveForum()

	if got := m.FilterQuery(); got != "" {
		t.Errorf("FilterQuery() = %q after leaving, want no filter over the chats", got)
	}
	if got := listTitles(m); !slices.Equal(got, []string{"Alice", "Go Serbia"}) {
		t.Errorf("rows = %v after leaving, want every chat back", got)
	}
}
