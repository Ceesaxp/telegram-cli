package chatview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/render"
)

// Following a link, vim's way: gx arms the first link in the cursored
// message and cycles on repeat, enter opens the armed one, esc drops it.
//
// gx rather than ctrl+], because gx is what vim actually binds to "open the
// URL under the cursor" (netrw); ctrl+] is jump-to-tag, which is a different
// idea about a different kind of destination. The cost is that g becomes a
// prefix and so stops meaning Top on its own — see [Model.pendingG].
//
// Arming rather than opening outright. A link's visible text and its
// destination are allowed to differ, which is exactly the shape of a
// phishing link (entityURI's own comment says so), so the destination is put
// on screen and the reader presses enter having read it. It also answers
// "which of these three links" without a second key.

// armedLink is the state of the link cursor: which message it belongs to and
// which link within it, counting from 1.
//
// Keyed by message ID rather than by the cursor's index, so a message
// arriving above does not silently re-aim it at a different message's link.
type armedLink struct {
	msgID int64
	index int

	// uri and safeURI are the destination as it was when the reader was
	// shown it, and lo/hi the range that was marked.
	//
	// Stored rather than re-derived. Re-reading the message at enter-time
	// meant an edit arriving in between — Telegram delivers those
	// unprompted, and this client refetches on every reaction too — could
	// put a different URI at the same index, so enter opened a destination
	// nobody had seen. That is the same mistake the forward picker's
	// captured Source exists to prevent, on a surface whose entire purpose
	// is showing you where you are about to go.
	uri, safeURI string
	lo, hi       int
}

// links are the cursored message's links, in reading order.
func (m Model) links() []render.Link {
	msg := m.cursorMessage()
	if msg == nil {
		return nil
	}
	return render.MessageLinks(msg)
}

// armNextLink is gx: arm the first link, or step to the next one when this
// message already has one armed.
//
// It wraps. Three links and four presses is a reader who has changed their
// mind, not one who wants the list to end.
func (m Model) armNextLink() (Model, tea.Cmd) {
	msg := m.cursorMessage()
	if msg == nil {
		return m, nil
	}
	all := render.MessageLinks(msg)
	// A link under an unrevealed spoiler cannot be armed, because the mark
	// would be invisible: a hidden spoiler paints foreground and background
	// the same colour, so anything laid over it is too. Revealing it to
	// show the mark would defeat the spoiler, and marking it anyway would
	// be a cursor nobody can see. x first, then gx.
	revealed := m.revealedID == msg.ID
	links := make([]render.Link, 0, len(all))
	hidden := 0
	for _, l := range all {
		if l.InSpoiler && !revealed {
			hidden++
			continue
		}
		links = append(links, l)
	}

	if len(links) == 0 {
		m.armed = armedLink{}
		info := "no links in this message"
		if hidden > 0 {
			info = "links here are inside a spoiler — press x to reveal them first"
		}
		return m, func() tea.Msg {
			return MediaPlayMsg{Status: "info", Info: info}
		}
	}

	next := 1
	if m.armed.msgID == msg.ID && m.armed.index >= 1 {
		next = m.armed.index%len(links) + 1
	}
	link := links[next-1]
	m.armed = armedLink{
		msgID: msg.ID, index: next,
		uri: link.URI, safeURI: link.SafeURI(),
		lo: link.Lo, hi: link.Hi,
	}
	m.cache.invalidate(msg.ID)

	return m, m.armedNotice(link, next, len(links))
}

// armedNotice puts the destination on screen. It is the whole reason gx arms
// rather than opens: the reader is deciding whether to follow a link, and
// the only thing that answers that is where it actually goes.
func (m Model) armedNotice(link render.Link, idx, total int) tea.Cmd {
	label := link.URI
	if !link.Openable() {
		label = link.URI + "  ⚠ this client will not open that scheme"
	}
	if total > 1 {
		label = itoa(idx) + "/" + itoa(total) + "  " + label
	}
	return func() tea.Msg {
		return MediaPlayMsg{Status: "info", Info: label}
	}
}

// openArmedLink is enter on an armed link.
//
// The scheme is checked here rather than when the list was built, so a
// refused one is still listed, still cycled past, and says why. What reaches
// the platform opener is [render.Link.SafeURI] — percent-encoded, length-
// bounded, and one of the four schemes a message plausibly means — never the
// raw string out of the message.
func (m Model) openArmedLink() (Model, tea.Cmd, bool) {
	if !m.HasArmedLink() {
		return m, nil, false
	}

	// The frozen value, not a fresh read of the message: what opens is what
	// was on screen when the reader decided to open it.
	uri, raw := m.armed.safeURI, m.armed.uri
	if uri == "" {
		return m, func() tea.Msg {
			return MediaPlayMsg{Status: "error", Info: "⚠ refusing to open " + raw}
		}, true
	}

	m.clearArmedLink()
	return m, func() tea.Msg {
		cmd := defaultOpenCmd(uri)
		if cmd == nil {
			return MediaPlayMsg{Status: "error", Info: "⚠ no way to open " + uri + " on this platform"}
		}
		// Start's error is the difference between "your browser is opening"
		// and "there is no xdg-open on this machine". Reporting the second
		// as the first is how a reader waits for a window that is never
		// coming.
		if err := cmd.Start(); err != nil {
			return MediaPlayMsg{Status: "error", Info: "⚠ could not open " + uri + ": " + err.Error()}
		}
		go cmd.Wait()
		return MediaPlayMsg{Status: "opened", Info: "opened " + uri}
	}, true
}

// armedLinkInfo reports whether a link is armed and still belongs to the
// message under the cursor.
//
// It no longer re-derives the link — see [armedLink] — so a message edited
// underneath a decision cannot change what enter does. It only answers
// whether the decision is still about the message the reader is looking at.
func (m Model) armedLinkInfo() bool {
	if m.armed.index < 1 {
		return false
	}
	msg := m.cursorMessage()
	return msg != nil && msg.ID == m.armed.msgID
}

// armedRange is the rune range the renderer should mark for msgID, or a zero
// range when this is not the armed message.
func (m Model) armedRange(msgID int64) (lo, hi int) {
	if !m.armedLinkInfo() || m.armed.msgID != msgID {
		return 0, 0
	}
	return m.armed.lo, m.armed.hi
}

// clearArmedLink drops the link cursor and repaints the message that had it.
func (m *Model) clearArmedLink() {
	if m.armed.index == 0 {
		return
	}
	m.cache.invalidate(m.armed.msgID)
	m.armed = armedLink{}
}

// dropArmedLinkOn releases the link cursor when the message it was aimed at
// is replaced.
//
// The frozen URI already guarantees enter cannot open something unseen; this
// is the other half. A replaced message may have different text at the marked
// range, so the mark would be pointing at whatever now sits there — a cursor
// describing a message that no longer exists. Re-arm on the new one.
func (m *Model) dropArmedLinkOn(msgID int64) {
	if m.armed.index >= 1 && m.armed.msgID == msgID {
		m.clearArmedLink()
	}
}

// HasArmedLink reports whether a link is armed, for the host's hint bar.
func (m Model) HasArmedLink() bool { return m.armedLinkInfo() }
