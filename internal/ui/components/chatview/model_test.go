package chatview

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

const testChatID = int64(1)

// fixedDate is a timestamp well in the past, so render.FormatTimestamp
// produces a stable absolute date for every test run (no day-boundary
// flakiness in the cached bubble's stamp).
const fixedDate = int32(1700000000)

// newTestModel builds a real Model (real store, real renderer, fixed dark
// theme) with a chat already open. tg is nil: no test runs a tea.Cmd, so
// no network call is ever made.
func newTestModel() Model {
	m := New(store.NewStore(), nil, theme.DarkRoles(false))
	m.SetSize(60, 20)
	m.chatID = testChatID
	m.chatTitle = "test"
	return m
}

// renderOne renders a single message the way the view does, returning the
// joined lines and the line count. Tests older than the grid were written
// against the bubble renderer's (string, int) shape; this keeps them
// readable without pretending the grid has that shape.
func renderOne(m Model, msg *telegram.Message) (string, int) {
	lines := m.gridBlock(msg, nil)
	return strings.Join(lines, "\n"), len(lines)
}

func textMessage(id, senderID int64, text string) *telegram.Message {
	return &telegram.Message{
		ID:       id,
		ChatID:   testChatID,
		SenderID: &telegram.MessageSenderUser{UserID: senderID},
		Date:     fixedDate,
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: text}},
	}
}

func TestBubbleCacheHitAndInvalidate(t *testing.T) {
	m := newTestModel()
	msg := textMessage(10, 100, "hello")
	m.store.Messages.Append(testChatID, msg)

	first, lines := renderOne(m, msg)
	if lines < 1 {
		t.Fatalf("expected at least one line, got %d", lines)
	}
	if got := m.cache.len(); got != 1 {
		t.Fatalf("expected 1 cache entry, got %d", got)
	}

	// Mutate the message behind the cache's back: a cache hit must return
	// the previously rendered bubble unchanged.
	msg.Content = &telegram.MessageText{Text: &telegram.FormattedText{Text: "completely different text that is much longer"}}
	cached, cachedLines := renderOne(m, msg)
	if cached != first || cachedLines != lines {
		t.Fatalf("expected cache hit to return the original bubble")
	}

	// After invalidation the message is rendered again.
	m.cache.invalidate(msg.ID)
	if got := m.cache.len(); got != 0 {
		t.Fatalf("expected empty cache after invalidate, got %d", got)
	}
	fresh, _ := renderOne(m, msg)
	if fresh == first {
		t.Fatalf("expected a re-render after invalidation")
	}
}

func TestCacheClearedOnWidthChange(t *testing.T) {
	m := newTestModel()
	msg := textMessage(10, 100, strings.Repeat("word ", 40))
	m.store.Messages.Append(testChatID, msg)

	wide, wideLines := renderOne(m, msg)
	entry, ok := m.cache.get(msg.ID)
	if !ok || entry.width != 60 {
		t.Fatalf("expected cache entry rendered at width 60, got %+v (ok=%v)", entry, ok)
	}

	m.SetSize(40, 20)
	if got := m.cache.len(); got != 0 {
		t.Fatalf("expected cache cleared on width change, got %d entries", got)
	}

	narrow, narrowLines := renderOne(m, msg)
	entry, ok = m.cache.get(msg.ID)
	if !ok || entry.width != 40 {
		t.Fatalf("expected cache entry rendered at width 40, got %+v (ok=%v)", entry, ok)
	}
	if narrow == wide || narrowLines <= wideLines {
		t.Fatalf("expected a narrower render to wrap to more lines: %d -> %d", wideLines, narrowLines)
	}

	// Same height, same width: no invalidation.
	m.SetSize(40, 30)
	if got := m.cache.len(); got != 1 {
		t.Fatalf("expected cache kept on height-only resize, got %d entries", got)
	}
}

func TestCacheInvalidatedOnEdit(t *testing.T) {
	m := newTestModel()
	msg := textMessage(10, 100, "original")
	m.store.Messages.Append(testChatID, msg)
	before, _ := renderOne(m, msg)

	edited := textMessage(10, 100, "edited text")
	m, _ = m.Update(messageFetchedMsg{chatID: testChatID, message: edited})

	if _, ok := m.cache.get(10); ok {
		t.Fatalf("expected the edited message's cache entry to be dropped")
	}
	after, _ := renderOne(m, m.store.Messages.Get(testChatID)[0])
	if after == before {
		t.Fatalf("expected the edited message to render differently")
	}
}

func TestCacheInvalidatedOnDeleteAndSendSucceeded(t *testing.T) {
	m := newTestModel()
	msg := textMessage(10, 100, "one")
	m.store.Messages.Append(testChatID, msg)
	renderOne(m, msg)

	m, _ = m.Update(telegram.MessageDeletedMsg{ChatId: testChatID, MessageIds: []int64{10}})
	if m.cache.len() != 0 {
		t.Fatalf("expected cache entry dropped on delete")
	}

	pending := textMessage(11, 100, "sending")
	m.store.Messages.Append(testChatID, pending)
	renderOne(m, pending)
	confirmed := textMessage(12, 100, "sent")
	m, _ = m.Update(telegram.MessageSendSucceededMsg{Message: confirmed, OldMessageId: 11})
	if _, ok := m.cache.get(11); ok {
		t.Fatalf("expected the old message ID's cache entry to be dropped")
	}
	if _, ok := m.cache.get(12); ok {
		t.Fatalf("expected the new message ID to have no stale cache entry")
	}
}

// TestLineIndexConsistency checks that the per-message line counts the
// cache hands out sum to exactly the number of lines View() lays out.
func TestLineIndexConsistency(t *testing.T) {
	m := newTestModel()
	texts := []string{
		"short",
		strings.Repeat("a longer message that has to wrap several times ", 3),
		"multi\nline\nbody",
		strings.Repeat("x", 200),
	}
	for i, txt := range texts {
		m.store.Messages.Append(testChatID, textMessage(int64(i+1), 100, txt))
	}

	msgs := m.store.Messages.Get(testChatID)
	blocks, counts := m.renderedMessages(msgs)
	total := totalRenderedLines(counts)

	var joined []string
	for i, block := range blocks {
		if counts[i] != len(block) {
			t.Fatalf("message %d: cached line count %d != %d", i, counts[i], len(block))
		}
		joined = append(joined, block...)
	}
	if len(joined) != total {
		t.Fatalf("cumulative index %d != laid out lines %d", total, len(joined))
	}

	// The whole window must reproduce the layout exactly.
	all := sliceLines(blocks, counts, 0, total)
	if strings.Join(all, "\n") != strings.Join(joined, "\n") {
		t.Fatalf("sliceLines(0,total) does not reproduce the full layout")
	}
	// And an arbitrary interior window must match the same lines.
	if total > 4 {
		got := sliceLines(blocks, counts, 2, total-2)
		want := joined[2 : total-2]
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("sliceLines interior window mismatch")
		}
	}

	// The line index must survive a second pass unchanged (cache hits).
	_, again := m.renderedMessages(msgs)
	for i := range counts {
		if counts[i] != again[i] {
			t.Fatalf("line count changed between passes at %d: %d != %d", i, counts[i], again[i])
		}
	}
}

// TestCursorFallsBackToTheBottomVisibleMessage walks the exact scrollOffset
// boundaries between messages of unequal height, with no cursor anchored.
func TestCursorFallsBackToTheBottomVisibleMessage(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Append(testChatID, textMessage(1, 100, "one"))
	m.store.Messages.Append(testChatID, textMessage(2, 100, strings.Repeat("wrap me please ", 8)))
	m.store.Messages.Append(testChatID, textMessage(3, 100, "a\nb\nc\nd\ne\nf\ng"))

	msgs := m.store.Messages.Get(testChatID)
	counts := m.lineCounts()
	if counts[0] == counts[1] || counts[1] == counts[2] {
		t.Fatalf("test needs unequal per-message line counts, got %v", counts)
	}

	cases := []struct {
		offset int
		want   int64
	}{
		{0, 3},
		{1, 3},
		{counts[2] - 1, 3},
		{counts[2], 2},
		{counts[2] + counts[1] - 1, 2},
		{counts[2] + counts[1], 1},
		{counts[2] + counts[1] + counts[0] - 1, 1},
		{counts[2] + counts[1] + counts[0], 1}, // past the top: clamps to oldest
		{10000, 1},
	}
	for _, tc := range cases {
		m.scrollOffset = tc.offset
		got := m.cursorMessage()
		if got == nil || got.ID != tc.want {
			t.Fatalf("offset %d: want message %d, got %v (counts=%v, msgs=%d)",
				tc.offset, tc.want, got, counts, len(msgs))
		}
	}
}

func TestStaleHistoryDiscardedByGeneration(t *testing.T) {
	m := newTestModel()
	m.OpenChatAt(testChatID, "first", 0)
	staleGen := m.gen

	// Switching chats bumps the generation.
	m.OpenChatAt(2, "second", 0)
	if m.gen == staleGen {
		t.Fatalf("expected OpenChatAt to bump the generation")
	}

	// Same chat ID as the now-open chat, but the previous generation:
	// the chatID guard would let this through, the generation must not.
	stale := historyLoadedMsg{gen: staleGen, chatID: 2, messages: []*telegram.Message{textMessage(5, 100, "stale")}}
	m2, cmd := m.Update(stale)
	if cmd != nil {
		t.Fatalf("expected no command for a stale generation")
	}
	if n := m2.store.Messages.Count(2); n != 0 {
		t.Fatalf("expected stale history to be dropped, store has %d messages", n)
	}

	// Current generation is accepted and moves on to the sender stage.
	fresh := historyLoadedMsg{gen: m.gen, chatID: 2, messages: []*telegram.Message{textMessage(5, 100, "fresh")}}
	m3, cmd := m.Update(fresh)
	if cmd == nil {
		t.Fatalf("expected the sender-fetch command for the current generation")
	}
	if n := m3.store.Messages.Count(2); n != 1 {
		t.Fatalf("expected the fresh page to be stored, got %d messages", n)
	}
	// First paint: the page is drawable, so the blocking load state is
	// over. The sender/photo work that follows is trailing meta and shows
	// only as the header glyph.
	if m3.loading || m3.loadStatus != "" {
		t.Fatalf("expected loading cleared at first paint, got loading=%v status=%q", m3.loading, m3.loadStatus)
	}
	if !m3.metaBusy {
		t.Fatalf("expected the trailing meta pipeline to be running")
	}
	if m3.statusLineVisible() {
		t.Fatalf("trailing meta must not take a body line")
	}
}

func TestStaleMetaStagesDiscarded(t *testing.T) {
	m := newTestModel()
	m.metaBusy = true
	stale := m.gen - 1

	m2, cmd := m.Update(sendersFetchedMsg{gen: stale, chatID: testChatID})
	if cmd != nil || !m2.metaBusy {
		t.Fatalf("stale sendersFetchedMsg must not end the meta pipeline")
	}
	m3, _ := m.Update(photosFetchedMsg{gen: stale, chatID: testChatID})
	if !m3.metaBusy {
		t.Fatalf("stale photosFetchedMsg must not end the meta pipeline")
	}

	// The current generation ends the pipeline once nothing is owed.
	m4, cmd := m.Update(sendersFetchedMsg{gen: m.gen, chatID: testChatID})
	if cmd != nil {
		t.Fatalf("empty work: expected no follow-up command")
	}
	if m4.metaBusy {
		t.Fatalf("expected the meta pipeline to end when the last stage lands")
	}
}

func TestScrollToMessageCentresTarget(t *testing.T) {
	m := newTestModel()
	for i := 1; i <= 8; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, strings.Repeat("line ", 6*i)))
	}

	if ok := m.scrollToMessage(3); !ok {
		t.Fatalf("expected message 3 to be found")
	}

	msgs := m.store.Messages.Get(testChatID)
	counts := m.lineCounts()
	total := totalRenderedLines(counts)
	bodyH := m.bodyHeight()

	startLine, msgLines := 0, 0
	pos := 0
	for i, msg := range msgs {
		if msg.ID == 3 {
			startLine, msgLines = pos, counts[i]
			break
		}
		pos += counts[i]
	}

	end := total - m.scrollOffset
	start := end - bodyH
	if start < 0 {
		start = 0
	}
	if msgLines <= bodyH && (startLine < start || startLine+msgLines > end) {
		t.Fatalf("target lines [%d,%d) not fully inside window [%d,%d)",
			startLine, startLine+msgLines, start, end)
	}

	// Unknown IDs are reported as not found.
	if m.scrollToMessage(999) {
		t.Fatalf("expected an unknown message ID to report not found")
	}
}

func TestJumpTargetGivesUpGracefully(t *testing.T) {
	m := newTestModel()
	m.chatID = testChatID
	m.gen = 7
	m.loading = true
	m.targetMsgID = 999 // never present in the pages below
	m.targetPages = maxTargetPages

	page := []*telegram.Message{textMessage(1, 100, "a"), textMessage(2, 100, "b")}
	m2, cmd := m.Update(historyLoadedMsg{gen: 7, chatID: testChatID, messages: page})
	if cmd == nil {
		t.Fatalf("expected the loaded page to still be processed")
	}
	if m2.targetMsgID != 0 {
		t.Fatalf("expected the jump target to be abandoned")
	}
	if m2.notice == "" {
		t.Fatalf("expected a notice explaining the message was not found")
	}
	if m2.scrollOffset != m2.maxScrollOffset() {
		t.Fatalf("expected to settle at the oldest loaded position, got %d want %d",
			m2.scrollOffset, m2.maxScrollOffset())
	}
}

func TestReadReceiptsGatedOnTerminalFocus(t *testing.T) {
	m := newTestModel()

	// Default (no focus events ever received) is focused: a read receipt
	// command is issued for incoming messages.
	m2, cmd := m.Update(telegram.NewMessageMsg{Message: textMessage(1, 100, "hi")})
	if cmd == nil {
		t.Fatalf("expected a read-receipt command while focused")
	}
	if m2.pendingReadID != 0 {
		t.Fatalf("expected no deferred read while focused")
	}

	// Blurred: no receipt, remember the newest message instead.
	m3, _ := m2.Update(tea.BlurMsg{})
	m4, cmd := m3.Update(telegram.NewMessageMsg{Message: textMessage(2, 100, "while away")})
	if cmd != nil {
		t.Fatalf("expected no read-receipt command while blurred")
	}
	m4, cmd = m4.Update(telegram.NewMessageMsg{Message: textMessage(3, 100, "also away")})
	if cmd != nil {
		t.Fatalf("expected no read-receipt command while blurred")
	}
	if m4.pendingReadID != 3 {
		t.Fatalf("expected the newest blurred message to be remembered, got %d", m4.pendingReadID)
	}

	// Regaining focus catches up exactly once.
	m5, cmd := m4.Update(tea.FocusMsg{})
	if cmd == nil {
		t.Fatalf("expected a catch-up read receipt on regaining focus")
	}
	if m5.pendingReadID != 0 || m5.blurred {
		t.Fatalf("expected the deferred read to be consumed, got pending=%d blurred=%v", m5.pendingReadID, m5.blurred)
	}
	m6, cmd := m5.Update(tea.FocusMsg{})
	if cmd != nil {
		t.Fatalf("expected no second catch-up receipt, got one")
	}
	_ = m6
}

func TestViewUsesCachedLineIndex(t *testing.T) {
	m := newTestModel()
	for i := 1; i <= 5; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, strings.Repeat("body ", 5*i)))
	}
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 1+m.bodyHeight() {
		t.Fatalf("expected header + %d body lines, got %d", m.bodyHeight(), len(lines))
	}
	if m.cache.len() != 5 {
		t.Fatalf("expected all five bubbles cached after one View, got %d", m.cache.len())
	}

	// A second View must not re-render the history: mutate every message
	// behind the cache's back and check the output is identical.
	//
	// Every message EXCEPT the one under the cursor, which View re-renders
	// on purpose so that selection stays out of the cache key — see
	// gridEntry. That is one message per frame, against a cache that
	// exists to keep the other several hundred off the hot path.
	msgs := m.store.Messages.Get(testChatID)
	cursor := m.cursorMessage()
	if cursor == nil {
		t.Fatal("expected a cursor")
	}
	for _, msg := range msgs {
		if msg == cursor {
			continue
		}
		msg.Content = &telegram.MessageText{Text: &telegram.FormattedText{Text: "MUTATED"}}
	}
	if m.View() != view {
		t.Fatalf("expected the second View to be served from the cache")
	}
}

// TestViewRedrawsTheCursorMessage is the other half of the cache contract:
// selection is deliberately NOT part of the cache key, so the selected
// message is redrawn each frame and its colours can never be stale.
func TestViewRedrawsTheCursorMessage(t *testing.T) {
	m := newTestModel()
	for i := 1; i <= 5; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, "body"))
	}
	view := m.View()

	cursor := m.cursorMessage()
	if cursor == nil {
		t.Fatal("expected a cursor")
	}
	cursor.Content = &telegram.MessageText{Text: &telegram.FormattedText{Text: "MUTATED"}}

	if m.View() == view {
		t.Fatal("expected the cursor message to be redrawn rather than served from the cache")
	}
}

// TestSelectionDoesNotChangeLineCount pins why that is safe. The line index
// is built from unselected renders; if selecting a message changed its
// height, every scroll offset, jump and hit-test in this package would be
// computed against a layout that is not the one on screen.
func TestSelectionDoesNotChangeLineCount(t *testing.T) {
	m := newTestModel()
	for _, text := range []string{
		"short",
		strings.Repeat("a longer message that has to wrap several times ", 3),
		"multi\nline\nbody",
	} {
		msg := textMessage(1, 100, text)
		plain := m.gridMessageLines(msg, nil, false)
		selected := m.gridMessageLines(msg, nil, true)
		if len(plain) != len(selected) {
			t.Fatalf("%q: %d lines unselected, %d selected", text, len(plain), len(selected))
		}
	}
}

func TestViewHeightWithStatusLine(t *testing.T) {
	m := newTestModel()
	for i := 1; i <= 3; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, strings.Repeat("body ", 10)))
	}

	if got := len(strings.Split(m.View(), "\n")); got != m.height {
		t.Fatalf("idle view: expected %d lines, got %d", m.height, got)
	}

	m.loading = true
	m.loadStatus = "Downloading photos..."
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("loading view: expected %d lines, got %d", m.height, len(lines))
	}
	if !strings.Contains(lines[1], "Downloading photos...") {
		t.Fatalf("expected the stage label on the status line, got %q", lines[1])
	}

	// A very long stage label must still occupy exactly one line.
	m.loadStatus = strings.Repeat("very long status ", 20)
	if got := len(strings.Split(m.View(), "\n")); got != m.height {
		t.Fatalf("long status: expected %d lines, got %d", m.height, got)
	}
}

// photoMessage builds a photo message whose thumbnail is a not-yet
// downloaded file, plus a caption that stands in for the rendered body.
func photoMessage(id, senderID int64, fileID, caption string) *telegram.Message {
	return &telegram.Message{
		ID:       id,
		ChatID:   testChatID,
		SenderID: &telegram.MessageSenderUser{UserID: senderID},
		Date:     fixedDate,
		Content: &telegram.MessagePhoto{
			Photo: &telegram.Photo{
				ID: id,
				Sizes: []*telegram.PhotoSize{
					{Type: "s", Width: 100, Height: 100, File: &telegram.File{ID: fileID}},
					{Type: "x", Width: 800, Height: 800, File: &telegram.File{ID: fileID + "-big"}},
				},
			},
			Caption: &telegram.FormattedText{Text: caption},
		},
	}
}

// visibleWindow reproduces View()'s [start,end) line window.
func visibleWindow(m Model) (int, int) {
	total := totalRenderedLines(m.lineCounts())
	end := total - m.scrollOffset
	if end > total {
		end = total
	}
	if end < 0 {
		end = 0
	}
	start := end - m.bodyHeight()
	if start < 0 {
		start = 0
	}
	return start, end
}

// messageLineSpan returns [first,last) rendered line indices of a message.
func messageLineSpan(m Model, id int64) (int, int) {
	counts := m.lineCounts()
	pos := 0
	for i, msg := range m.store.Messages.Get(testChatID) {
		if msg.ID == id {
			return pos, pos + counts[i]
		}
		pos += counts[i]
	}
	return -1, -1
}

// TestJumpSurvivesPhotoStage checks that a jump target stays on screen
// after the photo stage inflates the bubbles below it (a photo grows from
// a one-line placeholder to multi-line art once its thumbnail lands).
func TestJumpSurvivesPhotoStage(t *testing.T) {
	m := newTestModel()

	// Newest first, as the history command returns them: photos are
	// *newer* than the target, so growing them pushes the target up.
	var page []*telegram.Message
	for i := 12; i >= 8; i-- {
		page = append(page, photoMessage(int64(i), 100, fmt.Sprintf("file-%d", i), "photo"))
	}
	for i := 7; i >= 1; i-- {
		page = append(page, textMessage(int64(i), 100, fmt.Sprintf("message %d", i)))
	}

	m.OpenChatAt(testChatID, "jump", 4)
	m2, cmd := m.Update(historyLoadedMsg{gen: m.gen, chatID: testChatID, messages: page})
	if cmd == nil {
		t.Fatalf("expected the sender stage to be scheduled")
	}
	if m2.pendingJumpID != 4 {
		t.Fatalf("expected the jump target to be held until the last meta stage, got %d", m2.pendingJumpID)
	}
	if m2.targetMsgID != 0 {
		t.Fatalf("expected the search to be over once the target was found")
	}

	beforeOffset := m2.scrollOffset
	start, end := visibleWindow(m2)
	lo, hi := messageLineSpan(m2, 4)
	if lo < start || hi > end {
		t.Fatalf("target not visible right after the jump: [%d,%d) window [%d,%d)", lo, hi, start, end)
	}

	// The photo stage lands: every photo bubble grows a lot.
	for _, msg := range m2.store.Messages.Get(testChatID) {
		if _, ok := msg.Content.(*telegram.MessagePhoto); ok {
			msg.Content = &telegram.MessageText{
				Text: &telegram.FormattedText{Text: strings.TrimRight(strings.Repeat("art\n", 20), "\n")},
			}
		}
	}
	var photoIDs []int64
	for i := 8; i <= 12; i++ {
		photoIDs = append(photoIDs, int64(i))
	}

	// Stale generation must not move the view.
	stale, _ := m2.Update(photosFetchedMsg{gen: m2.gen - 1, chatID: testChatID, msgIDs: photoIDs})
	if stale.scrollOffset != beforeOffset || stale.pendingJumpID != 4 {
		t.Fatalf("a stale photo stage must not settle the jump")
	}

	m3, _ := m2.Update(photosFetchedMsg{gen: m2.gen, chatID: testChatID, msgIDs: photoIDs})
	if m3.pendingJumpID != 0 {
		t.Fatalf("expected the jump to be settled and cleared")
	}
	if m3.metaBusy {
		t.Fatalf("expected the meta pipeline to be ended by the last stage")
	}
	if m3.scrollOffset == beforeOffset {
		t.Fatalf("expected the jump to be re-applied after the bubbles grew")
	}
	start, end = visibleWindow(m3)
	lo, hi = messageLineSpan(m3, 4)
	if lo < start || hi > end {
		t.Fatalf("target pushed off screen by the photo stage: [%d,%d) window [%d,%d)", lo, hi, start, end)
	}
}

func TestJumpSettlesWhenSenderStageIsLast(t *testing.T) {
	m := newTestModel()
	m.chatID = testChatID
	m.gen = 3
	m.metaBusy = true
	m.pendingJumpID = 2
	for i := 1; i <= 6; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, "x"))
	}

	// Nothing owed after this stage: it is the last one.
	m2, cmd := m.Update(sendersFetchedMsg{gen: 3, chatID: testChatID})
	if cmd != nil {
		t.Fatalf("expected no follow-up command without further work")
	}
	if m2.pendingJumpID != 0 || m2.metaBusy {
		t.Fatalf("expected the jump settled and the pipeline ended, got pending=%d metaBusy=%v", m2.pendingJumpID, m2.metaBusy)
	}
}

func TestManualScrollCancelsPendingJump(t *testing.T) {
	m := newTestModel()
	m.focused = true
	for i := 1; i <= 6; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, "x"))
	}

	m.pendingJumpID = 3
	m.ScrollByLines(2)
	if m.pendingJumpID != 0 {
		t.Fatalf("expected ScrollByLines to cancel a pending jump")
	}

	m.pendingJumpID = 3
	m2, _ := m.handleKey(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	if m2.pendingJumpID != 0 {
		t.Fatalf("expected a scroll keypress to cancel a pending jump")
	}
}

func TestPhotoDownloadTargetsDedupByFile(t *testing.T) {
	downloaded := photoMessage(4, 100, "file-done", "already here")
	for _, sz := range downloaded.Content.(*telegram.MessagePhoto).Photo.Sizes {
		sz.File.Downloaded = true
	}

	msgs := []*telegram.Message{
		photoMessage(1, 100, "file-a", "a"),
		photoMessage(2, 100, "file-b", "b"),
		photoMessage(3, 100, "file-a", "forward of a"), // same file, second message
		downloaded,
		textMessage(5, 100, "not a photo"),
	}

	order, wanted := photoDownloadTargets(msgs, 0)
	if len(order) != 2 {
		t.Fatalf("expected one download per unique file, got %v", order)
	}
	if order[0] != "file-a" || order[1] != "file-b" {
		t.Fatalf("expected page order preserved, got %v", order)
	}
	if got := wanted["file-a"]; len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("expected both messages fanned out from file-a, got %v", got)
	}
	if _, ok := wanted["file-done"]; ok {
		t.Fatalf("expected an already downloaded file to be skipped")
	}
}

func TestDeletionReclampsScroll(t *testing.T) {
	m := newTestModel()
	for i := 1; i <= 10; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, strings.Repeat("line ", 8)))
	}
	m.scrollOffset = m.maxScrollOffset()
	if m.scrollOffset == 0 {
		t.Fatalf("test needs a history taller than the body")
	}

	var ids []int64
	for i := 1; i <= 8; i++ {
		ids = append(ids, int64(i))
	}
	m2, _ := m.Update(telegram.MessageDeletedMsg{ChatId: testChatID, MessageIds: ids})
	if m2.scrollOffset > m2.maxScrollOffset() {
		t.Fatalf("scroll not re-clamped after deletion: %d > %d", m2.scrollOffset, m2.maxScrollOffset())
	}
	if body := strings.Join(strings.Split(m2.View(), "\n")[1:], ""); strings.TrimSpace(body) == "" {
		t.Fatalf("expected a non-blank body after deletion")
	}

	// Peer-less deletions (DeleteFromAll) must re-clamp too.
	m3 := newTestModel()
	for i := 1; i <= 10; i++ {
		m3.store.Messages.Append(testChatID, textMessage(int64(i), 100, strings.Repeat("line ", 8)))
	}
	m3.scrollOffset = m3.maxScrollOffset()
	m4, _ := m3.Update(telegram.MessageDeletedMsg{ChatId: 0, MessageIds: ids})
	if m4.scrollOffset > m4.maxScrollOffset() {
		t.Fatalf("scroll not re-clamped after a peer-less deletion: %d > %d", m4.scrollOffset, m4.maxScrollOffset())
	}
	body := strings.Join(strings.Split(m4.View(), "\n")[1:], "")
	if strings.TrimSpace(body) == "" {
		t.Fatalf("expected a non-blank body after a peer-less deletion")
	}
}

// key builds a printable keypress; specialKey builds a named one.
func key(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)})
}

func specialKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

func ctrlKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Mod: tea.ModCtrl})
}

func shiftKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Text: string(r), Mod: tea.ModShift})
}

// TestKeyNamesAreWhatHandleKeySwitchesOn pins the key strings the switch
// statements in handleKey are written against: a rename upstream would
// otherwise silently disable pgup/pgdown/ctrl+f.
func TestKeyNamesAreWhatHandleKeySwitchesOn(t *testing.T) {
	cases := map[string]tea.KeyPressMsg{
		"pgup":   specialKey(tea.KeyPgUp),
		"pgdown": specialKey(tea.KeyPgDown),
		"esc":    specialKey(tea.KeyEscape),
		"enter":  specialKey(tea.KeyEnter),
		"ctrl+f": ctrlKey('f'),
		"n":      key('n'),
		"N":      shiftKey('N'),
	}
	for want, msg := range cases {
		if got := msg.String(); got != want {
			t.Fatalf("expected key string %q, got %q", want, got)
		}
	}
}

// TestPageScrollClamping walks pgup to the top and pgdown back to the
// bottom, checking the step size and that neither end can be overshot.
func TestPageScrollClamping(t *testing.T) {
	m := newTestModel()
	m.focused = true
	for i := 1; i <= 30; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, strings.Repeat("line ", 8)))
	}

	if want := m.bodyHeight() - 1; m.pageStep() != want {
		t.Fatalf("expected a page step of bodyHeight-1 = %d, got %d", want, m.pageStep())
	}
	maxOffset := m.maxScrollOffset()
	if maxOffset <= 2*m.pageStep() {
		t.Fatalf("test needs a history several pages tall, got max offset %d", maxOffset)
	}

	// One page up from the bottom.
	m2, _ := m.handleKey(specialKey(tea.KeyPgUp))
	if m2.scrollOffset != m.pageStep() {
		t.Fatalf("expected one page up = %d, got %d", m.pageStep(), m2.scrollOffset)
	}

	// Paging up past the top clamps to the oldest loaded line and stays.
	for i := 0; i < 50; i++ {
		m2, _ = m2.handleKey(specialKey(tea.KeyPgUp))
		if m2.scrollOffset > m2.maxScrollOffset() {
			t.Fatalf("pgup overshot the top: %d > %d", m2.scrollOffset, m2.maxScrollOffset())
		}
	}
	if m2.scrollOffset != m2.maxScrollOffset() {
		t.Fatalf("expected pgup to settle at the top, got %d want %d", m2.scrollOffset, m2.maxScrollOffset())
	}

	// One page down from the top.
	top := m2.scrollOffset
	m3, _ := m2.handleKey(specialKey(tea.KeyPgDown))
	if m3.scrollOffset != top-m2.pageStep() {
		t.Fatalf("expected one page down = %d, got %d", top-m2.pageStep(), m3.scrollOffset)
	}

	// Paging down past the bottom clamps to 0 and never goes negative.
	for i := 0; i < 50; i++ {
		m3, _ = m3.handleKey(specialKey(tea.KeyPgDown))
		if m3.scrollOffset < 0 {
			t.Fatalf("pgdown went negative: %d", m3.scrollOffset)
		}
	}
	if m3.scrollOffset != 0 {
		t.Fatalf("expected pgdown to settle at the newest message, got %d", m3.scrollOffset)
	}

	// A page key also cancels a jump waiting to settle.
	m3.pendingJumpID = 5
	m4, _ := m3.handleKey(specialKey(tea.KeyPgUp))
	if m4.pendingJumpID != 0 {
		t.Fatalf("expected a page keypress to cancel a pending jump")
	}
}

// TestPageScrollOnShortHistory checks the degenerate case: nothing to
// scroll, so both page keys are no-ops rather than negative offsets.
func TestPageScrollOnShortHistory(t *testing.T) {
	m := newTestModel()
	m.focused = true
	m.store.Messages.Append(testChatID, textMessage(1, 100, "only one"))

	m2, _ := m.handleKey(specialKey(tea.KeyPgUp))
	if m2.scrollOffset != 0 {
		t.Fatalf("expected pgup on a one-screen history to stay put, got %d", m2.scrollOffset)
	}
	m3, _ := m2.handleKey(specialKey(tea.KeyPgDown))
	if m3.scrollOffset != 0 {
		t.Fatalf("expected pgdown to stay at 0, got %d", m3.scrollOffset)
	}
}

// TestSearchModeCapturesKeys checks that ctrl+f opens the input, that
// characters land in the input instead of scrolling, and that esc cancels
// without running anything.
func TestSearchModeCapturesKeys(t *testing.T) {
	m := newTestModel()
	m.focused = true
	for i := 1; i <= 20; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, strings.Repeat("line ", 8)))
	}
	m.scrollOffset = 4

	m, _ = m.handleKey(ctrlKey('f'))
	if !m.searchActive || !m.SearchActive() {
		t.Fatalf("expected ctrl+f to open the search input")
	}
	if !m.statusLineVisible() {
		t.Fatalf("expected the search input to occupy the status line")
	}

	// "kj" would scroll by six lines if the scroll handler saw it.
	before := m.scrollOffset
	for _, r := range "kj hello" {
		m, _ = m.handleKey(key(r))
	}
	if m.scrollOffset != before {
		t.Fatalf("expected search keys not to scroll: %d -> %d", before, m.scrollOffset)
	}
	if m.searchInput.Value != "kj hello" {
		t.Fatalf("expected the typed text in the input, got %q", m.searchInput.Value)
	}

	// The search line must stay exactly one line, and the whole view must
	// still be exactly m.height lines tall.
	if n := strings.Count(m.renderSearchLine(), "\n"); n != 0 {
		t.Fatalf("expected a one-line search input, got %d extra lines", n)
	}
	if got := len(strings.Split(m.View(), "\n")); got != m.height {
		t.Fatalf("search view: expected %d lines, got %d", m.height, got)
	}

	// esc cancels: input closed and cleared, nothing dispatched.
	m2, cmd := m.handleKey(specialKey(tea.KeyEscape))
	if cmd != nil {
		t.Fatalf("expected esc to run nothing")
	}
	if m2.searchActive || m2.searchInput.Value != "" {
		t.Fatalf("expected esc to close and clear the input")
	}
	if m2.statusLineVisible() {
		t.Fatalf("expected the body line back after esc")
	}

	// Scrolling works again once the input is closed.
	m3, _ := m2.handleKey(key('k'))
	if m3.scrollOffset == m2.scrollOffset {
		t.Fatalf("expected scroll keys to work again after esc")
	}
}

// TestSearchEnterOnEmptyQueryDoesNotDispatch guards the client contract:
// SearchChatMessages rejects an empty query outright.
func TestSearchEnterOnEmptyQueryDoesNotDispatch(t *testing.T) {
	m := newTestModel()
	m.focused = true
	m, _ = m.handleKey(ctrlKey('f'))

	m2, cmd := m.handleKey(specialKey(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("expected no search command for an empty query")
	}
	if !m2.searchActive {
		t.Fatalf("expected to stay in the input after an empty enter")
	}
	if m2.notice == "" {
		t.Fatalf("expected a note telling the user to type a query")
	}

	// Whitespace only is the same case.
	m3, _ := m2.handleKey(key(' '))
	m4, cmd := m3.handleKey(specialKey(tea.KeyEnter))
	if cmd != nil || !m4.searchActive {
		t.Fatalf("expected a whitespace-only query to be rejected too")
	}
}

// TestSearchResultsCycleWithWrap covers n/N cycling, the "match i/n"
// note, and that stale results (wrong generation) are dropped.
func TestSearchResultsCycleWithWrap(t *testing.T) {
	m := newTestModel()
	m.focused = true
	for i := 1; i <= 12; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, strings.Repeat("line ", 6)))
	}
	m.searchQuery = "q"

	hits := []int64{9, 6, 3} // newest first, as the server returns them
	stale, _ := m.Update(searchResultsMsg{gen: m.gen - 1, chatID: testChatID, query: "q", hits: hits})
	if len(stale.searchHits) != 0 {
		t.Fatalf("expected results from a stale generation to be dropped")
	}
	other, _ := m.Update(searchResultsMsg{gen: m.gen, chatID: testChatID, query: "different", hits: hits})
	if len(other.searchHits) != 0 {
		t.Fatalf("expected results for a superseded query to be dropped")
	}

	m, _ = m.Update(searchResultsMsg{gen: m.gen, chatID: testChatID, query: "q", hits: hits})
	if !m.HasSearchResults() || m.searchIdx != 0 {
		t.Fatalf("expected to land on the first hit, got idx=%d hits=%v", m.searchIdx, m.searchHits)
	}
	if m.notice != "match 1/3" {
		t.Fatalf("expected a match counter, got %q", m.notice)
	}
	firstOffset := m.scrollOffset

	// n walks forward and wraps.
	want := []struct {
		idx    int
		notice string
	}{{1, "match 2/3"}, {2, "match 3/3"}, {0, "match 1/3"}}
	for _, w := range want {
		m, _ = m.handleKey(key('n'))
		if m.searchIdx != w.idx || m.notice != w.notice {
			t.Fatalf("n: expected idx %d note %q, got %d %q", w.idx, w.notice, m.searchIdx, m.notice)
		}
	}
	if m.scrollOffset != firstOffset {
		t.Fatalf("expected a full wrap to return to the first hit's position")
	}

	// N walks backwards and wraps the other way.
	back := []struct {
		idx    int
		notice string
	}{{2, "match 3/3"}, {1, "match 2/3"}, {0, "match 1/3"}}
	for _, w := range back {
		m, _ = m.handleKey(shiftKey('N'))
		if m.searchIdx != w.idx || m.notice != w.notice {
			t.Fatalf("N: expected idx %d note %q, got %d %q", w.idx, w.notice, m.searchIdx, m.notice)
		}
	}

	// Every hit must actually be brought into the visible window.
	for i := range hits {
		m.jumpToHit(i)
		start, end := visibleWindow(m)
		lo, hi := messageLineSpan(m, hits[i])
		if lo < start || hi > end {
			t.Fatalf("hit %d not visible: [%d,%d) window [%d,%d)", hits[i], lo, hi, start, end)
		}
	}

	// Opening another chat clears the results.
	m.OpenChatAt(testChatID, "again", 0)
	if m.HasSearchResults() || m.searchActive {
		t.Fatalf("expected a chat switch to clear the search state")
	}
}

// TestSearchNKeysAreInertWithoutResults makes sure n/N do nothing (rather
// than scroll or panic) when no search has run.
func TestSearchNKeysAreInertWithoutResults(t *testing.T) {
	m := newTestModel()
	m.focused = true
	for i := 1; i <= 12; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, "x"))
	}
	m.scrollOffset = 3

	for _, k := range []tea.KeyPressMsg{key('n'), shiftKey('N')} {
		m2, cmd := m.handleKey(k)
		if cmd != nil || m2.scrollOffset != 3 || m2.notice != "" {
			t.Fatalf("expected %q to be inert without results", k.String())
		}
	}
}

// TestSearchHitNotLoadedStartsHunt checks the fallback path: a hit that is
// not in the loaded window hands off to the existing backwards page walk.
func TestSearchHitNotLoadedStartsHunt(t *testing.T) {
	m := newTestModel()
	for i := 10; i <= 20; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, "x"))
	}
	m.searchHits = []int64{3}

	// No client: the hunt cannot start, and the model must say so rather
	// than pretend the jump worked.
	m.jumpToHit(0)
	if m.targetMsgID != 0 || m.loading {
		t.Fatalf("expected no hunt without a client, got target=%d loading=%v", m.targetMsgID, m.loading)
	}
	if m.notice != "message not in loaded history" {
		t.Fatalf("expected an honest notice, got %q", m.notice)
	}
}

// TestFirstPaintDoesNotWaitForMeta is the latency regression test: the
// moment the history page lands, the bubbles are drawable and the blocking
// status line is gone, even though sender/photo work is still queued.
func TestFirstPaintDoesNotWaitForMeta(t *testing.T) {
	m := newTestModel()
	m.OpenChatAt(testChatID, "chat", 0)
	if !m.loading {
		t.Fatalf("expected the initial history fetch to be a blocking stage")
	}

	// Newest first, as GetChatHistory returns them: text on top of the
	// history (what the reader sees), photos further back.
	var page []*telegram.Message
	for i := 12; i >= 6; i-- {
		page = append(page, textMessage(int64(i), int64(300+i), fmt.Sprintf("message %d", i)))
	}
	for i := 5; i >= 1; i-- {
		page = append(page, photoMessage(int64(i), int64(200+i), fmt.Sprintf("file-%d", i), "photo"))
	}

	m2, cmd := m.Update(historyLoadedMsg{gen: m.gen, chatID: testChatID, messages: page})
	if cmd == nil {
		t.Fatalf("expected the trailing meta pipeline to be scheduled")
	}
	if m2.loading || m2.loadStatus != "" {
		t.Fatalf("first paint must not be gated on meta: loading=%v status=%q", m2.loading, m2.loadStatus)
	}
	if !m2.metaBusy {
		t.Fatalf("expected the trailing pipeline to be marked busy")
	}
	if m2.statusLineVisible() {
		t.Fatalf("trailing meta must not take a body line")
	}

	// The body genuinely contains message text now, not a spinner.
	view := m2.View()
	lines := strings.Split(view, "\n")
	if len(lines) != m2.height {
		t.Fatalf("expected a full-height view, got %d lines", len(lines))
	}
	if !strings.Contains(view, "message 12") {
		t.Fatalf("expected message text on the first paint, got:\n%s", view)
	}
	// The header carries the only hint that meta is still running.
	if !strings.Contains(lines[0], "⟳") {
		t.Fatalf("expected a subtle meta indicator in the header, got %q", lines[0])
	}
}

// TestPhotoPrefetchIsCapped checks the open-time cap: at most
// photoPrefetchLimit thumbnails, and the newest ones.
func TestPhotoPrefetchIsCapped(t *testing.T) {
	st := store.NewStore()
	var msgs []*telegram.Message
	for i := 1; i <= 25; i++ { // oldest first, as the store holds them
		msgs = append(msgs, photoMessage(int64(i), 100, fmt.Sprintf("file-%d", i), "photo"))
	}

	targets := recentPhotoTargets(msgs, st, photoPrefetchLimit, 0)
	if len(targets) != photoPrefetchLimit {
		t.Fatalf("expected the prefetch capped at %d, got %d", photoPrefetchLimit, len(targets))
	}
	// The newest photoPrefetchLimit messages, in page order.
	for i, msg := range targets {
		if want := int64(25 - photoPrefetchLimit + 1 + i); msg.ID != want {
			t.Fatalf("target %d: expected message %d, got %d", i, want, msg.ID)
		}
	}

	// Text messages are never targets, and already-downloaded thumbnails
	// are skipped on the store's word, not the message's stale flag.
	mixed := []*telegram.Message{
		textMessage(1, 100, "text"),
		photoMessage(2, 100, "file-a", "a"),
		photoMessage(3, 100, "file-b", "b"),
	}
	st.Files.Update(&telegram.File{ID: "file-b", Path: "/tmp/b.jpg", Downloaded: true})
	targets = recentPhotoTargets(mixed, st, photoPrefetchLimit, 0)
	if len(targets) != 1 || targets[0].ID != 2 {
		t.Fatalf("expected only the undownloaded photo, got %v", targets)
	}

	// A zero/negative cap fetches nothing.
	if got := recentPhotoTargets(msgs, st, 0, 0); got != nil {
		t.Fatalf("expected no targets for a zero cap, got %d", len(got))
	}
}

func TestApplyMediaDisablesPhotoPrefetch(t *testing.T) {
	m := newTestModel()
	m.ApplyMedia(config.MediaConfig{ImageProtocol: "blocks", AutoDownloadPhotos: false})

	var msgs []*telegram.Message
	for i := 1; i <= 5; i++ {
		msgs = append(msgs, photoMessage(int64(i), 100, fmt.Sprintf("file-%d", i), "photo"))
	}
	if got := m.photoPrefetchTargets(msgs); got != nil {
		t.Fatalf("expected no photo prefetch when AutoDownloadPhotos=false, got %d", len(got))
	}
}

func TestApplyMediaImageProtocolConstructs(t *testing.T) {
	m := newTestModel()
	m.ApplyMedia(config.MediaConfig{ImageProtocol: "blocks"})
}

func TestPhotoDownloadSkipsOversize(t *testing.T) {
	small := photoMessage(1, 100, "file-small", "s")
	big := photoMessage(2, 100, "file-big", "b")
	unknown := photoMessage(3, 100, "file-unknown", "u")
	small.Content.(*telegram.MessagePhoto).Photo.Sizes[0].File.Size = 100
	big.Content.(*telegram.MessagePhoto).Photo.Sizes[0].File.Size = 3 << 20
	// unknown keeps Size 0

	const maxBytes = 1 << 20
	order, wanted := photoDownloadTargets([]*telegram.Message{small, big, unknown}, maxBytes)
	if len(order) != 2 || order[0] != "file-small" || order[1] != "file-unknown" {
		t.Fatalf("expected small+unknown, got %v", order)
	}
	if _, ok := wanted["file-big"]; ok {
		t.Fatalf("oversized photo should not be a download target")
	}
	if needsThumbnail(big, store.NewStore(), maxBytes) {
		t.Fatalf("oversized photo should not need a thumbnail")
	}
}

// TestVisiblePhotoTargetsFollowTheWindow checks the lazy half: photos far
// from the viewport are not fetched, photos near it are.
func TestVisiblePhotoTargetsFollowTheWindow(t *testing.T) {
	m := newTestModel()
	for i := 1; i <= 40; i++ {
		m.store.Messages.Append(testChatID, photoMessage(int64(i), 100, fmt.Sprintf("file-%d", i), "photo"))
	}

	m.scrollOffset = 0
	atBottom := m.visiblePhotoTargets(photoLazyMargin)
	if len(atBottom) == 0 {
		t.Fatalf("expected the newest photos to be targets at the bottom")
	}
	if len(atBottom) > photoPrefetchLimit {
		t.Fatalf("lazy targets must stay capped, got %d", len(atBottom))
	}
	for _, msg := range atBottom {
		if msg.ID < 20 {
			t.Fatalf("photo %d is nowhere near the bottom of the history", msg.ID)
		}
	}

	m.scrollOffset = m.maxScrollOffset()
	atTop := m.visiblePhotoTargets(photoLazyMargin)
	if len(atTop) == 0 {
		t.Fatalf("expected the oldest photos to be targets at the top")
	}
	for _, msg := range atTop {
		if msg.ID > 20 {
			t.Fatalf("photo %d is nowhere near the top of the history", msg.ID)
		}
	}

	// Once the store knows the files are down, nothing is left to fetch.
	for i := 1; i <= 40; i++ {
		m.store.Files.Update(&telegram.File{ID: fmt.Sprintf("file-%d", i), Path: "/tmp/x.jpg", Downloaded: true})
	}
	if got := m.visiblePhotoTargets(photoLazyMargin); len(got) != 0 {
		t.Fatalf("expected no targets once every thumbnail is downloaded, got %d", len(got))
	}
}

// TestBestPhotoSizeSkipsUnregisteredSizes covers the external-viewer path:
// the size handed to the opener must be the largest one that actually has
// a file behind it — a nil File used to panic here.
func TestBestPhotoSizeSkipsUnregisteredSizes(t *testing.T) {
	photo := &telegram.Photo{Sizes: []*telegram.PhotoSize{
		{Type: "s", Width: 100, Height: 100, File: &telegram.File{ID: "small"}},
		{Type: "y", Width: 1280, Height: 1280, File: &telegram.File{ID: "big"}},
		{Type: "w", Width: 2560, Height: 2560, File: nil}, // no download location
	}}
	best := bestPhotoSize(photo)
	if best == nil || best.File == nil || best.File.ID != "big" {
		t.Fatalf("expected the largest downloadable size, got %+v", best)
	}

	// Every size unregistered: no panic, no command.
	none := &telegram.Photo{Sizes: []*telegram.PhotoSize{{Type: "w", Width: 100, File: nil}}}
	if bestPhotoSize(none) != nil {
		t.Fatalf("expected no size when none is downloadable")
	}
	if thumbnailSize(none) != nil {
		t.Fatalf("expected no thumbnail when none is downloadable")
	}

	m := newTestModel()
	m.focused = true
	m.store.Messages.Append(testChatID, &telegram.Message{
		ID: 1, ChatID: testChatID, Date: fixedDate,
		SenderID: &telegram.MessageSenderUser{UserID: 100},
		Content:  &telegram.MessagePhoto{Photo: none},
	})
	if cmd := m.playMedia(); cmd != nil {
		t.Fatalf("expected no open command for a photo with no downloadable size")
	}
	if cmd := m.downloadFile(); cmd != nil {
		t.Fatalf("expected no save command for a photo with no downloadable size")
	}

	// A photo with no sizes at all is equally inert.
	m2 := newTestModel()
	m2.focused = true
	m2.store.Messages.Append(testChatID, &telegram.Message{
		ID: 1, ChatID: testChatID, Date: fixedDate,
		SenderID: &telegram.MessageSenderUser{UserID: 100},
		Content:  &telegram.MessagePhoto{Photo: &telegram.Photo{}},
	})
	if m2.playMedia() != nil || m2.downloadFile() != nil {
		t.Fatalf("expected a size-less photo to produce no media command")
	}
}

// TestMediaHintTracksTheTargetMessage checks the discoverability line: the
// header names the media keys exactly when the targeted message has media.
func TestMediaHintTracksTheTargetMessage(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Append(testChatID, textMessage(1, 100, "plain text"))
	if hint := m.mediaHint(); hint != "" {
		t.Fatalf("expected no media hint for a text message, got %q", hint)
	}

	m.store.Messages.Append(testChatID, photoMessage(2, 100, "file-a", "a photo"))
	if hint := m.mediaHint(); !strings.Contains(hint, "open photo") {
		t.Fatalf("expected a photo hint, got %q", hint)
	}
	if !strings.Contains(m.View(), "open photo") {
		t.Fatalf("expected the hint to reach the header")
	}
}

// TestSenderTargetsPrioritiseRecentMessages checks the sender stage split.
func TestSenderTargetsPrioritiseRecentMessages(t *testing.T) {
	st := store.NewStore()
	var msgs []*telegram.Message // oldest first
	for i := 1; i <= 30; i++ {
		msgs = append(msgs, textMessage(int64(i), int64(1000+i), "x"))
	}

	priority, trailing := senderTargets(msgs, st, senderPriorityWindow)
	if len(priority) != senderPriorityWindow {
		t.Fatalf("expected %d priority senders, got %d", senderPriorityWindow, len(priority))
	}
	if len(trailing) != 30-senderPriorityWindow {
		t.Fatalf("expected %d trailing senders, got %d", 30-senderPriorityWindow, len(trailing))
	}
	// Newest first, so the names on screen resolve before the rest.
	if priority[0] != 1030 || priority[len(priority)-1] != int64(1030-senderPriorityWindow+1) {
		t.Fatalf("expected the priority window newest-first, got %v", priority)
	}
	if trailing[0] != int64(1030-senderPriorityWindow) {
		t.Fatalf("expected the trailing list to continue backwards, got %v", trailing)
	}

	// Senders already in the store are not fetched again, and one sender
	// appearing many times is fetched once.
	st.Users.Set(&telegram.User{ID: 1030})
	same := []*telegram.Message{
		textMessage(1, 42, "a"), textMessage(2, 42, "b"), textMessage(3, 1030, "c"),
	}
	priority, trailing = senderTargets(same, st, senderPriorityWindow)
	if len(priority) != 1 || priority[0] != 42 || len(trailing) != 0 {
		t.Fatalf("expected one deduplicated unknown sender, got %v / %v", priority, trailing)
	}
}

// TestMetaPipelineIsLinear walks the whole trailing chain: priority
// senders -> photos -> trailing senders -> settle.
func TestMetaPipelineIsLinear(t *testing.T) {
	m := newTestModel()
	m.chatID = testChatID
	m.gen = 5
	m.metaBusy = true
	m.pendingJumpID = 1

	photos := []*telegram.Message{photoMessage(2, 100, "file-a", "a")}
	m.store.Messages.Append(testChatID, textMessage(1, 100, "x"))
	m.store.Messages.Append(testChatID, photos[0])

	// Priority senders land; photos are owed, so the chain continues.
	m2, cmd := m.Update(sendersFetchedMsg{
		gen: 5, chatID: testChatID, userIDs: []int64{100},
		work: metaWork{photos: photos, senders: []int64{101}},
	})
	if cmd == nil || !m2.metaBusy || m2.pendingJumpID != 1 {
		t.Fatalf("expected the photo stage to follow, pipeline still busy")
	}

	// Photos land; trailing senders are owed, so the chain continues.
	m3, cmd := m2.Update(photosFetchedMsg{
		gen: 5, chatID: testChatID, msgIDs: []int64{2},
		work: metaWork{senders: []int64{101}},
	})
	if cmd == nil || !m3.metaBusy || m3.pendingJumpID != 1 {
		t.Fatalf("expected the trailing sender stage to follow, pipeline still busy")
	}

	// Trailing senders land with nothing owed: last stage, jump settles.
	m4, cmd := m3.Update(sendersFetchedMsg{gen: 5, chatID: testChatID, userIDs: []int64{101}})
	if cmd != nil {
		t.Fatalf("expected the pipeline to end")
	}
	if m4.metaBusy || m4.pendingJumpID != 0 {
		t.Fatalf("expected the last stage to end the pipeline and settle the jump")
	}
}

// TestLazyMediaCmdIsIdempotentWhileBusy makes sure a held-down scroll key
// (or a spun mouse wheel) cannot pile up one download command per event.
func TestLazyMediaCmdIsIdempotentWhileBusy(t *testing.T) {
	m := newTestModel()
	for i := 1; i <= 10; i++ {
		m.store.Messages.Append(testChatID, photoMessage(int64(i), 100, fmt.Sprintf("file-%d", i), "photo"))
	}
	// No client: nothing is dispatched and nothing is claimed busy.
	if cmd := m.LazyMediaCmd(); cmd != nil || m.metaBusy {
		t.Fatalf("expected no lazy load without a client")
	}

	// With the pipeline already running, a second request is a no-op.
	m.metaBusy = true
	if cmd := m.LazyMediaCmd(); cmd != nil {
		t.Fatalf("expected no second lazy load while the pipeline is busy")
	}
}

// TestHeaderAndStatusStayOneLineWithWideGlyphs is the regression test for
// cell-vs-rune truncation: a CJK title is one rune but two cells wide, so
// a rune-count cut still overflows the panel and lipgloss wraps the header
// onto extra rows, pushing the body off the bottom of the view.
func TestHeaderAndStatusStayOneLineWithWideGlyphs(t *testing.T) {
	m := newTestModel()
	m.SetSize(40, 20)
	// 34 runes, 68 cells — comfortably past a width-40 panel.
	m.chatTitle = strings.Repeat("日本語会話", 6) + "テスト"
	m.notice = strings.Repeat("通知", 10)
	m.mediaStatus = strings.Repeat("再生中", 8)
	m.store.Messages.Append(testChatID, textMessage(1, 100, "body"))

	header := m.renderHeader()
	if got := lipgloss.Height(header); got != 1 {
		t.Fatalf("expected a one-line header, got %d lines:\n%s", got, header)
	}
	if got := lipgloss.Width(header); got > m.width {
		t.Fatalf("header is %d cells wide, panel is %d", got, m.width)
	}

	m.loading = true
	m.loadStatus = strings.Repeat("読み込み中", 12)
	status := m.renderStatusLine()
	if got := lipgloss.Height(status); got != 1 {
		t.Fatalf("expected a one-line status, got %d lines:\n%s", got, status)
	}
	if got := lipgloss.Width(status); got > m.width {
		t.Fatalf("status line is %d cells wide, panel is %d", got, m.width)
	}

	// And the search input, whose content is arbitrary user text.
	m.loading = false
	m.searchActive = true
	m.searchInput.Value = strings.Repeat("検索語", 15)
	m.searchInput.Cursor = len([]rune(m.searchInput.Value))
	search := m.renderSearchLine()
	if got := lipgloss.Height(search); got != 1 {
		t.Fatalf("expected a one-line search input, got %d lines:\n%s", got, search)
	}
	if got := lipgloss.Width(search); got > m.width {
		t.Fatalf("search line is %d cells wide, panel is %d", got, m.width)
	}

	// The whole view must still be exactly m.height lines in every mode.
	for _, mode := range []struct {
		name           string
		loading, searc bool
	}{{"idle", false, false}, {"loading", true, false}, {"search", false, true}} {
		v := m
		v.loading, v.searchActive = mode.loading, mode.searc
		if got := lipgloss.Height(v.View()); got != v.height {
			t.Fatalf("%s view: expected %d lines, got %d", mode.name, v.height, got)
		}
	}
}

// TestEscReleasesSearchResults covers the vim-style release: the first esc
// after a search drops the hits (so the host stops yielding n/N to this
// panel), the next esc is the host's plain "back" again.
func TestEscReleasesSearchResults(t *testing.T) {
	m := newTestModel()
	m.focused = true
	for i := 1; i <= 12; i++ {
		m.store.Messages.Append(testChatID, textMessage(int64(i), 100, strings.Repeat("line ", 6)))
	}
	m.searchQuery = "q"
	m, _ = m.Update(searchResultsMsg{gen: m.gen, chatID: testChatID, query: "q", hits: []int64{9, 6, 3}})
	if !m.HasSearchResults() || m.notice != "match 1/3" {
		t.Fatalf("setup: expected held results, got hits=%v notice=%q", m.searchHits, m.notice)
	}

	m2, cmd := m.handleKey(specialKey(tea.KeyEscape))
	if cmd != nil {
		t.Fatalf("expected esc to run nothing")
	}
	if m2.HasSearchResults() {
		t.Fatalf("expected esc to release the held hits")
	}
	if m2.notice != "" || m2.searchQuery != "" {
		t.Fatalf("expected the match note and query cleared, got notice=%q query=%q", m2.notice, m2.searchQuery)
	}
	// n/N are inert again, so the host is free to quick-type them.
	m3, _ := m2.handleKey(key('n'))
	if m3.HasSearchResults() || m3.notice != "" {
		t.Fatalf("expected n to be inert once the results were released")
	}

	// A second esc changes nothing here (the host handles "back").
	before := m2.scrollOffset
	m4, cmd := m2.handleKey(specialKey(tea.KeyEscape))
	if cmd != nil || m4.scrollOffset != before || m4.notice != "" {
		t.Fatalf("expected a second esc to be a no-op in the chat view")
	}

	// esc must not disturb an unrelated notice when no hits are held.
	m5 := m2
	m5.notice = "could not load messages"
	m6, _ := m5.handleKey(specialKey(tea.KeyEscape))
	if m6.notice != "could not load messages" {
		t.Fatalf("expected an unrelated notice to survive esc, got %q", m6.notice)
	}
}

// TestDeletionPrunesSearchHits keeps n/N off messages that no longer
// exist: hunting for one costs three history fetches and ends in a
// misleading "not in loaded history" notice, every cycle.
func TestDeletionPrunesSearchHits(t *testing.T) {
	setup := func() Model {
		m := newTestModel()
		m.focused = true
		for i := 1; i <= 12; i++ {
			m.store.Messages.Append(testChatID, textMessage(int64(i), 100, strings.Repeat("line ", 6)))
		}
		m.searchQuery = "q"
		m, _ = m.Update(searchResultsMsg{gen: m.gen, chatID: testChatID, query: "q", hits: []int64{9, 6, 3}})
		return m
	}

	// Targeted deletion of a hit the cursor is not on.
	m := setup()
	m, _ = m.handleKey(key('n')) // cursor on hit 6 (index 1)
	if m.searchIdx != 1 || m.notice != "match 2/3" {
		t.Fatalf("setup: expected the cursor on the second hit, got %d %q", m.searchIdx, m.notice)
	}
	m2, _ := m.Update(telegram.MessageDeletedMsg{ChatId: testChatID, MessageIds: []int64{9}})
	if len(m2.searchHits) != 2 || m2.searchHits[0] != 6 || m2.searchHits[1] != 3 {
		t.Fatalf("expected the deleted hit pruned, got %v", m2.searchHits)
	}
	if m2.searchIdx != 0 || m2.notice != "match 1/2" {
		t.Fatalf("expected the cursor to stay on message 6, got idx=%d notice=%q", m2.searchIdx, m2.notice)
	}

	// Deleting the message the cursor is on falls to the next hit along.
	m3, _ := m2.Update(telegram.MessageDeletedMsg{ChatId: testChatID, MessageIds: []int64{6}})
	if len(m3.searchHits) != 1 || m3.searchHits[0] != 3 {
		t.Fatalf("expected only the surviving hit, got %v", m3.searchHits)
	}
	if m3.searchIdx != 0 || m3.notice != "match 1/1" {
		t.Fatalf("expected the cursor on the next hit, got idx=%d notice=%q", m3.searchIdx, m3.notice)
	}

	// Peer-less deletions (DeleteFromAll) prune too.
	m4 := setup()
	m5, _ := m4.Update(telegram.MessageDeletedMsg{ChatId: 0, MessageIds: []int64{6, 3}})
	if len(m5.searchHits) != 1 || m5.searchHits[0] != 9 {
		t.Fatalf("expected a peer-less deletion to prune the hits, got %v", m5.searchHits)
	}

	// Deleting every hit releases the search entirely.
	m6, _ := m5.Update(telegram.MessageDeletedMsg{ChatId: testChatID, MessageIds: []int64{9}})
	if m6.HasSearchResults() || m6.searchQuery != "" || m6.notice != "" {
		t.Fatalf("expected the search released when no hit survives, got hits=%v notice=%q",
			m6.searchHits, m6.notice)
	}
	// And n is inert again rather than starting a doomed hunt.
	m7, cmd := m6.handleKey(key('n'))
	if cmd != nil || m7.notice != "" {
		t.Fatalf("expected n to be inert once every hit was deleted")
	}

	// A deletion that touches no hit leaves the cursor and note alone.
	m8 := setup()
	m9, _ := m8.Update(telegram.MessageDeletedMsg{ChatId: testChatID, MessageIds: []int64{1, 2}})
	if len(m9.searchHits) != 3 || m9.searchIdx != 0 || m9.notice != "match 1/3" {
		t.Fatalf("expected an unrelated deletion to leave the hits alone, got %v idx=%d %q",
			m9.searchHits, m9.searchIdx, m9.notice)
	}
}

// TestSearchLineKeepsCursorVisible checks the sliding window: a query
// longer than the panel still shows the end being typed, on one line.
func TestSearchLineKeepsCursorVisible(t *testing.T) {
	m := newTestModel()
	m.SetSize(30, 20)
	m.searchActive = true
	m.searchInput.Value = "abcdefghijklmnopqrstuvwxyz0123456789"
	m.searchInput.Cursor = len([]rune(m.searchInput.Value))

	line := m.renderSearchLine()
	if got := lipgloss.Height(line); got != 1 {
		t.Fatalf("expected one line, got %d:\n%s", got, line)
	}
	if got := lipgloss.Width(line); got > m.width {
		t.Fatalf("search line is %d cells wide, panel is %d", got, m.width)
	}
	if !strings.Contains(line, "6789") {
		t.Fatalf("expected the text under the cursor to stay visible, got %q", line)
	}

	// Cursor back at the start: the window scrolls back with it.
	m.searchInput.Cursor = 0
	line = m.renderSearchLine()
	if !strings.Contains(line, "abcd") {
		t.Fatalf("expected the start of the query visible with the cursor there, got %q", line)
	}
	if got := lipgloss.Height(line); got != 1 {
		t.Fatalf("expected one line, got %d", got)
	}

	// A pathologically narrow panel must not panic or wrap.
	for _, w := range []int{0, 1, 2, 5, 9, 10} {
		m.SetSize(w, 20)
		if got := lipgloss.Height(m.renderSearchLine()); got != 1 {
			t.Fatalf("width %d: expected one line, got %d", w, got)
		}
	}
}

// TestOpenFindMatchesCtrlF pins the exported entry point the host uses for
// its contextual find binding: it must land in exactly the state ctrl+f
// produces, so the host never has to re-emit a synthetic key event (which
// livelocks when the user has configured keys.search = "ctrl+f").
func TestOpenFindMatchesCtrlF(t *testing.T) {
	base := newTestModel()
	base.focused = true
	base.store.Messages.Append(testChatID, textMessage(1, 100, "hello"))
	base.notice = "match 1/3"

	viaKey, cmd := base.handleKey(ctrlKey('f'))
	if cmd != nil {
		t.Fatalf("expected ctrl+f to run nothing")
	}
	viaMethod := base
	viaMethod.OpenFind()

	if !viaMethod.SearchActive() || !viaMethod.searchInput.Focused {
		t.Fatalf("expected OpenFind to open a focused input")
	}
	if viaMethod.searchActive != viaKey.searchActive ||
		viaMethod.searchInput.Value != viaKey.searchInput.Value ||
		viaMethod.searchInput.Cursor != viaKey.searchInput.Cursor ||
		viaMethod.searchInput.Focused != viaKey.searchInput.Focused ||
		viaMethod.notice != viaKey.notice {
		t.Fatalf("OpenFind diverged from the ctrl+f path:\n method=%+v\n key=%+v",
			viaMethod.searchInput, viaKey.searchInput)
	}
	if viaMethod.notice != "" {
		t.Fatalf("expected the stale notice cleared, got %q", viaMethod.notice)
	}

	// Already open: a repeat press must not wipe a half-typed query.
	typed := viaMethod
	for _, r := range "part" {
		typed, _ = typed.handleKey(key(r))
	}
	reopened := typed
	reopened.OpenFind()
	if reopened.searchInput.Value != "part" || !reopened.SearchActive() {
		t.Fatalf("expected OpenFind to be a no-op while open, got %q", reopened.searchInput.Value)
	}

	// No chat open: nothing to search, so the input must stay closed.
	empty := newTestModel()
	empty.chatID = 0
	empty.OpenFind()
	if empty.SearchActive() {
		t.Fatalf("expected OpenFind to be inert with no chat open")
	}

	// The internal binding still works after the extraction, including
	// keeping previously held hits (only esc releases those).
	held := base
	held.searchHits = []int64{1}
	held, _ = held.handleKey(ctrlKey('f'))
	if !held.SearchActive() || !held.HasSearchResults() {
		t.Fatalf("expected ctrl+f to open the input without dropping held hits")
	}
}

// dispatchedAction runs a non-nil cmd and returns the MessageActionMsg it
// produced, failing the test if the cmd is nil or yields something else.
func dispatchedAction(t *testing.T, cmd tea.Cmd) MessageActionMsg {
	t.Helper()
	if cmd == nil {
		t.Fatalf("expected a non-nil cmd")
	}
	msg, ok := cmd().(MessageActionMsg)
	if !ok {
		t.Fatalf("expected a MessageActionMsg, got %T", msg)
	}
	return msg
}

// keysTestModel builds a model with history tall enough that a scroll
// keypress actually moves scrollOffset instead of being immediately
// reclamped to 0 by clampScrollUp, and a newest message (ID 1, sent by the
// local user, so "edit" is accepted) that the cursor resolves to at
// scrollOffset 0 for the mnemonic-dispatch assertions.
func keysTestModel() Model {
	m := newTestModel()
	m.focused = true
	m.myUserId = 100
	for i := int64(2); i <= 30; i++ {
		m.store.Messages.Append(testChatID, textMessage(i, 200, strings.Repeat("line ", 8)))
	}
	m.store.Messages.Append(testChatID, textMessage(1, 100, "mine")) // newest, at the bottom
	if m.maxScrollOffset() <= 6 {
		panic("test needs a history taller than a couple of scroll steps")
	}
	return m
}

// TestSetKeysDefaultsUnchanged pins that a Model fresh out of New() — which
// calls SetKeys(Keys{}) internally, never explicitly re-invoked here —
// dispatches every previously-hardcoded chat view key exactly as before
// chatview became configurable.
func TestSetKeysDefaultsUnchanged(t *testing.T) {
	base := keysTestModel()

	_, cmd := base.handleKey(key('r'))
	if got := dispatchedAction(t, cmd); got.Action != "reply" || got.MessageId != 1 {
		t.Fatalf("expected 'r' to reply by default, got %+v", got)
	}
	_, cmd = base.handleKey(key('e'))
	if got := dispatchedAction(t, cmd); got.Action != "edit" || got.MessageId != 1 {
		t.Fatalf("expected 'e' to edit by default, got %+v", got)
	}
	_, cmd = base.handleKey(key('d'))
	if got := dispatchedAction(t, cmd); got.Action != "delete" || got.MessageId != 1 {
		t.Fatalf("expected 'd' to delete by default, got %+v", got)
	}

	m := base
	m.scrollOffset = 0
	m, _ = m.handleKey(key('k'))
	if m.scrollOffset != 3 {
		t.Fatalf("expected 'k' to scroll up by default, offset = %d", m.scrollOffset)
	}
	m, _ = m.handleKey(specialKey(tea.KeyUp))
	if m.scrollOffset != 6 {
		t.Fatalf("expected up arrow to scroll up by default, offset = %d", m.scrollOffset)
	}
	m, _ = m.handleKey(key('j'))
	if m.scrollOffset != 3 {
		t.Fatalf("expected 'j' to scroll down by default, offset = %d", m.scrollOffset)
	}
	m, _ = m.handleKey(specialKey(tea.KeyDown))
	if m.scrollOffset != 0 {
		t.Fatalf("expected down arrow to scroll down by default, offset = %d", m.scrollOffset)
	}
}

// TestSetKeysConfiguredValuesDispatch checks that a non-default Keys value
// takes effect for both the mnemonic (replace) and motion (add) fields.
func TestSetKeysConfiguredValuesDispatch(t *testing.T) {
	m := keysTestModel()
	m.SetKeys(Keys{
		Reply:      "ctrl+r",
		Edit:       "v",
		Delete:     "t",
		ScrollUp:   "w",
		ScrollDown: "z",
		PageUp:     "u",
		PageDown:   "i",
	})

	if _, cmd := m.handleKey(ctrlKey('r')); dispatchedAction(t, cmd).Action != "reply" {
		t.Fatalf("expected configured ctrl+r to reply")
	}
	if _, cmd := m.handleKey(key('v')); dispatchedAction(t, cmd).Action != "edit" {
		t.Fatalf("expected configured 'v' to edit")
	}
	if _, cmd := m.handleKey(key('t')); dispatchedAction(t, cmd).Action != "delete" {
		t.Fatalf("expected configured 't' to delete")
	}

	// Replace semantics: the old mnemonic letters no longer do anything.
	if _, cmd := m.handleKey(key('r')); cmd != nil {
		t.Fatalf("expected bare 'r' to be inert once reply is rebound, got a cmd")
	}
	if _, cmd := m.handleKey(key('e')); cmd != nil {
		t.Fatalf("expected bare 'e' to be inert once edit is rebound, got a cmd")
	}
	if _, cmd := m.handleKey(key('d')); cmd != nil {
		t.Fatalf("expected bare 'd' to be inert once delete is rebound, got a cmd")
	}

	// Add semantics: the configured motion key works...
	m2 := m
	m2.scrollOffset = 0
	m2, _ = m2.handleKey(key('w'))
	if m2.scrollOffset != 3 {
		t.Fatalf("expected configured 'w' to scroll up, offset = %d", m2.scrollOffset)
	}
	m2, _ = m2.handleKey(key('z'))
	if m2.scrollOffset != 0 {
		t.Fatalf("expected configured 'z' to scroll down, offset = %d", m2.scrollOffset)
	}
	before := m2.scrollOffset
	m2, _ = m2.handleKey(key('u'))
	if m2.scrollOffset != before+m2.pageStep() {
		t.Fatalf("expected configured 'u' to page up")
	}
	m2, _ = m2.handleKey(key('i'))
	if m2.scrollOffset != before {
		t.Fatalf("expected configured 'i' to page down back to start")
	}

	// ...and so do the untouched hardcoded arrows/letters/pgup/pgdown: the
	// arrows and hjkl are always-on, configuration only adds a binding.
	m3 := m
	m3.scrollOffset = 0
	m3, _ = m3.handleKey(specialKey(tea.KeyUp))
	if m3.scrollOffset != 3 {
		t.Fatalf("expected the up arrow to still scroll up after rebinding scroll_up, offset = %d", m3.scrollOffset)
	}
	m3, _ = m3.handleKey(key('k'))
	if m3.scrollOffset != 6 {
		t.Fatalf("expected 'k' to still scroll up after rebinding scroll_up, offset = %d", m3.scrollOffset)
	}
	m3, _ = m3.handleKey(specialKey(tea.KeyDown))
	if m3.scrollOffset != 3 {
		t.Fatalf("expected the down arrow to still scroll down after rebinding scroll_down, offset = %d", m3.scrollOffset)
	}
	m3, _ = m3.handleKey(key('j'))
	if m3.scrollOffset != 0 {
		t.Fatalf("expected 'j' to still scroll down after rebinding scroll_down, offset = %d", m3.scrollOffset)
	}
	m3.scrollOffset = 5
	before = m3.scrollOffset
	m3, _ = m3.handleKey(specialKey(tea.KeyPgUp))
	if m3.scrollOffset != before+m3.pageStep() {
		t.Fatalf("expected pgup to still page up after rebinding page_up")
	}
	m3, _ = m3.handleKey(specialKey(tea.KeyPgDown))
	if m3.scrollOffset != before {
		t.Fatalf("expected pgdown to still page down after rebinding page_down")
	}
}

// TestSetKeysCollisionFallsBackToBuiltin covers the shadowing rule: a
// configured mnemonic that collides with one of chatview's fixed keys is
// ignored, and a configured motion extra that collides with an already
// claimed key (fixed, or an earlier-resolved field) is dropped rather than
// silently making something unreachable.
func TestSetKeysCollisionFallsBackToBuiltin(t *testing.T) {
	m := keysTestModel()
	// "j" is the fixed scroll-down key: reply must fall back to "r" rather
	// than shadowing scroll-down or leaving reply unreachable.
	m.SetKeys(Keys{Reply: "j"})

	if m.keys.reply != "r" {
		t.Fatalf("expected reply to fall back to the built-in 'r', got %q", m.keys.reply)
	}
	if _, cmd := m.handleKey(key('r')); dispatchedAction(t, cmd).Action != "reply" {
		t.Fatalf("expected 'r' to still reply")
	}
	// Scroll up first so there is room for 'j' to visibly scroll back down.
	m2, _ := m.handleKey(key('k'))
	before := m2.scrollOffset
	if before == 0 {
		t.Fatalf("test needs 'k' to have scrolled up")
	}
	m2, _ = m2.handleKey(key('j'))
	if m2.scrollOffset >= before {
		t.Fatalf("expected 'j' to keep scrolling down, not reply")
	}

	// A motion configured to a key another field already claimed: reply
	// claims "r" first (struct order), so scroll_up = "r" must get no
	// extra binding, and 'r' must still mean reply.
	m3 := keysTestModel()
	m3.SetKeys(Keys{ScrollUp: "r"})
	if m3.keys.scrollUpExtra != "" {
		t.Fatalf("expected scroll_up = 'r' to be dropped as a collision, got extra %q", m3.keys.scrollUpExtra)
	}
	if _, cmd := m3.handleKey(key('r')); dispatchedAction(t, cmd).Action != "reply" {
		t.Fatalf("expected 'r' to still reply, not scroll up")
	}
}

// TestActiveKeysReflectsWhatHandleKeyActuallyMatches pins ActiveKeys as the
// single source of truth a caller (e.g. internal/app's help card) must read
// instead of re-deriving its own "resolved" view of the config: it has to
// report the post-collision-fallback state, not the raw Keys SetKeys was
// given, or a help card could advertise a binding the panel doesn't honor.
func TestActiveKeysReflectsWhatHandleKeyActuallyMatches(t *testing.T) {
	// Unconfigured: the built-in mnemonics, and no motion extras.
	unconfigured := keysTestModel()
	got := unconfigured.ActiveKeys()
	want := Keys{Reply: "r", Edit: "e", Delete: "d"}
	if got != want {
		t.Fatalf("unconfigured ActiveKeys = %+v, want %+v", got, want)
	}

	// Accepted configuration: reports the configured values, including the
	// motion extras that were actually accepted.
	accepted := keysTestModel()
	accepted.SetKeys(Keys{
		Reply:      "ctrl+r",
		Edit:       "v",
		Delete:     "t",
		ScrollUp:   "w",
		ScrollDown: "z",
		PageUp:     "u",
		PageDown:   "i",
	})
	got = accepted.ActiveKeys()
	want = Keys{
		Reply: "ctrl+r", Edit: "v", Delete: "t",
		ScrollUp: "w", ScrollDown: "z", PageUp: "u", PageDown: "i",
	}
	if got != want {
		t.Fatalf("accepted ActiveKeys = %+v, want %+v", got, want)
	}

	// Colliding mnemonic: reply = "j" collides with the fixed scroll-down
	// key, so handleKey falls back to "r" — ActiveKeys must report "r", not
	// the rejected "j", or a help card would advertise a dead binding.
	collidingMnemonic := keysTestModel()
	collidingMnemonic.SetKeys(Keys{Reply: "j"})
	if got := collidingMnemonic.ActiveKeys().Reply; got != "r" {
		t.Fatalf("expected ActiveKeys().Reply = %q after a collision, got %q", "r", got)
	}

	// Rejected motion: scroll_up = "r" collides with reply's already-claimed
	// "r" (struct order), so no extra scroll-up binding was accepted.
	// ActiveKeys must report empty, not the rejected "r" — the built-ins
	// (up/k) are always live and are not this field's job to repeat.
	rejectedMotion := keysTestModel()
	rejectedMotion.SetKeys(Keys{ScrollUp: "r"})
	if got := rejectedMotion.ActiveKeys().ScrollUp; got != "" {
		t.Fatalf("expected ActiveKeys().ScrollUp = %q after a collision, got %q", "", got)
	}
}

// TestSetKeysUnreachableActionIsNeverSilentlyDoubleBound covers the
// resolver bug an adversarial review found: reply = "e" (edit_message left
// at its default "e") used to leave BOTH reply and edit bound to "e", with
// edit permanently unreachable underneath reply's earlier switch case —
// and nothing reported it, falsifying SetKeys's own claim that a
// configuration can never make a key silently unreachable. Now pass 3
// checks "claimed" on the fallback branch too, so edit resolves to ""
// (honestly unreachable) instead of quietly sharing reply's key.
func TestSetKeysUnreachableActionIsNeverSilentlyDoubleBound(t *testing.T) {
	m := keysTestModel()
	m.SetKeys(Keys{Reply: "e"})

	if got := m.ActiveKeys(); got.Reply != "e" || got.Edit != "" {
		t.Fatalf("ActiveKeys = %+v, want Reply=%q Edit=%q (unreachable)", got, "e", "")
	}

	// 'e' fires reply (it was configured to it)...
	if _, cmd := m.handleKey(key('e')); dispatchedAction(t, cmd).Action != "reply" {
		t.Fatalf("expected 'e' to reply, since reply was configured to it")
	}
	// ...and 'r', reply's old default, now claims nothing at all.
	if _, cmd := m.handleKey(key('r')); cmd != nil {
		t.Fatalf("expected bare 'r' to be inert once reply moved to 'e', got a cmd")
	}
	// Edit is unreachable, but delete (untouched by this collision) still
	// works — the collision does not cascade into fields it never touched.
	if _, cmd := m.handleKey(key('d')); dispatchedAction(t, cmd).Action != "delete" {
		t.Fatalf("expected 'd' (untouched) to still delete")
	}
}

// TestSetKeysExplicitConfigOutranksAnEarlierFieldsDefault covers the
// asymmetry an adversarial review found: edit_message = "r" (reply left
// unconfigured) used to be silently discarded, because reply's default
// letter "r" was claimed before edit's explicit config was ever
// considered — a one-sided rebind vanished, even though the full two-sided
// swap worked. The three-pass algorithm accepts every EXPLICIT config
// (pass 2) before any field tries its default (pass 3), so the explicit
// edit_message = "r" now wins; reply has no other letter to fall back to
// and honestly reports unreachable rather than re-claiming "r" out from
// under edit.
func TestSetKeysExplicitConfigOutranksAnEarlierFieldsDefault(t *testing.T) {
	m := keysTestModel()
	m.SetKeys(Keys{Edit: "r"})

	got := m.ActiveKeys()
	if got.Edit != "r" {
		t.Fatalf("expected the explicit edit config %q to be honored, got Edit=%q", "r", got.Edit)
	}
	if got.Reply != "" {
		t.Fatalf("expected reply to report unreachable once edit claimed its default letter, got Reply=%q", got.Reply)
	}

	if _, cmd := m.handleKey(key('r')); dispatchedAction(t, cmd).Action != "edit" {
		t.Fatalf("expected 'r' to edit (the explicit config), not reply")
	}
}

// TestSetKeysFullMnemonicSwapStillWorks is the companion to the one-sided
// case above: when BOTH sides of a swap are explicitly configured, pass 2
// claims each in field order and neither starves the other.
func TestSetKeysFullMnemonicSwapStillWorks(t *testing.T) {
	m := keysTestModel()
	m.SetKeys(Keys{Reply: "e", Edit: "r"})

	got := m.ActiveKeys()
	if got.Reply != "e" || got.Edit != "r" {
		t.Fatalf("expected the full swap to be honored, got %+v", got)
	}
	if _, cmd := m.handleKey(key('e')); dispatchedAction(t, cmd).Action != "reply" {
		t.Fatalf("expected 'e' to reply")
	}
	if _, cmd := m.handleKey(key('r')); dispatchedAction(t, cmd).Action != "edit" {
		t.Fatalf("expected 'r' to edit")
	}
}

// TestSetReservedKeysBlocksAppLevelShadowing covers the second adversarial
// finding: chatViewFixedKeys() only ever listed chatview's OWN keys, so
// e.g. reply = "q" was accepted and advertised on the help card even
// though app.go quits on "q". SetReservedKeys lets the app tell chatview
// about that surface before SetKeys resolves, so a configured mnemonic
// that would shadow an app-level key is rejected the same way a collision
// with a chatview-internal key is.
func TestSetReservedKeysBlocksAppLevelShadowing(t *testing.T) {
	reserved := []string{"q", "h", "l", "i", "c", "/", "?", "tab"}

	for _, try := range []string{"q", "h", "?"} {
		t.Run(try, func(t *testing.T) {
			m := keysTestModel()
			m.SetReservedKeys(reserved)
			m.SetKeys(Keys{Reply: try})

			if got := m.ActiveKeys().Reply; got != "r" {
				t.Fatalf("reply = %q is reserved at the app level and must fall back to %q, got %q", try, "r", got)
			}
			// The reserved key itself must not have been bound to
			// anything in this panel: without SetReservedKeys's
			// protection it would have been accepted as reply (none of
			// q/h/? collide with chatview's own fixed keys), so this
			// distinguishes "rejected" from "coincidentally inert".
			if _, cmd := m.handleKey(key(rune(try[0]))); cmd != nil {
				t.Fatalf("expected the reserved key %q to still be inert inside chatview, got a cmd", try)
			}
		})
	}

	// SetKeys behaves sanely (today's behavior) when SetReservedKeys was
	// never called: nothing is reserved, so reply = "q" is accepted.
	noReserved := keysTestModel()
	noReserved.SetKeys(Keys{Reply: "q"})
	if got := noReserved.ActiveKeys().Reply; got != "q" {
		t.Fatalf("expected reply = %q accepted when SetReservedKeys was never called, got %q", "q", got)
	}
}

// TestSetKeysMotionsUnaffectedByMnemonicResolution pins that the
// three-pass mnemonic rework did not change motion resolution: additive
// semantics still apply, and a configured motion still layers on top of
// the always-on hardcoded spellings.
func TestSetKeysMotionsUnaffectedByMnemonicResolution(t *testing.T) {
	m := keysTestModel()
	m.SetKeys(Keys{Reply: "e", Edit: "r", ScrollUp: "w"})

	if got := m.ActiveKeys().ScrollUp; got != "w" {
		t.Fatalf("expected the configured scroll_up extra to still be accepted, got %q", got)
	}

	before := m.scrollOffset
	m2, _ := m.handleKey(key('w'))
	if m2.scrollOffset <= before {
		t.Fatalf("expected configured 'w' to still scroll up")
	}
	m3, _ := m.handleKey(specialKey(tea.KeyUp))
	if m3.scrollOffset <= before {
		t.Fatalf("expected the built-in up arrow to still scroll up alongside the configured extra")
	}
}
