package chatlist

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/render"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"

	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/widgets"
)

func previewModel() Model {
	s := store.NewStore()
	s.Users.Set(&telegram.User{ID: 11, FirstName: "nadia"})
	s.Users.Set(&telegram.User{ID: 99, FirstName: "me"})
	return Model{store: s, myUserID: 99}
}

func entryFor(kind telegram.ChatType, sender int64, text string) *store.ChatEntry {
	return &store.ChatEntry{
		Chat: &telegram.Chat{ID: 1, Type: kind, Title: "somewhere"},
		LastMessage: &telegram.Message{
			ID: 5, SenderID: &telegram.MessageSenderUser{UserID: sender},
			Content: &telegram.MessageText{Text: &telegram.FormattedText{Text: text}},
		},
	}
}

// TestThePreviewNamesTheSenderWhereItSaysSomething.
func TestThePreviewNamesTheSenderWhereItSaysSomething(t *testing.T) {
	m := previewModel()

	cases := map[string]struct {
		entry *store.ChatEntry
		want  string
	}{
		"a group has several people in it": {
			entryFor(telegram.ChatTypeSupergroup, 11, "rebased, CI green"),
			"nadia: rebased, CI green",
		},
		"your own message, anywhere": {
			entryFor(telegram.ChatTypePrivate, 99, "pushing the tag now"),
			"you: pushing the tag now",
		},
		"a one-to-one chat is already named by its title": {
			entryFor(telegram.ChatTypePrivate, 11, "sounds good — thurs then"),
			"sounds good — thurs then",
		},
		"a channel's posts are the channel's": {
			entryFor(telegram.ChatTypeChannel, 11, "v0.4.1 — keymap overhaul"),
			"v0.4.1 — keymap overhaul",
		},
		"a sender nobody has named yet": {
			entryFor(telegram.ChatTypeSupergroup, 4242, "who said this"),
			"who said this",
		},
	}
	for name, tc := range cases {
		if got := m.previewWithSender(tc.entry); got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", name, got, tc.want)
		}
	}
}

// TestAnEmptyPreviewGainsNoName. "nadia: " over nothing is a row that says
// somebody spoke and refuses to say what.
func TestAnEmptyPreviewGainsNoName(t *testing.T) {
	entry := entryFor(telegram.ChatTypeSupergroup, 11, "")
	if got := previewModel().previewWithSender(entry); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestTheBadgeSaysHowLoudlyItIsWaiting. The number alone is the same claim
// for a chat that will interrupt you and one you have muted.
func TestTheBadgeSaysHowLoudlyItIsWaiting(t *testing.T) {
	if got := unreadBadge(4, false); got != "[4]" {
		t.Errorf("unmuted badge = %q, want [4]", got)
	}
	if got := unreadBadge(31, true); got != "(31)" {
		t.Errorf("muted badge = %q, want (31)", got)
	}
	// Three cells either way for a single digit, which is what keeps the
	// column aligned down a list of both kinds.
	if len(unreadBadge(4, false)) != len(unreadBadge(4, true)) {
		t.Error("the two badges are different widths for the same count")
	}
}

// TestBufferIndexIsTheRowTheReaderCanSee.
func TestBufferIndexIsTheRowTheReaderCanSee(t *testing.T) {
	m := previewModel()
	m.list = &widgets.List{}
	for _, id := range []string{"-1001", "901", "-1002"} {
		m.list.Items = append(m.list.Items, widgets.ListItem{ID: id})
	}

	for id, want := range map[int64]int{-1001: 1, 901: 2, -1002: 3, 4242: 0} {
		if got := m.BufferIndex(id); got != want {
			t.Errorf("BufferIndex(%d) = %d, want %d", id, got, want)
		}
	}
}

// TestTheUnreadBadgeIsCapped, as the design record has always specified.
// Past a thousand the exact number has stopped being information, and it is
// a column the preview has to give a cell back to for every digit.
func TestTheUnreadBadgeIsCapped(t *testing.T) {
	cases := map[int32]string{
		1:     "[1]",
		999:   "[999]",
		1000:  "[999+]",
		48213: "[999+]",
	}
	for count, want := range cases {
		if got := unreadBadge(count, false); got != want {
			t.Errorf("unreadBadge(%d) = %q, want %q", count, got, want)
		}
	}
	if got := unreadBadge(48213, true); got != "(999+)" {
		t.Errorf("a muted badge = %q, want (999+)", got)
	}

	// The cap is what bounds the column: without it the badge grows a cell
	// per digit and takes them from the preview beside it.
	if len(unreadBadge(48213, false)) > len("[9999]") {
		t.Errorf("an uncapped badge: %q", unreadBadge(48213, false))
	}
}

// TestRelativeTimesAgeWithoutTheListChanging is the defect a relative label
// has and an absolute one cannot: refreshList runs when the LIST changes,
// and in a quiet session nothing does, so a row built at "2m" went on
// saying "2m" for the rest of the afternoon.
func TestRelativeTimesAgeWithoutTheListChanging(t *testing.T) {
	now := time.Date(2025, time.August, 29, 21, 4, 0, 0, time.UTC)
	restore := render.PinClock(now)
	defer restore()

	local := time.Local
	time.Local = time.UTC
	defer func() { time.Local = local }()

	sent := int32(now.Add(-2 * time.Minute).Unix())

	// Through the real constructor: a hand-built Model has no list geometry
	// and View divides by its row height.
	s := store.NewStore()
	s.Chats.Set(&telegram.Chat{ID: 1, Type: telegram.ChatTypeSupergroup, Title: "infra-oncall",
		Order: int64(sent),
		LastMessage: &telegram.Message{
			ID: 5, ChatID: 1, Date: sent,
			SenderID: &telegram.MessageSenderUser{UserID: 11},
			Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "hello"}},
		}})
	s.Users.Set(&telegram.User{ID: 11, FirstName: "nadia"})

	m := New(s, nil, theme.DarkRoles(false))
	m.SetSize(38, 12)
	m.MarkLoadedForTest()

	if got := ansi.Strip(m.View()); !strings.Contains(got, "2m") {
		t.Fatalf("first paint:\n%s", got)
	}

	// Two hours pass and NOTHING arrives: no message, no folder change, no
	// filter. The only thing that has moved is the clock.
	restore()
	defer render.PinClock(now.Add(2 * time.Hour))()

	got := ansi.Strip(m.View())
	if strings.Contains(got, "2m") {
		t.Fatalf("the row is still claiming 2m three hours later:\n%s", got)
	}
	if !strings.Contains(got, "2h") {
		t.Fatalf("the row did not age to 2h:\n%s", got)
	}
}

// TestAgeingLeavesANonTimeMetaAlone. Not every Meta is a timestamp, and a
// row that has none must not have one invented for it.
func TestAgeingLeavesANonTimeMetaAlone(t *testing.T) {
	m := New(store.NewStore(), nil, theme.DarkRoles(false))
	m.list.Items = []widgets.ListItem{{ID: "1", Meta: "admin"}}

	m.ageMeta()
	if got := m.list.Items[0].Meta; got != "admin" {
		t.Fatalf("Meta = %q, want it untouched", got)
	}
}
