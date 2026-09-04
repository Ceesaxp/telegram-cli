package chatlist

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/Ceesaxp/telegram-cli/internal/ui/widgets"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// key returns a printable-character key press, matching how bubbletea
// reports e.g. digit keys.
func key(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)})
}

// specialKey returns a non-printable key press (arrows, escape, etc.).
func specialKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

// newTestModel builds a chatlist Model with a real store and theme but no
// telegram client, suitable for exercising filtering/state logic that
// never dials out.
func newTestModel() Model {
	s := store.NewStore()
	return New(s, nil, theme.DarkRoles(false))
}

func TestTruncatePreviewTextRuneSafe(t *testing.T) {
	// Cyrillic text: byte-slicing at 50 bytes would land mid-rune since
	// each Cyrillic letter is 2 bytes in UTF-8.
	cyrillic := strings.Repeat("привет мир ", 10) // far more than 50 runes
	got := truncatePreviewText(cyrillic, 10)

	runes := []rune(got)
	// "..." (3 runes) + up to 10 runes of content.
	if len(runes) > 13 {
		t.Fatalf("truncatePreviewText produced %d runes, want <= 13: %q", len(runes), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("truncatePreviewText(%q) = %q, want ellipsis suffix", cyrillic, got)
	}
	// Must be valid UTF-8 (no corrupted/mid-rune cut).
	if !utf8ValidString(got) {
		t.Fatalf("truncatePreviewText produced invalid UTF-8: %q", got)
	}
}

func TestTruncatePreviewTextFlattensNewlinesAndTabs(t *testing.T) {
	got := truncatePreviewText("line one\nline two\tvalue", 100)
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("truncatePreviewText(%q) = %q, want no newlines/tabs", "line one\nline two\tvalue", got)
	}
	want := "line one line two value"
	if got != want {
		t.Fatalf("truncatePreviewText = %q, want %q", got, want)
	}
}

func TestTruncatePreviewTextShortStringUnchanged(t *testing.T) {
	got := truncatePreviewText("short", 50)
	if got != "short" {
		t.Fatalf("truncatePreviewText(%q) = %q, want unchanged", "short", got)
	}
}

func utf8ValidString(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

func TestCycleFolderWraparound(t *testing.T) {
	m := newTestModel()
	m.folders = []*telegram.ChatFolder{
		{ID: telegram.AllChatsFolderID, Title: "All"},
		{ID: 1, Title: "Work"},
		{ID: 2, Title: "Family"},
	}
	m.activeFolder = 0

	m.CycleFolder(1)
	if m.activeFolder != 1 {
		t.Fatalf("after +1: activeFolder = %d, want 1", m.activeFolder)
	}
	m.CycleFolder(1)
	if m.activeFolder != 2 {
		t.Fatalf("after +1: activeFolder = %d, want 2", m.activeFolder)
	}
	m.CycleFolder(1)
	if m.activeFolder != 0 {
		t.Fatalf("after wrap +1: activeFolder = %d, want 0", m.activeFolder)
	}
	m.CycleFolder(-1)
	if m.activeFolder != 2 {
		t.Fatalf("after wrap -1: activeFolder = %d, want 2", m.activeFolder)
	}
}

func TestCycleFolderEmptyIsNoop(t *testing.T) {
	m := newTestModel()
	m.folders = nil
	m.activeFolder = 0
	m.CycleFolder(1)
	if m.activeFolder != 0 {
		t.Fatalf("CycleFolder on empty folders changed activeFolder to %d", m.activeFolder)
	}
}

func TestChatInFolderExcludedAlwaysOut(t *testing.T) {
	m := newTestModel()
	folder := &telegram.ChatFolder{
		Groups:          true,
		ExcludedChatIDs: []int64{100},
	}
	entry := &store.ChatEntry{Chat: &telegram.Chat{ID: 100, Type: telegram.ChatTypeSupergroup}}
	if m.chatInFolder(folder, entry) {
		t.Fatal("excluded chat should never be in folder")
	}
}

func TestChatInFolderPinnedAlwaysIn(t *testing.T) {
	m := newTestModel()
	folder := &telegram.ChatFolder{
		Groups:        true, // category flags set, but chat is a private chat
		PinnedChatIDs: []int64{200},
	}
	entry := &store.ChatEntry{Chat: &telegram.Chat{ID: 200, Type: telegram.ChatTypePrivate}}
	if !m.chatInFolder(folder, entry) {
		t.Fatal("pinned chat should always be in folder, even if it fails the category match")
	}
}

func TestChatInFolderIncludedAlwaysIn(t *testing.T) {
	m := newTestModel()
	folder := &telegram.ChatFolder{
		Channels:        true,
		IncludedChatIDs: []int64{300},
	}
	entry := &store.ChatEntry{Chat: &telegram.Chat{ID: 300, Type: telegram.ChatTypePrivate}}
	if !m.chatInFolder(folder, entry) {
		t.Fatal("included chat should always be in folder, even if it fails the category match")
	}
}

func TestChatInFolderCategoryFlags(t *testing.T) {
	m := newTestModel()

	groupFolder := &telegram.ChatFolder{Groups: true}
	group := &store.ChatEntry{Chat: &telegram.Chat{ID: 1, Type: telegram.ChatTypeSupergroup}}
	channel := &store.ChatEntry{Chat: &telegram.Chat{ID: 2, Type: telegram.ChatTypeChannel}}

	if !m.chatInFolder(groupFolder, group) {
		t.Error("supergroup should match a Groups folder")
	}
	if m.chatInFolder(groupFolder, channel) {
		t.Error("channel should not match a Groups-only folder")
	}

	channelFolder := &telegram.ChatFolder{Channels: true}
	if !m.chatInFolder(channelFolder, channel) {
		t.Error("channel should match a Channels folder")
	}
}

func TestChatInFolderBots(t *testing.T) {
	m := newTestModel()
	m.store.Users.Set(&telegram.User{ID: 42, IsBot: true})

	botFolder := &telegram.ChatFolder{Bots: true}
	bot := &store.ChatEntry{Chat: &telegram.Chat{ID: 42, Type: telegram.ChatTypePrivate}}
	if !m.chatInFolder(botFolder, bot) {
		t.Error("bot chat should match a Bots folder")
	}

	contactsFolder := &telegram.ChatFolder{Contacts: true}
	if m.chatInFolder(contactsFolder, bot) {
		t.Error("bot chat should not match a Contacts-only folder")
	}
}

func TestChatInFolderNoFlagsIncludesEverything(t *testing.T) {
	// The synthesized/default "All" folder carries no category flags at
	// all; it must not filter anything out.
	m := newTestModel()
	all := defaultAllFolder()

	entries := []*store.ChatEntry{
		{Chat: &telegram.Chat{ID: 1, Type: telegram.ChatTypePrivate}},
		{Chat: &telegram.Chat{ID: 2, Type: telegram.ChatTypeSupergroup}},
		{Chat: &telegram.Chat{ID: 3, Type: telegram.ChatTypeChannel}},
	}
	for _, e := range entries {
		if !m.chatInFolder(all, e) {
			t.Errorf("chat %d should be visible in the flagless All folder", e.Chat.ID)
		}
	}
}

func TestChatInFolderExplicitIncludeOnly(t *testing.T) {
	// A user-created "these chats" folder has no type flags, only
	// include_peers. Chats not on that list must not appear.
	m := newTestModel()
	folder := &telegram.ChatFolder{
		ID:              7,
		Title:           "Work",
		IncludedChatIDs: []int64{10, 20},
		PinnedChatIDs:   []int64{30},
	}
	inInclude := &store.ChatEntry{Chat: &telegram.Chat{ID: 10, Type: telegram.ChatTypePrivate}}
	inPinned := &store.ChatEntry{Chat: &telegram.Chat{ID: 30, Type: telegram.ChatTypeSupergroup}}
	other := &store.ChatEntry{Chat: &telegram.Chat{ID: 99, Type: telegram.ChatTypePrivate}}

	if !m.chatInFolder(folder, inInclude) {
		t.Error("included chat should be in an explicit folder")
	}
	if !m.chatInFolder(folder, inPinned) {
		t.Error("pinned chat should be in an explicit folder")
	}
	if m.chatInFolder(folder, other) {
		t.Error("unlisted chat must not appear in an explicit-only folder")
	}
}

func TestChatInFolderChatlistNoFlags(t *testing.T) {
	// dialogFilterChatlist has no category flags, only include/pin lists.
	m := newTestModel()
	folder := &telegram.ChatFolder{
		ID:              12,
		IncludedChatIDs: []int64{1},
	}
	listed := &store.ChatEntry{Chat: &telegram.Chat{ID: 1, Type: telegram.ChatTypeChannel}}
	other := &store.ChatEntry{Chat: &telegram.Chat{ID: 2, Type: telegram.ChatTypeChannel}}
	if !m.chatInFolder(folder, listed) {
		t.Error("chatlist include should be visible")
	}
	if m.chatInFolder(folder, other) {
		t.Error("chatlist folder must not show chats outside include_peers")
	}
}

func TestFolderLoadCmdNilClient(t *testing.T) {
	m := newTestModel()
	m.folders = []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 7, Title: "Work", IncludedChatIDs: []int64{2}},
	}
	m.activeFolder = 1
	if cmd := m.FolderLoadCmd(); cmd != nil {
		t.Fatal("FolderLoadCmd with nil telegram client must be a no-op")
	}
}

func TestCycleFolderRefiltersList(t *testing.T) {
	m := newTestModel()
	m.store.Chats.Set(&telegram.Chat{ID: 1, Type: telegram.ChatTypePrivate, Title: "Alice"})
	m.store.Chats.Set(&telegram.Chat{ID: 2, Type: telegram.ChatTypePrivate, Title: "Bob"})
	m.store.Chats.Set(&telegram.Chat{ID: 3, Type: telegram.ChatTypePrivate, Title: "Carol"})
	m.folders = []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 7, Title: "Work", IncludedChatIDs: []int64{2}},
	}
	m.activeFolder = 0
	m.refreshList()
	if got := len(m.list.Items); got != 3 {
		t.Fatalf("All folder: %d items, want 3", got)
	}

	m.CycleFolder(1)
	if m.ActiveFolderID() != 7 {
		t.Fatalf("active folder ID = %d, want 7", m.ActiveFolderID())
	}
	if got := len(m.list.Items); got != 1 {
		t.Fatalf("Work folder: %d items, want 1", got)
	}
	if m.list.Items[0].ID != "2" {
		t.Fatalf("Work folder item ID = %q, want 2", m.list.Items[0].ID)
	}
}

func TestChatInFolderExcludeMuted(t *testing.T) {
	m := newTestModel()
	folder := &telegram.ChatFolder{ExcludeMuted: true}
	muted := &store.ChatEntry{Chat: &telegram.Chat{ID: 1, Type: telegram.ChatTypePrivate, Muted: true}}
	unmuted := &store.ChatEntry{Chat: &telegram.Chat{ID: 2, Type: telegram.ChatTypePrivate, Muted: false}}

	if m.chatInFolder(folder, muted) {
		t.Error("muted chat should be excluded by ExcludeMuted")
	}
	if !m.chatInFolder(folder, unmuted) {
		t.Error("unmuted chat should not be excluded by ExcludeMuted")
	}
}

func TestChatInFolderExcludeRead(t *testing.T) {
	m := newTestModel()
	folder := &telegram.ChatFolder{ExcludeRead: true}
	read := &store.ChatEntry{Chat: &telegram.Chat{ID: 1, Type: telegram.ChatTypePrivate}, UnreadCount: 0}
	unread := &store.ChatEntry{Chat: &telegram.Chat{ID: 2, Type: telegram.ChatTypePrivate}, UnreadCount: 3}

	if m.chatInFolder(folder, read) {
		t.Error("read chat should be excluded by ExcludeRead")
	}
	if !m.chatInFolder(folder, unread) {
		t.Error("unread chat should not be excluded by ExcludeRead")
	}
}

func TestChatInFolderPinnedBypassesExcludeMutedAndRead(t *testing.T) {
	m := newTestModel()
	folder := &telegram.ChatFolder{
		ExcludeMuted:  true,
		ExcludeRead:   true,
		PinnedChatIDs: []int64{1},
	}
	entry := &store.ChatEntry{
		Chat:        &telegram.Chat{ID: 1, Type: telegram.ChatTypePrivate, Muted: true},
		UnreadCount: 0,
	}
	if !m.chatInFolder(folder, entry) {
		t.Fatal("pinned chat should bypass ExcludeMuted/ExcludeRead")
	}
}

func TestOrderForFolderPinnedOrder(t *testing.T) {
	c1 := &store.ChatEntry{Chat: &telegram.Chat{ID: 1}}
	c2 := &store.ChatEntry{Chat: &telegram.Chat{ID: 2}}
	c3 := &store.ChatEntry{Chat: &telegram.Chat{ID: 3}}

	folder := &telegram.ChatFolder{PinnedChatIDs: []int64{3, 1}}
	got := orderForFolder(folder, []*store.ChatEntry{c1, c2, c3})

	if len(got) != 3 {
		t.Fatalf("orderForFolder returned %d entries, want 3", len(got))
	}
	if got[0].Chat.ID != 3 || got[1].Chat.ID != 1 {
		t.Fatalf("orderForFolder pinned order wrong: got IDs %d,%d,%d want 3,1,2",
			got[0].Chat.ID, got[1].Chat.ID, got[2].Chat.ID)
	}
	if got[2].Chat.ID != 2 {
		t.Fatalf("orderForFolder non-pinned chat missing/misplaced: got %d, want 2", got[2].Chat.ID)
	}
}

func TestOrderForFolderNoPinnedKeepsOrder(t *testing.T) {
	c1 := &store.ChatEntry{Chat: &telegram.Chat{ID: 1}}
	c2 := &store.ChatEntry{Chat: &telegram.Chat{ID: 2}}
	in := []*store.ChatEntry{c1, c2}

	got := orderForFolder(&telegram.ChatFolder{}, in)
	if len(got) != 2 || got[0] != c1 || got[1] != c2 {
		t.Fatalf("orderForFolder with no pins should keep the input order unchanged")
	}
}

func TestNormalizeFoldersSynthesizesAll(t *testing.T) {
	got := normalizeFolders([]*telegram.ChatFolder{{ID: 5, Title: "Work"}})
	if len(got) != 2 {
		t.Fatalf("normalizeFolders returned %d folders, want 2", len(got))
	}
	if got[0].ID != telegram.AllChatsFolderID || got[0].Title == "" {
		t.Fatalf("normalizeFolders[0] = %+v, want a titled All folder", got[0])
	}
	if got[1].ID != 5 {
		t.Fatalf("normalizeFolders[1].ID = %d, want 5", got[1].ID)
	}
}

func TestNormalizeFoldersFillsServerProvidedAllTitle(t *testing.T) {
	// Mirrors chatFolderFromTG's DialogFilterDefault mapping: bare ID,
	// no title/emoticon.
	got := normalizeFolders([]*telegram.ChatFolder{{ID: telegram.AllChatsFolderID}})
	if len(got) != 1 {
		t.Fatalf("normalizeFolders returned %d folders, want 1", len(got))
	}
	if got[0].Title == "" {
		t.Fatal("normalizeFolders should fill in a display title for the bare default folder")
	}
}

// TestClickAtWithScrollOffset covers the same tab-bar row math combined
// with a non-zero list scroll offset.
func TestClickAtWithScrollOffset(t *testing.T) {
	m := newTestModel()
	m.loading = false // ClickAt is a no-op while the initial load is in flight

	m.list.SetItems([]widgets.ListItem{
		{ID: "10"},
		{ID: "11"},
		{ID: "12"},
	})
	m.list.Height = 4 // 2 visible items at a time
	m.list.Offset = 1 // scrolled past chat 10

	// Row 0 is still the tab bar regardless of scroll offset.
	if _, ok := m.ClickAt(0); ok {
		t.Fatal("ClickAt(0) on the tab bar row should not select a chat, even with Offset > 0")
	}

	// With Offset=1, the first visible row after the tab bar (row 1) is
	// chat 1 (ID 11), occupying rows 1-2; chat 2 (ID 12) occupies 3-4.
	if id, ok := m.ClickAt(1); !ok || id != 11 {
		t.Fatalf("ClickAt(1) with Offset=1 = (%d, %v), want (11, true)", id, ok)
	}
	if id, ok := m.ClickAt(2); !ok || id != 11 {
		t.Fatalf("ClickAt(2) with Offset=1 = (%d, %v), want (11, true)", id, ok)
	}
	if id, ok := m.ClickAt(3); !ok || id != 12 {
		t.Fatalf("ClickAt(3) with Offset=1 = (%d, %v), want (12, true)", id, ok)
	}
	if id, ok := m.ClickAt(4); !ok || id != 12 {
		t.Fatalf("ClickAt(4) with Offset=1 = (%d, %v), want (12, true)", id, ok)
	}
}

// TestClickAtNoopWhileLoading covers the loading-state guard added
// alongside the tab-bar row-math fix: View() shows only the spinner
// (no tab bar, no clickable list) while the initial chat load is in
// flight, so ClickAt must never resolve a chat in that state, however
// stale list.Items happens to be.
func TestClickAtNoopWhileLoading(t *testing.T) {
	m := newTestModel() // New() always starts with loading == true
	m.list.SetItems([]widgets.ListItem{{ID: "10"}, {ID: "11"}})
	m.list.Height = 20

	if _, ok := m.ClickAt(1); ok {
		t.Fatal("ClickAt should not select a chat while the initial load is in flight")
	}
}

// TestSelectDeltaLoadingGuard mirrors ClickAt's loading guard: SelectDelta
// must never resolve a chat while the initial load is still in flight,
// however stale list.Items happens to be.
func TestSelectDeltaLoadingGuard(t *testing.T) {
	m := newTestModel() // New() always starts with loading == true
	m.list.SetItems([]widgets.ListItem{{ID: "1"}, {ID: "2"}})

	if _, ok := m.SelectDelta(1); ok {
		t.Fatal("SelectDelta should be a no-op while the initial load is in flight")
	}
}

// TestSelectDeltaEmptyList covers the empty-list guard.
func TestSelectDeltaEmptyList(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.refreshList() // no chats in the store -> empty filtered list

	if _, ok := m.SelectDelta(1); ok {
		t.Fatal("SelectDelta on an empty list should return ok=false")
	}
}

// TestSelectDeltaWalksAndClamps walks the selection down then up across a
// multi-chat list and checks both ends clamp (no wrap) rather than
// returning ok=false or wrapping around.
func TestSelectDeltaWalksAndClamps(t *testing.T) {
	m := newTestModel()
	m.loading = false

	m.store.Chats.Set(&telegram.Chat{ID: 1, Type: telegram.ChatTypePrivate, Title: "One", Order: 1})
	m.store.Chats.Set(&telegram.Chat{ID: 2, Type: telegram.ChatTypePrivate, Title: "Two", Order: 2})
	m.store.Chats.Set(&telegram.Chat{ID: 3, Type: telegram.ChatTypePrivate, Title: "Three", Order: 3})
	m.list.Height = 20
	m.refreshList()

	// OrderedChats sorts by Order descending (no pins): chat 3, 2, 1.
	item := m.list.SelectedItem()
	if item == nil || item.ID != "3" {
		t.Fatalf("expected initial selection to be chat 3 (most recent), got %+v", item)
	}

	if id, ok := m.SelectDelta(1); !ok || id != 2 {
		t.Fatalf("SelectDelta(1) = (%d, %v), want (2, true)", id, ok)
	}
	if id, ok := m.SelectDelta(1); !ok || id != 1 {
		t.Fatalf("SelectDelta(1) = (%d, %v), want (1, true)", id, ok)
	}
	// Clamp at the bottom end (no wrap back to the top).
	if id, ok := m.SelectDelta(5); !ok || id != 1 {
		t.Fatalf("SelectDelta(5) at the end = (%d, %v), want (1, true)", id, ok)
	}
	if m.ActiveChatId() != 1 {
		t.Fatalf("ActiveChatId() = %d, want 1", m.ActiveChatId())
	}

	if id, ok := m.SelectDelta(-1); !ok || id != 2 {
		t.Fatalf("SelectDelta(-1) = (%d, %v), want (2, true)", id, ok)
	}
	// Clamp at the top end (no wrap back to the bottom).
	if id, ok := m.SelectDelta(-10); !ok || id != 3 {
		t.Fatalf("SelectDelta(-10) at the start = (%d, %v), want (3, true)", id, ok)
	}
}

// TestSelectDeltaFolderFiltered checks that SelectDelta walks only the
// current folder-filtered item list, skipping chats the active folder
// excludes rather than landing on/passing through them.
func TestSelectDeltaFolderFiltered(t *testing.T) {
	m := newTestModel()
	m.loading = false

	m.store.Chats.Set(&telegram.Chat{ID: 1, Type: telegram.ChatTypePrivate, Title: "DM", Order: 3})
	m.store.Chats.Set(&telegram.Chat{ID: 2, Type: telegram.ChatTypeSupergroup, Title: "Group A", Order: 2})
	m.store.Chats.Set(&telegram.Chat{ID: 3, Type: telegram.ChatTypeSupergroup, Title: "Group B", Order: 1})

	m.folders = []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 1, Title: "Groups", Groups: true},
	}
	m.activeFolder = 1 // the Groups-only folder
	m.list.Height = 20
	m.refreshList()

	if got := len(m.list.Items); got != 2 {
		t.Fatalf("expected 2 group chats in the filtered list, got %d", got)
	}

	item := m.list.SelectedItem()
	if item == nil || item.ID != "2" {
		t.Fatalf("expected initial selection to be group chat 2, got %+v", item)
	}
	if id, ok := m.SelectDelta(1); !ok || id != 3 {
		t.Fatalf("SelectDelta(1) = (%d, %v), want (3, true)", id, ok)
	}
	// The DM (chat 1) is filtered out of this folder entirely, so
	// clamping at the end must stop at chat 3, never selecting it.
	if id, ok := m.SelectDelta(5); !ok || id != 3 {
		t.Fatalf("SelectDelta(5) at the end = (%d, %v), want (3, true)", id, ok)
	}
}

// TestFolderKeyNamesMatchWhatUpdateSwitchesOn pins the key strings the
// switch in Update's tea.KeyPressMsg case is written against: an upstream
// rename of these constants would otherwise silently disable arrow/digit
// folder switching without any test failing closer to the bug.
func TestFolderKeyNamesMatchWhatUpdateSwitchesOn(t *testing.T) {
	cases := map[string]tea.KeyPressMsg{
		"left":  specialKey(tea.KeyLeft),
		"right": specialKey(tea.KeyRight),
		"1":     key('1'),
		"9":     key('9'),
	}
	for want, msg := range cases {
		if got := msg.String(); got != want {
			t.Errorf("key %+v .String() = %q, want %q", msg, got, want)
		}
	}
}

// TestUpdateArrowKeysCycleFoldersWhenFocused covers the terminal-
// independent folder switching added alongside the mouse/digit paths:
// left/right arrows cycle folders (wrapping) while the chat list panel
// has focus.
func TestUpdateArrowKeysCycleFoldersWhenFocused(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.focused = true
	m.folders = []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 1, Title: "Work"},
		{ID: 2, Title: "Family"},
	}
	m.activeFolder = 0

	m, _ = m.Update(specialKey(tea.KeyRight))
	if m.activeFolder != 1 {
		t.Fatalf("after right arrow: activeFolder = %d, want 1", m.activeFolder)
	}
	m, _ = m.Update(specialKey(tea.KeyRight))
	if m.activeFolder != 2 {
		t.Fatalf("after right arrow: activeFolder = %d, want 2", m.activeFolder)
	}
	m, _ = m.Update(specialKey(tea.KeyRight))
	if m.activeFolder != 0 {
		t.Fatalf("right arrow should wrap around: activeFolder = %d, want 0", m.activeFolder)
	}
	m, _ = m.Update(specialKey(tea.KeyLeft))
	if m.activeFolder != 2 {
		t.Fatalf("left arrow should wrap backward: activeFolder = %d, want 2", m.activeFolder)
	}
}

// TestBracketKeysAreNotThisPanels covers decision I-1's other half: [ and ]
// moved to app level, where they cycle folders from the chat view too. The
// panel-local copy is gone, so pressing them here does nothing — one
// behaviour with one implementation, rather than an app-level pair of alt
// chords and a panel-local pair of brackets that only agreed by hand.
func TestBracketKeysAreNotThisPanels(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.focused = true
	m.folders = []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 1, Title: "Work"},
		{ID: 2, Title: "Family"},
	}
	m.activeFolder = 0

	for _, r := range []rune{']', '['} {
		next, _ := m.Update(key(r))
		if next.activeFolder != 0 {
			t.Errorf("%q cycled the folder inside the panel: activeFolder = %d, want 0",
				string(r), next.activeFolder)
		}
	}

	// The arrows are this panel's own and still work.
	m, _ = m.Update(specialKey(tea.KeyRight))
	if m.activeFolder != 1 {
		t.Errorf("the right arrow stopped cycling: activeFolder = %d, want 1", m.activeFolder)
	}
}

// TestUpdateArrowKeysIgnoredWhenNotFocused checks the folder-switching
// keys are only live while the chat list panel itself has focus, matching
// how the rest of Update's key handling is gated on m.focused.
func TestUpdateArrowKeysIgnoredWhenNotFocused(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.focused = false
	m.folders = []*telegram.ChatFolder{defaultAllFolder(), {ID: 1, Title: "Work"}}
	m.activeFolder = 0

	m, _ = m.Update(specialKey(tea.KeyRight))
	if m.activeFolder != 0 {
		t.Fatalf("arrow keys should be a no-op while unfocused, got activeFolder=%d", m.activeFolder)
	}
}

// TestUpdateDigitKeysJumpToFolder covers the digit 1-9 folder-jump keys,
// including clamping when the digit exceeds the number of folders.
func TestUpdateDigitKeysJumpToFolder(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.focused = true
	m.folders = []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 1, Title: "Work"},
		{ID: 2, Title: "Family"},
	}
	m.activeFolder = 0

	m, _ = m.Update(key('3'))
	if m.activeFolder != 2 {
		t.Fatalf("digit '3' should jump to folder index 2, got %d", m.activeFolder)
	}
	m, _ = m.Update(key('1'))
	if m.activeFolder != 0 {
		t.Fatalf("digit '1' should jump to folder index 0 (All), got %d", m.activeFolder)
	}
	// Clamp: a digit beyond the folder count clamps to the last folder
	// instead of doing nothing or panicking.
	m, _ = m.Update(key('9'))
	if m.activeFolder != 2 {
		t.Fatalf("digit '9' with only 3 folders should clamp to the last one (index 2), got %d", m.activeFolder)
	}
}

// TestJumpToFolderClampsAndNoopsOnEmpty covers jumpToFolder's own guard
// directly (Update's digit dispatch is the primary caller).
func TestJumpToFolderClampsAndNoopsOnEmpty(t *testing.T) {
	m := newTestModel()
	m.folders = nil
	m.activeFolder = 0
	m.jumpToFolder(5)
	if m.activeFolder != 0 {
		t.Fatalf("jumpToFolder on an empty folder list should be a no-op, got activeFolder=%d", m.activeFolder)
	}
}

// TestClickAtXYSwitchesFolderOnTabBarClick covers the mouse path: a click
// on the tab bar row switches to whichever folder tab the column falls
// within, and never selects a chat.
// Folder-tab clicking moved out of this package with TUI 2.0: the tabs are
// drawn by the frame's top bar now, so the hit-test lives there
// (topbar.TabAt) and the app routes row 0 to it. The guarantee is unchanged
// and still covered — see TestTabAtHitsTheDrawnSpans in internal/ui/
// components/topbar and TestClickingATopBarTabSwitchesFolder in internal/app.
//
// What this package still owns, and still tests below: everything BELOW the
// header row, plus SelectFolderIndex, which is the other half of that click.

func TestClickAtXYBelowTabBarSelectsChat(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.SetSize(60, 20)
	m.list.SetItems([]widgets.ListItem{{ID: "42"}})

	if chatID, ok := m.ClickAtXY(0, 1); !ok || chatID != 42 {
		t.Fatalf("ClickAtXY below the tab bar = (%d, %v), want (42, true)", chatID, ok)
	}
}

// TestActiveFolderID covers the read-only accessor: the default All
// folder's ID when unset/empty, and the active folder's actual ID once
// folders are populated and CycleFolder has moved off the default.
func TestActiveFolderID(t *testing.T) {
	m := newTestModel()
	if got := m.ActiveFolderID(); got != telegram.AllChatsFolderID {
		t.Fatalf("ActiveFolderID() on a fresh model = %d, want AllChatsFolderID (%d)", got, telegram.AllChatsFolderID)
	}

	m.folders = []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 7, Title: "Work"},
		{ID: 9, Title: "Family"},
	}
	m.activeFolder = 0
	if got := m.ActiveFolderID(); got != telegram.AllChatsFolderID {
		t.Fatalf("ActiveFolderID() at index 0 = %d, want AllChatsFolderID (%d)", got, telegram.AllChatsFolderID)
	}

	m.CycleFolder(1)
	if got := m.ActiveFolderID(); got != 7 {
		t.Fatalf("ActiveFolderID() after CycleFolder(1) = %d, want 7 (Work)", got)
	}

	m.folders = nil
	if got := m.ActiveFolderID(); got != telegram.AllChatsFolderID {
		t.Fatalf("ActiveFolderID() with no folders = %d, want AllChatsFolderID (%d)", got, telegram.AllChatsFolderID)
	}
}

// ---------------------------------------------------------------------------
// Local filter (`/` in the chat list)
// ---------------------------------------------------------------------------

// newLoadedModel returns a chat list that has finished its initial load,
// is focused, has a real size, and holds the named chats (in the given
// order, most recent first). Chat IDs are the 1-based position in names.
func newLoadedModel(t *testing.T, names ...string) Model {
	t.Helper()
	m := newTestModel()
	m.loading = false
	m.SetSize(40, 20)
	m.SetFocused(true)

	for i, name := range names {
		m.store.Chats.Set(&telegram.Chat{
			ID:    int64(i + 1),
			Title: name,
			Type:  telegram.ChatTypePrivate,
			Order: int64(len(names) - i), // preserve the argument order
		})
	}
	m.refreshList()
	return m
}

// listTitles returns the currently visible chat titles, icon prefix
// stripped, so assertions read as the chats a user would see.
func listTitles(m Model) []string {
	out := make([]string, 0, len(m.list.Items))
	for _, it := range m.list.Items {
		// Titles are bare. The chat-type mark used to be a glyph glued to
		// the front of the title and had to be stripped here; TUI 2.0 draws
		// it as a sigil in its own column instead, so it never enters the
		// title string and cannot be truncated away with a long name.
		out = append(out, it.Title)
	}
	return out
}

func typeFilter(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(key(r))
	}
	return m
}

func TestOpenFilterActivatesInput(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Bob")
	if m.FilterActive() {
		t.Fatal("FilterActive() is true before OpenFilter")
	}

	m.OpenFilter()
	if !m.FilterActive() {
		t.Fatal("FilterActive() is false after OpenFilter")
	}
	if m.FilterQuery() != "" {
		t.Fatalf("FilterQuery() = %q on a fresh filter, want empty", m.FilterQuery())
	}
}

// TestOpenFilterWorksWhileLoading: OpenFilter resolves no row into a
// chat, so unlike ClickAt it does not carry a loading guard — a Model
// that never leaves the loading state (internal/app's key tests build
// one with a nil telegram client) must still be able to open the filter,
// or FilterActive() is unreachable and the "an open filter swallows
// h/l/q" negative cases cannot be written at all. Filtering an empty
// list is a no-op, not a panic.
func TestOpenFilterWorksWhileLoading(t *testing.T) {
	m := newTestModel() // New() always starts with loading == true
	m.SetSize(40, 20)
	m.SetFocused(true)

	m.OpenFilter()
	if !m.FilterActive() {
		t.Fatal("OpenFilter must work while the initial load is in flight")
	}

	m = typeFilter(m, "ali")
	if m.FilterQuery() != "ali" {
		t.Fatalf("FilterQuery() = %q while loading, want %q", m.FilterQuery(), "ali")
	}
	if len(m.list.Items) != 0 {
		t.Fatalf("filtering the not-yet-loaded list produced %d items, want 0", len(m.list.Items))
	}
	if m.list.SelectedItem() != nil {
		t.Fatal("SelectedItem() should be nil while the list is empty")
	}
	// View still shows only the spinner; it must not index into the
	// empty list or paint a chip on a tab bar that isn't there.
	if lipgloss.Height(m.View()) != 20 {
		t.Fatalf("View() while loading is not the full-height spinner panel: %q", m.View())
	}
}

// TestFilterTypedWhileLoadingAppliesOnceChatsArrive: the query survives
// the loading -> loaded transition and is applied by the refreshList in
// the chatsLoadedMsg branch, so the user gets the list they asked for
// rather than one that ignores what they typed.
func TestFilterTypedWhileLoadingAppliesOnceChatsArrive(t *testing.T) {
	m := newTestModel() // loading == true
	m.SetSize(40, 20)
	m.SetFocused(true)

	m.OpenFilter()
	m = typeFilter(m, "al")

	for i, name := range []string{"Alice", "Bob", "Carol Alpha"} {
		m.store.Chats.Set(&telegram.Chat{
			ID:    int64(i + 1),
			Title: name,
			Type:  telegram.ChatTypePrivate,
			Order: int64(3 - i),
		})
	}

	m, _ = m.Update(chatsLoadedMsg{})
	if m.loading {
		t.Fatal("chatsLoadedMsg did not clear the loading state")
	}
	if !m.FilterActive() {
		t.Fatal("the filter input should survive the loading -> loaded transition")
	}
	if got := listTitles(m); len(got) != 2 || got[0] != "Alice" || got[1] != "Carol Alpha" {
		t.Fatalf("the query typed while loading was not applied: list = %v, want [Alice, Carol Alpha]", got)
	}
	if !strings.Contains(ansi.Strip(m.View()), "/ al") {
		t.Fatalf("the filter query is missing from the header once the chats "+
			"arrived: %q", ansi.Strip(m.View()))
	}
}

// TestFilterTypingRefiltersLive is the core of review item 3: every
// keystroke narrows the visible list immediately, and backspace widens
// it again.
func TestFilterTypingRefiltersLive(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Bob", "Carol Alpha")
	if got := len(m.list.Items); got != 3 {
		t.Fatalf("unfiltered list has %d items, want 3", got)
	}

	m.OpenFilter()

	m = typeFilter(m, "al")
	if m.FilterQuery() != "al" {
		t.Fatalf("FilterQuery() = %q, want %q", m.FilterQuery(), "al")
	}
	if got := listTitles(m); len(got) != 2 || got[0] != "Alice" || got[1] != "Carol Alpha" {
		t.Fatalf("filter \"al\" shows %v, want [Alice, Carol Alpha]", got)
	}

	// Case-insensitive: an upper-case query matches the same chats.
	m, _ = m.Update(specialKey(tea.KeyBackspace))
	m, _ = m.Update(specialKey(tea.KeyBackspace))
	if m.FilterQuery() != "" {
		t.Fatalf("after two backspaces FilterQuery() = %q, want empty", m.FilterQuery())
	}
	if got := len(m.list.Items); got != 3 {
		t.Fatalf("after clearing the query the list has %d items, want 3", got)
	}

	m = typeFilter(m, "ALPHA")
	if got := listTitles(m); len(got) != 1 || got[0] != "Carol Alpha" {
		t.Fatalf("filter \"ALPHA\" shows %v, want [Carol Alpha]", got)
	}
}

// TestFilterEscClearsAndCloses: esc is the advertised way out — it drops
// the filter AND closes the input, restoring the full list.
func TestFilterEscClearsAndCloses(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Bob", "Carol Alpha")
	m.OpenFilter()
	m = typeFilter(m, "al")

	m, _ = m.Update(specialKey(tea.KeyEscape))
	if m.FilterActive() {
		t.Fatal("esc should close the filter input")
	}
	if m.FilterQuery() != "" {
		t.Fatalf("esc left FilterQuery() = %q, want empty", m.FilterQuery())
	}
	if got := len(m.list.Items); got != 3 {
		t.Fatalf("after esc the list has %d items, want the full 3", got)
	}
}

// TestFilterEnterKeepsFilterApplied: enter closes the input but leaves
// the list filtered — the state the chip's "esc:clear" hint exists for.
func TestFilterEnterKeepsFilterApplied(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Bob", "Carol Alpha")
	m.OpenFilter()
	m = typeFilter(m, "al")

	m, _ = m.Update(specialKey(tea.KeyEnter))
	if m.FilterActive() {
		t.Fatal("enter should close the filter input")
	}
	if m.FilterQuery() != "al" {
		t.Fatalf("enter left FilterQuery() = %q, want it to keep %q", m.FilterQuery(), "al")
	}
	if got := len(m.list.Items); got != 2 {
		t.Fatalf("after enter the list has %d items, want the filtered 2", got)
	}

	// The applied-but-closed filter is still clearable, and reopening
	// keeps the query so `/` doubles as "edit the filter".
	m.OpenFilter()
	if m.FilterQuery() != "al" || m.filterInput.Value != "al" {
		t.Fatalf("reopening dropped the query: FilterQuery()=%q input=%q", m.FilterQuery(), m.filterInput.Value)
	}
	m, _ = m.Update(specialKey(tea.KeyEscape))
	if m.FilterQuery() != "" || len(m.list.Items) != 3 {
		t.Fatalf("esc after reopening did not clear: query=%q items=%d", m.FilterQuery(), len(m.list.Items))
	}
}

// TestFilterAppliesWithinActiveFolder: the filter narrows the ACTIVE
// FOLDER's chats, never reaching across the folder tab — refreshList
// applies the folder membership test first and the filter second.
func TestFilterAppliesWithinActiveFolder(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.SetSize(40, 20)
	m.SetFocused(true)

	m.store.Chats.Set(&telegram.Chat{ID: 1, Title: "Alpha Group", Type: telegram.ChatTypeSupergroup, Order: 3})
	m.store.Chats.Set(&telegram.Chat{ID: 2, Title: "Alpha Person", Type: telegram.ChatTypePrivate, Order: 2})
	m.store.Chats.Set(&telegram.Chat{ID: 3, Title: "Beta Group", Type: telegram.ChatTypeSupergroup, Order: 1})

	m.folders = []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 7, Title: "Groups", Groups: true},
	}

	// In "All", the filter matches both Alpha chats.
	m.activeFolder = 0
	m.refreshList()
	m.OpenFilter()
	m = typeFilter(m, "alpha")
	if got := listTitles(m); len(got) != 2 {
		t.Fatalf("filter \"alpha\" in All shows %v, want both Alpha chats", got)
	}

	// Switching to the Groups folder keeps the filter and intersects
	// with it: the private Alpha chat is out, the Alpha group stays.
	m, _ = m.Update(specialKey(tea.KeyEnter)) // close the input, keep the filter
	m.CycleFolder(1)
	if got := listTitles(m); len(got) != 1 || got[0] != "Alpha Group" {
		t.Fatalf("filter \"alpha\" in the Groups folder shows %v, want [Alpha Group]", got)
	}

	// And clearing the filter inside that folder restores the folder's
	// chats only — never the whole account.
	m.ClearFilter()
	if got := listTitles(m); len(got) != 2 {
		t.Fatalf("after clearing the filter the Groups folder shows %v, want both groups", got)
	}
}

// TestFilterKeepsSelectionInBounds: the cursor is an index into a list
// that shrinks under it on every keystroke. It must never dangle past
// the end, and it should follow the selected chat while that chat still
// matches.
func TestFilterKeepsSelectionInBounds(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Bob", "Carol Alpha", "Dave")
	m.list.SelectIndex(3) // "Dave", the last row
	if got := m.list.SelectedID(); got != "4" {
		t.Fatalf("setup: SelectedID() = %q, want \"4\"", got)
	}

	m.OpenFilter()
	m = typeFilter(m, "a")
	// "Alice", "Carol Alpha" and "Dave" all match: the cursor follows
	// Dave rather than staying on index 3, which no longer exists.
	if got := m.list.SelectedID(); got != "4" {
		t.Fatalf("cursor lost its chat: SelectedID() = %q, want \"4\"", got)
	}

	m = typeFilter(m, "l")
	// "al" drops Dave; the cursor must land on a real row, not past the
	// end of the shrunken list.
	if m.list.Cursor < 0 || m.list.Cursor >= len(m.list.Items) {
		t.Fatalf("cursor %d out of bounds for %d items", m.list.Cursor, len(m.list.Items))
	}
	if m.list.SelectedItem() == nil {
		t.Fatal("SelectedItem() is nil after the list shrank under the cursor")
	}

	// Filtering down to nothing must not panic or dangle either.
	m = typeFilter(m, "zzz")
	if len(m.list.Items) != 0 {
		t.Fatalf("filter \"alzzz\" matched %d chats, want none", len(m.list.Items))
	}
	if m.list.SelectedItem() != nil {
		t.Fatal("SelectedItem() should be nil for an empty filtered list")
	}
}

// TestEnterOpensAVisibleChatAfterFiltering: Enter resolves the chat from
// the list's own selection, so a stale activeChatId (a chat the filter
// hid) can never be what Enter opens.
func TestEnterOpensAVisibleChatAfterFiltering(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Bob", "Carol Alpha")
	m.activeChatId = 2 // "Bob" is open in the chat view

	m.OpenFilter()
	m = typeFilter(m, "carol")
	m, _ = m.Update(specialKey(tea.KeyEnter)) // close input, keep filter

	if got := listTitles(m); len(got) != 1 || got[0] != "Carol Alpha" {
		t.Fatalf("setup: filtered list is %v, want [Carol Alpha]", got)
	}

	m2, cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on the filtered list emitted no ChatSelectedMsg")
	}
	msg, ok := cmd().(ChatSelectedMsg)
	if !ok {
		t.Fatalf("enter emitted %#v, want ChatSelectedMsg", cmd())
	}
	if msg.ChatId != 3 {
		t.Fatalf("enter opened chat %d, want the visible chat 3", msg.ChatId)
	}
	if m2.ActiveChatId() != 3 {
		t.Fatalf("ActiveChatId() = %d after enter, want 3", m2.ActiveChatId())
	}
}

// TestFilterConsumesNavigationAndFolderKeys: an open input is an
// explicit typing mode — j/k, the digits and '['/']' are text there, not
// motions.
func TestFilterConsumesNavigationAndFolderKeys(t *testing.T) {
	m := newLoadedModel(t, "j2 chat", "Bob")
	m.folders = []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 7, Title: "Work"},
	}
	m.refreshList()

	startFolder := m.activeFolder
	m.OpenFilter()
	m = typeFilter(m, "j2")

	if m.FilterQuery() != "j2" {
		t.Fatalf("FilterQuery() = %q, want %q — j/2 must be text, not motions", m.FilterQuery(), "j2")
	}
	if m.activeFolder != startFolder {
		t.Fatalf("digit key switched folders while filtering: activeFolder = %d, want %d", m.activeFolder, startFolder)
	}
	if got := listTitles(m); len(got) != 1 || got[0] != "j2 chat" {
		t.Fatalf("list shows %v, want [j2 chat]", got)
	}
}

// TestFilterSwallowsTheKeyThatOpenedIt: a caller that both calls
// OpenFilter and forwards the '/' key press must not end up with "/" as
// the first character of the query. A later '/' is ordinary text.
func TestFilterSwallowsTheKeyThatOpenedIt(t *testing.T) {
	m := newLoadedModel(t, "Alice", "a/b")
	m.OpenFilter()

	m, _ = m.Update(key('/'))
	if m.FilterQuery() != "" {
		t.Fatalf("a re-delivered '/' was typed into the query: %q", m.FilterQuery())
	}

	m = typeFilter(m, "a/")
	if m.FilterQuery() != "a/" {
		t.Fatalf("FilterQuery() = %q, want %q — only the opening '/' is swallowed", m.FilterQuery(), "a/")
	}
	if got := listTitles(m); len(got) != 1 || got[0] != "a/b" {
		t.Fatalf("filter %q shows %v, want [a/b]", "a/", got)
	}
}

// TestSlashOpensFilterFromTheChatList keeps the component usable
// standalone: '/' reaching Update directly opens the filter rather than
// falling through to the list widget.
func TestSlashOpensFilterFromTheChatList(t *testing.T) {
	m := newLoadedModel(t, "Alice")
	m, _ = m.Update(key('/'))
	if !m.FilterActive() {
		t.Fatal("'/' from the chat list did not open the filter input")
	}
	m = typeFilter(m, "ali")
	if m.FilterQuery() != "ali" {
		t.Fatalf("FilterQuery() = %q, want %q", m.FilterQuery(), "ali")
	}
}

// TestEscClearsAnAppliedFilterFromTheChatList covers the
// applied-but-closed state: esc still means "give me my chats back".
func TestEscClearsAnAppliedFilterFromTheChatList(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Bob")
	m.OpenFilter()
	m = typeFilter(m, "ali")
	m, _ = m.Update(specialKey(tea.KeyEnter))

	m, _ = m.Update(specialKey(tea.KeyEscape))
	if m.FilterQuery() != "" {
		t.Fatalf("esc in the applied-but-closed state left FilterQuery() = %q", m.FilterQuery())
	}
	if len(m.list.Items) != 2 {
		t.Fatalf("esc did not restore the full list: %d items", len(m.list.Items))
	}
}

// TestSetFocusedClosesTheFilterInput: a panel switch must not strand
// FilterActive() true while the keys go elsewhere. The applied filter
// survives (as with enter); only the input closes.
func TestSetFocusedClosesTheFilterInput(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Bob")
	m.OpenFilter()
	m = typeFilter(m, "ali")

	m.SetFocused(false)
	if m.FilterActive() {
		t.Fatal("losing focus should close the filter input")
	}
	if m.FilterQuery() != "ali" {
		t.Fatalf("losing focus dropped the applied filter: %q", m.FilterQuery())
	}
}

// TestFilterIndicatorVisibleWhileFiltered: a user must never wonder why
// chats are missing, and the indicator itself has to name the key that
// brings them back.
func TestFilterIndicatorVisibleWhileFiltered(t *testing.T) {
	m := newLoadedModel(t, "Alice", "Bob", "Carol Alpha")

	if strings.Contains(ansi.Strip(m.View()), "/al") {
		t.Fatal("unfiltered View() shows a filter chip")
	}

	m.OpenFilter()
	m = typeFilter(m, "al")

	open := ansi.Strip(m.View())
	if !strings.Contains(open, "/ al") {
		t.Fatalf("View() while typing does not show the query: %q", open)
	}
	// The clear hint moved from the filter chip to the list footer when the
	// chip was replaced by the header row, but it must still be somewhere:
	// a user who cannot see how to clear a filter is left staring at a
	// partial list.
	if !strings.Contains(open, "esc") {
		t.Fatalf("View() while typing does not advertise how to clear the filter: %q", open)
	}

	// The chip must survive `enter` — that is the state in which the
	// user has no input line to remind them a filter is on.
	m, _ = m.Update(specialKey(tea.KeyEnter))
	closed := ansi.Strip(m.View())
	if !strings.Contains(closed, "/ al") {
		t.Fatalf("View() with the input closed does not show the applied filter: %q", closed)
	}
	if !strings.Contains(closed, "esc") {
		t.Fatalf("View() with the input closed does not advertise how to clear: %q", closed)
	}

	// And it must disappear once the filter is gone.
	m.ClearFilter()
	if strings.Contains(ansi.Strip(m.View()), "/ al") {
		t.Fatal("the filter query outlived the filter")
	}
}

// tabTestFolders is a folder set with the shapes that break naive width
// math: a plain ASCII title, an emoji emoticon, a CJK title, and a long
// title that has to be truncated at every width tested below.
func tabTestFolders() []*telegram.ChatFolder {
	return []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 1, Title: "Work", Emoticon: "💼"},
		{ID: 2, Title: "Family", Emoticon: "👨‍👩‍👧"},
		{ID: 3, Title: "日本語のグループ", Emoticon: "🗾"},
	}
}

// --- decision I-2 / I-1: the cursor, and the unread walk ------------------

// TestCursorChatIdIsNotTheOpenChat: the two were deliberately decoupled so
// that j would not load a history per press, which stands. What did not
// stand was every key that leaves the list rightward reading the OPEN chat
// while the cursor sat somewhere else — jjjl landed in the wrong
// conversation.
func TestCursorChatIdIsNotTheOpenChat(t *testing.T) {
	m := listWithChats(t, 4)

	first := m.CursorChatId()
	if first == 0 {
		t.Fatal("no chat under the cursor")
	}
	if _, ok := m.OpenCursor(); !ok {
		t.Fatal("OpenCursor found nothing to open")
	}
	if m.ActiveChatId() != first {
		t.Fatalf("ActiveChatId = %d, want the opened %d", m.ActiveChatId(), first)
	}

	// Moving the cursor does not move the open chat.
	m.list.ScrollBy(2)
	moved := m.CursorChatId()
	if moved == first {
		t.Fatal("the cursor did not move")
	}
	if m.ActiveChatId() != first {
		t.Errorf("ActiveChatId = %d — moving the cursor opened a chat", m.ActiveChatId())
	}

	// And opening again takes the cursor with it.
	if got, _ := m.OpenCursor(); got != moved {
		t.Errorf("OpenCursor returned %d, want the cursored %d", got, moved)
	}
}

// TestSelectNextUnreadWalksDownAndWrapsOnce is the binding the chat list
// footer advertised for a release with nothing behind it. Down-then-wrap
// rather than "the first unread in the list": the list is ordered by
// recency, so starting from the top would mean pressing u twice went back to
// the same conversation.
func TestSelectNextUnreadWalksDownAndWrapsOnce(t *testing.T) {
	m := listWithChats(t, 5)
	ids := make([]int64, 0, 5)
	for _, item := range m.list.Items {
		ids = append(ids, itemChatId(item))
	}

	// Two unread, one below the cursor and one above it after a wrap.
	m.store.Chats.Set(&telegram.Chat{ID: ids[1], Title: "b", UnreadCount: 2})
	m.store.Chats.Set(&telegram.Chat{ID: ids[3], Title: "d", UnreadCount: 1})

	got, ok := m.SelectNextUnread()
	if !ok || got != ids[1] {
		t.Fatalf("first u chose (%d, %v), want %d", got, ok, ids[1])
	}
	got, ok = m.SelectNextUnread()
	if !ok || got != ids[3] {
		t.Fatalf("second u chose (%d, %v), want %d", got, ok, ids[3])
	}
	// Past the end it wraps, once, back to the first unread.
	got, ok = m.SelectNextUnread()
	if !ok || got != ids[1] {
		t.Fatalf("the wrap chose (%d, %v), want %d", got, ok, ids[1])
	}
}

// TestSelectNextUnreadReportsWhenThereIsNone, so the caller can say so
// rather than leaving a key that looks broken.
func TestSelectNextUnreadReportsWhenThereIsNone(t *testing.T) {
	m := listWithChats(t, 3)
	before := m.CursorChatId()

	if got, ok := m.SelectNextUnread(); ok {
		t.Errorf("SelectNextUnread chose %d with nothing unread", got)
	}
	if m.CursorChatId() != before {
		t.Error("the cursor moved with nothing unread")
	}

	empty := newTestModel()
	empty.loading = false
	if _, ok := empty.SelectNextUnread(); ok {
		t.Error("an empty list reported an unread chat")
	}
}

// listWithChats is a loaded, focused list holding n chats.
func listWithChats(t *testing.T, n int) Model {
	t.Helper()
	m := newTestModel()
	m.loading = false
	m.focused = true
	for i := range n {
		id := int64(100 + i)
		m.store.Chats.Set(&telegram.Chat{
			ID:    id,
			Title: "Chat " + itoa(int(id)),
			Type:  telegram.ChatTypePrivate,
		})
	}
	m.refreshList()
	if got := len(m.list.Items); got != n {
		t.Fatalf("seeded %d chats, want %d", got, n)
	}
	return m
}
