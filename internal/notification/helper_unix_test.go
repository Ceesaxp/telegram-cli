//go:build unix

package notification

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A helper killed at its timeout used to take only itself. One that is a
// script, or that starts programs of its own, left them running for as long
// as they cared to: here, two sleeps of a minute each. The whole process
// group goes now.
func TestATimedOutHelperTakesItsChildrenWithIt(t *testing.T) {
	pids := filepath.Join(t.TempDir(), "pids")
	standIns(t, spawnsAndHangs(pids), "notify-send")
	t.Cleanup(func() {
		for _, pid := range recordedPIDs(t, pids) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})

	if err := runHelper(time.Second, "notify-send"); err == nil {
		t.Fatal("precondition: a helper that hung reported success")
	}

	recorded := recordedPIDs(t, pids)
	if len(recorded) != 3 {
		t.Fatalf("precondition: the stand-in recorded %d processes, want 3 — itself and two of its own", len(recorded))
	}
	// Polled, because an orphan that has been killed is reaped by init in
	// its own time. The deadline only turns "never" into a failure.
	deadline := time.Now().Add(5 * time.Second)
	for {
		survivors := living(recorded)
		if len(survivors) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after the timeout, %v of the helper's processes %v are still running", survivors, recorded)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// spawnsAndHangs is a stand-in helper that records its own PID and those of
// two programs it starts — one in the background, one in the foreground,
// without exec — and then waits on the second.
func spawnsAndHangs(pids string) string {
	return "echo $$ >> '" + pids + "'\n" +
		"/bin/sleep 60 &\n" +
		"echo $! >> '" + pids + "'\n" +
		"/bin/sh -c 'echo $$ >> \"" + pids + "\"; exec /bin/sleep 60'"
}

// recordedPIDs is every PID the stand-in wrote down.
func recordedPIDs(t *testing.T, path string) []int {
	t.Helper()
	out, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}

	var pids []int
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatalf("the stand-in recorded %q as a PID", field)
		}
		pids = append(pids, pid)
	}
	return pids
}

// living is those of pids that are still running.
func living(pids []int) []int {
	var alive []int
	for _, pid := range pids {
		if syscall.Kill(pid, 0) == nil {
			alive = append(alive, pid)
		}
	}
	return alive
}
