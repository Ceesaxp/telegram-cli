package chatview

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Saving is a copy out of the cache, not the download itself.
//
// `s` used to report "💾 Saved photo → /home/you/.local/share/tele-tui/files/
// 1234567890" and stop there. That is the media CACHE — where a download
// lands so a photo drawn twice is fetched once — and it names files by their
// server-side id. Nothing had been saved in the sense the key implies: there
// was no destination a person would look in, and no filename they would
// recognise.

// saveInto copies src into dir under name, without overwriting anything, and
// returns the path it wrote.
//
// Never overwriting is the whole point of the suffixing below. A save that
// silently replaces an earlier file of the same name is a data-loss bug
// wearing the clothes of a convenience: "photo.jpg" is what every phone
// camera in the world calls its output, so the collision is the common case
// rather than the edge one.
func saveInto(dir, name, src string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("no download directory configured")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}

	dst, err := freeName(dir, name)
	if err != nil {
		return "", err
	}

	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("reading the download: %w", err)
	}
	defer in.Close()

	// O_EXCL so the name this picked cannot be taken between the check and
	// the create — by another save in the same second, or by anything else
	// on the machine.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return "", fmt.Errorf("writing %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return "", fmt.Errorf("writing %s: %w", dst, err)
	}
	return dst, nil
}

// freeName is dir/name, or dir/name (2), (3)... when that is taken.
//
// The suffix goes before the extension, where every file manager puts it, so
// the result is still recognisably a .jpg.
func freeName(dir, name string) (string, error) {
	name = safeName(name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)

	for n := 1; n < 1000; n++ {
		candidate := filepath.Join(dir, name)
		if n > 1 {
			candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		}
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("a thousand files are already called %q", name)
}

// safeName reduces a sender-supplied filename to something that can only land
// inside the download directory.
//
// The name comes off the wire, so it is not trustworthy: a document called
// "../../.ssh/authorized_keys" would otherwise be written wherever that
// resolves to. Taking the base is what confines it; the rest is for names
// that are empty, or are "." or ".." once the path is gone.
func safeName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	switch name {
	case "", ".", "..", string(filepath.Separator):
		return "telegram-file"
	}
	return name
}
