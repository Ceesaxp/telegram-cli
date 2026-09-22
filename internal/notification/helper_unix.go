//go:build unix

package notification

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// killGroupOnCancel starts cmd in a process group of its own and has its
// cancellation kill the whole group.
//
// Killing only the process cmd started leaves behind whatever that process
// started in turn — a helper that is a script, or a player that hands the
// sound to a child — running for as long as it cares to.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			// The group is already gone: the helper exited as the
			// timeout fell, and its own exit status is the answer.
			return os.ErrProcessDone
		}
		return err
	}
}
