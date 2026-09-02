package version

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestTheVersionLineNamesTheProgramAndThePlatform.
//
// Half the bug reports that matter are about a terminal on a platform, and
// asking afterwards costs a round trip with somebody who has already moved
// on.
func TestTheVersionLineNamesTheProgramAndThePlatform(t *testing.T) {
	got := String("tele-tui")

	for _, want := range []string{
		"tele-tui", Version, runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing %q", got, want)
		}
	}
}

// TestAStampedVersionWins over whatever the toolchain recorded: the release
// stamps a tag, and a tag is what people quote at each other.
func TestAStampedVersionWins(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	Version = "v0.4.2"
	if got := String("tele-tui"); !strings.HasPrefix(got, "tele-tui v0.4.2 ") {
		t.Fatalf("got %q, want it to lead with the stamped version", got)
	}
}

// TestAStampedCommitIsShortened to the length people actually quote.
func TestAStampedCommitIsShortened(t *testing.T) {
	old := Commit
	t.Cleanup(func() { Commit = old })

	Commit = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	if got := String("tele-tui"); !strings.Contains(got, "(a1b2c3d,") {
		t.Fatalf("got %q, want the short revision", got)
	}
	if strings.Contains(String("tele-tui"), Commit) {
		t.Fatal("the full revision was printed")
	}
}

// TestTheUnstampedBuildSaysDevRatherThanNothing. An empty version reads as a
// broken build; "dev" reads as what it is.
func TestTheUnstampedBuildSaysDevRatherThanNothing(t *testing.T) {
	// The package default, which is what a plain `go build` gets.
	if Version == "" {
		t.Fatal("the default version is empty")
	}
}

// TestAskedRecognisesTheWaysPeopleAskAndNothingElse.
//
// Checked before flag parsing in every binary, because two of the three take
// a subcommand as their first argument and would read "version" as one.
func TestAskedRecognisesTheWaysPeopleAskAndNothingElse(t *testing.T) {
	for _, args := range [][]string{
		{"version"}, {"-version"}, {"--version"},
	} {
		if !Asked(args) {
			t.Errorf("%v was not recognised", args)
		}
	}

	for _, args := range [][]string{
		nil,
		{},
		{"serve"},
		{"-v"},
		{"version", "extra"},
		{"-addr", ":8080"},
		{"--versions"},
	} {
		if Asked(args) {
			t.Errorf("%v was taken as a version request", args)
		}
	}
}

// TestEveryBinaryAnswers. Three mains, and a build where two agree and the
// third says something else is a build nobody can report a bug against.
//
// On the source rather than by running them: running three binaries means
// building three binaries, and what this is checking is that nobody added a
// fourth command without the two lines.
func TestEveryBinaryAnswers(t *testing.T) {
	root := repoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		t.Fatal(err)
	}

	found := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		found++

		main := filepath.Join(root, "cmd", entry.Name(), "main.go")
		raw, err := os.ReadFile(main)
		if err != nil {
			t.Errorf("cmd/%s has no main.go", entry.Name())
			continue
		}
		if !strings.Contains(string(raw), "version.Asked(os.Args[1:])") {
			t.Errorf("cmd/%s does not answer -version", entry.Name())
		}
	}
	if found < 3 {
		t.Fatalf("found %d commands, want at least the three that ship", found)
	}
}

// TestTheArchiveCarriesTheConfigReference.
//
// config.example.toml is the reference for every setting, and the release
// archive did not carry it for as long as the workflow packaged its own
// file list by hand. The Makefile is the single list now; this checks the
// list names files that are actually there, because a missing one is not
// found until a tag has already gone out.
func TestTheArchiveCarriesTheConfigReference(t *testing.T) {
	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}

	m := regexp.MustCompile(`(?m)^DIST_DOCS\s*=\s*(.*)$`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("the Makefile no longer says what ships beside the binaries")
	}

	docs := strings.Fields(m[1])
	if len(docs) == 0 {
		t.Fatal("DIST_DOCS is empty")
	}
	for _, doc := range docs {
		if _, err := os.Stat(filepath.Join(root, doc)); err != nil {
			t.Errorf("DIST_DOCS names %s, which is not there: %v", doc, err)
		}
	}

	for _, want := range []string{"README.md", "config.example.toml"} {
		if !strings.Contains(m[1], want) {
			t.Errorf("the archive does not carry %s", want)
		}
	}
}

// TestTheReleaseWorkflowPackagesThroughTheMakefile, so the archive people
// download is the one a person can build and open locally. Two packaging
// paths are two things to keep in step, and the one nobody runs by hand is
// the one that rots.
func TestTheReleaseWorkflowPackagesThroughTheMakefile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)

	if !strings.Contains(workflow, "make dist") {
		t.Error("the release workflow does not package through the Makefile")
	}
	if strings.Contains(workflow, "go build -trimpath") {
		t.Error("the release workflow builds its own binaries again")
	}
}

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
