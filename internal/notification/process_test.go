package notification

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// process stands in for a helper the notifier or the player would start —
// notify-send, osascript, paplay, afplay. It records what it was started
// with and how many ran at once, and each run lasts until the test lets it
// exit, which is what a slow desktop looks like from here.
type process struct {
	started chan struct{}
	release chan struct{}

	mu      sync.Mutex
	running int
	peak    int
	runs    [][2]string
}

func newProcess() *process {
	return &process{
		// Room for one start per call a test makes, so a broken bound
		// shows up as a count rather than as a hang.
		started: make(chan struct{}, 64),
		release: make(chan struct{}),
	}
}

// notify is the process as the notifier's system seam sees it.
func (p *process) notify(title, body string) { p.run(title, body) }

// play is the process as the sound player's seam sees it.
func (p *process) play() { p.run("", "") }

func (p *process) run(title, body string) {
	p.mu.Lock()
	p.running++
	p.peak = max(p.peak, p.running)
	p.runs = append(p.runs, [2]string{title, body})
	p.mu.Unlock()

	p.started <- struct{}{}
	<-p.release

	p.mu.Lock()
	p.running--
	p.mu.Unlock()
}

// awaitStart waits for the next run to begin. The timeout only turns a hang
// into a failure; nothing is measured against it.
func (p *process) awaitStart(t *testing.T) {
	t.Helper()
	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("no process was started")
	}
}

// exit lets every run finish: the one in flight, and any started later.
func (p *process) exit() { close(p.release) }

// installed stands in for exec.LookPath on a machine with exactly these
// programs on its PATH.
func installed(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, n := range names {
			if n == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", exec.ErrNotFound
	}
}

// failingPrograms puts programs with these names first on the PATH, each of
// which exits non-zero without doing anything — a notify-send with no
// notification daemon behind it, a paplay with no sound server.
func failingPrograms(t *testing.T, names ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-ins are shell scripts")
	}

	dir := t.TempDir()
	for _, name := range names {
		script := []byte("#!/bin/sh\nexit 1\n")
		if err := os.WriteFile(filepath.Join(dir, name), script, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

// stdout is everything written to standard output while f runs — which is
// the terminal, and not this package's to write to.
func stdout(t *testing.T, f func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	f()

	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// report is every run so far, in order, and the most that ran at once.
func (p *process) report() ([][2]string, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][2]string(nil), p.runs...), p.peak
}
