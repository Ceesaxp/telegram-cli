package chatview

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// typingFrames is the marker in the sender column while somebody is
// composing: a pulse travelling along the three dots the static marker
// always drew, as issue #51 proposed it.
//
// Frame 0 is deliberately the resting `···` rather than a frame with the
// bullet already in it. That is the marker the moment typing starts, and
// it is the marker the golden frame fixtures in docs/fixtures capture —
// those are the acceptance artifact for column alignment (design record,
// decision 11), and a fixture that pinned an arbitrary mid-animation frame
// would describe a moment rather than a layout.
//
// The dots stay U+00B7 and the pulse is U+2022; both measure one cell, so
// every frame is the same three cells wide and the row cannot shear.
var typingFrames = []string{"···", "•··", "·•·", "··•"}

// typingFrameRate is how long each frame holds. Fast enough to read as
// motion, slow enough that a whole cycle (four frames, 1.6s) is calm next
// to a thread nobody is scrolling.
const typingFrameRate = 400 * time.Millisecond

// typingTTL is how long one typing action stands before it lapses.
//
// This is Telegram's own rule: an action is valid for six seconds and a
// client that means it re-sends every few. Without the deadline the set is
// emptied only by an explicit cancel — so a cancel lost to a connection
// blip left "nadia is typing…" on screen for the rest of the session.
// That was survivable while the marker was static. An animation that never
// stops is not: it would hold a 400ms redraw open forever, and it would be
// the most eye-catching thing on a screen where nothing is happening.
const typingTTL = 6 * time.Second

// typingUser is one person composing, and when their action lapses.
type typingUser struct {
	id    int64
	until time.Time
}

// typingTickMsg advances the marker. It carries the generation of the
// chain that produced it: a chat switch stops the current chain and a new
// action starts another, and without the check the stopped chain's last
// tick would arrive afterwards and re-arm a second timer. Two chains halve
// the frame time, and every further overlap halves it again.
type typingTickMsg struct {
	at  time.Time
	gen int
}

func typingTick(gen int) tea.Cmd {
	return tea.Tick(typingFrameRate, func(t time.Time) tea.Msg {
		return typingTickMsg{at: t, gen: gen}
	})
}

// startTypingAnim arms the marker if it is not already running. Returns
// nil when a chain is live or nobody is typing, so the caller can pass it
// straight back to Bubble Tea.
func (m *Model) startTypingAnim() tea.Cmd {
	if m.typingGen != 0 || len(m.typing) == 0 {
		return nil
	}
	m.typingGen++
	m.typingFrame = 0
	return typingTick(m.typingGen)
}

// stopTypingAnim ends the current chain and invalidates its in-flight
// tick. Called when the set empties and when the open chat changes.
func (m *Model) stopTypingAnim() {
	m.typingGen++
	m.typingFrame = 0
}

// applyChatAction folds one chat action into the set of users typing, and
// drops anyone whose action has lapsed.
func applyChatAction(typing []typingUser, msg telegram.ChatActionMsg, now time.Time) []typingUser {
	typing = pruneTyping(typing, now)
	switch msg.Action.(type) {
	case *telegram.ChatActionTyping:
		for i := range typing {
			if typing[i].id == msg.UserId {
				// A repeat is a refresh, not a second typist: Telegram
				// re-sends every few seconds for exactly this reason.
				typing[i].until = now.Add(typingTTL)
				return typing
			}
		}
		return append(typing, typingUser{id: msg.UserId, until: now.Add(typingTTL)})
	default:
		// Anything that is not typing — cancel, or one of the upload and
		// recording actions this client does not render — ends it.
		out := typing[:0]
		for _, u := range typing {
			if u.id != msg.UserId {
				out = append(out, u)
			}
		}
		return out
	}
}

// pruneTyping drops the actions that have lapsed as of now.
func pruneTyping(typing []typingUser, now time.Time) []typingUser {
	out := typing[:0]
	for _, u := range typing {
		if u.until.After(now) {
			out = append(out, u)
		}
	}
	return out
}
