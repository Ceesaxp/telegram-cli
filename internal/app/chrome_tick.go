package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// The chrome rows have two things on them that change on their own: the
// clock, and a transient notice that is supposed to give the row back.
//
// Neither did. `refreshChrome` sets the clock, and it only ran on a window
// resize, on authentication, and on a folder-tab click — so the top bar
// showed the time of the last resize for the rest of the session, and
// `hintbar.ClearNotice` had no caller at all, which is why a notice owned
// the hint bar until something else replaced it. Both are the same missing
// piece: this program had no periodic tick.
//
// A clock with minute resolution does not need a one-second pulse, but the
// notice does, and one timer is cheaper to reason about than two. Bubble
// Tea diffs frames, so a tick that changes nothing costs a comparison
// rather than a repaint.

// noticeLife is how long a transient notice owns the hint bar. The design
// record says four seconds (docs/tui-2.0.md, "Top bar, chat list, and hint
// bar").
const noticeLife = 4 * time.Second

// chromeTickMsg is the pulse. It carries the time so the notice's age is
// measured against the same clock the top bar draws, rather than against a
// second call to time.Now a few microseconds later.
type chromeTickMsg time.Time

func chromeTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return chromeTickMsg(t)
	})
}

// expireNotice hands the hint bar back once a notice has had its four
// seconds.
//
// The composer's copy of the notice is not touched: it clears itself on the
// next edit, on escape, and on send, which is the right rule for a row the
// user is typing into. This one is for the row nobody types into.
// One condition, not two. "There is no notice" leaves noticeAt at its zero
// value, which is arbitrarily old, so the age check already covers it — and
// clearing a notice that is not there is a no-op. An explicit IsZero branch
// would be a second mechanism that can only ever agree with this one.
func (m *Model) expireNotice(now time.Time) {
	if now.Sub(m.noticeAt) < noticeLife {
		return
	}
	m.hintBar.ClearNotice()
	m.noticeAt = time.Time{}
}
