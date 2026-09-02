package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/imtaqin/telegram-cli/internal/render"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/composer"
	"github.com/imtaqin/telegram-cli/internal/ui/components/topbar"
)

// The world the goldens draw.
//
// docs/fixtures holds cell-exact renderings of a working session: nine
// chats, an incident thread with a code block, a photo, reactions and an
// unread run, somebody typing, and a reply half-written in the composer.
// Byte equality against them means building that session here, once, and
// rendering it at each fixture's size.
//
// Everything is FIXED. The clock is pinned, the sender IDs are constants,
// and nothing is derived from the machine the test runs on — a frame that
// renders differently on a Tuesday is a frame no fixture can hold.

// sceneNow is the instant every golden frame is rendered at: the 21:04 the
// fixtures' top bar shows, on the Friday that makes the thread's earlier
// block fall on the "TUE 26 AUG" its day divider names.
var sceneNow = time.Date(2025, time.August, 29, 21, 4, 0, 0, time.UTC)

// Sender IDs. The local user is sceneYou; the rest are the names the
// fixtures put in the sender column.
const (
	sceneYou   int64 = 900
	sceneNadia int64 = 901
	sceneIvo   int64 = 902
	sceneSam   int64 = 903
	sceneMira  int64 = 904
	sceneBot   int64 = 905
	sceneJonas int64 = 906
	sceneTape  int64 = 907
)

// Chat IDs, in the order the fixtures list them.
const (
	sceneInfra    int64 = -1001
	sceneNadiaDM  int64 = 901
	sceneRelay    int64 = -1002
	sceneWire     int64 = sceneYou // Saved Messages: the ~ row
	sceneOps      int64 = -1004
	sceneMiraDM   int64 = 904
	sceneDesign   int64 = -1005
	sceneTapeChan int64 = -1006
	sceneJonasDM  int64 = 906
)

// sceneAt is a time on the fixtures' day, as a unix timestamp.
func sceneAt(hour, minute int) int32 { return sceneOn(29, hour, minute) }

// sceneOn is a time on any August 2025 day, for the thread's earlier block.
func sceneOn(day, hour, minute int) int32 {
	return int32(time.Date(2025, time.August, day, hour, minute, 0, 0, time.UTC).Unix())
}

// sceneAgo is an instant measured back from the fixtures' clock.
func sceneAgo(d time.Duration) int32 { return int32(sceneNow.Add(-d).Unix()) }

// pinSceneClock fixes the renderer's clock AND its zone for the duration of
// a test.
//
// The zone matters as much as the instant: every timestamp on screen is
// formatted through time.Local, so the same message renders 20:44 in Berlin
// and 18:44 in London, and a fixture can only hold one of them. UTC is the
// one zone that is the same everywhere.
func pinSceneClock(t *testing.T) {
	t.Helper()
	local := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = local })
	t.Cleanup(render.PinClock(sceneNow))
}

func sceneText(id, chatID, sender int64, at int32, text string) *telegram.Message {
	return &telegram.Message{
		ID: id, ChatID: chatID,
		SenderID: &telegram.MessageSenderUser{UserID: sender},
		Date:     at,
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: text}},
	}
}

// sceneChat is one row of the chat list.
type sceneChat struct {
	id     int64
	kind   telegram.ChatType
	title  string
	sender int64
	last   string
	unread int32
	muted  bool
	ago    time.Duration
}

// sceneChats is the list the fixtures draw, newest first. The relative
// times in the fixture — 2m, 6m, 14m, 1h, 2h, 4h, yd, yd, 2d — are what
// these ages render as against the pinned clock.
var sceneChats = []sceneChat{
	{sceneInfra, telegram.ChatTypeSupergroup, "infra-oncall", sceneNadia, "rebased, CI green", 4, false, 2 * time.Minute},
	{sceneNadiaDM, telegram.ChatTypePrivate, "Nadia Feld", sceneYou, "pushing the tag now", 0, false, 6 * time.Minute},
	{sceneRelay, telegram.ChatTypeSupergroup, "relay-protocol", sceneIvo, "the 429 is upstream", 2, false, 14 * time.Minute},
	{sceneWire, telegram.ChatTypePrivate, "wire notes", sceneYou, "still drafting", 0, false, time.Hour},
	{sceneOps, telegram.ChatTypeChannel, "ops-alerts", sceneBot, "p95 back under 400ms", 31, true, 2 * time.Hour},
	{sceneMiraDM, telegram.ChatTypePrivate, "Mira Okonkwo", sceneMira, "sounds good — thurs then", 0, false, 4 * time.Hour},
	{sceneDesign, telegram.ChatTypeSupergroup, "design-crit", sceneYou, "left comments on 3", 0, false, 26 * time.Hour},
	{sceneTapeChan, telegram.ChatTypeChannel, "tape/changelog", sceneTape, "v0.4.1 — keymap overhaul", 0, true, 30 * time.Hour},
	{sceneJonasDM, telegram.ChatTypePrivate, "Jonas Vik", sceneJonas, "thanks, that unblocked me", 0, false, 50 * time.Hour},
}

// sceneRail fills the context rail's three sections, as the fixtures at 120
// columns and wider draw them.
func sceneRail(m *Model) {
	pinned := []*telegram.Message{
		sceneText(1901, sceneInfra, sceneIvo, sceneAt(14, 0), "Deploy window 14:00–16:00 UTC, every Tuesday"),
		sceneText(1902, sceneInfra, sceneNadia, sceneAt(9, 30), "Runbook: auth p95 spikes → check the backfill first"),
	}
	files := []*telegram.Message{
		{ID: 1903, ChatID: sceneInfra, Date: sceneAt(20, 41),
			SenderID: &telegram.MessageSenderUser{UserID: sceneSam},
			Content: &telegram.MessageDocument{Document: &telegram.Document{
				FileName: "auth-p95-2608.png", MimeType: "image/png",
				File: &telegram.File{ID: "doc:1903", Size: 188_416},
			}}},
		{ID: 1904, ChatID: sceneInfra, Date: sceneAt(20, 12),
			SenderID: &telegram.MessageSenderUser{UserID: sceneNadia},
			Content: &telegram.MessageDocument{Document: &telegram.Document{
				FileName: "incident-0812.md", MimeType: "text/markdown",
				File: &telegram.File{ID: "doc:1904", Size: 6_144},
			}}},
		{ID: 1905, ChatID: sceneInfra, Date: sceneAt(19, 55),
			SenderID: &telegram.MessageSenderUser{UserID: sceneIvo},
			Content: &telegram.MessageDocument{Document: &telegram.Document{
				FileName: "backoff.patch", MimeType: "text/x-patch",
				File: &telegram.File{ID: "doc:1905", Size: 2_048},
			}}},
	}
	members := []*telegram.ChatMember{
		{MemberID: &telegram.MessageSenderUser{UserID: sceneIvo}, Status: &telegram.ChatMemberStatusAdministrator{}},
		{MemberID: &telegram.MessageSenderUser{UserID: sceneNadia}, Status: &telegram.ChatMemberStatusMember{}},
		{MemberID: &telegram.MessageSenderUser{UserID: sceneSam}, Status: &telegram.ChatMemberStatusMember{}},
		{MemberID: &telegram.MessageSenderUser{UserID: sceneMira}, Status: &telegram.ChatMemberStatusMember{}},
		{MemberID: &telegram.MessageSenderUser{UserID: sceneJonas}, Status: &telegram.ChatMemberStatusMember{}},
	}
	m.rail.SetDataForTest(sceneInfra, pinned, files, members, 24)
}

// scene is one fixture world: the chats down the left, the thread in the
// middle, and the reply half-written in the composer.
type scene struct {
	chats  []sceneChat
	thread func(*store.Store)
	title  string
	// reply is the composer's parked reply, as the app builds it: who,
	// then what.
	reply string
	// unreadAfter is the last message read, which is where the unread
	// divider goes. Zero for a scene with nothing unread in the open chat.
	unreadAfter int64
}

func mainScene() scene {
	return scene{
		chats: sceneChats, thread: sceneThread, title: "infra-oncall",
		reply: "nadia: Rebased onto main, CI is green now. 4412 ready for the second approval.",
		// Four unread, so the divider sits under 20:52.
		unreadAfter: 2103,
	}
}

func wideScene() scene {
	return scene{
		chats: wideChats, thread: wideThread, title: "デプロイ調整",
		reply: "مريم: اختبار أخير — last line, cursor sits here.",
	}
}

// sceneModel builds the session the fixtures draw, sized to w×h. rail says
// whether the context rail is open, which the wider fixtures draw.
func sceneModel(t *testing.T, w, h int, rail bool) Model {
	return buildScene(t, mainScene(), w, h, rail)
}

func buildScene(t *testing.T, sc scene, w, h int, rail bool) Model {
	t.Helper()
	pinSceneClock(t)

	m := mainModel(t, PanelChatList)
	m.composer.SetEditingMode(composer.ModeEmacs)
	s := m.store

	online := &telegram.UserStatusOnline{Expires: int32(sceneNow.Add(5 * time.Minute).Unix())}
	for _, u := range []struct {
		id     int64
		name   string
		status telegram.UserStatus
	}{
		{sceneYou, "you", online},
		{sceneNadia, "nadia", online},
		{sceneIvo, "ivo", online},
		{sceneSam, "sam", online},
		{sceneMira, "mira", &telegram.UserStatusOffline{WasOnline: sceneAgo(4 * time.Hour)}},
		{sceneBot, "bot", &telegram.UserStatusOffline{WasOnline: sceneAgo(2 * time.Hour)}},
		{sceneJonas, "jonas", &telegram.UserStatusOffline{WasOnline: sceneAgo(50 * time.Hour)}},
		{sceneTape, "tape", &telegram.UserStatusEmpty{}},
		{sceneTanaka, "田中さくら", online},
		{sceneAegir, "Ægir Þórsdóttir", online},
		{sceneMaryam, "مريم", online},
	} {
		s.Users.Set(&telegram.User{ID: u.id, FirstName: u.name, Status: u.status})
	}

	for _, c := range sc.chats {
		last := sceneText(c.id*10, c.id, c.sender, sceneAgo(c.ago), c.last)
		chat := &telegram.Chat{
			ID: c.id, Type: c.kind, Title: c.title, Muted: c.muted,
			UnreadCount: c.unread, LastMessage: last,
			// Newest first: the store orders by Order descending.
			Order: int64(last.Date),
		}
		if c.id == sceneInfra && sc.unreadAfter != 0 {
			// The divider sits under the last message read, and every
			// check mark up to it is a double.
			chat.LastReadInboxMessageID = sc.unreadAfter
			chat.LastReadOutboxMessageID = sc.unreadAfter
		}
		s.Chats.Set(chat)
	}
	s.Chats.SetMemberCount(sceneInfra, 24)

	sc.thread(s)

	m.chatList.SetMyUserID(sceneYou)
	m.chatList.SetDraftChats(map[int64]bool{sceneWire: true})
	m.chatList.SetFoldersForTest([]string{"all", "unread", "work", "channels", "archive"})
	m.chatList.MarkLoadedForTest()

	m.chatView.SetMyUserId(sceneYou)
	m.chatView.OpenChat(sceneInfra, sc.title)
	m.chatView.MarkLoadedForTest()
	m.composer.SetChatId(sceneInfra)
	m.composer.EnterReplyMode(2105, sc.reply)
	m.composer.SetParseMarkdown(true)

	m.deviceCount = 1
	m.topBar.SetConnection(topbar.Connected, "connected")

	// Before sizing: the layout gives the rail its column only when it is
	// open, and a rail switched on afterwards would have no width to draw
	// into.
	m.railOpen = rail
	if rail {
		sceneRail(&m)
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = updated.(Model)

	// Somebody is typing, which owns the scroller's last row.
	typist := sceneNadia
	if len(sc.chats) > 0 && sc.chats[0].sender != sceneNadia {
		typist = sc.chats[0].sender
	}
	typed, _ := m.Update(telegram.ChatActionMsg{
		ChatId: sceneInfra, UserId: typist, Action: &telegram.ChatActionTyping{},
	})
	return typed.(Model)
}

// sceneThread fills the incident chat with the history the tall fixtures
// draw: a review conversation on the Tuesday, and today's incident.
func sceneThread(s *store.Store) {
	tuesday := []*telegram.Message{
		sceneText(2090, sceneInfra, sceneIvo, sceneOn(26, 9, 12),
			"Deploy window is 14:00–15:30. Anything landing before that needs review now."),
		sceneText(2091, sceneInfra, sceneNadia, sceneOn(26, 9, 14),
			"Two PRs from me. 4412 is the retry backoff, 4419 is cosmetic."),
		sceneText(2092, sceneInfra, sceneYou, sceneOn(26, 9, 15),
			"Taking 4412. Backoff caps at 30s?"),
		sceneText(2093, sceneInfra, sceneNadia, sceneOn(26, 9, 16),
			"Yes, jittered. Full ±20%."),
	}
	tuesday[1].Reactions = []*telegram.Reaction{{Emoji: "👍", Count: 3}}
	tuesday[2].IsOutgoing = true
	tuesday[3].ReplyToMessageID = 2092

	// A picture sent as a file, which is what a screenshot pasted out of a
	// dashboard arrives as: it has a name and an extension, where a photo
	// has neither.
	shot := &telegram.Message{
		ID: 2100, ChatID: sceneInfra,
		SenderID: &telegram.MessageSenderUser{UserID: sceneIvo},
		Date:     sceneAt(20, 41),
		Content: &telegram.MessageDocument{
			Document: &telegram.Document{
				FileName: "auth-p95-2608.png", MimeType: "image/png",
				File: &telegram.File{ID: "doc:2100", Size: 188_416},
			},
			Caption: &telegram.FormattedText{
				Text: "Rollout paused — session hits spiked on the auth path. @nadia can you look?",
			},
		},
		Reactions: []*telegram.Reaction{
			{Emoji: "👀", Count: 5},
			{Emoji: "🔥", Count: 2},
		},
	}

	const lead = "The offending query, for the record:"
	code := "SELECT s.* FROM sessions s\n" +
		"  JOIN devices d ON d.sid = s.id\n" +
		"- WHERE s.touched > now() - '7d'::interval\n" +
		"+ WHERE s.touched > now() - '1h'::interval"
	query := &telegram.Message{
		ID: 2101, ChatID: sceneInfra,
		SenderID: &telegram.MessageSenderUser{UserID: sceneSam},
		Date:     sceneAt(20, 44),
		Content: &telegram.MessageText{Text: &telegram.FormattedText{
			Text: lead + "\n" + code,
			Entities: []*telegram.TextEntity{{
				Offset: int32(len([]rune(lead)) + 1), Length: int32(len([]rune(code))),
				Type: &telegram.TextEntityTypePreCode{Language: "sql"},
			}},
		}},
	}

	read := sceneText(2103, sceneInfra, sceneYou, sceneAt(20, 52),
		"Confirmed from the queue dashboard. Resuming.")
	read.IsOutgoing = true

	reply := sceneText(2105, sceneInfra, sceneNadia, sceneAt(21, 1),
		"Rebased onto main, CI is green now. 4412 ready for the second approval.")
	reply.ReplyToMessageID = 2104

	approved := sceneText(2106, sceneInfra, sceneSam, sceneAt(21, 2),
		"Approved. Merging behind the flag.")
	approved.Reactions = []*telegram.Reaction{{Emoji: "🚀", Count: 4}}

	today := []*telegram.Message{
		shot,
		query,
		sceneText(2102, sceneInfra, sceneNadia, sceneAt(20, 47),
			"That is the migration backfill, not the rollout. It drains in ~20 min."),
		read,
		sceneText(2104, sceneInfra, sceneIvo, sceneAt(20, 58), "Resumed. Canary at 5%."),
		reply,
		approved,
		sceneText(2107, sceneInfra, sceneIvo, sceneAt(21, 3),
			"Good. I will write the incident note either way — cheap to have, expensive to reconstruct."),
	}

	for _, msg := range append(tuesday, today...) {
		s.Messages.Append(sceneInfra, msg)
	}
}

// The wide-rune world.
//
// wide-runes-120x40 is the same session drawn with the text that breaks
// grids: CJK at two cells a rune, Arabic that runs right to left inside a
// left-to-right row, combining accents that are one grapheme and several
// runes, a ZWJ family that is one cluster and seven, and emoji reactions
// whose drawn width the tables and the terminal disagree about.
//
// It is a separate scene rather than a flag on the first one because that
// is what it is: nothing here is a variation on "infra-oncall", and a
// scene that tried to be both would be readable as neither.

const (
	sceneTanaka int64 = 910
	sceneAegir  int64 = 911
	sceneMaryam int64 = 912
)

var wideChats = []sceneChat{
	{sceneInfra, telegram.ChatTypeSupergroup, "デプロイ調整", sceneTanaka, "リベース済み、CI緑", 4, false, 2 * time.Minute},
	{sceneNadiaDM, telegram.ChatTypePrivate, "Ægir Þórsdóttir", sceneYou, "pushing the tag now", 0, false, 6 * time.Minute},
	{sceneRelay, telegram.ChatTypeSupergroup, "مجموعة-الترحيل", sceneMaryam, "429 من المصدر", 2, false, 14 * time.Minute},
	{sceneWire, telegram.ChatTypePrivate, "wire notes", sceneYou, "still drafting", 0, false, time.Hour},
	{sceneOps, telegram.ChatTypeChannel, "ops-alerts 🔔", sceneBot, "p95 back under 400ms", 31, true, 2 * time.Hour},
	{sceneMiraDM, telegram.ChatTypePrivate, "Zoë Ćurić", sceneMira, "sounds good — thurs then", 0, false, 4 * time.Hour},
	{sceneDesign, telegram.ChatTypeSupergroup, "设计评审", sceneYou, "left comments on 3", 0, false, 26 * time.Hour},
	{sceneTapeChan, telegram.ChatTypeChannel, "tape/changelog", sceneTape, "v0.4.1 — keymap overhaul", 0, true, 30 * time.Hour},
	{sceneJonasDM, telegram.ChatTypePrivate, "Jonas Vik", sceneJonas, "thanks, that unblocked me", 0, false, 50 * time.Hour},
}

// wideThread is the conversation the wide-rune fixture draws.
func wideThread(s *store.Store) {
	accents := sceneText(3002, sceneInfra, sceneAegir, sceneAt(8, 12),
		"Naïve café ré́sumé — combining accents must not shear the grid.")
	accents.Reactions = []*telegram.Reaction{
		{Emoji: "👍", Count: 3}, {Emoji: "🎉", Count: 12},
	}

	confirmed := sceneText(3004, sceneInfra, sceneYou, sceneAt(8, 15),
		"确认。队列面板显示已恢复。Resuming rollout.")
	confirmed.IsOutgoing = true
	confirmed.Reactions = []*telegram.Reaction{
		{Emoji: "🚀", Count: 4}, {Emoji: "🔥", Count: 2}, {Emoji: "👀", Count: 15},
	}

	graph := &telegram.Message{
		ID: 3005, ChatID: sceneInfra,
		SenderID: &telegram.MessageSenderUser{UserID: sceneTanaka},
		Date:     sceneAt(8, 16),
		Content: &telegram.MessageDocument{
			Document: &telegram.Document{
				FileName: "負荷-p95-2608.png", MimeType: "image/png",
				File: &telegram.File{ID: "doc:3005", Size: 188_416},
			},
			Caption: &telegram.FormattedText{
				Text: "ありがとう 🙏 全角記号：、。がずれないこと。",
			},
		},
	}

	zwj := sceneText(3006, sceneInfra, sceneAegir, sceneAt(8, 18),
		"Zero-width joiner sequence: \U0001F468‍\U0001F469‍\U0001F467‍\U0001F466 renders as one cluster.")
	zwj.ReplyToMessageID = 3005

	for _, msg := range []*telegram.Message{
		sceneText(3001, sceneInfra, sceneTanaka, sceneAt(8, 10),
			"デプロイの時間帯は14:00から15:30です。それまでにレビューをお願いします。"),
		accents,
		sceneText(3003, sceneInfra, sceneMaryam, sceneAt(8, 14),
			"مرحبا — the retry backoff caps at 30s. RTL name, LTR body."),
		confirmed,
		graph,
		zwj,
		sceneText(3007, sceneInfra, sceneMaryam, sceneAt(8, 19),
			"اختبار أخير — last line, cursor sits here."),
	} {
		s.Messages.Append(sceneInfra, msg)
	}
}
