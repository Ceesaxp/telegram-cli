package app

import (
	tea "charm.land/bubbletea/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
	"github.com/charmbracelet/x/ansi"
)

// TestTheHintBarCountsOnlyWhatItHas. A client with nothing unread should
// not spend six cells saying "0 unread", and a client with no chat open has
// no history to size.
func TestTheHintBarCountsOnlyWhatItHas(t *testing.T) {
	m := mainModel(t, PanelChatList)
	m.chatList.MarkLoadedForTest()

	if got := m.hintBarCounters(); got != "0 buffers" {
		t.Fatalf("an empty client said %q", got)
	}

	m.store.Chats.Set(&telegram.Chat{ID: 1, Title: "infra-oncall", Type: telegram.ChatTypeSupergroup})
	m.store.Chats.Set(&telegram.Chat{ID: 2, Title: "relay", Type: telegram.ChatTypeSupergroup, UnreadCount: 2})
	m.chatList.MarkLoadedForTest()
	m.chatList.SetSize(38, 20)
	_ = m.chatList.View() // the list only counts what it has drawn

	if got := m.hintBarCounters(); !strings.Contains(got, "2 buffers") ||
		!strings.Contains(got, "2 unread") || strings.Contains(got, "idx") {
		t.Fatalf("got %q", got)
	}

	m.chatView.OpenChat(1, "infra-oncall")
	for i := range 3 {
		m.store.Messages.Append(1, &telegram.Message{
			ID: int64(i + 1), ChatID: 1,
			SenderID: &telegram.MessageSenderUser{UserID: 9},
			Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "hi"}},
		})
	}
	if got := m.hintBarCounters(); !strings.HasPrefix(got, "idx 3 msgs · ") {
		t.Fatalf("got %q, want the history size first", got)
	}
}

// TestTheCountersReadInPriorityOrder. The bar cuts from the left, so what
// is unread outlives how many chats there are, which outlives the size of
// the history already on screen.
func TestTheCountersReadInPriorityOrder(t *testing.T) {
	m := mainModel(t, PanelChatList)
	m.store.Chats.Set(&telegram.Chat{ID: 1, Title: "a", Type: telegram.ChatTypeSupergroup, UnreadCount: 4})
	m.chatList.MarkLoadedForTest()
	m.chatList.SetSize(38, 20)
	_ = m.chatList.View()
	m.chatView.OpenChat(1, "a")
	m.store.Messages.Append(1, &telegram.Message{
		ID: 1, ChatID: 1,
		SenderID: &telegram.MessageSenderUser{UserID: 9},
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "hi"}},
	})

	got := m.hintBarCounters()
	idx, buffers, unread := strings.Index(got, "idx"), strings.Index(got, "buffers"), strings.Index(got, "unread")
	if idx < 0 || buffers < 0 || unread < 0 {
		t.Fatalf("got %q, want all three counts", got)
	}
	if !(idx < buffers && buffers < unread) {
		t.Fatalf("got %q, want the counts in priority order", got)
	}
}

// TestOpeningAChatNumbersItImmediately.
//
// The buffer number was refreshed on the one-second chrome tick, so a
// freshly opened chat spent up to a second with no number on its header —
// and switching between rows could show the previous chat's, which is the
// exact stale state the field exists to avoid.
func TestOpeningAChatNumbersItImmediately(t *testing.T) {
	m := mainModel(t, PanelChatList)
	for id, title := range map[int64]string{1: "infra-oncall", 2: "relay-protocol"} {
		m.store.Chats.Set(&telegram.Chat{
			ID: id, Title: title, Type: telegram.ChatTypeSupergroup, Order: id,
		})
	}
	m.chatList.MarkLoadedForTest()
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = sized.(Model)
	_ = m.chatList.View()

	// Not a tick, not a resize: the open itself.
	updated, _ := m.Update(chatlist.ChatSelectedMsg{ChatId: 1})
	m = updated.(Model)

	want := m.chatList.BufferIndex(1)
	if want == 0 {
		t.Fatal("precondition: chat 1 should be in the list")
	}
	if got := ansi.Strip(m.chatView.View()); !strings.Contains(got, "buf "+strconv.Itoa(want)) {
		t.Fatalf("the header carries no buffer number on the frame it opened:\n%s", got)
	}

	// And switching does not leave the previous chat's number behind.
	updated, _ = m.Update(chatlist.ChatSelectedMsg{ChatId: 2})
	m = updated.(Model)

	second := m.chatList.BufferIndex(2)
	if second == want {
		t.Skip("both chats are the same row; nothing to tell apart")
	}
	if got := ansi.Strip(m.chatView.View()); !strings.Contains(got, "buf "+strconv.Itoa(second)) {
		t.Fatalf("the header kept the previous chat's number:\n%s", got)
	}
}

// TestTheBufferCountSaysWhatItIsACountOf.
//
// The count has always followed the filter — chatlist.Count is the rendered
// list — so filtering already dropped it. What it did not do was say why,
// and a number falling from twelve to three on its own reads as chats going
// missing rather than as a list being narrowed.
func TestTheBufferCountSaysWhatItIsACountOf(t *testing.T) {
	m := filterModel(t, "Alice", "Bob", "Carol Alpha")

	if got := m.bufferCount(); got != "3 buffers" {
		t.Fatalf("unfiltered: %q, want %q", got, "3 buffers")
	}

	m.chatList.OpenFilter()
	m = typeIntoFilter(t, m, "al")

	if n := m.chatList.Count(); n != 2 {
		t.Fatalf("precondition: the filter shows %d of 3", n)
	}
	if got := m.bufferCount(); got != "2 of 3 buffers" {
		t.Errorf("filtered: %q, want %q", got, "2 of 3 buffers")
	}
	if !strings.Contains(m.hintBarCounters(), "2 of 3 buffers") {
		t.Errorf("the hint bar does not carry it: %q", m.hintBarCounters())
	}
}

// TestTheQualifierSurvivesClosingTheFilterInput.
//
// enter closes the input and leaves the filter applied. That is the state
// where a bare count misleads most — the header has no cursor in it any
// more, so nothing else on screen is obviously mid-filter — which is why
// this reads the applied query rather than FilterActive.
func TestTheQualifierSurvivesClosingTheFilterInput(t *testing.T) {
	m := filterModel(t, "Alice", "Bob", "Carol Alpha")
	m.chatList.OpenFilter()
	m = typeIntoFilter(t, m, "al")

	m.chatList, _ = m.chatList.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.chatList.FilterActive() {
		t.Fatal("precondition: enter did not close the input")
	}
	if m.chatList.FilterQuery() == "" {
		t.Fatal("precondition: enter dropped the filter as well as the input")
	}
	if got := m.bufferCount(); got != "2 of 3 buffers" {
		t.Errorf("with the input closed: %q, want the count still qualified", got)
	}
}

// TestAFilterThatExcludesNothingIsNotAnnounced. Six cells to say a list is
// the same size as itself is six cells the unread count could have had.
func TestAFilterThatExcludesNothingIsNotAnnounced(t *testing.T) {
	m := filterModel(t, "Alpha", "Alpine", "Alto")
	m.chatList.OpenFilter()
	m = typeIntoFilter(t, m, "al")

	if n := m.chatList.Count(); n != 3 {
		t.Fatalf("precondition: the filter excluded something (%d of 3)", n)
	}
	if got := m.bufferCount(); got != "3 buffers" {
		t.Errorf("a filter matching everything says %q", got)
	}
}

// TestTheTwoSurfacesAgreeAboutTheSameList. The chat list's filter header
// draws its own "shown/total" in that column; the hint bar draws one in the
// frame. Different packages, one list — nothing else would catch them
// drifting apart.
func TestTheTwoSurfacesAgreeAboutTheSameList(t *testing.T) {
	m := filterModel(t, "Alice", "Bob", "Carol Alpha")
	m.chatList.OpenFilter()
	m = typeIntoFilter(t, m, "al")

	header := ansi.Strip(m.chatList.View())
	if !strings.Contains(header, "2/3") {
		t.Fatalf("the filter header does not draw 2/3:\n%s", firstLine(header))
	}
	if got := m.bufferCount(); got != "2 of 3 buffers" {
		t.Errorf("the hint bar says %q while the header says 2/3", got)
	}
}

// filterModel is a main-screen model whose chat list holds the named chats.
func filterModel(t *testing.T, titles ...string) Model {
	t.Helper()
	m := mainModel(t, PanelChatList)
	for i, title := range titles {
		m.store.Chats.Set(&telegram.Chat{
			ID: int64(100 + i), Title: title, Type: telegram.ChatTypePrivate,
		})
	}
	m.chatList.MarkLoadedForTest()
	m.chatList.SetSize(40, 20)
	_ = m.chatList.View() // the list only counts what it has drawn
	if got := m.chatList.Count(); got != len(titles) {
		t.Fatalf("the list holds %d chats, want %d", got, len(titles))
	}
	return m
}

func typeIntoFilter(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m.chatList, _ = m.chatList.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestTheTotalIsTheFolderNotTheAccount.
//
// refreshList narrows twice — by folder, then by the typed query — and only
// the second of those is what the reader just did. Counting the whole
// account instead makes the denominator describe a narrowing the filter did
// not perform: in a Groups folder holding two of three chats, a query
// matching both groups excludes nothing, and "2 of 3 buffers" announces a
// filter that is not filtering.
//
// Every other counter test uses the All folder, where the two totals
// coincide and this is invisible.
func TestTheTotalIsTheFolderNotTheAccount(t *testing.T) {
	m := mainModel(t, PanelChatList)
	m.store.Chats.Set(&telegram.Chat{ID: 1, Title: "Alice", Type: telegram.ChatTypePrivate})
	m.store.Chats.Set(&telegram.Chat{ID: 2, Title: "alpha-team", Type: telegram.ChatTypeSupergroup})
	m.store.Chats.Set(&telegram.Chat{ID: 3, Title: "alpine-ops", Type: telegram.ChatTypeSupergroup})
	m.chatList.MarkLoadedForTest()
	m.chatList.SetSize(40, 20)
	m.chatList.SetFolderForTest(&telegram.ChatFolder{ID: 7, Title: "Groups", Groups: true})
	_ = m.chatList.View()

	if got := m.chatList.Count(); got != 2 {
		t.Fatalf("the Groups folder shows %d chats, want the two groups", got)
	}
	if got := m.chatList.TotalCount(); got != 2 {
		t.Fatalf("TotalCount() = %d in a folder of 2 (out of 3 chats), want 2", got)
	}
	if got := m.bufferCount(); got != "2 buffers" {
		t.Fatalf("unfiltered inside a folder: %q", got)
	}

	// A query that excludes nothing must still say so.
	m.chatList.OpenFilter()
	m = typeIntoFilter(t, m, "alp")
	if got := m.chatList.Count(); got != 2 {
		t.Fatalf("precondition: %q matched %d of the folder's 2", "alp", got)
	}
	if got := m.bufferCount(); got != "2 buffers" {
		t.Errorf("a query matching the whole folder says %q — it announces a "+
			"narrowing the filter did not do", got)
	}

	// And one that does exclude something counts against the folder.
	m = typeIntoFilter(t, m, "ha")
	if got := m.chatList.Count(); got != 1 {
		t.Fatalf("precondition: %q matched %d of the folder's 2", "alpha", got)
	}
	if got := m.bufferCount(); got != "1 of 2 buffers" {
		t.Errorf("filtered inside a folder: %q, want %q", got, "1 of 2 buffers")
	}
}

// TestTheHeaderAgreesInsideAFolderToo. The chat list draws its own
// "shown/total" and the hint bar draws one in the frame; they were made to
// share a denominator, and the folder is where a wrong one shows up.
func TestTheHeaderAgreesInsideAFolderToo(t *testing.T) {
	m := mainModel(t, PanelChatList)
	m.store.Chats.Set(&telegram.Chat{ID: 1, Title: "Alice", Type: telegram.ChatTypePrivate})
	m.store.Chats.Set(&telegram.Chat{ID: 2, Title: "alpha-team", Type: telegram.ChatTypeSupergroup})
	m.store.Chats.Set(&telegram.Chat{ID: 3, Title: "alpine-ops", Type: telegram.ChatTypeSupergroup})
	m.chatList.MarkLoadedForTest()
	m.chatList.SetSize(40, 20)
	m.chatList.SetFolderForTest(&telegram.ChatFolder{ID: 7, Title: "Groups", Groups: true})
	m.chatList.OpenFilter()
	m = typeIntoFilter(t, m, "alpha")

	if header := ansi.Strip(m.chatList.View()); !strings.Contains(header, "1/2") {
		t.Errorf("the filter header does not draw 1/2 inside the folder:\n%s", firstLine(header))
	}
	if got := m.bufferCount(); got != "1 of 2 buffers" {
		t.Errorf("the hint bar says %q while the header says 1/2", got)
	}
}
