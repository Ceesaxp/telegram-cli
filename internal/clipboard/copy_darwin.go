//go:build darwin

package clipboard

func writeText(text string) error {
	return runCopy(text, "pbcopy")
}
