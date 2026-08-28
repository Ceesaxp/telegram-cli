package clipboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withTempRoot points tempRoot at a fresh sandbox directory for the
// duration of the test, so spool()'s sweep never touches the developer's
// real system temp directory, and resets the cached spoolDir singleton
// afterwards so the next test starts clean. It returns the sandbox path.
func withTempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	prev := tempRoot
	tempRoot = func() string { return root }
	t.Cleanup(func() { tempRoot = prev })
	t.Cleanup(Cleanup)
	return root
}

func TestSweepRemovesDeadPidDir(t *testing.T) {
	root := withTempRoot(t)

	dead := filepath.Join(root, spoolDirPrefix+"999999")
	if err := os.MkdirAll(dead, 0o700); err != nil {
		t.Fatal(err)
	}
	then := time.Now().Add(-(staleSpoolAge + time.Hour))
	if err := os.Chtimes(dead, then, then); err != nil {
		t.Fatal(err)
	}

	if _, err := spool(); err != nil {
		t.Fatalf("spool: %v", err)
	}

	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Errorf("dead-pid dir %q survived the sweep", dead)
	}
}

func TestSweepKeepsFreshUnparseableDir(t *testing.T) {
	root := withTempRoot(t)

	fresh := filepath.Join(root, spoolDirPrefix+"not-a-pid")
	if err := os.MkdirAll(fresh, 0o700); err != nil {
		t.Fatal(err)
	}
	// Leave its mtime at "now" (whatever MkdirAll just set it to).

	if _, err := spool(); err != nil {
		t.Fatalf("spool: %v", err)
	}

	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh unparseable-name dir %q was swept: %v", fresh, err)
	}
}

func TestSweepRemovesOldUnparseableDir(t *testing.T) {
	root := withTempRoot(t)

	old := filepath.Join(root, spoolDirPrefix+"not-a-pid")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	then := time.Now().Add(-(staleSpoolAge + time.Hour))
	if err := os.Chtimes(old, then, then); err != nil {
		t.Fatal(err)
	}

	if _, err := spool(); err != nil {
		t.Fatalf("spool: %v", err)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old unparseable-name dir %q survived the sweep", old)
	}
}

// The identity check in sweepStaleSpools ("own is never touched") is what's
// under test here, not the mtime grace period — the own dir is deliberately
// backdated past staleSpoolAge and sweepStaleSpools is called directly
// (rather than via spool(), which would just re-touch it) to isolate that.
func TestSweepNeverRemovesOwnDir(t *testing.T) {
	withTempRoot(t)

	dir, err := spool()
	if err != nil {
		t.Fatalf("spool: %v", err)
	}
	then := time.Now().Add(-(staleSpoolAge + time.Hour))
	if err := os.Chtimes(dir, then, then); err != nil {
		t.Fatal(err)
	}
	sweepStaleSpools(dir)

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("own spool dir %q was swept: %v", dir, err)
	}
}

// A pre-existing path at our deterministic spool location must never be
// trusted blindly: on a shared machine, an attacker who can predict our pid
// could plant a symlink there ahead of us, pointing at something sensitive
// (e.g. ~/.ssh/authorized_keys), hoping a later WriteFile follows it.
// createSpoolDir must detect that and fall back to a randomly-suffixed
// directory instead of writing through the trap.
func TestCreateSpoolDirFallsBackOnPreCreatedSymlink(t *testing.T) {
	root := withTempRoot(t)

	target := filepath.Join(t.TempDir(), "attacker-target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	trap := filepath.Join(root, fmt.Sprintf("%s%d", spoolDirPrefix, os.Getpid()))
	if err := os.Symlink(target, trap); err != nil {
		t.Skipf("cannot create a symlink in this environment: %v", err)
	}

	dir, err := spool()
	if err != nil {
		t.Fatalf("spool: %v", err)
	}

	if dir == trap {
		t.Fatalf("spool() returned the attacker-planted path %q instead of falling back", trap)
	}
	if filepath.Dir(dir) != root {
		t.Errorf("fallback dir %q was not created under the sandbox root %q", dir, root)
	}
	if !strings.HasPrefix(filepath.Base(dir), spoolDirPrefix) {
		t.Errorf("fallback dir %q doesn't carry the expected prefix %q", dir, spoolDirPrefix)
	}

	// The trap itself must be left alone — still the attacker's symlink,
	// never replaced or written through.
	info, err := os.Lstat(trap)
	if err != nil {
		t.Fatalf("lstat trap: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("attacker symlink was replaced instead of left untouched")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("read attacker target dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("something was written through the attacker symlink into %q: %v", target, entries)
	}
}

func TestSpoolDirPID(t *testing.T) {
	cases := []struct {
		dir     string
		wantPID int
		wantOK  bool
	}{
		{spoolDirPrefix + "1234", 1234, true},
		{spoolDirPrefix + "0", 0, false},
		{spoolDirPrefix + "-5", 0, false},
		{spoolDirPrefix + "abc123", 0, false},
		{spoolDirPrefix, 0, false},
	}
	for _, c := range cases {
		pid, ok := spoolDirPID(c.dir)
		if pid != c.wantPID || ok != c.wantOK {
			t.Errorf("spoolDirPID(%q) = (%d, %v), want (%d, %v)", c.dir, pid, ok, c.wantPID, c.wantOK)
		}
	}
}
