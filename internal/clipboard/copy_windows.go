//go:build windows

package clipboard

// clip.exe reads stdin and sets the clipboard. It ships with Windows, so
// unlike the Unix helpers there is nothing to look up first.
func writeText(text string) error {
	return runCopy(text, "clip.exe")
}
