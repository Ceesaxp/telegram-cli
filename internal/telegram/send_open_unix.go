//go:build unix

package telegram

import (
	"os"
	"syscall"
)

// sendOpenFlags opens a file to send without waiting on it. A fifo opened
// for reading otherwise blocks until something writes to it, before its
// type can be asked; O_NONBLOCK makes the open return at once so the fifo
// is refused like any other file that is not regular. A regular file
// ignores the flag, so reading the one that is sent is unchanged.
const sendOpenFlags = os.O_RDONLY | syscall.O_NONBLOCK
