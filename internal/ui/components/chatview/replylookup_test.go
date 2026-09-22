package chatview

import (
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/charmbracelet/x/ansi"
)

// longRepliedHistory is an open chat of n messages in which every tenth one
// replies to the message five above it — the shape of a busy group read far
// back, which is where a reply row that walks the whole history per quote
// turns a redraw quadratic.
func longRepliedHistory(n int) Model {
	m := newTestModel()
	m.SetSize(100, 30)
	m.store.Messages.Activate(testChatID)
	for id := int64(1); id <= int64(n); id++ {
		msg := textMessage(id, 200+id%3, "a line of chat that is long enough to wrap once or twice in the pane")
		if id%10 == 0 {
			msg.ReplyToMessageID = id - 5
		}
		m.store.Messages.Append(testChatID, msg)
	}
	return m
}

// A reply row quotes the sender and the opening of the message it answers.
// Pinned exactly, so a change to how the quoted message is found cannot
// change what the row says.
func TestReplyRowQuotesTheLoadedMessage(t *testing.T) {
	m := gridModel(t, 67)
	msg := textMessage(10, 201, "agreed")
	msg.ReplyToMessageID = 1

	got := ansi.Strip(m.gridReplyRow(msg, gridGeometryFor(67)))
	if want := "↳ nadia Rollout paused — session hits spi…"; got != want {
		t.Fatalf("quote = %q, want %q", got, want)
	}
}

// The quoted message going away — deleted on the server, say — turns the
// quote back into the placeholder, rather than quoting what is gone.
func TestReplyToADeletedMessageSaysSo(t *testing.T) {
	m := gridModel(t, 67)
	msg := textMessage(10, 201, "agreed")
	msg.ReplyToMessageID = 1

	m.store.Messages.Delete(testChatID, []int64{1})

	quote := ansi.Strip(m.gridReplyRow(msg, gridGeometryFor(67)))
	if !strings.Contains(quote, "earlier message") || strings.Contains(quote, "nadia") {
		t.Fatalf("quote of a deleted message = %q, want the placeholder", quote)
	}
}

// Finding the quoted message used to take a copy of the whole chat and walk
// it, once per reply drawn, so redrawing a paged-back history cost messages
// times replies. A reply row has to cost the same whatever the length of
// the history around it.
func TestReplyRowDoesNotCopyTheHistory(t *testing.T) {
	const n = 20000
	m := longRepliedHistory(n)
	g := gridGeometryFor(m.width)
	historyCopy := uint64(n) * uint64(unsafe.Sizeof((*telegram.Message)(nil)))

	tests := map[string]int64{
		"a loaded message": n / 2,
		"a missing one":    n * 10,
	}
	for name, target := range tests {
		t.Run(name, func(t *testing.T) {
			msg := textMessage(n+1, 200, "answering")
			msg.ReplyToMessageID = target
			m.gridReplyRow(msg, g) // anything lazily built on first use

			const calls = 50
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			for range calls {
				m.gridReplyRow(msg, g)
			}
			runtime.ReadMemStats(&after)

			perCall := (after.TotalAlloc - before.TotalAlloc) / calls
			t.Logf("%d bytes per reply row; the history's copy would be %d", perCall, historyCopy)
			if perCall >= historyCopy/4 {
				t.Fatalf("a reply row allocates %d bytes in a %d-message chat; "+
					"a copy of its history is %d", perCall, n, historyCopy)
			}
		})
	}
}

// BenchmarkRenderHistoryWithReplies is the redraw a resize or a theme change
// forces: every block drawn again from nothing, 5,000 messages, 500 of them
// replies.
func BenchmarkRenderHistoryWithReplies(b *testing.B) {
	m := longRepliedHistory(5000)
	b.ReportAllocs()
	for b.Loop() {
		m.cache.clear()
		m.renderedMessages(m.store.Messages.Get(testChatID))
	}
}

// BenchmarkReplyRows is the reply half of that redraw alone: one reply row
// per message, which is what the quote lookup costs without the bodies.
func BenchmarkReplyRows(b *testing.B) {
	m := longRepliedHistory(5000)
	g := gridGeometryFor(m.width)
	msgs := m.store.Messages.Get(testChatID)
	b.ReportAllocs()
	for b.Loop() {
		for _, msg := range msgs {
			m.gridReplyRow(msg, g)
		}
	}
}
