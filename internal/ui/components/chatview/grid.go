package chatview

import (
	"strconv"
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/render"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// The TUI 2.0 thread is a column grid, not a stack of bubbles
// (docs/tui-2.0.md, "Thread grid"). Every message begins at the same body
// column, so a reader's eye follows one straight edge down the whole
// conversation instead of zig-zagging between left and right bubbles.
//
//	col  0        leading space
//	col  1        cursor bar on the selected message
//	col  2        leading space
//	cols 3..7     faint HH:MM, first line only
//	cols 8..9     spacing
//	cols 10..     sender name, right-aligned and elided
//	             spacing
//	body..        content, wrapped
//	last          blank
//
// The whole point is the fixed left edge of the body, so nothing below is
// allowed to be approximate: every field has a display-cell budget and the
// assembled row is cut to the pane width whatever the content does.
const (
	gridTimeCol  = 3
	gridTimeW    = 5
	gridFieldGap = 2

	// gridSenderWide is the sender column at a comfortable pane width;
	// gridSenderNarrow is what it compresses to when the body would
	// otherwise fall below gridMinBodyW. See gridGeometryFor.
	gridSenderWide   = 12
	gridSenderNarrow = 8

	// gridTrailW is the blank cell kept at the right edge, so body text
	// never touches the panel rule.
	gridTrailW = 1

	// gridMinBodyW is the narrowest body worth reading. Below it the
	// gutter gives up four cells rather than the prose giving up four
	// characters per line.
	gridMinBodyW = 32
)

// gridGeometry is the resolved column arithmetic for one pane width.
type gridGeometry struct {
	Width     int
	SenderCol int
	SenderW   int
	BodyCol   int
	BodyW     int
}

// gridGeometryFor resolves the grid columns for a thread pane width.
//
// A fixed 24-cell gutter does not survive a narrow pane: with the rail open
// at 120 columns the thread is 50 wide, and 24 of them spent on time and
// sender leaves 25 for the message — a column of broken words. The gutter
// therefore compresses to 20, taking the four cells out of the sender name,
// which is the field that degrades most gracefully: a name is recognisable
// truncated, a sentence is not.
//
// The threshold is pinned by two goldens (80x24 and 120x40 are both narrow;
// 100x30, 137x29 and 200x60 are wide), so the arithmetic here is checked
// against drawn frames rather than against prose.
func gridGeometryFor(width int) gridGeometry {
	senderCol := gridTimeCol + gridTimeW + gridFieldGap

	senderW := gridSenderWide
	wideBody := width - (senderCol + gridSenderWide + gridFieldGap) - gridTrailW
	if wideBody < gridMinBodyW {
		senderW = gridSenderNarrow
	}

	bodyCol := senderCol + senderW + gridFieldGap
	bodyW := width - bodyCol - gridTrailW
	if bodyW < 1 {
		bodyW = 1
	}
	return gridGeometry{
		Width:     width,
		SenderCol: senderCol,
		SenderW:   senderW,
		BodyCol:   bodyCol,
		BodyW:     bodyW,
	}
}

// senderIdentity is the ID a sender's colour is hashed from. Chats that
// post as themselves (channels, anonymous admins) hash from the chat.
func senderIdentity(msg *telegram.Message) int64 {
	switch s := msg.SenderID.(type) {
	case *telegram.MessageSenderUser:
		return s.UserID
	case *telegram.MessageSenderChat:
		return s.ChatID
	}
	return 0
}

// sendState is what is known about an outgoing message's progress.
//
// There is no failure state here, deliberately: nothing in the client
// currently reports a send failure, and a glyph for a state that can never
// be reached would be decoration pretending to be information. The design
// record lists a red failure mark; it arrives with the data.
type sendState int

const (
	sendNone    sendState = iota // incoming message: no state to show
	sendPending                  // no server ID yet
	sendSent                     // delivered, not read by the other side
	sendRead                     // read
)

// glyph is the mark drawn after an outgoing message's last body line.
func (s sendState) glyph() string {
	switch s {
	case sendPending:
		return "·"
	case sendSent:
		return "✓"
	case sendRead:
		return "✓✓"
	}
	return ""
}

func (s sendState) colour(r theme.Roles) lipgloss.Color {
	if s == sendRead {
		return r.Green
	}
	return r.Faint
}

// sendStateFor reads an outgoing message's progress out of the store.
//
// "Read" is real information here rather than an assumption: it comes from
// the chat's last-read-outbox marker. The bubble renderer this replaces
// drew two checks on every sent message, which claimed the recipient had
// read things they had not.
func (m Model) sendStateFor(msg *telegram.Message) sendState {
	if msg == nil || !isOwnMessage(msg, m.myUserId) {
		return sendNone
	}
	if msg.ID == 0 {
		return sendPending
	}
	if entry, ok := m.store.Chats.Get(m.chatID); ok && entry.Chat != nil {
		if msg.ID <= entry.Chat.LastReadOutboxMessageID {
			return sendRead
		}
	}
	return sendSent
}

// gridDivider is a left label with a rule running to the right edge:
//
//	" TODAY ─────────────────────────────────────── "
//
// A label alone would read as a message with no sender; the rule is what
// makes it obviously a seam in the conversation rather than part of it.
func gridDivider(label string, width int, labelColour, ruleColour lipgloss.Color) string {
	if width < 1 {
		return ""
	}
	head := " " + label + " "
	ruleW := width - cell.Width(head) - gridTrailW
	if ruleW < 0 {
		ruleW = 0
	}
	line := " " +
		lipgloss.NewStyle().Foreground(labelColour).Render(label) + " " +
		lipgloss.NewStyle().Foreground(ruleColour).Render(strings.Repeat("─", ruleW))
	return cell.Fit(line, width)
}

// gridBodyRow assembles one grid line from its gutter fields and body text,
// cut to exactly the pane width.
//
// time and sender are empty on continuation lines: the body column is the
// constant, and everything to its left is there only on the row that starts
// a message.
func (g gridGeometry) row(bar, time, sender, body string, r theme.Roles) string {
	if bar == "" {
		bar = " "
	}

	line := " " + bar + " " +
		lipgloss.NewStyle().Foreground(r.Faint).Render(cell.Fit(time, gridTimeW)) +
		strings.Repeat(" ", gridFieldGap) +
		cell.Fit(sender, g.SenderW) +
		strings.Repeat(" ", gridFieldGap) +
		body

	return cell.Fit(line, g.Width)
}

// gridSender renders the sender field: the name right-aligned in its
// column, elided if it does not fit, in that sender's colour.
//
// The local user is always green and always called "you" — lower case,
// because it is a pronoun and not a name, and because it should not compete
// with the names around it for attention.
func (m Model) gridSender(msg *telegram.Message, g gridGeometry) string {
	name, colour := m.senderFor(msg)
	return lipgloss.NewStyle().Foreground(colour).
		Render(cell.PadLeft(cell.Truncate(name, g.SenderW), g.SenderW))
}

// senderFor resolves a message's sender name and colour. It is separate
// from gridSender because the colour is a decision worth testing on its
// own: under a test binary lipgloss renders to a profile with no colour at
// all, so an assertion made on rendered output could not see it.
func (m Model) senderFor(msg *telegram.Message) (string, lipgloss.Color) {
	if isOwnMessage(msg, m.myUserId) {
		return "you", m.roles.Green
	}
	name := render.SenderName(msg, m.store)
	if name == "" {
		name = "—"
	}
	return name, theme.SenderColour(senderIdentity(msg), m.roles)
}

// gridReplyRow is the one body-aligned row a reply gets: the quoted sender
// and the start of what they said.
//
// One row, not a framed quote block: a reply is context for the message
// under it, and a quote that can grow taller than its own reply inverts
// that. When the quoted message is not in loaded history the row says so
// rather than showing an ID, which tells the reader nothing.
func (m Model) gridReplyRow(msg *telegram.Message, g gridGeometry) string {
	if msg.ReplyToMessageID == 0 {
		return ""
	}

	r := m.roles
	text := "earlier message"
	for _, other := range m.store.Messages.Get(m.chatID) {
		if other.ID != msg.ReplyToMessageID {
			continue
		}
		who := "you"
		if !isOwnMessage(other, m.myUserId) {
			who = render.SenderName(other, m.store)
		}
		body := ""
		// Twice the body width, so a quote taken from the first line of
		// the cited message is a whole sentence rather than the fragment
		// that happened to fit a wrapped line. Spoilers stay hidden: a
		// quote is not the message the cursor is on.
		opts := render.BodyOptions{Width: g.BodyW * 2}
		if lines := m.renderer.RenderBody(other, m.store, opts); len(lines) > 0 {
			body = strings.TrimSpace(stripSGR(lines[0]))
		}
		text = strings.TrimSpace(who + " " + body)
		break
	}

	quote := "↳ " + text
	return lipgloss.NewStyle().Foreground(r.Dim).Render(cell.Truncate(quote, g.BodyW))
}

// stripSGR removes SGR escape sequences, so a quoted line can be re-styled
// as a quote. A reply row is dim by definition; carrying the original
// message's bold and colour into it would make the quote shout louder than
// the reply that cites it.
//
// It is deliberately narrow: the only escapes this package ever puts into a
// body line are SGR colour and attribute codes from the entity renderer and
// from lipgloss.
func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' && s[j] != 0x1b {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// gridMessageLines renders one message as exact-width grid lines, including
// any dividers that belong above it.
//
// Dividers are attached to the message below them rather than being
// separate entries in the history, which is what keeps the scroll index one
// count per message: every line-arithmetic routine in this package —
// scrollToMessage, sliceLines, visibleMessages — stays exactly as it was.
func (m Model) gridMessageLines(msg *telegram.Message, prev *telegram.Message, selected bool) []string {
	g := gridGeometryFor(m.width)
	r := m.roles

	var out []string

	// A day divider whenever the calendar day changes, and one above the
	// oldest loaded message: at the top of what is loaded, "which day is
	// this?" is exactly the question the reader has.
	if prev == nil || !render.SameDay(prev.Date, msg.Date) {
		out = append(out, gridDivider(render.FormatDayLabel(msg.Date), g.Width, r.Dim, r.RuleSoft))
	}

	// The unread divider is placed from the marker captured when the chat
	// opened, not from the live one, so it stays where the reader left it
	// while they read past it.
	if m.unreadFromID != 0 && msg.ID == m.unreadFromID {
		label := "NEW"
		if m.unreadCount > 0 {
			label = strconv.Itoa(m.unreadCount) + " NEW"
		}
		out = append(out, gridDivider(label, g.Width, r.Amber, r.Amber))
	}

	body := m.renderer.RenderBody(msg, m.store, render.BodyOptions{
		Width: g.BodyW,
		// Spoilers only ever open on the message the cursor is on, and
		// only after x. Revealing them anywhere else would defeat the
		// point of a spoiler on a screen somebody else can see.
		RevealSpoilers: m.revealedID != 0 && msg.ID == m.revealedID,
	})

	var rows []string
	if msg.IsForwarded {
		rows = append(rows, lipgloss.NewStyle().Foreground(r.Dim).Render("↪ forwarded"))
	}
	if reply := m.gridReplyRow(msg, g); reply != "" {
		rows = append(rows, reply)
	}
	rows = append(rows, body...)

	// The outgoing state rides the end of the last body line when there is
	// room for it, and takes its own line when there is not — never
	// truncating the message to make space for a tick.
	if state := m.sendStateFor(msg); state != sendNone {
		mark := lipgloss.NewStyle().Foreground(state.colour(r)).Render(state.glyph())
		last := rows[len(rows)-1]
		if cell.Width(last)+2+cell.Width(state.glyph()) <= g.BodyW {
			rows[len(rows)-1] = last + "  " + mark
		} else {
			rows = append(rows, cell.PadLeft(mark, g.BodyW))
		}
	}

	// The bar runs down every row of the message, and its colour is the
	// only thing on screen that says which panel has the keyboard.
	//
	// Cyan when this panel is focused, ghost when it is not — the same rule
	// the chat list has always followed, and which this one did not: two
	// panels drawing an equally bright cursor is two panels claiming to be
	// active, and the reader has to guess.
	bar := " "
	if selected {
		colour := r.Ghost
		if m.focused {
			colour = r.Cyan
		}
		bar = lipgloss.NewStyle().Foreground(colour).Render("▌")
	}

	stamp := render.FormatClock(msg.Date)
	sender := m.gridSender(msg, g)

	// Where the message itself starts. Any dividers already in out belong
	// to the boundary above this message, not to the message, and the band
	// must not claim them: a day divider lit up as part of the selection
	// says the cursor is on the divider, which is not a place it can be.
	msgTop := len(out)

	for i, row := range rows {
		if i == 0 {
			out = append(out, g.row(bar, stamp, sender, row, r))
			continue
		}
		// The time and sender fields belong to the message's first row
		// only; the bar belongs to all of them.
		out = append(out, g.row(bar, "", "", row, r))
	}

	// The selected message is a band of curline across the full pane, not
	// just a mark in the gutter: the cursor has to be findable at a glance
	// on a screen with no other highlight on it.
	//
	// cell.Fill rather than a wrapping Background: every one of these rows
	// opens a styled span for the time, the sender and the body, and each
	// one closes with a reset that would take the band with it — leaving a
	// single cyan cell in the gutter, which is precisely the "just a mark"
	// this band exists not to be.
	if selected {
		for i := msgTop; i < len(out); i++ {
			out[i] = cell.Fill(r.CurLine, out[i], g.Width)
		}
	}

	return out
}

// applyChatAction folds one chat action into the set of users typing.
func applyChatAction(typing []int64, msg telegram.ChatActionMsg) []int64 {
	switch msg.Action.(type) {
	case *telegram.ChatActionTyping:
		for _, id := range typing {
			if id == msg.UserId {
				return typing
			}
		}
		return append(typing, msg.UserId)
	default:
		// Anything that is not typing — cancel, or one of the upload and
		// recording actions this client does not render — ends it.
		out := typing[:0]
		for _, id := range typing {
			if id != msg.UserId {
				out = append(out, id)
			}
		}
		return out
	}
}

// typingNames resolves the typing user IDs to display names.
func (m Model) typingNames() []string {
	if len(m.typing) == 0 {
		return nil
	}
	names := make([]string, 0, len(m.typing))
	for _, id := range m.typing {
		names = append(names, m.store.Users.DisplayName(id))
	}
	return names
}

// gridTypingRow is the bottom row of the scroller while someone is typing.
//
// Its marker sits in the sender column, so the row lines up with the
// messages above it instead of shifting the grid sideways for as long as
// somebody is composing.
func (m Model) gridTypingRow() string {
	names := m.typingNames()
	if len(names) == 0 {
		return ""
	}

	g := gridGeometryFor(m.width)
	r := m.roles

	verb := " is typing…"
	if len(names) > 1 {
		verb = " are typing…"
	}
	text := strings.Join(names, ", ") + verb

	marker := lipgloss.NewStyle().Foreground(r.Ghost).Render(cell.PadLeft("···", g.SenderW))
	body := lipgloss.NewStyle().Foreground(r.Dim).Render(cell.Truncate(text, g.BodyW))
	return g.row(" ", "", marker, body, r)
}
