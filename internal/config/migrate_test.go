package config

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// changeMap turns a migration summary into field -> new value for easy
// assertions, and fails on a duplicate field (a sign the table has a
// copy-pasted entry).
func changeMap(t *testing.T, changes []MigrationChange) map[string]MigrationChange {
	t.Helper()
	out := make(map[string]MigrationChange, len(changes))
	for _, c := range changes {
		if _, dup := out[c.Field]; dup {
			t.Fatalf("field %q reported twice", c.Field)
		}
		out[c.Field] = c
	}
	return out
}

// TestMigrateReplacesStaleDefaults covers the whole point of the flag: a
// config carrying the bindings an old config.example.toml shipped gets them
// swapped for the current ones.
func TestMigrateReplacesStaleDefaults(t *testing.T) {
	cfg := &Config{Keys: KeyConfig{
		FocusChatList: "ctrl+1",
		FocusChatView: "ctrl+2",
		FocusComposer: "ctrl+3",
		Contacts:      "ctrl+k",
		NextChat:      "ctrl+j",
		PrevChat:      "ctrl+k",
		PageUp:        "ctrl+u",
		PageDown:      "ctrl+d",
	}}
	got := changeMap(t, Migrate(cfg, nil))

	want := map[string]string{
		"keys.focus_chat_list": "f1",
		"keys.focus_chat_view": "f2",
		"keys.focus_composer":  "f3",
		"keys.contacts":        "alt+c",
		"keys.next_chat":       "alt+j",
		"keys.prev_chat":       "alt+k",
	}
	for field, v := range want {
		c, ok := got[field]
		if !ok {
			t.Errorf("%s was not migrated", field)
			continue
		}
		if c.New != v {
			t.Errorf("%s -> %q, want %q", field, c.New, v)
		}
		if c.Old == "" {
			t.Errorf("%s reported no old value", field)
		}
	}

	// And the struct itself was updated, not just the report.
	if cfg.Keys.Contacts != "alt+c" || cfg.Keys.NextChat != "alt+j" {
		t.Errorf("cfg not mutated: contacts=%q next_chat=%q",
			cfg.Keys.Contacts, cfg.Keys.NextChat)
	}

	// page_up/page_down carry retired defaults too, but nothing dispatches
	// them (chatview hardcodes pgup/pgdown), so rewriting them would be
	// churn the user cannot observe. Left exactly as found.
	if cfg.Keys.PageUp != "ctrl+u" || cfg.Keys.PageDown != "ctrl+d" {
		t.Errorf("inert paging fields were rewritten: page_up=%q page_down=%q",
			cfg.Keys.PageUp, cfg.Keys.PageDown)
	}
	for _, field := range []string{"keys.page_up", "keys.page_down"} {
		if c, reported := got[field]; reported {
			t.Errorf("%s was reported as a change: %+v", field, c)
		}
	}
}

// TestMigrateIsCaseInsensitive: a stale default someone retyped in a
// different case is still a stale default.
func TestMigrateStaleMatchIsNormalized(t *testing.T) {
	cfg := &Config{Keys: KeyConfig{Contacts: "  CTRL+K  "}}
	Migrate(cfg, nil)
	if cfg.Keys.Contacts != "alt+c" {
		t.Errorf("contacts = %q, want alt+c", cfg.Keys.Contacts)
	}
}

// TestMigrateLeavesCustomizations covers the other half: anything that is not
// a retired default is a choice, and survives untouched.
func TestMigrateLeavesCustomizations(t *testing.T) {
	cfg := &Config{Keys: KeyConfig{
		Quit:          "ctrl+x",
		FocusChatList: "f9",
		Contacts:      "alt+p",
		NextChat:      "ctrl+n",
		PageUp:        "b",
	}}
	Migrate(cfg, nil)

	for field, want := range map[string]string{
		"quit": "ctrl+x", "focus_chat_list": "f9", "contacts": "alt+p",
		"next_chat": "ctrl+n", "page_up": "b",
	} {
		var got string
		for _, f := range keyFields {
			if f.name == field {
				got = f.get(&cfg.Keys)
			}
		}
		if got != want {
			t.Errorf("%s = %q, want the user's %q", field, got, want)
		}
	}
}

// TestMigrateFillsNewFields covers a config written before these fields
// existed — the reason someone upgrading has no folder or help bindings.
func TestMigrateFillsNewFields(t *testing.T) {
	cfg := &Config{Keys: KeyConfig{Quit: "ctrl+c", Contacts: "alt+c"}}
	cfg.Storage.SessionFile = "/data/tele/session.json"
	got := changeMap(t, Migrate(cfg, nil))

	for field, want := range map[string]string{
		"keys.next_folder":   "alt+l",
		"keys.prev_folder":   "alt+h",
		"keys.contacts_alt":  "f4",
		"keys.global_search": "ctrl+g",
		"keys.help":          "?",
		"ui.compose_editing": ComposeEditingAuto,
	} {
		c, ok := got[field]
		if !ok {
			t.Errorf("%s was not filled in", field)
			continue
		}
		if c.New != want {
			t.Errorf("%s -> %q, want %q", field, c.New, want)
		}
		if c.Old != "" {
			t.Errorf("%s reported an old value %q for an absent field", field, c.Old)
		}
	}

	// state_file becomes the path the client would otherwise derive.
	if c, ok := got["storage.state_file"]; !ok {
		t.Error("storage.state_file was not filled in")
	} else if want := filepath.Join("/data/tele", "state.db"); c.New != want {
		t.Errorf("storage.state_file -> %q, want %q", c.New, want)
	}

	// Fields that were already set are not reported as changes.
	if _, reported := got["keys.quit"]; reported {
		t.Error("keys.quit was rewritten despite already holding the default")
	}
}

// TestMigrateIsIdempotent: running it twice must be a no-op the second time,
// or the summary lies about what changed.
func TestMigrateIsIdempotent(t *testing.T) {
	cfg := &Config{Keys: KeyConfig{Contacts: "ctrl+k"}}
	cfg.Storage.SessionFile = "/data/session.json"
	if len(Migrate(cfg, nil)) == 0 {
		t.Fatal("precondition: first pass changed nothing")
	}
	if changes := Migrate(cfg, nil); len(changes) != 0 {
		t.Errorf("second pass changed %d field(s): %v", len(changes), changes)
	}
}

// TestMigrateCurrentDefaultsAreAStableFixpoint: a config already holding the
// shipped defaults must not be rewritten.
func TestMigrateCurrentDefaultsAreAStableFixpoint(t *testing.T) {
	cfg := defaultConfig()
	changes := Migrate(cfg, nil)
	// Two changes are legitimate even against the shipped defaults:
	// state_file is intentionally empty there (derived at runtime), and
	// parse_markdown is deliberately off for new configs but on for anyone
	// migrating — see UIConfig.ParseMarkdown.
	allowed := map[string]bool{"storage.state_file": true, "ui.parse_markdown": true}
	for _, c := range changes {
		if !allowed[c.Field] {
			t.Errorf("default config was rewritten: %s", c)
		}
	}
}

// TestMigrateStaleValueThatIsAlsoCurrent guards a subtle case: prev_chat's
// stale value (ctrl+k) is a different field's stale value too. Each field
// must consult only its own list.
func TestMigratePerFieldStaleLists(t *testing.T) {
	// ctrl+j is stale for next_chat but a perfectly good custom quit key.
	cfg := &Config{Keys: KeyConfig{Quit: "ctrl+j", NextChat: "ctrl+j"}}
	Migrate(cfg, nil)
	if cfg.Keys.Quit != "ctrl+j" {
		t.Errorf("quit = %q, want the user's ctrl+j", cfg.Keys.Quit)
	}
	if cfg.Keys.NextChat != "alt+j" {
		t.Errorf("next_chat = %q, want alt+j", cfg.Keys.NextChat)
	}
}

func TestMigrateNilConfig(t *testing.T) {
	if changes := Migrate(nil, nil); changes != nil {
		t.Errorf("Migrate(nil, nil) = %v, want nil", changes)
	}
}

// TestMigrationChangeString covers the summary rendering, including the
// absent-field case the user sees most often.
func TestMigrationChangeString(t *testing.T) {
	got := MigrationChange{Field: "keys.help", Absent: true, New: "?"}.String()
	if want := "(absent)"; !strings.Contains(got, want) || !strings.Contains(got, "keys.help") || !strings.Contains(got, "?") {
		t.Errorf("String() = %q, want it to mention %q", got, want)
	}
	got = MigrationChange{Field: "keys.contacts", Old: "ctrl+k", New: "alt+c"}.String()
	if !strings.Contains(got, "ctrl+k") || !strings.Contains(got, "alt+c") {
		t.Errorf("String() = %q, want both values", got)
	}
}

// TestMigrateEndToEnd exercises the whole file path the -migrate-config flag
// drives: read a real config off disk, back it up, migrate, write it back,
// and reload it. It is the check that the pieces compose — the pure-function
// tests above say nothing about whether the result round-trips through TOML.
func TestMigrateEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// A config as an early adopter would have it: the retired defaults from
	// an old config.example.toml, one genuine customization, and none of the
	// fields added since.
	original := `# my hand-written config, with comments
[telegram]
api_id = 12345
api_hash = "deadbeef"

[storage]
session_file = "/data/tele/session.json"

[keys]
quit = "ctrl+c"
focus_chat_list = "ctrl+1"   # the old default
focus_chat_view = "ctrl+2"
focus_composer = "ctrl+3"
contacts = "ctrl+k"
next_chat = "ctrl+j"
prev_chat = "ctrl+k"
page_up = "ctrl+u"
page_down = "ctrl+d"
search = "?"
`
	// Deliberately world-readable, the mode a hand-created config often has.
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELETUI_CONFIG", path)

	if got := ConfigPath(); got != path {
		t.Fatalf("ConfigPath() = %q, want %q", got, path)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// LoadRawFile, not the defaults-applied cfg: Load fills absent fields
	// from defaultConfig, which would make every field added since this
	// config was written look like it was already there.
	raw, err := LoadRawFile(path)
	if err != nil {
		t.Fatalf("LoadRawFile: %v", err)
	}

	changes := Migrate(cfg, raw)
	if len(changes) == 0 {
		t.Fatal("nothing to migrate in a config full of retired defaults")
	}

	// The summary must name every key the file gained, not just the ones
	// whose value changed — otherwise it under-reports what was written.
	reported := changeMap(t, changes)
	for _, field := range []string{
		"keys.next_folder", "keys.prev_folder", "keys.contacts_alt",
		"keys.global_search", "keys.help", "ui.compose_editing",
		"storage.state_file",
	} {
		c, ok := reported[field]
		if !ok {
			t.Errorf("%s was written but not reported as a change", field)
			continue
		}
		if c.Old != "" {
			t.Errorf("%s reported old value %q for a key the file never had", field, c.Old)
		}
	}
	// And the retired defaults are reported with what they replaced.
	if c, ok := reported["keys.contacts"]; !ok || c.Old != "ctrl+k" || c.New != "alt+c" {
		t.Errorf("keys.contacts reported as %+v, want ctrl+k -> alt+c", c)
	}
	// A key the user chose is absent from the summary entirely.
	if c, ok := reported["keys.search"]; ok {
		t.Errorf("the user's search binding was rewritten: %+v", c)
	}
	SortChanges(changes)
	if !sort.SliceIsSorted(changes, func(i, j int) bool {
		return changes[i].Field < changes[j].Field
	}) {
		t.Error("SortChanges did not sort the summary")
	}

	backup, err := BackupFile(path)
	if err != nil {
		t.Fatalf("BackupFile: %v", err)
	}
	if err := SaveTo(path, cfg); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	// The backup is byte-identical, comments and all — the migrated file
	// cannot keep them, so this is the only copy of what the user wrote.
	kept, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if string(kept) != original {
		t.Error("the backup is not a byte-for-byte copy of the original")
	}
	if !strings.Contains(string(kept), "# my hand-written config") {
		t.Error("the backup lost the user's comments")
	}

	// The rewritten config holds an api_hash and a phone number, so it must
	// come out 0600 even though it went in 0644.
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("migrated config mode = %v, want 0600", got)
	}
	if info, err := os.Stat(backup); err != nil {
		t.Fatal(err)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("backup mode = %v, want 0600", got)
	}

	// Reload from disk: the migration has to survive the TOML round trip.
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reloading the migrated config: %v", err)
	}

	for field, want := range map[string]string{
		"focus_chat_list": "f1",
		"focus_chat_view": "f2",
		"focus_composer":  "f3",
		"contacts":        "alt+c",
		"next_chat":       "alt+j",
		"prev_chat":       "alt+k",
		// Inert, so left exactly as the file had them.
		"page_up":   "ctrl+u",
		"page_down": "ctrl+d",
		// Fields introduced after this config was written.
		"next_folder":   "alt+l",
		"prev_folder":   "alt+h",
		"contacts_alt":  "f4",
		"global_search": "ctrl+g",
		"help":          "?",
		// Untouched: the user chose these.
		"quit":   "ctrl+c",
		"search": "?",
	} {
		var got string
		for _, f := range keyFields {
			if f.name == field {
				got = f.get(&reloaded.Keys)
			}
		}
		if got != want {
			t.Errorf("after migration, keys.%s = %q, want %q", field, got, want)
		}
	}

	if reloaded.UI.ComposeEditing != ComposeEditingAuto {
		t.Errorf("ui.compose_editing = %q, want %q", reloaded.UI.ComposeEditing, ComposeEditingAuto)
	}
	if want := filepath.Join("/data/tele", "state.db"); reloaded.Storage.StateFile != want {
		t.Errorf("storage.state_file = %q, want %q", reloaded.Storage.StateFile, want)
	}
	// Non-key settings survive untouched.
	if reloaded.Telegram.APIID != 12345 || reloaded.Telegram.APIHash != "deadbeef" {
		t.Errorf("credentials were disturbed: id=%d hash=%q",
			reloaded.Telegram.APIID, reloaded.Telegram.APIHash)
	}

	// Running it again is a no-op, so a user can re-run the flag safely.
	rawAgain, err := LoadRawFile(path)
	if err != nil {
		t.Fatalf("LoadRawFile on the migrated file: %v", err)
	}
	if again := Migrate(reloaded, rawAgain); len(again) != 0 {
		t.Errorf("a second migration changed %d field(s): %v", len(again), again)
	}
}

// TestBackupFileMissingSource: -migrate-config checks for the file first, but
// BackupFile must still fail rather than write an empty backup.
func TestBackupFileMissingSource(t *testing.T) {
	if _, err := BackupFile(filepath.Join(t.TempDir(), "absent.toml")); err == nil {
		t.Error("BackupFile succeeded on a missing file")
	}
}

// TestDetectKeyCollisions covers the warning the migration owes the user: it
// can introduce a default that clashes with a binding they chose, and it
// refuses to rewrite their choice, so the clash has to be named.
func TestDetectKeyCollisions(t *testing.T) {
	t.Run("the collision this migration creates", func(t *testing.T) {
		// A user who bound search to "?" gets help = "?" filled in.
		cfg := defaultConfig()
		cfg.Keys.Search = "?"
		got := DetectKeyCollisions(cfg)
		if len(got) != 1 {
			t.Fatalf("got %d collisions, want 1: %v", len(got), got)
		}
		for _, want := range []string{`"?"`, "help", "search"} {
			if !strings.Contains(got[0], want) {
				t.Errorf("collision %q does not mention %s", got[0], want)
			}
		}
	})

	t.Run("clean defaults have none", func(t *testing.T) {
		if got := DetectKeyCollisions(defaultConfig()); len(got) != 0 {
			t.Errorf("the shipped defaults collide: %v", got)
		}
	})

	t.Run("comparison is normalized", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.Keys.Contacts = "ALT+C"
		cfg.Keys.NextFolder = "Alt+C"
		got := DetectKeyCollisions(cfg)
		if len(got) != 1 || !strings.Contains(got[0], "alt+c") {
			t.Errorf("got %v, want one normalized alt+c collision", got)
		}
	})

	t.Run("inert fields are ignored", func(t *testing.T) {
		// reply/forward are not dispatched from app.go, so sharing a value
		// with each other is meaningless and must not be reported.
		cfg := defaultConfig()
		cfg.Keys.Reply = "x"
		cfg.Keys.Forward = "x"
		if got := DetectKeyCollisions(cfg); len(got) != 0 {
			t.Errorf("unwired fields were reported as colliding: %v", got)
		}
	})

	t.Run("nil and empty", func(t *testing.T) {
		if got := DetectKeyCollisions(nil); got != nil {
			t.Errorf("DetectKeyCollisions(nil) = %v, want nil", got)
		}
		// Empty bindings are absent, not a collision between themselves.
		if got := DetectKeyCollisions(&Config{}); len(got) != 0 {
			t.Errorf("empty config reported collisions: %v", got)
		}
	})
}

// TestSaveToTightensPermissions covers the review's permission finding: the
// config holds an api_hash and a phone number, and os.WriteFile's mode
// argument applies only when it creates the file — so rewriting an existing
// world-readable config left it world-readable.
func TestSaveToTightensPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[keys]\nquit = \"ctrl+c\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o644 {
		t.Fatalf("precondition: mode is %v, want 0644", info.Mode().Perm())
	}

	if err := SaveTo(path, defaultConfig()); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode after rewrite = %v, want 0600", got)
	}

	// No temp files left behind by the atomic rename.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("left a temp file behind: %s", e.Name())
		}
	}
}

func TestBackupFilePermissionsAndCollision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := BackupFile(path)
	if err != nil {
		t.Fatalf("BackupFile: %v", err)
	}
	// Compared against the resolved path: the backup lands beside the real
	// file, and on macOS the temp root is itself a symlink (/var ->
	// /private/var), so the two spellings differ here without any symlink
	// of the test's own.
	if want := filepath.Join(evalDir(t, dir), "config.toml.bak"); first != want {
		t.Errorf("first backup = %q, want %q", first, want)
	}
	// The backup holds the same secrets as the config, so it gets the same
	// restrictive mode regardless of the source file's.
	info, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("backup mode = %v, want 0600", got)
	}

	// A second migration must not overwrite the real original with a copy
	// of the already-migrated file.
	if err := os.WriteFile(path, []byte("migrated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := BackupFile(path)
	if err != nil {
		t.Fatalf("second BackupFile: %v", err)
	}
	if second == first {
		t.Fatal("the second backup overwrote the first")
	}
	kept, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != "original\n" {
		t.Errorf("the first backup now holds %q, want the original", string(kept))
	}
}

// TestMigrateEnablesMarkdownForUpgraders covers the coordinator's decision:
// off by default for new configs, on for anyone migrating, and reported
// either way so it is never a silent rewrite of what the user types.
func TestMigrateEnablesMarkdownForUpgraders(t *testing.T) {
	if defaultConfig().UI.ParseMarkdown {
		t.Error("parse_markdown should default to false for new configs")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\ntheme = \"dark\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := LoadRawFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	got := changeMap(t, Migrate(cfg, raw))

	c, ok := got["ui.parse_markdown"]
	if !ok {
		t.Fatal("enabling markdown was not reported")
	}
	if c.New != "true" || !c.Absent {
		t.Errorf("reported %+v, want an absent -> true change", c)
	}
	if !cfg.UI.ParseMarkdown {
		t.Error("cfg.UI.ParseMarkdown was not enabled")
	}

	// A user who explicitly turned it off keeps it off.
	if err := os.WriteFile(path, []byte("[ui]\nparse_markdown = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err = LoadRawFile(path)
	if err != nil {
		t.Fatal(err)
	}
	off := &Config{}
	for _, c := range Migrate(off, raw) {
		if c.Field == "ui.parse_markdown" {
			t.Errorf("an explicit parse_markdown = false was overridden: %s", c)
		}
	}
	if off.UI.ParseMarkdown {
		t.Error("an explicit parse_markdown = false was flipped on")
	}
}

// TestMigrateDoesNotAbsolutizePaths: Load expands "~/" so the running app
// gets a usable path, but persisting that expansion would rewrite the user's
// config to hardcode this machine's home directory.
func TestMigrateDoesNotAbsolutizePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "[storage]\nsession_file = \"~/tele/session.json\"\nfiles_dir = \"~/tele/files\"\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELETUI_CONFIG", path)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(cfg.Storage.SessionFile, "~") {
		t.Fatal("precondition: Load did not expand the path")
	}
	raw, err := LoadRawFile(path)
	if err != nil {
		t.Fatal(err)
	}
	Migrate(cfg, raw)
	if err := SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"~/tele/session.json", "~/tele/files"} {
		if !strings.Contains(string(written), want) {
			t.Errorf("the rewritten config lost the unexpanded %q:\n%s", want, written)
		}
	}
	if home, _ := os.UserHomeDir(); home != "" && strings.Contains(string(written), home) {
		t.Errorf("the rewritten config hardcodes the home directory:\n%s", written)
	}
}

// TestMigrateFillsPathsPortably: a path the file lacked is written in the
// "~/" form the defaults are declared in, not expanded against this machine.
func TestMigrateFillsPathsPortably(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[telegram]\napi_id = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELETUI_CONFIG", path)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := LoadRawFile(path)
	if err != nil {
		t.Fatal(err)
	}
	Migrate(cfg, raw)
	if err := SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{DefaultSessionFile, DefaultFilesDir} {
		if !strings.Contains(string(written), want) {
			t.Errorf("the config does not carry the portable default %q:\n%s", want, written)
		}
	}
	if home, _ := os.UserHomeDir(); home != "" && strings.Contains(string(written), home) {
		t.Errorf("the config hardcodes the home directory:\n%s", written)
	}
	// Still usable: Load expands them at runtime.
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(loaded.Storage.SessionFile, "~") {
		t.Error("Load did not expand the portable path")
	}
}

// TestMigrateStateFileRespectsAnEmptySessionFile: an explicit
// session_file = "" opts out of on-disk state, and inventing a state path
// next to a session file that does not exist would override that.
func TestMigrateStateFileRespectsAnEmptySessionFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[storage]\nsession_file = \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := LoadRawFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	for _, c := range Migrate(cfg, raw) {
		if c.Field == "storage.state_file" {
			t.Errorf("state_file was invented despite an empty session_file: %s", c)
		}
	}
	if cfg.Storage.StateFile != "" {
		t.Errorf("state_file = %q, want empty", cfg.Storage.StateFile)
	}
}

// TestRawFileReportsStructure covers what the summary owes the user about a
// whole-schema rewrite: which tables it adds, and what it silently drops.
func TestRawFileReportsStructure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := "[telegram]\napi_id = 1\n\n[keys]\nquit = \"ctrl+c\"\nfly_to_moon = \"ctrl+m\"\n\n[nonsense]\nfoo = 1\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := LoadRawFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !raw.Has("keys", "quit") {
		t.Error("Has missed a key the file contained")
	}
	if raw.Has("keys", "help") {
		t.Error("Has claimed a key the file did not contain")
	}

	unknown := raw.Unknown()
	for _, want := range []string{"keys.fly_to_moon", "nonsense.foo"} {
		if !slices.Contains(unknown, want) {
			t.Errorf("Unknown() = %v, want it to include %q", unknown, want)
		}
	}
	if slices.Contains(unknown, "keys.quit") {
		t.Error("Unknown() flagged a recognized key")
	}

	missing := raw.MissingSections()
	for _, want := range []string{"storage", "ui", "media", "notifications"} {
		if !slices.Contains(missing, want) {
			t.Errorf("MissingSections() = %v, want it to include %q", missing, want)
		}
	}
	for _, present := range []string{"telegram", "keys"} {
		if slices.Contains(missing, present) {
			t.Errorf("MissingSections() = %v, but %q was in the file", missing, present)
		}
	}
}

// TestMigratePresentButEmptyIsNotAbsent: `contacts = ""` is a binding that
// can never match. It gets repaired, and the summary says it was empty
// rather than missing, because they are different mistakes.
func TestMigratePresentButEmptyIsNotAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[keys]\ncontacts = \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := LoadRawFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	got := changeMap(t, Migrate(cfg, raw))

	c, ok := got["keys.contacts"]
	if !ok {
		t.Fatal("an empty binding was not repaired")
	}
	if c.Absent {
		t.Error("an empty binding was reported as absent")
	}
	if rendered := c.String(); !strings.Contains(rendered, `("")`) {
		t.Errorf("String() = %q, want it to show the empty value", rendered)
	}
	// A key the file genuinely lacked still reads as absent.
	if help, ok := got["keys.help"]; !ok || !help.Absent {
		t.Errorf("keys.help = %+v, want an absent change", help)
	}
}

// TestDetectKeyCollisionsIgnoresAliasPairs: contacts/contacts_alt and
// search/global_search are matched at one dispatch site, so pointing both at
// one key is redundant, not ambiguous.
func TestDetectKeyCollisionsIgnoresAliasPairs(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*Config)
	}{
		{"contacts pair", func(c *Config) { c.Keys.ContactsAlt = c.Keys.Contacts }},
		{"search pair", func(c *Config) { c.Keys.GlobalSearch = c.Keys.Search }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultConfig()
			tc.set(cfg)
			if got := DetectKeyCollisions(cfg); len(got) != 0 {
				t.Errorf("an alias pair was reported as a collision: %v", got)
			}
		})
	}

	// A genuine three-way clash is still reported, even when two of the
	// three are an alias pair.
	cfg := defaultConfig()
	cfg.Keys.Contacts = "f7"
	cfg.Keys.ContactsAlt = "f7"
	cfg.Keys.Help = "f7"
	got := DetectKeyCollisions(cfg)
	if len(got) != 1 || !strings.Contains(got[0], "help") {
		t.Errorf("got %v, want the three-way f7 clash reported", got)
	}
}

// TestMigrateThroughASymlink covers the regression the atomic-rename fix
// introduced: rename replaces a symlink rather than writing through it. With
// the usual dotfiles layout that destroyed the link, silently demoted the
// dotfiles copy from source of truth, and left the real file at its original
// mode — so for symlink users the 0600 tightening never happened.
func TestMigrateThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	dotfiles := filepath.Join(dir, "dotfiles")
	confDir := filepath.Join(dir, "config", "tele-tui")
	for _, d := range []string{dotfiles, confDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	real := filepath.Join(dotfiles, "config.toml")
	link := filepath.Join(confDir, "config.toml")
	original := "# dotfiles copy\n[telegram]\napi_id = 42\napi_hash = \"deadbeef\"\n\n[keys]\ncontacts = \"ctrl+k\"\n"
	// 0644, the mode a file checked into a dotfiles repo usually has.
	if err := os.WriteFile(real, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("TELETUI_CONFIG", link)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := LoadRawFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if changes := Migrate(cfg, raw); len(changes) == 0 {
		t.Fatal("precondition: nothing to migrate")
	}
	backup, err := BackupFile(link)
	if err != nil {
		t.Fatalf("BackupFile: %v", err)
	}
	if err := SaveTo(link, cfg); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	// The symlink survives, still pointing where it did.
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("the symlink is gone: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a regular file")
	}
	if dest, err := os.Readlink(link); err != nil || dest != real {
		t.Errorf("symlink points at %q (err %v), want %q", dest, err, real)
	}

	// The real file in the dotfiles directory got the migrated content...
	written, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "alt+c") {
		t.Errorf("the target did not receive the migration:\n%s", written)
	}
	if strings.Contains(string(written), "ctrl+k") {
		t.Errorf("the target still holds the retired binding:\n%s", written)
	}
	// ...at 0600, which is the whole point: it holds an api_hash.
	if st, err := os.Stat(real); err != nil {
		t.Fatal(err)
	} else if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("target mode = %v, want 0600", got)
	}

	// The backup sits beside the target, not beside the link, and is 0600.
	if got, want := filepath.Dir(backup), evalDir(t, dotfiles); got != want {
		t.Errorf("backup is in %q, want it beside the target in %q", got, want)
	}
	if st, err := os.Stat(backup); err != nil {
		t.Fatalf("backup missing: %v", err)
	} else if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("backup mode = %v, want 0600", got)
	}
	kept, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != original {
		t.Error("the backup is not a byte-for-byte copy of the original")
	}

	// No temp files left on either side of the link.
	for _, d := range []string{dotfiles, confDir} {
		entries, err := os.ReadDir(d)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp") {
				t.Errorf("left a temp file behind in %s: %s", d, e.Name())
			}
		}
	}

	// And the app reads the migrated config back through the link.
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Keys.Contacts != "alt+c" {
		t.Errorf("reloaded contacts = %q, want alt+c", reloaded.Keys.Contacts)
	}
}

// evalDir resolves a directory the way the write path does, so path
// comparisons survive platforms where the temp root is itself a symlink
// (macOS /var -> /private/var).
func evalDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return resolved
}

// TestSaveTargetsTheConfigTheAppReads covers the first-run wizard under
// $TELETUI_CONFIG: Save used to write the default location unconditionally,
// so the credentials it collected landed in a file Load never looks at and
// the next launch asked for them again.
func TestSaveTargetsTheConfigTheAppReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "elsewhere", "config.toml")
	t.Setenv("TELETUI_CONFIG", path)
	// Somewhere the default resolution would land if Save ignored the env.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))

	cfg := defaultConfig()
	cfg.Telegram.APIID = 4242
	cfg.Telegram.APIHash = "deadbeef"
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Save did not write to $TELETUI_CONFIG: %v", err)
	}
	// Nothing was written to the default location.
	if _, err := os.Stat(filepath.Join(dir, "xdg", "tele-tui", "config.toml")); err == nil {
		t.Error("Save also wrote to the default location")
	}

	// The credentials survive the next launch, which is the point.
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Telegram.APIID != 4242 || reloaded.Telegram.APIHash != "deadbeef" {
		t.Errorf("reloaded credentials = %d/%q, want 4242/deadbeef",
			reloaded.Telegram.APIID, reloaded.Telegram.APIHash)
	}

	// And it is written at 0600 like every other config write.
	if st, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %v, want 0600", got)
	}
}

// TestSaveFallsBackToTheDefaultPath: with no env override, Save still writes
// where the app looks by default.
func TestSaveFallsBackToTheDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TELETUI_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", dir)

	if err := Save(defaultConfig()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tele-tui", "config.toml")); err != nil {
		t.Errorf("Save did not write to the default location: %v", err)
	}
}
