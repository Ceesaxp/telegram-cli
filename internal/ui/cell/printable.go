package cell

import "strings"

// Printable makes text safe to put on a terminal: every C0 and C1 control,
// DEL, and any byte that is not UTF-8 becomes U+FFFD.
//
// Text the client reports quotes what it is about, and what it quotes can be
// anybody's — a theme file is made to be passed around, so a key spelled
// "\x1b]0;…" would retitle the window, and worse sequences exist. The guard
// goes where the text is shown, because a guard at each place a message is
// built is a guard that the next one forgets. Replaced rather than dropped,
// so the reader can see something was there; newlines and tabs go too, as
// the one-line messages this is for have none of their own.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return '�'
		}
		// strings.Map hands a byte that is not UTF-8 over as U+FFFD and,
		// given it back, writes U+FFFD in its place.
		return r
	}, s)
}
