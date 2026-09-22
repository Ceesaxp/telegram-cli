//go:build unix

package telegram

import (
	"errors"
	"os"
	"syscall"
)

// sendOpenFlags opens a file to send without waiting on it. A fifo opened
// for reading otherwise blocks until something writes to it, before its
// type can be asked; O_NONBLOCK makes the open return at once so the fifo
// is refused like any other file that is not regular. A regular file
// ignores the flag, so reading the one that is sent is unchanged.
const sendOpenFlags = os.O_RDONLY | syscall.O_NONBLOCK

// whyInsideOS is whyInside for the errors unix gives names to. A chain of
// more links than os.Root follows, and a file used as a directory on the
// way, are both inside the root and say so themselves. What open(2) says
// of a socket — ENXIO on Linux, EOPNOTSUPP on macOS — or of a device with
// nothing behind it is that it is not a regular file, which is the clearer
// way to put it.
func whyInsideOS(err error) error {
	switch {
	case errors.Is(err, syscall.ELOOP), errors.Is(err, syscall.ENOTDIR):
		return err
	case errors.Is(err, syscall.ENXIO), errors.Is(err, syscall.EOPNOTSUPP):
		return errNotRegular
	}
	return nil
}
