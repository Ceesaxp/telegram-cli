package attach

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry is one row of a directory listing.
type Entry struct {
	// Name is the entry's own name. A directory carries no trailing slash
	// here — the view adds one, and completion adds one, but a name that
	// sometimes ends in a separator is a name every comparison has to
	// remember to normalise.
	Name string

	// Dir is set for a directory. It decides the glyph, the size column,
	// and what Enter does.
	Dir bool

	// Size is the file's size in bytes; Items is a directory's entry count,
	// and -1 when the directory could not be read to count them.
	Size  int64
	Items int

	// ModTime is what the mtime column shows.
	ModTime time.Time

	// Image is set for a file this client would send as a photo. It is the
	// only thing the send-mode toggle applies to.
	Image bool
}

// expandHome turns a leading ~ into the home directory. A bare "~" and
// "~/..." both count; "~other" does not, because this is not a shell and
// resolving another user's home is not something a file picker should be
// guessing at.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// collapseHome is expandHome's inverse, for display. The picker shows
// "~/Downloads/" rather than "/Users/someone/Downloads/" because the prompt
// row is 60 cells wide and the home prefix is the part carrying no
// information.
func collapseHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	switch {
	case path == home:
		return "~"
	case strings.HasPrefix(path, home+string(filepath.Separator)):
		return "~" + path[len(home):]
	}
	return path
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

// parentOf is the directory above dir, for going up.
//
// dir always ends in a separator, and the result does too except at the
// root, where there is nowhere further to go and the answer is itself. "~/"
// goes to the real parent of home rather than stopping there: the spec's
// "never past ~/" left no route to /etc, and a picker that cannot reach a
// file is not a picker.
func parentOf(dir string) string {
	trimmed := strings.TrimSuffix(dir, "/")
	if trimmed == "" {
		return dir
	}
	up := filepath.Dir(expandHome(trimmed))
	if up == "" || up == "." {
		return ""
	}
	up = collapseHome(up)
	if !strings.HasSuffix(up, "/") {
		up += "/"
	}
	return up
}

// readDir lists one directory, sorted directories-first and then by name.
//
// Dotfiles are omitted, the way a shell omits them, unless the reader has
// typed a leading dot — which is the only way anyone ever wants to see
// them, and the only signal available that they do.
//
// A directory's item count costs one extra read per subdirectory. That is
// paid once per listing rather than once per keystroke, because filtering
// never re-reads: the listing is the thing that is cached and the tail is
// the thing that changes.
func readDir(dir string, showHidden bool) ([]Entry, error) {
	items, err := os.ReadDir(expandHome(dir))
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
		if entry.Dir {
			entry.Items = countItems(filepath.Join(expandHome(dir), name))
		} else {
			entry.Image = IsImage(name)
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
// everywhere else on their machine.
func matches(name, tail string) bool {
	return strings.HasPrefix(strings.ToLower(name), strings.ToLower(tail))
}

// IsImage reports whether this client would send the file as a photo.
//
// By extension, because the picker must not read a file to draw a row: a
// listing is drawn on every keystroke and a directory of large files would
// make typing wait on the disk. The extensions are the ones the send path
// itself can handle.
//
// Exported because a file dropped on the composer with no picker open is
// staged directly, and it has to send the same way it would have if it had
// been picked — one answer to "is this a photo", not two.
func IsImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return true
	}
	return false
}

// kindOf names the media class a file belongs to, which decides its glyph.
func kindOf(e Entry) string {
	if e.Dir {
		return "directory"
	}
	if IsImage(e.Name) {
		return "image"
	}
	switch strings.ToLower(filepath.Ext(e.Name)) {
	case ".mp3", ".m4a", ".ogg", ".oga", ".opus", ".wav", ".flac", ".aac":
		return "audio"
	case ".mp4", ".mov", ".mkv", ".webm", ".avi", ".m4v":
		return "video"
	}
	return "document"
}

// formatSize is the size column: a byte count for a file, an item count for
// a directory, and nothing at all for a directory that could not be read.
func formatSize(e Entry) string {
	if e.Dir {
		switch {
		case e.Items < 0:
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
