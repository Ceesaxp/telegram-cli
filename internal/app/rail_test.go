package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/chatlist"
	"github.com/imtaqin/telegram-cli/internal/ui/layout"
)

// wideModel is a terminal wide enough for the rail, with a chat open.
func wideModel(t *testing.T) Model {
	t.Helper()
	m := mainModel(t, PanelChatView)
	m.width, m.height = layout.MinRailWidth, 40
	m.store.Chats.Set(&telegram.Chat{
		ID: 42, Type: telegram.ChatTypeSupergroup, Title: "infra-oncall", Order: 42,
	})
	m.updateLayout()
	return send(t, m, chatlist.ChatSelectedMsg{ChatId: 42})
}

// TestOpeningAChatCostsNoRailRequest is decision 6. The primary history
// paint must never compete with rail work, and a user who keeps the rail
// closed must never pay for it at all.
func TestOpeningAChatCostsNoRailRequest(t *testing.T) {
	m := wideModel(t)
	if m.railOpen {
		t.Fatal("precondition: the rail defaults to closed")
	}
	if got := len(m.rail.Sections()); got != 0 {
		t.Errorf("a closed rail described %d sections after a chat opened", got)
	}
	if m.layout.RailWidth != 0 {
		t.Errorf("a closed rail took %d columns", m.layout.RailWidth)
	}
}

// TestBacktickTogglesTheRail, and the columns come out of the thread.
func TestBacktickTogglesTheRail(t *testing.T) {
	m := wideModel(t)
	thread := m.layout.ThreadWidth

	m = send(t, m, decodeKey(t, "`"))
	if !m.railOpen {
		t.Fatal("backtick did not open the rail")
	}
	if m.layout.RailWidth != layout.RailWidth {
		t.Errorf("RailWidth = %d, want %d", m.layout.RailWidth, layout.RailWidth)
	}
	if want := thread - layout.RailWidth - layout.RuleWidth; m.layout.ThreadWidth != want {
		t.Errorf("ThreadWidth = %d, want %d — the rail's columns came from somewhere else",
			m.layout.ThreadWidth, want)
	}

	m = send(t, m, decodeKey(t, "`"))
	if m.railOpen {
		t.Error("backtick did not close the rail")
	}
	if m.layout.ThreadWidth != thread {
		t.Errorf("the thread did not get its columns back: %d, want %d",
			m.layout.ThreadWidth, thread)
	}
}

// TestOpenRailDrawsItsSections. Nothing is fetched here — no client — so
// every section says "unavailable", which is exactly the honest state and
// the one a disconnected user sees.
func TestOpenRailDrawsItsSections(t *testing.T) {
	m := wideModel(t)
	m = send(t, m, decodeKey(t, "`"))

	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"PINNED", "MEMBERS", "FILES"} {
		if !strings.Contains(view, want) {
			t.Errorf("the rail does not show %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "unavailable") {
		t.Errorf("a section with no data does not say so:\n%s", view)
	}
}

// TestTheRailPreferenceSurvivesANarrowTerminal.
//
// Layout decides whether the rail is drawn; the toggle decides whether it is
// wanted. Overwriting the preference on a narrow terminal would mean a user
// who widened their window had to ask again for something they never turned
// off.
func TestTheRailPreferenceSurvivesANarrowTerminal(t *testing.T) {
	m := wideModel(t)
	m = send(t, m, decodeKey(t, "`"))
	if m.layout.RailWidth == 0 {
		t.Fatal("precondition: the rail is drawn on a wide terminal")
	}

	m.width = layout.MinRailWidth - 1
	m.updateLayout()
	if m.layout.RailWidth != 0 {
		t.Errorf("the rail is drawn at %d columns", m.width)
	}
	if !m.railOpen {
		t.Error("narrowing the terminal turned the preference off")
	}

	m.width = layout.MinRailWidth
	m.updateLayout()
	if m.layout.RailWidth == 0 {
		t.Error("widening the terminal did not bring the rail back")
	}
}

// TestAnOpenRailFollowsTheChat. The rail is about the chat you are looking
// at, so switching chats with it open has to repoint it — otherwise it goes
// on describing the chat you left, which is worse than describing nothing.
func TestAnOpenRailFollowsTheChat(t *testing.T) {
	m := wideModel(t)
	m.store.Chats.Set(&telegram.Chat{
		ID: 43, Type: telegram.ChatTypePrivate, Title: "alice", Order: 43,
	})

	m = send(t, m, decodeKey(t, "`"))
	if got := sectionTitles(m); strings.Join(got, ",") != "pinned,members,files" {
		t.Fatalf("precondition: a group's rail shows %v", got)
	}

	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: 43})
	if got := sectionTitles(m); strings.Join(got, ",") != "files,links" {
		t.Errorf("after switching to a DM the rail shows %v, want the DM's sections", got)
	}
}

// TestTogglingTheRailOnANarrowTerminalStillRecordsThePreference, so widening
// the window brings it up rather than needing to be asked again.
func TestTogglingTheRailOnANarrowTerminalStillRecordsThePreference(t *testing.T) {
	m := wideModel(t)
	m.width = layout.MinRailWidth - 1
	m.updateLayout()

	m = send(t, m, decodeKey(t, "`"))
	if !m.railOpen {
		t.Fatal("the toggle refused on a terminal too narrow to draw the rail")
	}
	if m.layout.RailWidth != 0 {
		t.Errorf("the rail was drawn at %d columns", m.width)
	}

	m.width = layout.MinRailWidth
	m.updateLayout()
	if m.layout.RailWidth == 0 {
		t.Error("widening did not bring up the rail the user had asked for")
	}
}

// sectionTitles is what the rail currently describes.
func sectionTitles(m Model) []string {
	var out []string
	for _, s := range m.rail.Sections() {
		out = append(out, s.Title)
	}
	return out
}

// TestBacktickIsTypedIntoTheComposer. A backtick is a character somebody may
// well want to send — it is how code is quoted — so the composer owns it
// while it has focus, the same rule the colon follows.
func TestBacktickIsTypedIntoTheComposer(t *testing.T) {
	m := wideModel(t)
	m.setFocus(PanelComposer)

	m = send(t, m, decodeKey(t, "`"))
	if m.railOpen {
		t.Error("a backtick typed into the composer toggled the rail")
	}
	if !strings.Contains(m.composer.Draft(), "`") {
		t.Errorf("the backtick did not reach the draft: %q", m.composer.Draft())
	}
}

// TestRailRowsAreExactlyTheirRegion. The frame fits every row, but a rail
// that overshoots is clipped rather than shearing — and a clipped rail is a
// rail with its sizes cut off, which is the column that matters.
func TestRailRowsAreExactlyTheirRegion(t *testing.T) {
	m := wideModel(t)
	m = send(t, m, decodeKey(t, "`"))

	for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if got := ansi.StringWidth(line); got != m.width {
			t.Fatalf("row is %d cells, terminal is %d: %q", got, m.width, line)
		}
	}
}
