//go:build unix

package telegram

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
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

// A path inside a root that cannot be opened for a reason of its own is
// refused for that reason, not as outside: the caller named the right
// directory, and "outside" would send them looking for a typo.
func TestSendRootsSayWhyAPathInsideCannotBeSent(t *testing.T) {
	root, _, _ := sendRoot(t)
	// Nine links in a chain, one more than os.Root follows.
	prev := "file.txt"
	for i := 9; i >= 1; i-- {
		name := fmt.Sprintf("link%d", i)
		if err := os.Symlink(prev, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
		prev = name
	}
	// A socket's path is limited to about a hundred bytes, which a test's
	// own temporary directory can already exceed.
	sockets, err := os.MkdirTemp("", "sr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockets) })
	ln, err := net.Listen("unix", filepath.Join(sockets, "s"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	for name, tc := range map[string]struct {
		path, root, want string
	}{
		"a chain of links too long to follow": {filepath.Join(root, "link1"), root, "too many levels of symbolic links"},
		"a file used as a directory":          {filepath.Join(root, "file.txt", "x"), root, "not a directory"},
		"a socket":                            {filepath.Join(sockets, "s"), sockets, "not a regular file"},
	} {
		t.Run(name, func(t *testing.T) {
			f, err := openAllowed(tc.path, tc.root)
			if err == nil {
				f.Close()
				t.Fatalf("opened %s, want it refused", tc.path)
			}
			if want := fmt.Sprintf("%q: %s", tc.path, tc.want); !strings.Contains(err.Error(), want) {
				t.Errorf("error = %v, want %s", err, want)
			}
			if strings.Contains(err.Error(), "outside") {
				t.Errorf("error = %v, want no talk of outside for a path that is inside", err)
			}
		})
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
