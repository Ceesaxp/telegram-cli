//go:build linux || freebsd || openbsd || netbsd || dragonfly

package clipboard

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// reader is one way of getting data out of the X11 or Wayland clipboard.
type reader struct {
	// types lists the MIME types currently on the clipboard.
	types func(ctx context.Context) ([]string, error)
	// read returns the clipboard contents for one MIME type.
	read func(ctx context.Context, mime string) ([]byte, error)
}

func readImage(dir string) (string, error) {
	r, err := pickReader()
	if err != nil {
		return "", err
	}

	typesCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	available, err := r.types(typesCtx)
	cancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", ErrNoImage
	}

	for _, mime := range preferredMimes {
		if !contains(available, mime) {
			continue
		}
		readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		data, err := r.read(readCtx, mime)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return "", err
			}
			continue
		}
		if len(data) == 0 {
			continue
		}
		dest := spoolPath(dir, extForMime(mime))
		if err := os.WriteFile(dest, data, 0o600); err != nil {
			return "", fmt.Errorf("clipboard: spool image: %w", err)
		}
		return dest, nil
	}
	return "", ErrNoImage
}

func readFilePath() (string, error) {
	r, err := pickReader()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := r.read(ctx, "text/uri-list")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", ErrNoImage
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "file://") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil {
			continue
		}
		return u.Path, nil
	}
	return "", ErrNoImage
}

// pickReader returns the clipboard helper to use: wl-clipboard under Wayland,
// xclip otherwise.
func pickReader() (reader, error) {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("wl-paste"); err == nil {
			return wlPaste(), nil
		}
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		return xclip(), nil
	}
	if _, err := exec.LookPath("wl-paste"); err == nil {
		return wlPaste(), nil
	}
	return reader{}, ErrNoTool
}

func wlPaste() reader {
	return reader{
		types: func(ctx context.Context) ([]string, error) {
			out, err := exec.CommandContext(ctx, "wl-paste", "--list-types").Output()
			if err != nil {
				return nil, timeoutErr(ctx, "list clipboard types", err)
			}
			return splitLines(string(out)), nil
		},
		read: func(ctx context.Context, mime string) ([]byte, error) {
			out, err := exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", mime).Output()
			if err != nil {
				return nil, timeoutErr(ctx, "read clipboard data", err)
			}
			return out, nil
		},
	}
}

func xclip() reader {
	return reader{
		types: func(ctx context.Context) ([]string, error) {
			out, err := exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-t", "TARGETS", "-o").Output()
			if err != nil {
				return nil, timeoutErr(ctx, "list clipboard types", err)
			}
			return splitLines(string(out)), nil
		},
		read: func(ctx context.Context, mime string) ([]byte, error) {
			out, err := exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-t", mime, "-o").Output()
			if err != nil {
				return nil, timeoutErr(ctx, "read clipboard data", err)
			}
			return out, nil
		},
	}
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// preferredMimes lists clipboard image types in the order we want them,
// best-for-Telegram first.
var preferredMimes = []string{
	"image/png",
	"image/jpeg",
	"image/webp",
	"image/gif",
	"image/bmp",
	"image/tiff",
}
