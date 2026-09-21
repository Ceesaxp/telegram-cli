package theme

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// The example themes in docs/themes are documentation a reader copies into
// place, so they have to work exactly as shipped: every one loads through
// both halves of the loader with no warning at all. Without this they rot
// into the best demonstration of a broken format there is.
//
// And the set keeps one theme that writes [colors256] and one that does
// not, so both colour-depth paths have a real specimen, not only a test's.
func TestTheShippedThemesLoadWithoutAWarning(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(repoRoot(t), "docs", "themes", "*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("docs/themes has no themes in it")
	}

	var with256, without256 int
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			spec, builtin, warnings := config.LoadTheme(path, "", "")
			if spec == nil {
				t.Fatalf("did not load: %q", warnings)
			}
			warnings = append(warnings, CheckSpec(spec, builtin)...)
			for _, w := range warnings {
				t.Errorf("warns: %s", w)
			}
			if len(spec.Colors256) > 0 {
				with256++
			} else {
				without256++
			}
		})
	}
	if with256 == 0 || without256 == 0 {
		t.Errorf("%d shipped themes write [colors256] and %d do not; want at least one of each",
			with256, without256)
	}
}

// repoRoot is the directory holding go.mod, found by walking up from this
// package.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}
