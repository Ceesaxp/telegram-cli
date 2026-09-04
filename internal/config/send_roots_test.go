package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// homeConfig returns a Config whose paths sit under a temp HOME, so the
// "~/" defaults expand somewhere the test owns.
func homeConfig(t *testing.T) *Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return &Config{Storage: StorageConfig{FilesDir: expandPath(DefaultFilesDir)}}
}

func TestSendRootsDefaultsToCacheAndOutbox(t *testing.T) {
	cfg := homeConfig(t)

	got := cfg.SendRoots()
	want := []string{expandPath(DefaultFilesDir), expandPath(DefaultOutboxDir)}
	if !slices.Equal(got, want) {
		t.Fatalf("SendRoots() = %q, want %q", got, want)
	}
}

// The working directory is what issue #48 removed. Nothing in the default
// set may be derived from where the process happened to start.
func TestSendRootsExcludesWorkingDirectory(t *testing.T) {
	cfg := homeConfig(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(cfg.SendRoots(), cwd) {
		t.Fatalf("SendRoots() = %q, must not contain the working directory %q", cfg.SendRoots(), cwd)
	}
}

func TestSendRootsUsesConfiguredDirs(t *testing.T) {
	cfg := homeConfig(t)
	outbox := t.TempDir()
	cfg.Storage.SendDirs = []string{outbox}

	got := cfg.SendRoots()
	want := []string{cfg.Storage.FilesDir, outbox}
	if !slices.Equal(got, want) {
		t.Fatalf("SendRoots() = %q, want %q", got, want)
	}
	// A configured list replaces the outbox default rather than adding to
	// it: the operator chose the set.
	if slices.Contains(got, expandPath(DefaultOutboxDir)) {
		t.Errorf("SendRoots() = %q, still carries the default outbox", got)
	}
}

// The media cache is not configurable and not optional: download_media
// hands out paths inside it, so a caller must be able to send one back.
func TestSendRootsAlwaysIncludesFilesDir(t *testing.T) {
	cfg := homeConfig(t)
	cfg.Storage.SendDirs = []string{t.TempDir()}

	if got := cfg.SendRoots(); !slices.Contains(got, cfg.Storage.FilesDir) {
		t.Fatalf("SendRoots() = %q, want it to contain files_dir %q", got, cfg.Storage.FilesDir)
	}
}

// `send_dirs = [""]` is a mistake, not an opt-out. Treating it as one
// would leave the cache as the only source, which reads as a broken
// send_file rather than as a policy.
func TestSendRootsIgnoresBlankEntries(t *testing.T) {
	cfg := homeConfig(t)
	cfg.Storage.SendDirs = []string{"", "   "}

	got := cfg.SendRoots()
	want := []string{cfg.Storage.FilesDir, expandPath(DefaultOutboxDir)}
	if !slices.Equal(got, want) {
		t.Fatalf("SendRoots() = %q, want %q", got, want)
	}
}

func TestSendRootsExpandsTildes(t *testing.T) {
	cfg := homeConfig(t)
	cfg.Storage.SendDirs = []string{"~/outbox"}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "outbox")
	if got := cfg.SendRoots(); !slices.Contains(got, want) {
		t.Fatalf("SendRoots() = %q, want it to contain %q", got, want)
	}
}

func TestPrepareSendRootsCreatesTheOutbox(t *testing.T) {
	cfg := homeConfig(t)

	roots, missing, err := cfg.PrepareSendRoots()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outbox := expandPath(DefaultOutboxDir)
	info, statErr := os.Stat(outbox)
	if statErr != nil {
		t.Fatalf("outbox %q was not created: %v", outbox, statErr)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("outbox mode = %o, want 700", perm)
	}
	if !slices.Contains(roots, outbox) {
		t.Errorf("roots = %q, want it to contain the outbox", roots)
	}
	// files_dir is reported missing here because only newClient creates
	// it; the outbox must not be.
	if slices.Contains(missing, outbox) {
		t.Errorf("missing = %q, must not contain the outbox it just created", missing)
	}
}

// An operator's own directory is never created. Making a typo real is
// worse than reporting it, and ResolveAllowedSendPath skips a root it
// cannot resolve — silently, which is why the warning has to come from
// here.
func TestPrepareSendRootsReportsMissingOperatorDirs(t *testing.T) {
	cfg := homeConfig(t)
	if err := os.MkdirAll(cfg.Storage.FilesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	typo := filepath.Join(t.TempDir(), "no-such-dir")
	cfg.Storage.SendDirs = []string{typo}

	_, missing, err := cfg.PrepareSendRoots()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(missing, []string{typo}) {
		t.Fatalf("missing = %q, want %q", missing, []string{typo})
	}
	if _, statErr := os.Stat(typo); statErr == nil {
		t.Errorf("PrepareSendRoots created the operator's missing directory %q", typo)
	}
}
