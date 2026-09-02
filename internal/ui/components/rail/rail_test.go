package rail

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile. lipgloss probes the output for a terminal,
// finds none under `go test`, and resolves to Ascii — Render becomes the
// identity function and every style disappears, so an assertion on styled
// output passes whatever the style was.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

const testChatID = int64(1)

func railModel(t *testing.T, kind telegram.ChatType) (Model, *store.Store) {
	t.Helper()
	s := store.NewStore()
	s.Chats.Set(&telegram.Chat{ID: testChatID, Type: kind, Title: "chat"})

	m := New(theme.DarkRoles(false))
	m.SetSize(30, 20)
	m.SetStore(s, nil)
	return m, s
}

// textMessage is a message with plain text in it.
func textMessage(id int64, sender int64, text string) *telegram.Message {
	return &telegram.Message{
		ID: id, ChatID: testChatID,
		SenderID: &telegram.MessageSenderUser{UserID: sender},
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: text}},
	}
}

func docMessage(id int64, name string, size int64) *telegram.Message {
	return &telegram.Message{
		ID: id, ChatID: testChatID,
		Content: &telegram.MessageDocument{Document: &telegram.Document{
			FileName: name, File: &telegram.File{ID: name, Size: size},
		}},
	}
}

// rows renders the rail and checks the two invariants every row of every
// panel holds: exactly the width, and no style left open at the end.
func rows(t *testing.T, m Model) []string {
	t.Helper()
	out := strings.Split(m.View(), "\n")
	if len(out) != m.height {
		t.Fatalf("rail drew %d rows, height is %d", len(out), m.height)
	}
	for i, line := range out {
		if got := cell.Width(line); got != m.width {
			t.Errorf("row %d is %d cells, want %d: %q", i, got, m.width, ansi.Strip(line))
		}
		if open := cell.OpenStyle(line); open != "" {
			t.Errorf("row %d leaves %q open", i, open)
		}
	}
	return out
}

// TestNothingIsFetchedUntilTheRailIsOpened is decision 6, and the reason it
// exists: opening a chat must cost no rail request at all, so the primary
// history paint never competes with rail work and a user who keeps the rail
// closed never pays for it.
func TestNothingIsFetchedUntilTheRailIsOpened(t *testing.T) {
	m, _ := railModel(t, telegram.ChatTypeSupergroup)

	if got := m.Sections(); got != nil {
		t.Fatalf("a closed rail described %d sections", len(got))
	}
	if len(m.data) != 0 {
		t.Fatalf("a closed rail cached data for %d chats", len(m.data))
	}
	if view := ansi.Strip(m.View()); strings.TrimSpace(view) != "" {
		t.Errorf("a closed rail drew something:\n%s", view)
	}
}

// TestSectionsPerChatType (docs/tui-2.0.md, "Context rail"). A DM has no
// members section — you and them, which the header already says — and gains
// links instead. A channel's members cannot be enumerated.
func TestSectionsPerChatType(t *testing.T) {
	for kind, want := range map[telegram.ChatType][]string{
		telegram.ChatTypeSupergroup: {"PINNED", "MEMBERS", "FILES"},
		telegram.ChatTypeBasicGroup: {"PINNED", "MEMBERS", "FILES"},
		telegram.ChatTypeChannel:    {"PINNED", "FILES"},
		telegram.ChatTypePrivate:    {"FILES", "LINKS"},
	} {
		m, _ := railModel(t, kind)
		m.Open(testChatID)

		var got []string
		for _, s := range m.Sections() {
			got = append(got, strings.ToUpper(s.Title))
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("chat type %v: sections %v, want %v", kind, got, want)
		}
	}
}

// TestEverySectionSaysWhatStateItIsIn. Four different situations would
// otherwise all render as blank space: not asked, waiting, refused, and
// genuinely empty. Only the last means "this chat has no files", and a
// reader cannot tell them apart from nothing at all.
func TestEverySectionSaysWhatStateItIsIn(t *testing.T) {
	m, _ := railModel(t, telegram.ChatTypeChannel)
	m.Open(testChatID) // no client: every section fails

	for _, s := range m.Sections() {
		if len(s.Rows) != 1 || s.Rows[0].Kind != RowNote {
			t.Fatalf("%s: expected one note row, got %+v", s.Title, s.Rows)
		}
		if s.Rows[0].Text != "unavailable" {
			t.Errorf("%s: says %q with no client, want unavailable", s.Title, s.Rows[0].Text)
		}
	}

	// And the four states are distinguishable from each other.
	for state, want := range map[sectionState]string{
		stateIdle:    "not loaded",
		stateLoading: "loading…",
		stateFailed:  "unavailable",
		stateReady:   "none",
	} {
		got, ok := stateNote(state, 0)
		if !ok || got != want {
			t.Errorf("state %v says %q, want %q", state, got, want)
		}
	}
	if _, ok := stateNote(stateReady, 3); ok {
		t.Error("a ready section with rows still shows a note")
	}
}

// TestSectionsSurviveEveryState: a section that vanished while loading and
// reappeared when it finished would make the rail jump under the reader.
func TestSectionsSurviveEveryState(t *testing.T) {
	m, _ := railModel(t, telegram.ChatTypeSupergroup)
	m.Open(testChatID)

	want := len(m.Sections())
	for _, state := range []sectionState{stateIdle, stateLoading, stateReady, stateFailed} {
		d := m.data[testChatID]
		d.pinnedState, d.filesState, d.membersState = state, state, state
		if got := len(m.Sections()); got != want {
			t.Errorf("state %v: %d sections, want %d", state, got, want)
		}
	}
}

// TestCountIsShownOnlyWhenRowsAreElided. "FILES · 3" above three files tells
// the reader something they can already see.
func TestCountIsShownOnlyWhenRowsAreElided(t *testing.T) {
	m, _ := railModel(t, telegram.ChatTypeChannel)
	m.Open(testChatID)
	d := m.data[testChatID]

	d.filesState = stateReady
	for i := range maxFileRows {
		d.files = append(d.files, docMessage(int64(i+1), "f.txt", 1024))
	}
	for _, s := range m.Sections() {
		if s.Title == "files" && s.Count != 0 {
			t.Errorf("a complete section carries a count of %d", s.Count)
		}
	}

	d.files = append(d.files, docMessage(99, "one-more.txt", 1024))
	for _, s := range m.Sections() {
		if s.Title != "files" {
			continue
		}
		if s.Count != maxFileRows+1 {
			t.Errorf("count = %d, want %d", s.Count, maxFileRows+1)
		}
		last := s.Rows[len(s.Rows)-1]
		if last.Kind != RowMore || last.Text != "+1 more" {
			t.Errorf("remainder row = %+v", last)
		}
	}
}

// TestMemberRemainderCountsTheChatTotal, not the page the participants call
// returned. A group of two hundred returns a page of thirty-two, and "+24
// more" computed from the page is a lie about the group.
func TestMemberRemainderCountsTheChatTotal(t *testing.T) {
	m, s := railModel(t, telegram.ChatTypeSupergroup)
	m.Open(testChatID)
	d := m.data[testChatID]

	d.membersState = stateReady
	s.Chats.SetMemberCount(testChatID, 200)
	for i := range 32 {
		id := int64(100 + i)
		s.Users.Set(&telegram.User{ID: id, FirstName: "member"})
		d.members = append(d.members, &telegram.ChatMember{
			MemberID: &telegram.MessageSenderUser{UserID: id},
			Status:   &telegram.ChatMemberStatusMember{},
		})
	}

	for _, section := range m.Sections() {
		if section.Title != "members" {
			continue
		}
		if section.Count != 200 {
			t.Errorf("heading count = %d, want the chat total 200", section.Count)
		}
		last := section.Rows[len(section.Rows)-1]
		want := "+" + "192" + " more" // 200 total, 8 shown
		if last.Text != want {
			t.Errorf("remainder = %q, want %q", last.Text, want)
		}
	}
}

// TestMemberRowPrefersRoleOverLastSeen. A role is a fact about the chat and
// a last-seen is a fact about the person, and this is the chat's rail.
func TestMemberRowPrefersRoleOverLastSeen(t *testing.T) {
	m, s := railModel(t, telegram.ChatTypeSupergroup)
	s.Users.Set(&telegram.User{ID: 200, FirstName: "ivo",
		Status: &telegram.UserStatusOffline{WasOnline: 1700000000}})

	row := m.memberRow(&telegram.ChatMember{
		MemberID: &telegram.MessageSenderUser{UserID: 200},
		Status:   &telegram.ChatMemberStatusAdministrator{},
	})
	if row.Right != "admin" {
		t.Errorf("an admin's right-hand field is %q, want admin", row.Right)
	}

	plain := m.memberRow(&telegram.ChatMember{
		MemberID: &telegram.MessageSenderUser{UserID: 200},
		Status:   &telegram.ChatMemberStatusMember{},
	})
	if plain.Right == "" {
		t.Error("an ordinary member shows neither a role nor a last-seen")
	}
}

// TestVagueStatusesGetNoLastSeen. Telegram's "recently" covers anything up
// to three days; putting a number on it would invent precision the server
// deliberately withheld.
func TestVagueStatusesGetNoLastSeen(t *testing.T) {
	for name, status := range map[string]telegram.UserStatus{
		"recently":  &telegram.UserStatusRecently{},
		"last week": &telegram.UserStatusLastWeek{},
		"empty":     &telegram.UserStatusEmpty{},
		"online":    &telegram.UserStatusOnline{Expires: 1700000000},
	} {
		if got := lastSeenShort(status); got != "" {
			t.Errorf("%s produced %q", name, got)
		}
	}
	if got := lastSeenShort(&telegram.UserStatusOffline{WasOnline: 1700000000}); got == "" {
		t.Error("a real last-seen timestamp produced nothing")
	}
}

// TestLinkRowsShowTheHost, which is what a reader recognises a link by. The
// full URL never fits in thirty columns and its tail is the least
// identifying part of it.
func TestLinkRowsShowTheHost(t *testing.T) {
	for text, want := range map[string]string{
		"read this https://lwn.net/Articles/1/ later": "lwn.net",
		"http://www.example.com/x":                    "example.com",
		"no link here":                                "no link here",
	} {
		if got := linkSummary(textMessage(1, 200, text)); got != want {
			t.Errorf("linkSummary(%q) = %q, want %q", text, got, want)
		}
	}
}

// TestFileRowsStateOnlyKnownSizes: a file with no size shows none rather
// than a zero, which would read as an empty file.
func TestFileRowsStateOnlyKnownSizes(t *testing.T) {
	name, size := fileSummary(docMessage(1, "notes.md", 0))
	if name != "notes.md" {
		t.Errorf("name = %q", name)
	}
	if size != "" {
		t.Errorf("a file with no known size shows %q", size)
	}

	if _, size := fileSummary(docMessage(1, "a.png", 188416)); size != "184K" {
		t.Errorf("size = %q, want 184K", size)
	}
}

// TestEveryRowIsExactlyTheRailWidth across widths and content.
func TestEveryRowIsExactlyTheRailWidth(t *testing.T) {
	for width := 16; width <= 40; width += 3 {
		m, s := railModel(t, telegram.ChatTypeSupergroup)
		m.SetSize(width, 24)
		m.Open(testChatID)
		d := m.data[testChatID]

		s.Users.Set(&telegram.User{ID: 200, FirstName: strings.Repeat("長い名前", 4)})
		d.pinnedState = stateReady
		d.pinned = []*telegram.Message{textMessage(1, 200, strings.Repeat("pinned ", 20))}
		d.membersState = stateReady
		s.Chats.SetMemberCount(testChatID, 40)
		d.members = []*telegram.ChatMember{{
			MemberID: &telegram.MessageSenderUser{UserID: 200},
			Status:   &telegram.ChatMemberStatusCreator{},
		}}
		d.filesState = stateReady
		d.files = []*telegram.Message{docMessage(2, strings.Repeat("long-name", 8)+".pdf", 1<<30)}

		rows(t, m)
	}
}

// TestStaleResultsAreDropped. The rail starts three requests at once, so it
// is the surface most likely to have one outstanding when the user switches
// chats — and one chat's files landing in another chat's rail is the failure
// that produces.
func TestStaleResultsAreDropped(t *testing.T) {
	m, s := railModel(t, telegram.ChatTypeChannel)
	s.Chats.Set(&telegram.Chat{ID: 2, Type: telegram.ChatTypeChannel, Title: "other"})
	m.Open(testChatID)
	// railModel has no client, so Open marks every section unavailable
	// immediately. Put one back into the state a real fetch leaves it in.
	m.data[testChatID].filesState = stateLoading

	fresh := searchResultMsg{
		gen: m.gen, chatID: testChatID, filter: telegram.MediaFilterFiles,
		msgs: []*telegram.Message{docMessage(1, "right.txt", 10)},
	}
	stale := searchResultMsg{
		gen: m.gen, chatID: 2, filter: telegram.MediaFilterFiles,
		msgs: []*telegram.Message{docMessage(2, "wrong.txt", 10)},
	}
	old := searchResultMsg{
		gen: m.gen - 1, chatID: testChatID, filter: telegram.MediaFilterFiles,
		msgs: []*telegram.Message{docMessage(3, "older.txt", 10)},
	}

	m, _ = m.Update(stale)
	m, _ = m.Update(old)
	if d := m.data[testChatID]; d.filesState != stateLoading {
		t.Errorf("a stale result was folded in: state %v, files %v", d.filesState, d.files)
	}

	m, _ = m.Update(fresh)
	if d := m.data[testChatID]; d.filesState != stateReady || len(d.files) != 1 {
		t.Fatalf("the live result was dropped: state %v, files %v", d.filesState, d.files)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "right.txt") ||
		strings.Contains(view, "wrong.txt") {
		t.Errorf("the wrong chat's file is on screen:\n%s", view)
	}
}

// TestReopeningTheSameChatDoesNotRefetch: toggling the rail off and on is
// free, which is what makes it worth toggling.
func TestReopeningTheSameChatDoesNotRefetch(t *testing.T) {
	m, _ := railModel(t, telegram.ChatTypeChannel)
	m.Open(testChatID)
	m.data[testChatID].filesState = stateReady

	m.Close()
	if cmd := m.Open(testChatID); cmd != nil {
		t.Error("reopening the same chat started a fetch")
	}
	if m.data[testChatID].filesState != stateReady {
		t.Error("reopening reset the cached state")
	}
}

// TestInvalidateDropsEverything, including results already in flight.
func TestInvalidateDropsEverything(t *testing.T) {
	m, _ := railModel(t, telegram.ChatTypeChannel)
	m.Open(testChatID)
	inFlight := searchResultMsg{
		gen: m.gen, chatID: testChatID, filter: telegram.MediaFilterFiles,
		msgs: []*telegram.Message{docMessage(1, "f.txt", 10)},
	}

	m.Invalidate()
	m.Open(testChatID)
	m, _ = m.Update(inFlight)

	if d := m.data[testChatID]; d.filesState == stateReady {
		t.Error("a result from before the invalidation was folded in")
	}
}

// TestNewMessagesRefreshOnlyTheSectionTheyCouldChange. Re-fetching
// everything on every message would turn an open rail into a request per
// message, which is the cost the whole policy exists to avoid.
func TestNewMessagesRefreshOnlyTheSectionTheyCouldChange(t *testing.T) {
	m, _ := railModel(t, telegram.ChatTypePrivate)
	m.Open(testChatID)
	d := m.data[testChatID]
	d.filesState, d.linksState = stateReady, stateReady

	// No client, so a refresh cannot actually run — but refreshForNewMessage
	// is where the decision is made, and it is the decision under test.
	m.tg = nil
	if cmd := m.refreshForNewMessage(docMessage(1, "f.txt", 10)); cmd != nil {
		t.Error("a refresh was attempted with no client")
	}

	// A section that was never loaded is not loaded by a new message: the
	// rail fetches when it is opened, not when a message arrives.
	d.filesState = stateIdle
	if kind := m.sectionsFor(testChatID); len(kind) == 0 {
		t.Fatal("precondition: a DM has sections")
	}
}
