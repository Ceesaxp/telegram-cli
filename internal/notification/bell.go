package notification

import (
	"sync"
	"time"
)

// bellLimiter lets the terminal bell ring at most once in minSoundInterval.
//
// The bell stands in for a notifier or a sound player the machine does not
// have, and a burst of messages would otherwise ring it once each.
type bellLimiter struct {
	now func() time.Time

	mu   sync.Mutex
	last time.Time // when it last rang
}

func newBellLimiter(now func() time.Time) *bellLimiter {
	return &bellLimiter{now: now}
}

// ring is the bell for the caller to write, or "" when it rang less than
// minSoundInterval ago.
func (b *bellLimiter) ring() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	if now.Sub(b.last) < minSoundInterval {
		return ""
	}
	b.last = now
	return bell
}
