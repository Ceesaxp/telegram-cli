package notification

import (
	"fmt"
	"sync"
)

// coalescedTitle heads a notification that stands for more than one message.
const coalescedTitle = "tele-tui"

// coalescer runs one notifier process at a time, and folds everything that
// arrives while one is running into a single notification waiting behind it.
//
// A burst — a busy group, or the backlog replayed after a reconnect — used to
// start a notifier process per message, all at once. Nobody reads fifty
// alerts: they read the first one and the number.
//
// The worker is started by a notification and exits as soon as nothing is
// waiting, so no goroutine lives as long as the process does.
type coalescer struct {
	send func(title, body string)

	mu     sync.Mutex
	busy   bool
	closed bool
	next   pending
	worker sync.WaitGroup
}

// pending is the notification waiting behind the one in flight.
type pending struct {
	title, body string // the first message's own, said when it is alone
	count       int    // how many messages it stands for; zero when none
}

// add folds one more message in.
func (p *pending) add(title, body string) {
	if p.count == 0 {
		p.title, p.body = title, body
	}
	p.count++
}

// text is what the notification says: the message itself when it stands
// for one, and how many when it stands for more.
func (p pending) text() (title, body string) {
	if p.count == 1 {
		return p.title, p.body
	}
	return coalescedTitle, fmt.Sprintf("%d new messages", p.count)
}

func newCoalescer(send func(title, body string)) *coalescer {
	return &coalescer{send: send}
}

// post hands a notification to the worker and returns at once: the caller is
// the event loop, and the process may take as long as the desktop does.
func (c *coalescer) post(title, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	if !c.busy {
		c.busy = true
		c.worker.Add(1)
		go c.run(title, body)
		return
	}
	c.next.add(title, body)
}

// run posts one notification, then whatever gathered behind it, until
// nothing has.
func (c *coalescer) run(title, body string) {
	defer c.worker.Done()
	for {
		c.send(title, body)

		c.mu.Lock()
		next := c.next
		c.next = pending{}
		if next.count == 0 {
			c.busy = false
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		title, body = next.text()
	}
}

// close stops the coalescer and waits for its worker.
func (c *coalescer) close() {
	c.stop()
	c.wait()
}

// stop keeps anything new from starting, including what is already waiting.
func (c *coalescer) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.next = pending{}
}

// wait returns once the worker, if one is running, has finished. It is for
// when nothing more is being posted: a worker started during the wait need
// not be waited for.
func (c *coalescer) wait() {
	c.worker.Wait()
}
