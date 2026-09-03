package chatview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/charmbracelet/lipgloss"
)

// searchResultsMsg carries the outcome of an in-chat search. It is guarded
// by both gen and chatID on arrival, so a search started in one chat can
// never land in another (or in a later generation of the same chat).
type searchResultsMsg struct {
	gen    int
	chatID int64
	query  string
	hits   []int64
	err    error
}

// clearSearch drops both the input and any held results. Called whenever
// the chat changes and before every new search.
func (m *Model) clearSearch() {
	m.searchActive = false
	m.searchInput.Reset()
	m.searchQuery = ""
	m.searchHits = nil
	m.searchIdx = 0
}

// OpenFind opens the in-chat search input, exactly as the panel's own
// ctrl+f binding does. It exists so the host can route a contextual
// binding (e.g. "/" pressed with the chat view focused) straight into this
// component with a method call. Re-emitting a synthetic ctrl+f key event
// through the command loop is the alternative, and it livelocks the moment
// a user configures keys.search = "ctrl+f": the forwarded key re-matches
// the host binding, which forwards it again, forever.
//
// A no-op when the input is already open (so a repeated press does not
// wipe a half-typed query) or when no chat is open (there is nothing to
// search, and the input would render over the placeholder view).
func (m *Model) OpenFind() {
	if m.searchActive || m.chatID == 0 {
		return
	}
	m.searchActive = true
	m.searchInput.Focused = true
	m.searchInput.Reset()
	m.notice = ""
}

// pruneSearchHits drops message IDs that no longer exist from the held
// hits, keeping the cursor on a valid entry and the "match i/n" note
// truthful. Called from both deletion branches: a hit that has been
// deleted can never be scrolled to, so n/N would otherwise spend a
// three-page backwards hunt on it and land on a misleading notice.
func (m *Model) pruneSearchHits(deleted []int64) {
	if len(m.searchHits) == 0 || len(deleted) == 0 {
		return
	}
	gone := make(map[int64]struct{}, len(deleted))
	for _, id := range deleted {
		gone[id] = struct{}{}
	}

	oldIdx := m.searchIdx
	kept := make([]int64, 0, len(m.searchHits))
	// newIdx counts the survivors that sat before the cursor, so it ends
	// up on the cursor's own message when that survived, and otherwise on
	// the next surviving hit along — where n would have gone anyway.
	newIdx := 0
	for i, id := range m.searchHits {
		if _, dead := gone[id]; dead {
			continue
		}
		if i < oldIdx {
			newIdx++
		}
		kept = append(kept, id)
	}
	if len(kept) == len(m.searchHits) {
		return
	}

	m.searchHits = kept
	if len(kept) == 0 {
		m.searchIdx = 0
		m.searchQuery = ""
		if strings.HasPrefix(m.notice, "match ") {
			m.notice = ""
		}
		return
	}
	if newIdx >= len(kept) {
		newIdx = len(kept) - 1
	}
	m.searchIdx = newIdx
	if strings.HasPrefix(m.notice, "match ") {
		m.notice = fmt.Sprintf("match %d/%d", m.searchIdx+1, len(kept))
	}
}

// handleSearchKey routes a keypress to the search input. While the input
// is open it swallows everything: esc cancels, enter runs the search, and
// every other key is text (so j/k/g do NOT scroll the history).
//
// Keys are matched on msg.String(); see the note in handleKey (model.go)
// and internal/keys.Press's doc comment — String() is safe here only
// because nothing in this component binds an alt-modified key, which the
// Kitty protocol reports as composed text on macOS.
func (m Model) handleSearchKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if msg.String() == "esc" {
		// Cancel the input only. Results from an earlier search survive,
		// so n/N keep working after an accidental ctrl+f.
		m.searchActive = false
		m.searchInput.Reset()
		return m, nil
	}

	if submitted := m.searchInput.Update(msg); !submitted {
		return m, nil
	}

	query := strings.TrimSpace(m.searchInput.Value)
	if query == "" {
		// SearchChatMessages rejects an empty query outright (the server
		// would too), so never call through with one — stay in the input
		// and say what is missing.
		m.notice = "type a query"
		return m, nil
	}

	m.searchActive = false
	m.searchInput.Reset()
	m.searchQuery = query
	m.searchHits = nil
	m.searchIdx = 0
	m.notice = "searching..."
	if m.tg == nil {
		m.notice = ""
		return m, nil
	}
	return m, m.searchChatCmd(m.gen, m.chatID, query)
}

func (m Model) searchChatCmd(gen int, chatID int64, query string) tea.Cmd {
	tg := m.tg
	return func() tea.Msg {
		found, err := tg.SearchChatMessages(chatID, query, 0, searchResultLimit)
		if err != nil {
			return searchResultsMsg{gen: gen, chatID: chatID, query: query, err: err}
		}
		hits := make([]int64, 0, len(found))
		seen := make(map[int64]bool, len(found))
		for _, msg := range found {
			if msg == nil || msg.ID == 0 || seen[msg.ID] {
				continue
			}
			seen[msg.ID] = true
			hits = append(hits, msg.ID)
		}
		return searchResultsMsg{gen: gen, chatID: chatID, query: query, hits: hits}
	}
}

func (m Model) handleSearchResults(msg searchResultsMsg) (Model, tea.Cmd) {
	if msg.gen != m.gen || msg.chatID != m.chatID || msg.query != m.searchQuery {
		return m, nil
	}
	if msg.err != nil {
		m.searchHits = nil
		m.notice = "search failed"
		return m, nil
	}
	if len(msg.hits) == 0 {
		m.searchHits = nil
		m.notice = fmt.Sprintf("no match for %q", msg.query)
		return m, nil
	}
	m.searchHits = msg.hits
	m.searchIdx = 0
	cmd := m.jumpToHit(0)
	return m, cmd
}

// jumpToHit scrolls to hit i, wrapping around both ends, and notes the
// position as "match i/n". When the hit is not in the loaded window it
// hands the message ID to the existing jump machinery, which pages
// backwards up to maxTargetPages looking for it.
func (m *Model) jumpToHit(i int) tea.Cmd {
	n := len(m.searchHits)
	if n == 0 {
		return nil
	}
	m.searchIdx = ((i % n) + n) % n
	id := m.searchHits[m.searchIdx]
	m.notice = fmt.Sprintf("match %d/%d", m.searchIdx+1, n)

	if m.scrollToMessage(id) {
		// The trailing meta pipeline can still change bubble heights and
		// push the hit off screen; let it re-apply the jump if so.
		if m.metaBusy {
			m.pendingJumpID = id
		}
		return nil
	}

	// Not loaded: reuse the OpenChatAt hunt — historyLoadedMsg walks
	// backwards page by page until it finds targetMsgID or gives up.
	oldest := m.store.Messages.OldestMessageId(m.chatID)
	if oldest == 0 || m.tg == nil {
		m.notice = "message not in loaded history"
		return nil
	}
	m.targetMsgID = id
	m.targetPages = 0
	m.pendingJumpID = 0
	m.loading = true
	m.loadStatus = "Searching for message..."
	return m.loadHistoryCmd(m.gen, m.chatID, oldest)
}

// renderSearchLine draws the search input in place of the status line. It
// is exactly one line tall, which is what bodyHeight subtracts for it.
//
// The value is laid out here rather than through widgets.TextArea.View():
// that helper pads to a rune-agnostic Width and lets lipgloss WRAP a value
// wider than the panel, which would silently turn this strip into two
// rows. Everything here is cell-accurate (ansi), and the window slides
// with the cursor so a query longer than the panel still shows what is
// being typed.
func (m Model) renderSearchLine() string {
	const prefix = " search: "
	const cursor = "\u2588"

	style := lipgloss.NewStyle().Foreground(m.roles.Dim)
	inner := m.width - style.GetHorizontalFrameSize()
	budget := inner - cell.Width(prefix)
	if budget < 1 {
		budget = 1
	}

	runes := []rune(m.searchInput.Value)
	at := m.searchInput.Cursor
	if at > len(runes) {
		at = len(runes)
	}
	if at < 0 {
		at = 0
	}
	head, tail := string(runes[:at]), string(runes[at:])

	// Scroll the window right so the cursor always stays inside it.
	if over := cell.Width(head) + cell.Width(cursor) - budget; over > 0 {
		head = cell.ClampLeft(head, over)
	}
	line := prefix + head + cursor + tail
	if inner > 0 {
		line = cell.Clamp(line, inner)
	}
	return style.Width(m.width).Render(line)
}

// mediaHint is the one-line affordance shown in the header when the
// message the action keys would act on carries openable media. The full
// key help lives in the host's help line, but the chat view is the only
// place that knows *which* message is currently targeted.
func (m Model) mediaHint() string {
	msg := m.cursorMessage()
	if msg == nil || msg.Content == nil {
		return ""
	}
	switch msg.Content.(type) {
	case *telegram.MessagePhoto:
		return "enter: open photo · s: save"
	case *telegram.MessageVideo, *telegram.MessageAnimation, *telegram.MessageVideoNote:
		return "enter: play · s: save"
	case *telegram.MessageAudio, *telegram.MessageVoiceNote:
		return "enter: play · s: save"
	case *telegram.MessageDocument:
		return "enter: open · s: save"
	}
	return ""
}
