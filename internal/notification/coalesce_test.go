package notification

import "testing"

// Stopping drops the notification waiting behind the one in flight: the
// client is going away, and "3 new messages" from something that has
// already quit is an alert about nothing the reader can open.
func TestStoppingDropsWhatIsWaiting(t *testing.T) {
	p := newProcess()
	c := newCoalescer(func(title, body string) { _ = p.notify(title, body) })

	c.post("Ana", "see you at six")
	p.awaitStart(t)
	c.post("Ben", "running late")
	c.stop()
	p.exit()
	c.wait()

	runs, _ := p.report()
	if want := [2]string{"Ana", "see you at six"}; len(runs) != 1 || runs[0] != want {
		t.Errorf("after stopping, the notifier ran %q, want only [%q]", runs, want)
	}
}
