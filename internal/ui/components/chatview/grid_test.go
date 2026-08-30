package chatview

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/render"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// gridModel is a small thread with two other people in it, a message of
// mine that has been read, one that has not, a reply, and an unread run.
// It is the smallest history that exercises every part of the grid.
func gridModel(t *testing.T, width int) Model {
	t.Helper()

	m := newTestModel()
	m.SetSize(width, 20)
	m.myUserId = 100
	m.store.Chats.Set(&telegram.Chat{
		ID:                      testChatID,
		Type:                    telegram.ChatTypeSupergroup,
		Title:                   "infra-oncall",
		LastReadInboxMessageID:  3,
		LastReadOutboxMessageID: 3,
		UnreadCount:             4,
	})
	m.store.Users.Set(&telegram.User{ID: 200, FirstName: "nadia"})
	m.store.Users.Set(&telegram.User{ID: 201, FirstName: "sam"})
	m.chatID = testChatID
	m.chatTitle = "infra-oncall"
	m.unreadAfterID = 3
	m.unreadCount = 4

	add := func(id, sender int64, text string) *telegram.Message {
		msg := textMessage(id, sender, text)
		m.store.Messages.Append(testChatID, msg)
		return msg
	}
	add(1, 200, "Rollout paused — session hits spiked on the auth path.")
	add(2, 201, "The offending query, for the record: select star from sessions")
	add(3, 100, "Confirmed from the queue dashboard. Resuming.")
	add(4, 100, "Sent but not read yet.")
	reply := add(5, 200, "Resumed. Canary at 5%.")
	reply.ReplyToMessageID = 3
	add(6, 201, "Approved. Merging behind the flag.")

	m.resolveUnreadDivider()
	return m
}

// TestThreadGridRendersTheDocumentedLayout is the whole grid in one
// readable block: gutter columns, day and unread dividers, sender names,
// the reply quote, delivery marks, the cursor bar and the typing row.
//
// The clock and day label are substituted rather than written out, because
// they are rendered in the machine's local timezone and the fixture would
// otherwise pass only where it was written.
func TestThreadGridRendersTheDocumentedLayout(t *testing.T) {
	m := gridModel(t, 67)
	m.typing = []int64{200}

	clock := render.FormatClock(fixedDate)
	day := render.FormatDayLabel(fixedDate)
	rule := strings.Repeat("─", 67-cell.Width(" "+day+" ")-1)

	want := []string{
		" # infra-oncall │ group                                   ln 12/12 ",
		" " + day + " " + rule + " ",
		"   " + clock + "         nadia  Rollout paused — session hits spiked on    ",
		"                        the auth path.                             ",
		"   " + clock + "           sam  The offending query, for the record:       ",
		"                        select star from sessions                  ",
		"   " + clock + "           you  Confirmed from the queue dashboard.        ",
		"                        Resuming.  ✓✓                              ",
		"   " + clock + "           you  Sent but not read yet.  ✓                  ",
		" 4 NEW ─────────────────────────────────────────────────────────── ",
		"   " + clock + "         nadia  ↳ you Confirmed from the queue dashboard.… ",
		"                        Resumed. Canary at 5%.                     ",
		" ▌ " + clock + "           sam  Approved. Merging behind the flag.         ",
		"                   ···  nadia is typing…                           ",
	}

	got := strings.Split(ansi.Strip(m.View()), "\n")
	// The view is padded to the panel height; the content is at the bottom,
	// under the header.
	body := append([]string{got[0]}, got[len(got)-(len(want)-1):]...)

	for i := range want {
		if i >= len(body) {
			t.Fatalf("line %d missing; got only %d lines", i, len(body))
		}
		if body[i] != want[i] {
			t.Errorf("line %d:\n want %q\n  got %q", i, want[i], body[i])
		}
	}
}

// TestGridGeometryMatchesTheFixtures pins the gutter arithmetic against the
// thread widths the shipped goldens actually draw at. Two of the five are
// narrow, which is the whole reason the compression rule exists.
func TestGridGeometryMatchesTheFixtures(t *testing.T) {
	cases := []struct {
		fixture string
		width   int
		senderW int
		bodyCol int
		bodyW   int
	}{
		{"frame-80x24", 49, gridSenderNarrow, 20, 28},
		{"frame-120x40", 50, gridSenderNarrow, 20, 29},
		{"frame-100x30", 61, gridSenderWide, 24, 36},
		{"frame-137x29", 67, gridSenderWide, 24, 42},
		{"frame-200x60", 130, gridSenderWide, 24, 105},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			g := gridGeometryFor(tc.width)
			if g.SenderW != tc.senderW || g.BodyCol != tc.bodyCol || g.BodyW != tc.bodyW {
				t.Fatalf("width %d: sender %d body col %d width %d, want %d/%d/%d",
					tc.width, g.SenderW, g.BodyCol, g.BodyW, tc.senderW, tc.bodyCol, tc.bodyW)
			}
		})
	}
}

// TestGridGeometryCompressesExactlyAtTheThreshold: the rule is "compress
// when a wide gutter would leave the body under 32 cells", and the two
// widths either side of that are the ones a refactor gets wrong.
func TestGridGeometryCompressesExactlyAtTheThreshold(t *testing.T) {
	// A wide gutter is 24 cells plus the trailing blank, so 32 cells of
	// body needs 57 columns.
	if g := gridGeometryFor(57); g.SenderW != gridSenderWide {
		t.Fatalf("57 columns leaves a 32-cell body and must stay wide, got sender %d", g.SenderW)
	}
	if g := gridGeometryFor(56); g.SenderW != gridSenderNarrow {
		t.Fatalf("56 columns leaves a 31-cell body and must compress, got sender %d", g.SenderW)
	}
}

// TestEveryGridLineIsExactlyThePaneWidth sweeps widths and content. A row
// wider than its region shears the frame; a row narrower than it leaves a
// hole that the panel behind shows through.
func TestEveryGridLineIsExactlyThePaneWidth(t *testing.T) {
	texts := []string{
		"short",
		strings.Repeat("wrap this sentence several times over ", 4),
		strings.Repeat("x", 300),
		"你好世界🎉こんにちは😀漢字テスト",
		"multi\nline\nbody",
		"",
	}

	for width := 20; width <= 140; width += 7 {
		m := newTestModel()
		m.SetSize(width, 24)
		m.chatID = testChatID
		m.myUserId = 100
		for i, text := range texts {
			m.store.Messages.Append(testChatID, textMessage(int64(i+1), int64(200+i%3), text))
		}
		m.typing = []int64{200}

		for _, line := range strings.Split(ansi.Strip(m.View()), "\n") {
			if got := ansi.StringWidth(line); got != width {
				t.Fatalf("width %d: line is %d cells: %q", width, got, line)
			}
		}
	}
}

// TestSenderColumnIsRightAlignedAndElided: names line up against the body
// column, which is what lets a reader scan down the column for one person.
// A name too long for the field is cut rather than allowed to push the body
// sideways.
func TestSenderColumnIsRightAlignedAndElided(t *testing.T) {
	m := gridModel(t, 67)
	g := gridGeometryFor(67)

	senderField := func(msg *telegram.Message) string {
		line := ansi.Strip(m.gridMessageLines(msg, msg, false)[0])
		field := string([]rune(line)[g.SenderCol : g.SenderCol+g.SenderW])
		if got := ansi.StringWidth(field); got != g.SenderW {
			t.Fatalf("sender field is %d cells, want %d: %q", got, g.SenderW, field)
		}
		return field
	}

	// A short name is padded on the LEFT, so its last letter sits against
	// the body column with every other name in the thread.
	m.store.Users.Set(&telegram.User{ID: 202, FirstName: "ivo"})
	short := senderField(textMessage(7, 202, "hello"))
	if !strings.HasPrefix(short, " ") || strings.HasSuffix(short, " ") {
		t.Fatalf("short name %q is not right-aligned in its field", short)
	}
	if strings.TrimSpace(short) != "ivo" {
		t.Fatalf("sender field is %q, want ivo", short)
	}

	// A long one is cut rather than allowed to push the body sideways.
	m.store.Users.Set(&telegram.User{ID: 203, FirstName: "Bartholomew", LastName: "Fitzgerald"})
	long := senderField(textMessage(8, 203, "hello"))
	if !strings.Contains(long, "…") {
		t.Fatalf("a name longer than the field must be elided, got %q", long)
	}
}

// TestOwnMessagesAreYou: the local user is a pronoun in their own thread,
// not a name — and always the same colour, so "mine" reads at a glance.
func TestOwnMessagesAreYou(t *testing.T) {
	m := gridModel(t, 67)
	g := gridGeometryFor(67)

	mine := textMessage(8, 100, "mine")
	line := ansi.Strip(m.gridMessageLines(mine, mine, false)[0])
	field := strings.TrimSpace(string([]rune(line)[g.SenderCol : g.SenderCol+g.SenderW]))
	if field != "you" {
		t.Fatalf("own sender field is %q, want %q", field, "you")
	}

	name, colour := m.senderFor(mine)
	if name != "you" {
		t.Fatalf("own sender name is %q, want %q", name, "you")
	}
	if colour != m.roles.Green {
		t.Fatalf("own sender colour is %v, want green %v", colour, m.roles.Green)
	}

	// And it is green regardless of what the hash would have said for that
	// user ID: "mine" is a fixed role, not a bucket.
	if colour == theme.SenderColour(m.myUserId, m.roles) && m.roles.Green != theme.SenderColour(m.myUserId, m.roles) {
		t.Fatal("own sender fell through to the hashed palette")
	}
}

// TestSenderColoursAreStableAndSpread: the colour is an identity cue, so it
// must be the same every time for the same person, and consecutive user IDs
// — which colleagues who signed up together actually have — must not all
// land in one bucket.
func TestSenderColoursAreStableAndSpread(t *testing.T) {
	roles := theme.DarkRoles(false)

	for _, id := range []int64{1, 7, 12345, -9, 1 << 40} {
		if theme.SenderColour(id, roles) != theme.SenderColour(id, roles) {
			t.Fatalf("colour for %d is not stable", id)
		}
	}

	seen := map[string]int{}
	for id := int64(1000); id < 1064; id++ {
		seen[string(theme.SenderColour(id, roles))]++
	}
	if len(seen) != 4 {
		t.Fatalf("64 consecutive IDs used %d of 4 colours: %v", len(seen), seen)
	}
	for colour, n := range seen {
		if n < 8 {
			t.Errorf("colour %s got only %d of 64 consecutive IDs", colour, n)
		}
	}
}

// TestDayDividerOnlyAtDayBoundaries. A divider on every message would be
// noise; a divider on none leaves a reader unable to say whether a message
// arrived today or last March.
func TestDayDividerOnlyAtDayBoundaries(t *testing.T) {
	m := newTestModel()
	m.SetSize(60, 20)
	m.chatID = testChatID

	const day = 24 * 60 * 60
	first := textMessage(1, 200, "monday")
	sameDay := textMessage(2, 200, "monday again")
	nextDay := textMessage(3, 200, "tuesday")
	nextDay.Date = fixedDate + day

	if got := m.gridMessageLines(first, nil, false); !isDivider(got[0]) {
		t.Fatalf("the oldest loaded message must carry a day label, got %q", ansi.Strip(got[0]))
	}
	if got := m.gridMessageLines(sameDay, first, false); isDivider(got[0]) {
		t.Fatalf("a message on the same day must not repeat the label, got %q", ansi.Strip(got[0]))
	}
	if got := m.gridMessageLines(nextDay, sameDay, false); !isDivider(got[0]) {
		t.Fatalf("a message on a new day must carry a label, got %q", ansi.Strip(got[0]))
	}
}

func isDivider(line string) bool {
	return strings.Contains(ansi.Strip(line), "─")
}

// TestUnreadDividerStaysWhereTheChatWasOpened is the point of snapshotting
// the marker. Read receipts move the live one as soon as the messages are
// on screen; a divider that followed it would walk down the screen ahead of
// the reader and never show them the boundary they opened the chat to find.
func TestUnreadDividerStaysWhereTheChatWasOpened(t *testing.T) {
	m := gridModel(t, 67)
	if m.unreadFromID != 5 {
		t.Fatalf("expected the divider above message 5 (4 is my own), got %d", m.unreadFromID)
	}

	before := ansi.Strip(m.View())
	if !strings.Contains(before, "4 NEW") {
		t.Fatalf("expected an unread divider:\n%s", before)
	}

	// The peer reads everything: the live marker moves to the newest.
	entry, _ := m.store.Chats.Get(testChatID)
	entry.Chat.LastReadInboxMessageID = 6
	entry.UnreadCount = 0

	if got := ansi.Strip(m.View()); got != before {
		t.Fatalf("the divider moved when the read marker did:\n%s", got)
	}
}

// TestDeliveryMarksComeFromTheReadMarker. The bubble renderer this replaces
// drew two checks on every sent message, which told the user their message
// had been read whenever it had merely been sent.
func TestDeliveryMarksComeFromTheReadMarker(t *testing.T) {
	m := gridModel(t, 67)

	read := m.store.Messages.Get(testChatID)[2]   // ID 3, at the read marker
	unread := m.store.Messages.Get(testChatID)[3] // ID 4, past it
	theirs := m.store.Messages.Get(testChatID)[0] // not mine at all

	if got := m.sendStateFor(read); got != sendRead {
		t.Errorf("message at the read marker: state %v, want read", got)
	}
	if got := m.sendStateFor(unread); got != sendSent {
		t.Errorf("message past the read marker: state %v, want sent", got)
	}
	if got := m.sendStateFor(theirs); got != sendNone {
		t.Errorf("incoming message: state %v, want none", got)
	}

	pending := textMessage(0, 100, "not sent yet")
	if got := m.sendStateFor(pending); got != sendPending {
		t.Errorf("unconfirmed send: state %v, want pending", got)
	}
}

// TestDeliveryMarkNeverTruncatesTheMessage: when the last line is already
// full, the mark takes a line of its own rather than eating the text it is
// annotating.
func TestDeliveryMarkNeverTruncatesTheMessage(t *testing.T) {
	m := gridModel(t, 67)
	g := gridGeometryFor(67)

	text := strings.Repeat("a", g.BodyW) // exactly fills one body line
	msg := textMessage(9, 100, text)

	lines := m.gridMessageLines(msg, msg, false)
	joined := ansi.Strip(strings.Join(lines, ""))
	if got := strings.Count(joined, "a"); got != g.BodyW {
		t.Fatalf("expected all %d characters to survive, found %d", g.BodyW, got)
	}
	if !strings.Contains(joined, "✓") {
		t.Fatalf("expected a delivery mark somewhere in %q", joined)
	}
}

// TestCursorBarMarksExactlyOneMessage. The bar is the only thing on the
// screen saying which message r, e and d will act on.
func TestCursorBarMarksExactlyOneMessage(t *testing.T) {
	m := gridModel(t, 67)
	view := ansi.Strip(m.View())

	bars := strings.Count(view, "▌")
	if bars != 1 {
		t.Fatalf("expected exactly one cursor bar, found %d:\n%s", bars, view)
	}

	cursor := m.cursorMessage()
	if cursor == nil {
		t.Fatal("expected a cursor")
	}
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(line, "▌") {
			continue
		}
		if !strings.Contains(line, "Approved") {
			t.Fatalf("the bar is not on the cursor message %d: %q", cursor.ID, line)
		}
	}
}

// TestReplyQuoteIsOneBodyAlignedRow. A reply is context for the message
// under it; a quote that can grow taller than its own reply inverts that.
func TestReplyQuoteIsOneBodyAlignedRow(t *testing.T) {
	m := gridModel(t, 67)
	g := gridGeometryFor(67)

	reply := m.store.Messages.Get(testChatID)[4] // ID 5, replies to 3
	lines := m.gridMessageLines(reply, nil, false)

	quote := ""
	for _, line := range lines {
		if strings.Contains(line, "↳") {
			if quote != "" {
				t.Fatalf("expected a single quote row, found another: %q", ansi.Strip(line))
			}
			quote = ansi.Strip(line)
		}
	}
	if !strings.Contains(quote, "↳ you") {
		t.Fatalf("expected the quote to name the sender it cites: %q", quote)
	}
	if idx := strings.Index(quote, "↳"); idx != g.BodyCol {
		t.Fatalf("quote starts at column %d, body column is %d", idx, g.BodyCol)
	}
	if got := ansi.StringWidth(quote); got != 67 {
		t.Fatalf("quote row is %d cells, want 67", got)
	}
}

// TestReplyToUnloadedMessageSaysSo: an ID means nothing to a reader, so an
// unresolvable quote describes the relationship instead of showing one.
func TestReplyToUnloadedMessageSaysSo(t *testing.T) {
	m := gridModel(t, 67)

	msg := textMessage(10, 200, "answering something older")
	msg.ReplyToMessageID = 99999

	quote := ansi.Strip(m.gridReplyRow(msg, gridGeometryFor(67)))
	if !strings.Contains(quote, "earlier message") {
		t.Fatalf("expected an honest placeholder, got %q", quote)
	}
	if strings.Contains(quote, "99999") {
		t.Fatalf("a message ID is not something a reader can use: %q", quote)
	}
}

// TestTypingRowAlignsWithTheGrid: its marker sits in the sender column, so
// the row lines up with the messages above it rather than shifting the grid
// sideways for as long as someone is composing.
func TestTypingRowAlignsWithTheGrid(t *testing.T) {
	m := gridModel(t, 67)
	g := gridGeometryFor(67)

	if got := m.gridTypingRow(); got != "" {
		t.Fatalf("expected no typing row when nobody is typing, got %q", got)
	}

	m.typing = []int64{200}
	row := ansi.Strip(m.gridTypingRow())
	if idx := strings.Index(row, "···"); idx+3 != g.BodyCol-gridFieldGap {
		t.Fatalf("typing marker ends at column %d, sender column ends at %d",
			idx+3, g.BodyCol-gridFieldGap)
	}
	if !strings.Contains(row, "nadia is typing") {
		t.Fatalf("expected the typist named: %q", row)
	}
	if got := ansi.StringWidth(row); got != 67 {
		t.Fatalf("typing row is %d cells, want 67", got)
	}

	m.typing = []int64{200, 201}
	if row := ansi.Strip(m.gridTypingRow()); !strings.Contains(row, "are typing") {
		t.Fatalf("expected a plural verb for two typists: %q", row)
	}
}

// TestChatActionsTrackWhoIsTyping, including the stop: a set that only ever
// grows would leave a phantom typist on screen for the rest of the session.
func TestChatActionsTrackWhoIsTyping(t *testing.T) {
	m := gridModel(t, 67)

	typing := func(user int64) telegram.ChatActionMsg {
		return telegram.ChatActionMsg{ChatId: testChatID, UserId: user, Action: &telegram.ChatActionTyping{}}
	}
	stopped := func(user int64) telegram.ChatActionMsg {
		return telegram.ChatActionMsg{ChatId: testChatID, UserId: user, Action: &telegram.ChatActionCancel{}}
	}

	m, _ = m.Update(typing(200))
	m, _ = m.Update(typing(200)) // repeats must not double up
	m, _ = m.Update(typing(201))
	if got := len(m.typing); got != 2 {
		t.Fatalf("expected two typists, got %d: %v", got, m.typing)
	}

	m, _ = m.Update(stopped(200))
	if len(m.typing) != 1 || m.typing[0] != 201 {
		t.Fatalf("expected only 201 still typing, got %v", m.typing)
	}

	// Another chat's actions are not this thread's business.
	m, _ = m.Update(telegram.ChatActionMsg{ChatId: testChatID + 1, UserId: 500, Action: &telegram.ChatActionTyping{}})
	if len(m.typing) != 1 {
		t.Fatalf("another chat's typist leaked in: %v", m.typing)
	}
}

// TestHeaderKeepsThePositionWhenTheTitleIsLong. "Where am I in this
// history" is fixed-width and always true; the subtitle is the part a
// reader can lose. Budgeting the left side first would drop the position
// off a narrow pane, which is the one cell you cannot do without.
func TestHeaderKeepsThePositionWhenTheTitleIsLong(t *testing.T) {
	for _, width := range []int{30, 40, 49, 67, 130} {
		m := gridModel(t, width)
		m.chatTitle = strings.Repeat("a very long chat title ", 10)

		header := ansi.Strip(strings.Split(m.View(), "\n")[0])
		if got := ansi.StringWidth(header); got != width {
			t.Fatalf("width %d: header is %d cells: %q", width, got, header)
		}
		if !strings.Contains(header, "ln ") {
			t.Fatalf("width %d: header dropped the position: %q", width, header)
		}
	}
}

// TestHeaderShowsATransientOverTheStandingSubtitle: a media status or a
// search position is what just changed, and the chat's kind is still true a
// second later.
func TestHeaderShowsATransientOverTheStandingSubtitle(t *testing.T) {
	m := gridModel(t, 67)

	if header := ansi.Strip(strings.Split(m.View(), "\n")[0]); !strings.Contains(header, "group") {
		t.Fatalf("expected the chat kind as the standing subtitle: %q", header)
	}

	m.notice = "match 3/7"
	header := ansi.Strip(strings.Split(m.View(), "\n")[0])
	if !strings.Contains(header, "match 3/7") {
		t.Fatalf("expected the notice in the subtitle: %q", header)
	}
	if strings.Contains(header, "group") {
		t.Fatalf("expected the notice to replace the standing subtitle: %q", header)
	}
}

// TestHeaderPositionCountsLines, not messages: lines are what the scroll
// position actually is. A message count would jump by one while the screen
// moved by twenty.
func TestHeaderPositionCountsLines(t *testing.T) {
	m := gridModel(t, 67)
	m.SetSize(67, 8) // short enough that there is somewhere to scroll to
	total := totalRenderedLines(m.lineCounts())

	if got, want := m.headerPosition(), fmt.Sprintf("ln %d/%d", total, total); got != want {
		t.Fatalf("at the bottom: %q, want %q", got, want)
	}

	m.ScrollByLines(4)
	if got, want := m.headerPosition(), fmt.Sprintf("ln %d/%d", total-4, total); got != want {
		t.Fatalf("after scrolling four lines: %q, want %q", got, want)
	}
}
