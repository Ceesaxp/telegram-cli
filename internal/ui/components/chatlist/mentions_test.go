package chatlist

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/Ceesaxp/telegram-cli/internal/ui/widgets"
)

// mentionedModel is a loaded list whose one chat, the group 1, has three
// unread mentions and no unread messages: the mentions sit below the read
// mark, which the dialog allows.
func mentionedModel(t *testing.T) Model {
	t.Helper()
	m := newLoadedModel(t)
	m.store.Chats.Set(&telegram.Chat{
		ID:                  1,
		Title:               "infra-oncall",
		Type:                telegram.ChatTypeSupergroup,
		Order:               1,
		UnreadMentionsCount: 3,
	})
	m.refreshList()
	return m
}

// unreadMentions is the chat's unread-mentions count as the store holds it.
func unreadMentions(t *testing.T, m Model, chatID int64) int32 {
	t.Helper()
	entry, ok := m.store.Chats.Get(chatID)
	if !ok {
		t.Fatalf("chat %d is not in the store", chatID)
	}
	return entry.UnreadMentionsCount
}

// Clearing some mentions takes exactly those off: no update carries the
// count, so the list's arithmetic is all there is until the next reload.
func TestClearingSomeMentionsTakesThemOffTheCount(t *testing.T) {
	m := mentionedModel(t)
	*m.dirty = false

	m, _ = m.Update(telegram.ChatMentionsReadMsg{ChatId: 1, MessageIds: []int64{4, 5}})

	if got := unreadMentions(t, m, 1); got != 1 {
		t.Errorf("unread mentions = %d after clearing 2 of 3, want 1", got)
	}
	if !*m.dirty {
		t.Error("the list was not marked for a redraw")
	}
}

// Clearing every mention leaves none, however many the list had counted.
func TestClearingAllMentionsZeroesTheCount(t *testing.T) {
	m := mentionedModel(t)
	*m.dirty = false

	m, _ = m.Update(telegram.ChatMentionsReadMsg{ChatId: 1, All: true})

	if got := unreadMentions(t, m, 1); got != 0 {
		t.Errorf("unread mentions = %d after clearing them all, want 0", got)
	}
	if !*m.dirty {
		t.Error("the list was not marked for a redraw")
	}
}

// previewRowOf draws the list and returns chatID's second row, ANSI
// stripped, as the reader would see it.
func previewRowOf(t *testing.T, m Model, chatID int64) string {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i, it := range m.list.Items {
		if it.ID == fmt.Sprint(chatID) {
			// One filter header row, then two rows per chat.
			return lines[1+2*i+1]
		}
	}
	t.Fatalf("chat %d has no row", chatID)
	return ""
}

// The @ follows the store's count: drawn while the chat has an unread
// mention, even with nothing else unread, and gone once they are cleared.
func TestTheListDrawsTheMentionFromTheCount(t *testing.T) {
	m := mentionedModel(t)

	if got := strings.TrimRight(previewRowOf(t, m, 1), " "); !strings.HasSuffix(got, " @") {
		t.Errorf("row two = %q with 3 unread mentions, want it to end in the @ chip", got)
	}

	m, _ = m.Update(telegram.ChatMentionsReadMsg{ChatId: 1, All: true})

	if got := previewRowOf(t, m, 1); strings.Contains(got, "@") {
		t.Errorf("row two = %q after every mention was cleared, want no @", got)
	}
}

// mentionItem is a group's row with an unread mention and, unless a test
// says otherwise, nothing else unread.
func mentionItem() widgets.ListItem {
	return widgets.ListItem{
		Title:    "infra-oncall",
		Subtitle: "nadia: can you look at the deploy",
		Meta:     "2m",
		Kind:     int(telegram.ChatTypeSupergroup),
		Mention:  true,
	}
}

// A mention can sit below the read mark, so a chat can have one with
// nothing else unread. The @ has to show on its own then, where the badge
// would be, or the one message that asked for the reader is invisible.
func TestAMentionAloneIsDrawnWhereTheBadgeWouldBe(t *testing.T) {
	preview := renderOne(mentionItem(), false, false, 38)[1]

	if got := strings.TrimRight(preview, " "); !strings.HasSuffix(got, " @") {
		t.Errorf("row two = %q, want it to end in the @ chip", preview)
	}
	if got := string([]rune(preview)[36]); got != "@" {
		t.Errorf("col 36 = %q, want the @ where a one-cell badge ends: %q", got, preview)
	}
}

// With unread messages as well, the @ is its own chip left of the count,
// a cell apart, and the badge keeps the right edge it always had.
func TestAMentionSitsLeftOfTheBadge(t *testing.T) {
	item := mentionItem()
	item.Badge = unreadBadge(3, false)

	preview := []rune(renderOne(item, false, false, 38)[1])

	if got := string(preview[32:37]); got != "@ [3]" {
		t.Errorf("cols 32-36 = %q, want the @ chip, a gap, then the badge: %q",
			got, string(preview))
	}
}

// Muting a chat silences it, but a mention is the one thing Telegram lets
// through. The @ on a muted row is drawn exactly as an unmuted badge would
// draw it, while the count beside it stays subdued.
func TestAMutedChatsMentionIsNotSubdued(t *testing.T) {
	m := rowModel()
	item := mentionItem()
	item.Muted = true
	item.Badge = unreadBadge(3, true)

	preview := m.renderRow(item, false, false, 38)[1]

	loud := m.renderBadge(widgets.ListItem{Badge: "@"})
	if !strings.Contains(preview, loud) {
		t.Errorf("the muted chat's @ is not in the unmuted badge's colours:\n%s\nwant it to contain\n%s",
			escaped(preview), escaped(loud))
	}
	quiet := m.renderBadge(widgets.ListItem{Badge: unreadBadge(3, true), Muted: true})
	if !strings.Contains(preview, quiet) {
		t.Errorf("the muted chat's count lost its subdued colours:\n%s", escaped(preview))
	}
}

// A chat that is waiting for the reader has its title in fg rather than
// dim. Unread messages only do that for a chat that is not muted; a
// mention does it either way, since it is what pierces mute.
func TestAMentionBrightensTheTitleEvenWhenMuted(t *testing.T) {
	r := theme.DarkRoles(false)
	inFg := "38;5;" + string(r.Fg) + "m" + "infra-oncall"

	for name, tc := range map[string]struct {
		mention, muted bool
		badge          string
		wantFg         bool
	}{
		"a mention alone":                 {mention: true, wantFg: true},
		"a mention in a muted chat":       {mention: true, muted: true, badge: unreadBadge(3, true), wantFg: true},
		"unread messages":                 {badge: unreadBadge(3, false), wantFg: true},
		"unread messages in a muted chat": {muted: true, badge: unreadBadge(3, true)},
		"nothing unread":                  {},
	} {
		t.Run(name, func(t *testing.T) {
			item := mentionItem()
			item.Mention, item.Muted, item.Badge = tc.mention, tc.muted, tc.badge

			title := rowModel().renderRow(item, false, false, 38)[0]

			if got := strings.Contains(title, inFg); got != tc.wantFg {
				t.Errorf("title in fg = %v, want %v:\n%s", got, tc.wantFg, escaped(title))
			}
		})
	}
}

// The @ takes cells from the preview, never from the column. A row that
// does not account for it is cut at the right edge by the final fit, which
// keeps the width and loses the chips, so both are checked.
func TestAMentionRowFitsTheColumn(t *testing.T) {
	long := strings.Repeat("字 preview ", 12)

	for name, tc := range map[string]struct {
		badge string
		muted bool
		trail string
	}{
		"@ alone":           {trail: "@"},
		"@ and a count":     {badge: unreadBadge(3, false), trail: "@ [3]"},
		"@ and a wide one":  {badge: unreadBadge(5000, false), trail: "@ [999+]"},
		"@ in a muted chat": {badge: unreadBadge(3, true), muted: true, trail: "@ (3)"},
	} {
		for _, width := range []int{30, 38} {
			for _, selected := range []bool{false, true} {
				item := mentionItem()
				item.Subtitle, item.Badge, item.Muted = long, tc.badge, tc.muted

				preview := rowModel().renderRow(item, selected, true, width)[1]

				if got := cell.Width(preview); got != width {
					t.Errorf("%s at %d (selected %v): row two is %d cells: %q",
						name, width, selected, got, ansi.Strip(preview))
				}
				if got := strings.TrimRight(ansi.Strip(preview), " "); !strings.HasSuffix(got, " "+tc.trail) {
					t.Errorf("%s at %d (selected %v): row two = %q, want it to end in %q",
						name, width, selected, got, tc.trail)
				}
			}
		}
	}
}

// escaped makes a styled line readable in a failure message.
func escaped(s string) string {
	return strings.ReplaceAll(s, "\x1b", "ESC")
}

// The @ is a claim that somebody asked for the reader. A chat without an
// unread mention must not make it, whatever else it has unread. Row one is
// not checked: the DM sigil is also an @.
func TestNoMentionDrawsNoChip(t *testing.T) {
	for name, badge := range map[string]string{
		"nothing unread":  "",
		"unread messages": unreadBadge(3, false),
		"a muted chat's":  unreadBadge(3, true),
	} {
		t.Run(name, func(t *testing.T) {
			item := mentionItem()
			item.Mention = false
			item.Badge = badge

			if preview := renderOne(item, false, false, 38)[1]; strings.Contains(preview, "@") {
				t.Errorf("row two = %q, drawn with an @ and no mention", preview)
			}
		})
	}
}
