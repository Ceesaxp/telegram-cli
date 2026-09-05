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
	links := render.MessageLinks(msg)
	if len(links) == 0 {
		m.armed = armedLink{}
		return m, func() tea.Msg {
			return MediaPlayMsg{Status: "info", Info: "no links in this message"}
		}
	}

	next := 1
	if m.armed.msgID == msg.ID && m.armed.index >= 1 {
		next = m.armed.index%len(links) + 1
	}
	m.armed = armedLink{msgID: msg.ID, index: next}
	m.cache.invalidate(msg.ID)

	return m, m.armedNotice(links[next-1], next, len(links))
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
	link, _, _, ok := m.armedLinkInfo()
	if !ok {
		return m, nil, false
	}

	uri := link.SafeURI()
	if uri == "" {
		return m, func() tea.Msg {
			return MediaPlayMsg{Status: "error", Info: "⚠ refusing to open " + link.URI}
		}, true
	}

	m.clearArmedLink()
	return m, func() tea.Msg {
		if cmd := defaultOpenCmd(uri); cmd != nil {
			cmd.Start()
			go cmd.Wait()
		}
		return MediaPlayMsg{Status: "opened", Info: "opened " + uri}
	}, true
}

// armedLinkInfo is the armed link, its position and its total, or ok=false
// when nothing is armed or the message it belonged to has gone.
func (m Model) armedLinkInfo() (render.Link, int, int, bool) {
	if m.armed.index < 1 {
		return render.Link{}, 0, 0, false
	}
	msg := m.cursorMessage()
	if msg == nil || msg.ID != m.armed.msgID {
		return render.Link{}, 0, 0, false
	}
	links := render.MessageLinks(msg)
	if m.armed.index > len(links) {
		return render.Link{}, 0, 0, false
	}
	return links[m.armed.index-1], m.armed.index, len(links), true
}

// armedRange is the rune range the renderer should mark for msgID, or a zero
// range when this is not the armed message.
func (m Model) armedRange(msgID int64) (lo, hi int) {
	if m.armed.index < 1 || m.armed.msgID != msgID {
		return 0, 0
	}
	msg := m.cursorMessage()
	if msg == nil || msg.ID != msgID {
		return 0, 0
	}
	links := render.MessageLinks(msg)
	if m.armed.index > len(links) {
		return 0, 0
	}
	l := links[m.armed.index-1]
	return l.Lo, l.Hi
}

// clearArmedLink drops the link cursor and repaints the message that had it.
func (m *Model) clearArmedLink() {
	if m.armed.index == 0 {
		return
	}
	m.cache.invalidate(m.armed.msgID)
	m.armed = armedLink{}
}

// HasArmedLink reports whether a link is armed, for the host's hint bar.
func (m Model) HasArmedLink() bool {
	_, _, _, ok := m.armedLinkInfo()
	return ok
}
