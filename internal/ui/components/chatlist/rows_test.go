package chatlist

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
	"github.com/imtaqin/telegram-cli/internal/ui/widgets"
)

func rowModel() Model {
	m := Model{roles: theme.DarkRoles(false)}
	return m
}

func renderOne(item widgets.ListItem, selected, focused bool, width int) []string {
	lines := rowModel().renderRow(item, selected, focused, width)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

// TestRowGeometryMatchesTheGolden pins the field offsets against the ones
// measured out of docs/fixtures at a 38-cell column. These are not chosen
// numbers — they are what the acceptance artifact draws.
func TestRowGeometryMatchesTheGolden(t *testing.T) {
	lines := renderOne(widgets.ListItem{
		Title:    "infra-oncall",
		Subtitle: "nadia: rebased, CI green",
		Meta:     "2m",
		Badge:    "4",
		Kind:     int(telegram.ChatTypeSupergroup),
	}, true, true, 38)

	title, preview := []rune(lines[0]), []rune(lines[1])

	if got := string(title[0]); got != "▌" {
		t.Errorf("col 0 = %q, want the selection bar", got)
	}
	if got := string(title[1]); got != "#" {
		t.Errorf("col 1 = %q, want the supergroup sigil", got)
	}
	if got := string(title[3:15]); got != "infra-oncall" {
		t.Errorf("title starts %q at col 3, want it to start there", got)
	}
	if got := string(title[32:34]); got != "2m" {
		t.Errorf("cols 32-33 = %q, want the relative time", got)
	}

	if got := string(preview[0:3]); got != "   " {
		t.Errorf("preview row indent = %q, want three spaces", got)
	}
	if got := string(preview[3:9]); got != "nadia:" {
		t.Errorf("preview starts %q at col 3", got)
	}
	// The badge is the count padded one cell either side, so " 4 " occupies
	// the same three cells the fixture writes as "[4]".
	if got := string(preview[34:37]); got != " 4 " {
		t.Errorf("cols 34-36 = %q, want the padded unread badge", got)
	}
}

// TestTimeColumnIsFixed is why the time is not right-aligned: at a fixed
// offset the times line up with each other down the list, which is the whole
// point of giving them a column.
func TestTimeColumnIsFixed(t *testing.T) {
	for _, meta := range []string{"2m", "14m", "1h", "yd", "2d"} {
		lines := renderOne(widgets.ListItem{Title: "chat", Meta: meta}, false, false, 38)
		runes := []rune(lines[0])
		if got := string(runes[32 : 32+len([]rune(meta))]); got != meta {
			t.Errorf("meta %q starts at %q, want it at column 32", meta, got)
		}
	}
}

func TestSigils(t *testing.T) {
	tests := []struct {
		name  string
		kind  telegram.ChatType
		saved bool
		want  string
	}{
		{"dm", telegram.ChatTypePrivate, false, "@"},
		{"basic group", telegram.ChatTypeBasicGroup, false, "#"},
		{"supergroup", telegram.ChatTypeSupergroup, false, "#"},
		{"channel", telegram.ChatTypeChannel, false, "!"},
		{"saved messages", telegram.ChatTypePrivate, true, "~"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := renderOne(widgets.ListItem{
				Title: "x", Kind: int(tc.kind), Saved: tc.saved,
			}, false, false, 38)
			if got := string([]rune(lines[0])[1]); got != tc.want {
				t.Errorf("sigil = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSavedMessagesOutranksTheChatType: Saved Messages IS a private chat as
// far as Telegram is concerned, so the check order matters or it draws as a
// plain DM.
func TestSavedMessagesOutranksTheChatType(t *testing.T) {
	lines := renderOne(widgets.ListItem{
		Title: "Saved Messages", Kind: int(telegram.ChatTypePrivate), Saved: true,
	}, false, false, 38)
	if got := string([]rune(lines[0])[1]); got != "~" {
		t.Errorf("sigil = %q, want ~ — saved messages must not read as a DM", got)
	}
}

// TestMutedSaysSoInWords: dimming alone is not readable in isolation. You
// cannot tell a muted row from an ordinary one without another row beside it
// to compare against, so the state is spelled out.
func TestMutedSaysSoInWords(t *testing.T) {
	lines := renderOne(widgets.ListItem{
		Title: "ops-alerts", Meta: "2h", Muted: true,
	}, false, false, 38)

	if !strings.Contains(lines[0], "muted") {
		t.Errorf("a muted chat does not say so: %q", lines[0])
	}
}

// TestMutedMarkerSurvivesALongTitle: the title gives way to the marker, not
// the other way round — a truncated name is still recognisable, a missing
// "muted" is invisible.
func TestMutedMarkerSurvivesALongTitle(t *testing.T) {
	lines := renderOne(widgets.ListItem{
		Title: strings.Repeat("long-name-", 10), Muted: true,
	}, false, false, 38)

	if !strings.Contains(lines[0], "muted") {
		t.Errorf("the muted marker was truncated away: %q", lines[0])
	}
}

func TestSelectionBarOnlyOnTheSelectedRow(t *testing.T) {
	sel := renderOne(widgets.ListItem{Title: "x"}, true, true, 38)
	if got := string([]rune(sel[0])[0]); got != "▌" {
		t.Errorf("selected row col 0 = %q, want the bar", got)
	}

	other := renderOne(widgets.ListItem{Title: "x"}, false, false, 38)
	if got := string([]rune(other[0])[0]); got != " " {
		t.Errorf("unselected row col 0 = %q, want a space", got)
	}
}

// TestRowsAreExactlyWide is the property the frame depends on, exercised
// against the content most likely to break it.
func TestRowsAreExactlyWide(t *testing.T) {
	items := []widgets.ListItem{
		{Title: "short"},
		{Title: strings.Repeat("四", 40), Subtitle: strings.Repeat("字", 40), Meta: "14m", Badge: "999+"},
		{Title: "📢📢📢📢", Subtitle: "👨‍👩‍👧‍👦 family", Meta: "1h", Badge: "7"},
		{Title: "", Subtitle: "", Meta: "", Badge: ""},
		{Title: "muted one", Muted: true, Badge: "31", Meta: "2h"},
	}

	for _, width := range []int{20, 30, 38, 60} {
		for i, item := range items {
			for _, selected := range []bool{false, true} {
				lines := renderOne(item, selected, true, width)
				if len(lines) != 2 {
					t.Fatalf("item %d: %d lines, want 2", i, len(lines))
				}
				for n, line := range lines {
					if got := cell.Width(line); got != width {
						t.Errorf("width %d item %d line %d: %d cells: %q",
							width, i, n+1, got, line)
					}
				}
			}
		}
	}
}

// TestBadgeNeverEatsThePreviewsColumn: a wide badge shrinks the preview
// rather than overflowing past the row's right edge.
func TestBadgeNeverEatsThePreviewsColumn(t *testing.T) {
	lines := renderOne(widgets.ListItem{
		Title:    "chat",
		Subtitle: strings.Repeat("preview ", 20),
		Badge:    "999+",
	}, false, false, 38)

	if got := cell.Width(lines[1]); got != 38 {
		t.Errorf("preview row is %d cells with a wide badge, want 38: %q", got, lines[1])
	}
	if !strings.Contains(lines[1], "999+") {
		t.Errorf("the badge was pushed off the row: %q", lines[1])
	}
}

func TestFilterHeaderIsExactlyWide(t *testing.T) {
	for _, width := range []int{20, 38, 60} {
		m := rowModel()
		m.list = &widgets.List{}
		if got := cell.Width(m.renderFilterHeader(width)); got != width {
			t.Errorf("width %d: header is %d cells", width, got)
		}
	}
}

func TestFooterIsExactlyWide(t *testing.T) {
	for _, width := range []int{20, 38, 60} {
		if got := cell.Width(rowModel().renderListFooter(width)); got != width {
			t.Errorf("width %d: footer is %d cells", width, got)
		}
	}
}

// TestFooterOffersTheWayOutOfAFilter: a user who cannot see how to clear a
// filter is left staring at a partial list wondering where their chats went.
func TestFooterOffersTheWayOutOfAFilter(t *testing.T) {
	m := rowModel()
	m.filter = "al"
	if got := ansi.Strip(m.renderListFooter(38)); !strings.Contains(got, "esc") {
		t.Errorf("footer with a filter applied = %q, want it to name esc", got)
	}

	m.filter = ""
	if got := ansi.Strip(m.renderListFooter(38)); !strings.Contains(got, "j/k") {
		t.Errorf("footer with no filter = %q, want the motions", got)
	}
}

// TestClickAtAccountsForTheHeaderRow replaces TestClickAtAccountsForTabBar,
// which died with the folder tab bar. The offset it guarded is still here,
// just for a different row: local row 0 is the filter header, so the first
// chat starts at row 1 and a click must not be attributed one row early.
func TestClickAtAccountsForTheHeaderRow(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Bob")
	m.SetSize(38, 12)

	if _, ok := m.ClickAt(0); ok {
		t.Error("row 0 is the filter header; it must not select a chat")
	}

	// Rows 1-2 are the first chat, 3-4 the second.
	first, ok := m.ClickAt(1)
	if !ok {
		t.Fatal("row 1 should select the first chat")
	}
	second, ok := m.ClickAt(3)
	if !ok {
		t.Fatal("row 3 should select the second chat")
	}
	if first == second {
		t.Errorf("rows 1 and 3 selected the same chat (%d); the two-line "+
			"stride is not being honoured", first)
	}
}

// TestFilterHeaderIsCellAccurateWithAQuery replaces TestFilterChipIsCellAccurate.
// The query is user text — it can be emoji, CJK, anything — and it shares a
// row with the match count, so a rune-counted budget would push the count
// off the end or tear the column.
func TestFilterHeaderIsCellAccurateWithAQuery(t *testing.T) {
	queries := []string{"", "al", "四字熟語", "📢📢📢📢📢", strings.Repeat("x", 80)}

	for _, q := range queries {
		for _, width := range []int{12, 20, 30, 38, 60} {
			m := rowModel()
			m.list = &widgets.List{}
			m.filter = q
			if got := cell.Width(m.renderFilterHeader(width)); got != width {
				t.Errorf("query %q at width %d: header is %d cells", q, width, got)
			}
		}
	}
}
