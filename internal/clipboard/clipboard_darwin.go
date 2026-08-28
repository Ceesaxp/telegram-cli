package clipboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// pasteboardScript writes the clipboard, coerced to the given four-char
// pasteboard class, to the file named by the first argument. Coercion fails
// (and the script errors out) when the clipboard holds no such flavor.
const pasteboardScript = `on run argv
	set outFile to item 1 of argv
	set clipData to (the clipboard as «class %s»)
	set fh to open for access (POSIX file outFile) with write permission
	try
		set eof fh to 0
		write clipData to fh
	on error errMsg number errNum
		close access fh
		error errMsg number errNum
	end try
	close access fh
end run`

// filePathScript prints the POSIX path of a file reference on the clipboard.
// Note that AppleScript happily coerces plain text into a file URL, so the
// caller must check that the path actually exists.
const filePathScript = `POSIX path of (the clipboard as «class furl»)`

func readImage(dir string) (string, error) {
	// PNG first: nearly every app offers it, and it needs no conversion.
	png := spoolPath(dir, "png")
	err := writePasteboard("PNGf", png)
	if err == nil && hasData(png) {
		return png, nil
	}
	os.Remove(png)
	if errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}

	// Some apps (Preview, older Cocoa apps) only put TIFF on the pasteboard.
	tiff := spoolPath(dir, "tiff")
	if err := writePasteboard("TIFF", tiff); err != nil || !hasData(tiff) {
		os.Remove(tiff)
		if errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", ErrNoImage
	}
	defer os.Remove(tiff)

	converted := strings.TrimSuffix(tiff, ".tiff") + ".png"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, cerr := exec.CommandContext(ctx, "sips", "-s", "format", "png", tiff, "--out", converted).CombinedOutput()
	if cerr != nil {
		os.Remove(converted)
		return "", timeoutErr(ctx, "convert TIFF to PNG",
			fmt.Errorf("clipboard: convert TIFF to PNG: %w: %s", cerr, strings.TrimSpace(string(out))))
	}
	if !hasData(converted) {
		os.Remove(converted)
		return "", ErrNoImage
	}
	return converted, nil
}

func readFilePath() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "osascript", "-e", filePathScript).Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", timeoutErr(ctx, "read file path", err)
		}
		return "", ErrNoImage
	}
	return strings.TrimSpace(string(out)), nil
}

// writePasteboard runs the AppleScript that dumps one pasteboard flavor to
// dest.
func writePasteboard(class, dest string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-", dest)
	cmd.Stdin = strings.NewReader(fmt.Sprintf(pasteboardScript, class))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return timeoutErr(ctx, fmt.Sprintf("read %s flavor", class),
			fmt.Errorf("clipboard: read %s flavor: %w: %s", class, err, strings.TrimSpace(stderr.String())))
	}
	return nil
}
