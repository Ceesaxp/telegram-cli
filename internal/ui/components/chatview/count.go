package chatview

// The vi count prefix: 9{ moves nine messages back, 3j scrolls three times.
//
// Digits are free here. The chat list binds 1–9 to its folder tabs, but that
// is the chat list; nothing in the thread has ever wanted a digit, so the
// prefix costs no binding.
//
// It applies to the MOTIONS and to nothing else. A count on a motion means
// "again, this many times", which is what a reader typing it expects; a count
// on r or y or enter would have to mean something invented. Any key that is
// not a digit and not a motion clears the pending count rather than carrying
// it — a number typed and then forgotten about must not attach itself to the
// next thing pressed.

// maxCount bounds what a count can accumulate to.
//
// Not a guard against a hostile user — there isn't one — but against a
// leaned-on key. moveCursor and the scroll clamps already refuse to go past
// the ends, so the only thing an unbounded count buys is an integer overflow
// on the way there.
const maxCount = 9999

// countDigit folds one digit into the pending count and reports whether it
// was consumed.
//
// A leading "0" is not a count: vi gives bare 0 to the start of the line, and
// while this thread has no such motion, a "0" that silently began a count
// would make "01" mean one and "0" mean nothing visible at all. Once a count
// is under way, 0 is an ordinary digit.
func (m *Model) countDigit(key string) bool {
	if len(key) != 1 || key[0] < '0' || key[0] > '9' {
		return false
	}
	if key == "0" && m.pendingCount == 0 {
		return false
	}
	if n := m.pendingCount*10 + int(key[0]-'0'); n <= maxCount {
		m.pendingCount = n
	}
	return true
}

// takeCount consumes the pending count, returning at least 1.
//
// Consuming rather than reading: a count applies to one motion and is spent
// by it, so "9{" then "{" moves nine and then one.
func (m *Model) takeCount() int {
	n := m.pendingCount
	m.pendingCount = 0
	if n < 1 {
		return 1
	}
	return n
}

// countLabel is the pending count as the header shows it, or "".
//
// Shown because a prefix that is invisible is a prefix you cannot tell from a
// key that did nothing: typing 9 and seeing no change is indistinguishable
// from typing 9 into a surface that ignores digits, which is exactly what
// this thread did until now.
func (m Model) countLabel() string {
	if m.pendingCount == 0 {
		return ""
	}
	return itoa(m.pendingCount)
}
