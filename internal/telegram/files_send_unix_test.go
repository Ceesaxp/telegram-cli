//go:build unix

package telegram

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A file the process cannot read is unreadable, not outside: as with a
// missing one, the caller named the right directory.
func TestSendRootsSaysAnUnreadableFileIsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	root, file, _ := sendRoot(t)
	if err := os.Chmod(file, 0); err != nil {
		t.Fatal(err)
	}

	f, err := openAllowed(file, root)
	if err == nil {
		f.Close()
		t.Fatalf("opened %s, want it refused", file)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("error = %v, want one that says permission was denied", err)
	}
	if strings.Contains(err.Error(), "outside") {
		t.Errorf("error = %v, want no talk of outside for a path that is inside", err)
	}
}

// A fifo is refused, and promptly. Opening one for reading waits for a
// writer, so an open that asks the path first and the type second hangs
// the handler until someone writes — which is anyone who can write to the
// root.
func TestSendRootsRefusesAFifoWithoutWaitingForAWriter(t *testing.T) {
	root, _, _ := sendRoot(t)
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	type result struct {
		f   *os.File
		err error
	}
	done := make(chan result, 1)
	go func() {
		f, err := openAllowed(fifo, root)
		done <- result{f, err}
	}()

	select {
	case r := <-done:
		wantNotRegular(t, r.f, r.err, fifo)
	case <-time.After(2 * time.Second):
		// Be the writer it is waiting for, so the goroutine can finish.
		if w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			w.Close()
		}
		t.Fatal("opening a fifo blocked waiting for a writer")
	}
}
