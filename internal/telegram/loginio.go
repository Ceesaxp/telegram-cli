package telegram

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadAuthLine reads one line from in. When hide is true and in is a
// terminal, the input is not echoed (for 2FA passwords). Piped input
// always falls back to a normal line read so tests and scripted logins
// still work.
func ReadAuthLine(in *os.File, hide bool) (string, error) {
	if hide && term.IsTerminal(int(in.Fd())) {
		secret, err := term.ReadPassword(int(in.Fd()))
		// term.ReadPassword does not echo a newline after Enter.
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(secret), nil
	}
	// Reader size 1 so a leftover line is not buffered away from the
	// next ReadAuthLine call (the CLI login loop reads field by field).
	br := bufio.NewReaderSize(in, 1)
	line, err := br.ReadString('\n')
	if err != nil && !(err == io.EOF && len(line) > 0) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
