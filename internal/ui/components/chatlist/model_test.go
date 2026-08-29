package chatlist

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
	"github.com/imtaqin/telegram-cli/internal/ui/widgets"
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
	th := theme.DarkTheme()
	return New(s, nil, th)
}

func TestApplyMediaImageProtocolConstructs(t *testing.T) {
	m := newTestModel()
	m.ApplyMedia(config.MediaConfig{ImageProtocol: "blocks"})
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

// TestClickAtAccountsForTabBar covers the regression where ClickAt passed
// the raw panel-local row straight into ItemAtRow without allowing for
// the folder tab bar occupying row 0: clicks on the tab bar opened the
// first chat, and clicks on a row's second (subtitle) line opened the
// chat below it.
func TestClickAtAccountsForTabBar(t *testing.T) {
	m := newTestModel()
	m.loading = false // ClickAt is a no-op while the initial load is in flight
	if got := m.tabBarHeight(); got != 1 {
		t.Fatalf("tabBarHeight() = %d, want 1 (default All folder always present)", got)
	}

	m.list.SetItems([]widgets.ListItem{
		{ID: "10"},
		{ID: "11"},
		{ID: "12"},
	})
	m.list.Height = 20 // tall enough to show every item without scrolling

	// Row 0 is the tab bar: must not resolve to any chat.
	if _, ok := m.ClickAt(0); ok {
		t.Fatal("ClickAt(0) on the tab bar row should not select a chat")
	}

	// Chat 0 (ID 10) occupies rows 1 (title) and 2 (subtitle), i.e. the
	// two rows right after the 1-row tab bar.
	if id, ok := m.ClickAt(1); !ok || id != 10 {
		t.Fatalf("ClickAt(1) = (%d, %v), want (10, true)", id, ok)
	}
	if id, ok := m.ClickAt(2); !ok || id != 10 {
		t.Fatalf("ClickAt(2) = (%d, %v), want (10, true)", id, ok)
	}

	// Chat 1 (ID 11) occupies rows 3-4.
	if id, ok := m.ClickAt(3); !ok || id != 11 {
		t.Fatalf("ClickAt(3) = (%d, %v), want (11, true)", id, ok)
	}
	if id, ok := m.ClickAt(4); !ok || id != 11 {
		t.Fatalf("ClickAt(4) = (%d, %v), want (11, true)", id, ok)
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

// TestUpdateBracketKeysCycleFoldersWhenFocused checks the lazygit-style
// '['/']' aliases cycle folders exactly like the left/right arrows
// (including wraparound), gated on focus the same way.
func TestUpdateBracketKeysCycleFoldersWhenFocused(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.focused = true
	m.folders = []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 1, Title: "Work"},
		{ID: 2, Title: "Family"},
	}
	m.activeFolder = 0

	m, _ = m.Update(key(']'))
	if m.activeFolder != 1 {
		t.Fatalf("after ']': activeFolder = %d, want 1", m.activeFolder)
	}
	m, _ = m.Update(key(']'))
	if m.activeFolder != 2 {
		t.Fatalf("after ']': activeFolder = %d, want 2", m.activeFolder)
	}
	m, _ = m.Update(key(']'))
	if m.activeFolder != 0 {
		t.Fatalf("']' should wrap around: activeFolder = %d, want 0", m.activeFolder)
	}
	m, _ = m.Update(key('['))
	if m.activeFolder != 2 {
		t.Fatalf("'[' should wrap backward: activeFolder = %d, want 2", m.activeFolder)
	}

	m.focused = false
	m, _ = m.Update(key(']'))
	if m.activeFolder != 2 {
		t.Fatalf("']' should be a no-op while unfocused, got activeFolder=%d", m.activeFolder)
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
func TestClickAtXYSwitchesFolderOnTabBarClick(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.SetSize(60, 20)
	m.folders = []*telegram.ChatFolder{
		defaultAllFolder(),
		{ID: 1, Title: "Work"},
		{ID: 2, Title: "Family"},
	}
	m.activeFolder = 0

	tabs := m.visibleFolderTabs()
	if len(tabs) != 3 {
		t.Fatalf("expected 3 visible tabs at width 60, got %d", len(tabs))
	}

	x := tabs[1].start
	if chatID, ok := m.ClickAtXY(x, 0); ok || chatID != 0 {
		t.Fatalf("a tab-bar click should never select a chat, got (%d, %v)", chatID, ok)
	}
	if m.activeFolder != tabs[1].index {
		t.Fatalf("ClickAtXY(%d, 0) should switch to folder index %d, got %d", x, tabs[1].index, m.activeFolder)
	}
}

// TestClickAtXYBelowTabBarSelectsChat covers the row-below-the-tab-bar
// path, which must behave exactly like the existing ClickAt.
func TestClickAtXYBelowTabBarSelectsChat(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.SetSize(60, 20)
	m.list.SetItems([]widgets.ListItem{{ID: "42"}})

	if chatID, ok := m.ClickAtXY(0, 1); !ok || chatID != 42 {
		t.Fatalf("ClickAtXY below the tab bar = (%d, %v), want (42, true)", chatID, ok)
	}
}

// TestClickAtXYMissBetweenTabsIsNoop covers a click on the tab bar row
// that lands past every tab: it must not select a chat and must not
// change the active folder.
func TestClickAtXYMissBetweenTabsIsNoop(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.SetSize(60, 20)
	m.folders = []*telegram.ChatFolder{defaultAllFolder(), {ID: 1, Title: "Work"}}
	m.activeFolder = 0

	tabs := m.visibleFolderTabs()
	last := tabs[len(tabs)-1]
	if chatID, ok := m.ClickAtXY(last.end+50, 0); ok || chatID != 0 {
		t.Fatalf("click past all tabs should not select a chat, got (%d, %v)", chatID, ok)
	}
	if m.activeFolder != 0 {
		t.Fatalf("click past all tabs should not change the active folder, got %d", m.activeFolder)
	}
}

// TestClickAtXYPinnedSecondTabHit pins a concrete panel-local x/y click on
// the second folder tab to a known folder switch — but critically,
// against renderFolderTabs()'s ACTUAL rendered/styled output, not
// against visibleFolderTabs()'s internal bookkeeping. An earlier version
// of this test located "Work"'s column via visibleFolderTabs() and then
// clicked that same column — self-referential, so it could not catch (and
// didn't: see FINDING 2/7) a bug where visibleFolderTabs()'s reported
// boundaries didn't match what renderFolderTabs() actually painted (the
// bare label was measured, but Tab/TabActive's own Padding(0,2) widens
// each tab by 4 cells once rendered). Finding "Work"'s column in the
// styled string itself — the same thing a real mouse click would be
// aimed at — is what makes this a genuine regression test.
func TestClickAtXYPinnedSecondTabHit(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.SetSize(60, 20)
	m.folders = []*telegram.ChatFolder{
		{ID: telegram.AllChatsFolderID, Title: "All"},
		{ID: 1, Title: "Work"},
		{ID: 2, Title: "Family"},
	}
	m.activeFolder = 0

	rendered := m.renderFolderTabs()
	plain := ansi.Strip(rendered)
	col := strings.Index(plain, "Work")
	if col < 0 {
		t.Fatalf("rendered tab bar does not contain %q at all: %q", "Work", plain)
	}

	if chatID, ok := m.ClickAtXY(col, 0); ok || chatID != 0 {
		t.Fatalf("ClickAtXY(%d, 0) should not select a chat, got (%d, %v)", col, chatID, ok)
	}
	if m.activeFolder != 1 {
		t.Fatalf("clicking column %d (inside the rendered %q label) should switch to folder index 1 (Work), got %d",
			col, "Work", m.activeFolder)
	}
}

// TestVisibleFolderTabsAccountForStylePadding is FINDING 2's direct
// regression test: Tab/TabActive carry their own horizontal padding
// (Padding(0,2), a 4-cell frame in the shipped theme), so each tab's
// on-screen width must be (at least) the label's width plus that frame —
// not just the bare label's width, which is what originally caused every
// tab's hit-test column range to undercount by 4 cells.
func TestVisibleFolderTabsAccountForStylePadding(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.SetSize(60, 20)
	m.folders = []*telegram.ChatFolder{
		{ID: telegram.AllChatsFolderID, Title: "All"},
		{ID: 1, Title: "Work"},
	}
	m.activeFolder = 0

	tabs := m.visibleFolderTabs()
	if len(tabs) != 2 {
		t.Fatalf("expected 2 visible tabs, got %d", len(tabs))
	}

	frame := m.theme.Tab.GetHorizontalFrameSize()
	if frame == 0 {
		t.Skip("theme.Tab has no horizontal padding in this build; the bug this test guards against can't manifest")
	}
	for _, tb := range tabs {
		labelW := ansi.StringWidth(folderLabel(m.folders[tb.index]))
		gotW := tb.end - tb.start
		if gotW < labelW+frame {
			t.Errorf("tab %d: hit-test width %d, want >= label width %d + style frame %d = %d (rendered=%q)",
				tb.index, gotW, labelW, frame, labelW+frame, tb.rendered)
		}
		// And the reported width must match what's actually rendered —
		// the whole point of measuring the styled text.
		if gotW != ansi.StringWidth(tb.rendered) {
			t.Errorf("tab %d: hit-test width %d does not match rendered width %d",
				tb.index, gotW, ansi.StringWidth(tb.rendered))
		}
	}
}

// TestRenderFolderTabsNeverWraps is FINDING 2's second regression test:
// the tab bar must never spill onto a second line — a folder tab the
// model reports as "visible" (via visibleFolderTabs) must actually be
// painted, not silently wrapped off-screen by the old bare
// Width(m.width) call on the concatenated tab text.
func TestRenderFolderTabsNeverWraps(t *testing.T) {
	m := newTestModel()
	m.loading = false
	m.SetSize(28, 20) // the app's default ChatListWidth-ish geometry
	m.folders = []*telegram.ChatFolder{
		{ID: telegram.AllChatsFolderID, Title: "All"},
		{ID: 1, Title: "Work"},
		{ID: 2, Title: "Family"},
		{ID: 3, Title: "Channels"},
	}
	m.activeFolder = 0

	rendered := m.renderFolderTabs()
	if n := lipgloss.Height(rendered); n != 1 {
		t.Fatalf("renderFolderTabs() produced %d lines, want exactly 1: %q", n, rendered)
	}
	if w := ansi.StringWidth(rendered); w > m.width {
		t.Fatalf("renderFolderTabs() has display width %d > panel width %d: %q", w, m.width, rendered)
	}

	// Every tab visibleFolderTabs() reports must actually appear in the
	// rendered text — "visible" and "painted" must agree.
	plain := ansi.Strip(rendered)
	for _, tb := range m.visibleFolderTabs() {
		label := folderLabel(m.folders[tb.index])
		if !strings.Contains(plain, label) {
			t.Errorf("folder %d (%q) is reported visible but not present in the rendered tab bar: %q",
				tb.index, label, plain)
		}
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
