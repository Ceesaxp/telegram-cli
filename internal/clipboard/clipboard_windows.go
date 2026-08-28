package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// exitNoData is the exit code our PowerShell snippets use to report an empty
// clipboard, as opposed to a real failure.
const exitNoData = 2

func readImage(dir string) (string, error) {
	dest := spoolPath(dir, "png")
	script := fmt.Sprintf(
		`Add-Type -AssemblyName System.Windows.Forms,System.Drawing; `+
			`$img = [System.Windows.Forms.Clipboard]::GetImage(); `+
			`if ($img -eq $null) { exit %d }; `+
			`$img.Save('%s', [System.Drawing.Imaging.ImageFormat]::Png)`,
		exitNoData, psQuote(dest))

	if _, err := runPowerShell(script); err != nil {
		os.Remove(dest)
		return "", err
	}
	if !hasData(dest) {
		os.Remove(dest)
		return "", ErrNoImage
	}
	return dest, nil
}

func readFilePath() (string, error) {
	script := fmt.Sprintf(
		`Add-Type -AssemblyName System.Windows.Forms; `+
			`$files = [System.Windows.Forms.Clipboard]::GetFileDropList(); `+
			`if ($files.Count -eq 0) { exit %d }; `+
			`[Console]::Out.Write($files[0])`, exitNoData)

	out, err := runPowerShell(script)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// runPowerShell runs script in a single-threaded apartment, which the
// clipboard API requires, maps the empty-clipboard exit code to ErrNoImage,
// and bounds the whole invocation to 15s — PowerShell startup plus STA
// initialization can be slow, so this is generous relative to the other
// helpers in this package.
func runPowerShell(script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-STA", "-Command", script).Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", timeoutErr(ctx, "powershell", err)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == exitNoData {
			return "", ErrNoImage
		}
		return "", fmt.Errorf("clipboard: powershell: %w", err)
	}
	return string(out), nil
}

// psQuote escapes a string for use inside a PowerShell single-quoted literal.
func psQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
