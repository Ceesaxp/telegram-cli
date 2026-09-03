package attach

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imtaqin/telegram-cli/internal/clipboard"
)

// Paths inside this package are always SLASH-separated, whatever the
// platform's own separator is.
//
// One representation, converted at the two edges: [native] on the way to the
// filesystem and [display] on the way back. Mixing them is what broke this
// component on Windows before anyone ran it there — `collapseHome` produced
// `~\Downloads`, `Open` appended a `/`, `splitPath` only knew about `/`, and
// the first Ctrl+T opened on a literal relative path that does not exist.
// The picker is a text surface where the reader types separators, and one
// separator is what a reader can type.
//
// sep is the platform's separator, and a var rather than the constant so a
// test can pin the other one — the same seam the clock has, and for the same
// reason: a rule that only runs on one platform is a rule only that
// platform's users get to discover.
var sep = filepath.Separator

// toSlash and fromSlash convert between the internal spelling and the
// platform's. Both are identity where the separator already is a slash.
func toSlash(p string) string {
	if sep == '/' {
		return p
	}
	return strings.ReplaceAll(p, string(sep), "/")
}

func fromSlash(p string) string {
	if sep == '/' {
		return p
	}
	return strings.ReplaceAll(p, "/", string(sep))
}

// Entry is one row of a directory listing.
type Entry struct {
	// Name is the entry's own name. A directory carries no trailing slash
	// here — the view adds one, and completion adds one, but a name that
	// sometimes ends in a separator is a name every comparison has to
	// remember to normalise.
	Name string

	// Dir is set for a directory, INCLUDING a symlink that resolves to one:
	// what matters here is whether Enter can go into it. A broken link is
	// not a directory, because nothing can be entered.
	Dir bool

	// Size is the file's size in bytes; Items is a directory's entry count,
	// -1 when the directory could not be read to count them, and 0 before
	// anything has tried (see countInto, which only counts what is drawn).
	Size  int64
	Items int

	// ModTime is what the mtime column shows.
	ModTime time.Time

	// Image is set for a file this client would send as a photo. It is the
	// only thing the send-mode toggle applies to.
	Image bool

	// counted distinguishes "no items" from "not counted yet", which the
	// zero value cannot.
	counted bool
}

// native is the filesystem path a slash path names: the home expanded and
// the separators put back.
func native(p string) string { return fromSlash(expandHome(p)) }

// display is native's inverse, for anything arriving from the filesystem or
// from a drop.
func display(p string) string { return collapseHome(toSlash(p)) }

// expandHome turns a leading ~ into the home directory. A bare "~" and
// "~/..." both count; "~other" does not, because this is not a shell and
// resolving another user's home is not something a file picker should be
// guessing at.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	home = toSlash(home)
	if p == "~" {
		return home
	}
	return strings.TrimSuffix(home, "/") + p[1:]
}

// collapseHome is expandHome's inverse, for display. The picker shows
// "~/Downloads/" rather than "/Users/someone/Downloads/" because the prompt
// row is 60 cells wide and the home prefix is the part carrying no
// information.
func collapseHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	home = strings.TrimSuffix(toSlash(home), "/")
	switch {
	case p == home:
		return "~"
	case strings.HasPrefix(p, home+"/"):
		return "~" + p[len(home):]
	}
	return p
}

// splitPath separates what has been typed into the directory to list and the
// prefix to filter by.
//
// The separator belongs to the directory: "~/Downloads/back" lists
// "~/Downloads/" and filters on "back", and "~/Downloads/" lists it and
// filters on nothing. A path with no separator at all is a prefix in the
// current directory.
func splitPath(typed string) (dir, tail string) {
	if i := strings.LastIndexByte(typed, '/'); i >= 0 {
		return typed[:i+1], typed[i+1:]
	}
	return "", typed
}

// volume is the drive or share prefix of a slash path — "C:" or "//host/share"
// — and empty where there is none.
//
// Its own implementation rather than filepath.VolumeName, which answers for
// the platform the binary was COMPILED for. That would make every Windows
// path rule unreachable from a test run anywhere else, which is exactly how
// this component shipped a first version that could not open its own default
// directory on Windows.
func volume(p string) string {
	if sep == '/' {
		return ""
	}
	if len(p) >= 2 && p[1] == ':' && isDriveLetter(p[0]) {
		return p[:2]
	}
	if strings.HasPrefix(p, "//") {
		rest := p[2:]
		host := strings.IndexByte(rest, '/')
		if host <= 0 {
			return ""
		}
		share := strings.IndexByte(rest[host+1:], '/')
		if share < 0 {
			return p
		}
		return p[:2+host+1+share]
	}
	return ""
}

func isDriveLetter(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

// isRoot reports whether dir is as far up as this path can go: "/" on Unix,
// and a drive or share root such as "C:/" on Windows.
func isRoot(dir string) bool {
	if dir == "/" {
		return true
	}
	if v := volume(dir); v != "" {
		return strings.TrimSuffix(dir, "/") == strings.TrimSuffix(v, "/")
	}
	return false
}

// parentOf is the directory above dir, for going up.
//
// dir always ends in a separator, and so does the result — except at a root,
// where there is nowhere further to go and the answer is itself. "~/" goes to
// the real parent of home rather than stopping there: the design's "never
// past ~/" left no route to /etc, and a picker that cannot reach a file is
// not a picker.
func parentOf(dir string) string {
	if dir == "" || isRoot(dir) {
		return dir
	}
	expanded := expandHome(dir)
	if isRoot(expanded) {
		return withSlash(collapseHome(expanded))
	}
	trimmed := strings.TrimSuffix(expanded, "/")
	if trimmed == "" {
		return dir
	}
	up := path.Dir(trimmed)
	if up == "" || up == "." {
		return ""
	}
	// path.Dir strips a volume down to "C:", which on Windows is a relative
	// reference rather than the drive root it looks like — withSlash is
	// what puts the separator back, here as everywhere else.
	return withSlash(collapseHome(up))
}

// readDir lists one directory, sorted directories-first and then by name.
//
// Dotfiles are omitted, the way a shell omits them, unless the reader has
// typed a leading dot — which is the only way anyone ever wants to see
// them, and the only signal available that they do.
//
// It costs exactly one read: the item counts the size column shows are NOT
// gathered here. Counting every subdirectory up front turned opening a home
// directory into one read per child, which on a network mount froze the
// whole TUI — see countInto, which counts only what is about to be drawn.
func readDir(dir string, showHidden bool) ([]Entry, error) {
	root := native(dir)
	items, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	out := make([]Entry, 0, len(items))
	for _, item := range items {
		name := item.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}

		entry := Entry{Name: name, Dir: item.IsDir()}
		if info, err := item.Info(); err == nil {
			entry.Size = info.Size()
			entry.ModTime = info.ModTime()
		}

		// A symlink to a directory reports IsDir false, because DirEntry
		// describes the link. Whether Enter can go into it is a question
		// about the TARGET, so ask — but only for links, so an ordinary
		// listing still costs one syscall per entry rather than two. A
		// broken link stays a file: nothing can be entered.
		if !entry.Dir && item.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.IsDir() {
				entry.Dir = true
			}
		}

		if !entry.Dir {
			entry.Image = clipboard.IsImagePath(name)
		}
		out = append(out, entry)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// countInto fills in the item counts for the entries named by idx, which is
// the window about to be drawn.
//
// Bounded on purpose. A count costs a whole extra directory read, and the
// listing is rebuilt on every keystroke; counting all of them made opening
// a directory cost one read per child, and every one of those reads is
// synchronous on Bubble Tea's update path. Six is what the reader can see.
//
// Already-counted entries are left alone, so walking the cursor down a
// listing does not re-read anything.
func countInto(entries []Entry, dir string, idx []int) {
	root := native(dir)
	for _, i := range idx {
		if i < 0 || i >= len(entries) {
			continue
		}
		if entries[i].counted || !entries[i].Dir {
			continue
		}
		entries[i].Items = countItems(filepath.Join(root, entries[i].Name))
		entries[i].counted = true
	}
}

// countItems is a directory's visible entry count, or -1 when it cannot be
// read. A directory you have no permission to list is a real thing to run
// into in a home directory, and "0 items" would be a lie about it.
func countItems(path string) int {
	items, err := os.ReadDir(path)
	if err != nil {
		return -1
	}
	n := 0
	for _, item := range items {
		if !strings.HasPrefix(item.Name(), ".") {
			n++
		}
	}
	return n
}

// matches reports whether an entry's name starts with the typed tail.
//
// Case-insensitively, which is not what a shell does and is deliberate: the
// default macOS filesystem is itself case-insensitive, so a case-sensitive
// filter would hide a file the reader can open by that exact name
// everywhere else on their machine. Where that leniency and an exactly typed
// path disagree, the typed path wins — see Model.Chosen.
func matches(name, tail string) bool {
	return strings.HasPrefix(strings.ToLower(name), strings.ToLower(tail))
}

// IsImage reports whether this client would send the file as a photo.
//
// It defers to the clipboard's answer rather than keeping a second list.
// Ctrl+V has been sending images for several releases and that list is the
// one proven against the send path — it excludes TIFF on purpose, because
// Telegram's InputMediaUploadedPhoto rejects it, and a picker that offered
// photo mode for a .tif would promise a send that fails.
//
// Exported because a file dropped on the composer with no picker open is
// staged directly, and it has to send the same way it would have if it had
// been picked.
func IsImage(name string) bool { return clipboard.IsImagePath(name) }

// kindOf names the media class a file belongs to, which decides its glyph.
func kindOf(e Entry) string {
	if e.Dir {
		return "directory"
	}
	// By extension, and never by reading the file: a listing is drawn on
	// every keystroke, and a directory of large files would make typing
	// wait on the disk.
	switch strings.ToLower(filepath.Ext(e.Name)) {
	case ".mp3", ".m4a", ".ogg", ".oga", ".opus", ".wav", ".flac", ".aac":
		return "audio"
	case ".mp4", ".mov", ".mkv", ".webm", ".avi", ".m4v":
		return "video"
	}
	// Anything the send path treats as a photo, plus the image formats it
	// sends as documents: both are pictures to a reader looking for one.
	switch strings.ToLower(filepath.Ext(e.Name)) {
	case ".tif", ".tiff", ".heic", ".avif", ".svg":
		return "image"
	}
	if IsImage(e.Name) {
		return "image"
	}
	return "document"
}

// formatSize is the size column: a byte count for a file, an item count for
// a directory, and nothing at all for a directory nothing has counted or
// could count.
func formatSize(e Entry) string {
	if e.Dir {
		switch {
		case !e.counted, e.Items < 0:
			return ""
		case e.Items == 1:
			return "1 item"
		}
		return strconv.Itoa(e.Items) + " items"
	}
	return humanBytes(e.Size)
}

// humanBytes is a size at the precision people read rather than the one the
// filesystem stores: one decimal below ten units, none above, so the column
// never needs more than the six cells "999 MB" takes.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	value, exp := float64(n)/unit, 0
	for value >= unit && exp < 3 {
		value /= unit
		exp++
	}
	suffix := [...]string{"KB", "MB", "GB", "TB"}[exp]
	if value < 10 {
		return strconv.FormatFloat(value, 'f', 1, 64) + " " + suffix
	}
	return strconv.FormatFloat(value, 'f', 0, 64) + " " + suffix
}

// formatTime is the mtime column: a clock time for something touched today
// and a date for anything older.
//
// The same rule the chat list uses for its own timestamps, and for the same
// reason — today's files are the ones a picker is usually being opened for,
// and an exact time is what distinguishes them from each other.
func formatTime(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	ty, tm, td := t.Date()
	ny, nm, nd := now.Date()
	if ty == ny && tm == nm && td == nd {
		return t.Format("15:04")
	}
	return t.Format("2 Jan")
}
