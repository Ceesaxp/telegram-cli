package attach

import (
	"net/url"
	"os"
	"strings"
)

// UnquotePath turns what a terminal delivers when a file is dropped on it
// into a path a stat call will find.
//
// Dragging a file onto a terminal is how people already hand a path to a
// program, and it is the natural gesture for this surface — typing a path
// is the part nobody wants to do. But a drop does not arrive as keystrokes:
// the terminal pastes it, escaped the way a SHELL would need it, because
// what it is really doing is typing a command line for you. Three spellings
// are in use:
//
//	/Users/a/My\ Files/x.png     iTerm2, Terminal.app — backslash-escaped
//	'/Users/a/My Files/x.png'    quoted, single or double
//	file:///Users/a/My%20Files/  a URL, from several Linux terminals
//
// Getting this wrong fails on exactly the files people drag: the ones with
// a space in the name. Everything else already works by being typed.
//
// A multi-file drop delivers several paths separated by spaces, which is
// ambiguous against a single path that contains one. Only the first is
// taken, and only where the separation is unambiguous — a URL list — which
// is the honest reading while decision 5 keeps one staged attachment.
func UnquotePath(text string) string {
	text = strings.Trim(text, " \t\r\n")
	if text == "" {
		return ""
	}

	// A URL list is the one form where whitespace definitely separates
	// paths rather than appearing inside one.
	if strings.HasPrefix(text, "file://") {
		if i := strings.IndexAny(text, " \t\r\n"); i >= 0 {
			text = text[:i]
		}
		return fromFileURL(text)
	}

	// Quoted whole: the quotes are the terminal's, not the path's.
	if len(text) >= 2 {
		if q := text[0]; (q == '\'' || q == '"') && text[len(text)-1] == q {
			inner := text[1 : len(text)-1]
			if q == '\'' {
				// Single quotes are literal in every shell that emits them.
				return inner
			}
			return unescape(inner)
		}
	}

	return unescape(text)
}

// fromFileURL decodes a file:// URL, dropping a localhost authority.
func fromFileURL(text string) string {
	parsed, err := url.Parse(text)
	if err != nil {
		return ""
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		// A path on another machine is not a path this client can read, and
		// silently attaching a same-named local file would be worse than
		// failing.
		return ""
	}
	return parsed.Path
}

// unescape removes backslash escaping, which is how both macOS terminals
// deliver a dropped path.
//
// Not where the backslash IS the separator: nothing on Windows escapes with
// it, and undoing escapes there would turn a dropped C:\Users\me\x.png into
// C:Usersmex.png — every drop on the platform, silently.
//
// A trailing lone backslash is kept rather than dropped: it is a legal
// character in a filename, and a path ending in one is more likely to be a
// real name than a truncated escape.
func unescape(text string) string {
	if sep != '/' || !strings.ContainsRune(text, '\\') {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			i++
			out.WriteByte(text[i])
			continue
		}
		out.WriteByte(text[i])
	}
	return out.String()
}

// ResolvePath is the whole answer to "is this paste a file, and which one":
// the path a dropped or pasted string names, ready to hand to the composer,
// and whether it unambiguously names one at all.
//
// One function rather than a predicate beside a converter. The two were
// separate for one round and the app used the predicate's answer with the
// converter's output — so a "~/shot.png" drop passed the check, which
// expands the tilde, and staged the literal string, which does not exist.
// A question and its answer computed by different code is a pair that can
// disagree.
//
// The test is "unambiguously a path" rather than "possibly a path", because
// a paste that merely resembles one must not silently become an attachment
// instead of the message somebody meant to send: one line, rooted or
// home-relative or a URL, naming a file that is actually there.
func ResolvePath(text string) (string, bool) {
	trimmed := strings.Trim(text, " \t\r\n")
	if trimmed == "" || strings.ContainsAny(trimmed, "\n\r") {
		return "", false
	}
	if !rooted(trimmed) &&
		!strings.HasPrefix(trimmed, "~/") &&
		!strings.HasPrefix(trimmed, "file://") &&
		!strings.HasPrefix(trimmed, "'") &&
		!strings.HasPrefix(trimmed, "\"") {
		return "", false
	}

	unquoted := UnquotePath(trimmed)
	if unquoted == "" {
		return "", false
	}
	resolved := native(toSlash(unquoted))
	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() {
		return "", false
	}
	return resolved, true
}

// rooted reports whether a path names a place rather than a relative step:
// a leading separator, or a drive or share root on Windows.
func rooted(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	return sep != '/' &&
		(strings.HasPrefix(p, string(sep)) || volume(toSlash(p)) != "")
}
