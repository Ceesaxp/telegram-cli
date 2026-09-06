package chatview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/render"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
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

// TelegramLinkMsg is an armed link that pointed back INTO Telegram, handed
// to the host to navigate to.
//
// It carries the parse, not the URI, because deciding where to go is the
// host's job and reading a link is this panel's. The panel does no network
// call and no navigation of its own: that is the same division the search
// results follow (search.SearchResultMsg → the host's openChatAt), and it
// is what keeps "every way a chat gets opened" a single function.
type TelegramLinkMsg struct {
	telegram.TmeLink
	// URI is the destination as it was shown to the reader, for the notice
	// the host writes when the link cannot be followed after all.
	URI string
}

// JumpBackMsg is ctrl+o: go back to where the last jump left from.
//
// vi's own spelling — ctrl+o walks the jumplist backwards — and it is empty
// of arguments for the same reason [TelegramLinkMsg] carries a parse rather
// than a destination: the panel knows a key was pressed, and the host is
// the only thing that knows where the reader has been. ctrl+o is free here
// because the app dispatches nothing on it and the composer's ctrl+o (edit
// the draft in $EDITOR) only ever reaches a FOCUSED composer, which is not
// this panel.
type JumpBackMsg struct{}

// openArmedLink is enter on an armed link.
//
// The scheme is checked here rather than when the list was built, so a
// refused one is still listed, still cycled past, and says why. What reaches
// the platform opener is [render.Link.SafeURI] — percent-encoded, length-
// bounded, and one of the four schemes a message plausibly means — never the
// raw string out of the message.
//
// A link back into Telegram is intercepted before the opener and travels to
// the host instead. Sending the reader to a browser to be told "VIEW IN
// TELEGRAM" — by a client that is already showing them Telegram — is a
// round trip through two applications to arrive at a chat this one has
// open. [telegram.ParseTmeLink] decides which links those are, and refuses
// everything it is not certain about, so a link it does not recognise still
// opens exactly the way it did before.
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

	// The SAFE form is parsed, the same string the opener would have been
	// given: the raw one out of the message has not been through the scheme
	// allowlist or the encoding sweep, so reading it here would mean this
	// client navigating on a URI nothing had checked.
	if link, ok := telegram.ParseTmeLink(uri); ok {
		m.clearArmedLink()
		return m, func() tea.Msg {
			return TelegramLinkMsg{TmeLink: link, URI: uri}
		}, true
	}

	m.clearArmedLink()
	return m, func() tea.Msg {
		cmd := defaultOpenCmd(uri)
		if cmd == nil {
			return MediaPlayMsg{Status: "error", Info: "⚠ no way to open " + uri + " on this platform"}
		}
		// startOpener rather than cmd.Start() directly, so a test can stand
		// in for the platform opener instead of actually launching a
		// browser. Its error is the difference between "your browser is
		// opening" and "there is no xdg-open on this machine". Reporting the
		// second as the first is how a reader waits for a window that is
		// never coming.
		if err := startOpener(cmd); err != nil {
			return MediaPlayMsg{Status: "error", Info: "⚠ could not open " + uri + ": " + err.Error()}
		}
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
