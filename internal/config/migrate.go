package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// MigrationChange records one field the migration rewrote.
type MigrationChange struct {
	// Field is the TOML key, qualified by its table (e.g. "keys.contacts").
	Field string
	// Old is the value the config carried, empty when Absent.
	Old string
	// Absent distinguishes a key the file never had from one it set to the
	// empty string. Both read as "" in Old, but they are different
	// mistakes: the first is an old config missing a field added since, the
	// second is someone who wrote `contacts = ""` and deserves to see that
	// their (broken) setting was replaced rather than merely filled in.
	Absent bool
	// New is the value written in its place.
	New string
}

// String renders a change for the migration summary.
func (c MigrationChange) String() string {
	old := c.Old
	switch {
	case c.Absent:
		old = "(absent)"
	case old == "":
		old = `("")`
	}
	return fmt.Sprintf("%-22s %s -> %s", c.Field, old, c.New)
}

// staleKeyDefaults lists values that were once shipped as defaults (in
// config.example.toml and in older builds) and have since been rebound.
//
// A field holding one of these is treated as never having been chosen: the
// user got it from an example file, not from a decision, and leaving it
// behind is how someone ends up with ctrl+k opening contacts while the help
// overlay and every document say alt+c. A value that matches nothing here is
// a real customization and is never touched.
//
// Keys are TOML field names; values are the stale defaults for that field.
var staleKeyDefaults = map[string][]string{
	// page_up/page_down are deliberately absent: they are inert (chatview
	// hardcodes pgup/pgdown), so rewriting them changes nothing a user can
	// observe while adding noise to the summary. See migratableKeyFields.
	"focus_chat_list": {"ctrl+1"},
	"focus_chat_view": {"ctrl+2"},
	"focus_composer":  {"ctrl+3"},
	// ctrl+k was the old contacts binding. It now belongs to the composer's
	// readline kill-to-end-of-line, so leaving it bound here breaks typing.
	"contacts": {"ctrl+k"},
	// ctrl+j/ctrl+k moved to alt+j/alt+k for the same reason — ctrl+j is
	// the composer's newline.
	"next_chat": {"ctrl+j"},
	"prev_chat": {"ctrl+k"},
}

// migratableKeyFields lists the [keys] fields the migration will rewrite:
// exactly the ones internal/app dispatches on. The rest (reply, forward,
// scroll_up, page_up, …) are parsed and preserved but never consulted, so
// changing them cannot change behavior — rewriting them would only add noise
// to the summary and churn to the file.
var migratableKeyFields = map[string]bool{
	"quit": true, "focus_chat_list": true, "focus_chat_view": true,
	"focus_composer": true, "search": true, "global_search": true,
	"contacts": true, "contacts_alt": true, "help": true,
	"next_chat": true, "prev_chat": true,
	"next_folder": true, "prev_folder": true,
}

// keyFields maps each TOML key name to accessors on a KeyConfig, so the
// migration can walk the struct generically instead of repeating every
// field three times.
var keyFields = []struct {
	name string
	get  func(*KeyConfig) string
	set  func(*KeyConfig, string)
}{
	{"quit", func(k *KeyConfig) string { return k.Quit }, func(k *KeyConfig, v string) { k.Quit = v }},
	{"focus_chat_list", func(k *KeyConfig) string { return k.FocusChatList }, func(k *KeyConfig, v string) { k.FocusChatList = v }},
	{"focus_chat_view", func(k *KeyConfig) string { return k.FocusChatView }, func(k *KeyConfig, v string) { k.FocusChatView = v }},
	{"focus_composer", func(k *KeyConfig) string { return k.FocusComposer }, func(k *KeyConfig, v string) { k.FocusComposer = v }},
	{"search", func(k *KeyConfig) string { return k.Search }, func(k *KeyConfig, v string) { k.Search = v }},
	{"global_search", func(k *KeyConfig) string { return k.GlobalSearch }, func(k *KeyConfig, v string) { k.GlobalSearch = v }},
	{"contacts", func(k *KeyConfig) string { return k.Contacts }, func(k *KeyConfig, v string) { k.Contacts = v }},
	{"contacts_alt", func(k *KeyConfig) string { return k.ContactsAlt }, func(k *KeyConfig, v string) { k.ContactsAlt = v }},
	{"help", func(k *KeyConfig) string { return k.Help }, func(k *KeyConfig, v string) { k.Help = v }},
	{"next_chat", func(k *KeyConfig) string { return k.NextChat }, func(k *KeyConfig, v string) { k.NextChat = v }},
	{"prev_chat", func(k *KeyConfig) string { return k.PrevChat }, func(k *KeyConfig, v string) { k.PrevChat = v }},
	{"next_folder", func(k *KeyConfig) string { return k.NextFolder }, func(k *KeyConfig, v string) { k.NextFolder = v }},
	{"prev_folder", func(k *KeyConfig) string { return k.PrevFolder }, func(k *KeyConfig, v string) { k.PrevFolder = v }},
	{"reply", func(k *KeyConfig) string { return k.Reply }, func(k *KeyConfig, v string) { k.Reply = v }},
	{"edit_message", func(k *KeyConfig) string { return k.EditMessage }, func(k *KeyConfig, v string) { k.EditMessage = v }},
	{"delete_message", func(k *KeyConfig) string { return k.DeleteMessage }, func(k *KeyConfig, v string) { k.DeleteMessage = v }},
	{"forward", func(k *KeyConfig) string { return k.Forward }, func(k *KeyConfig, v string) { k.Forward = v }},
	{"scroll_up", func(k *KeyConfig) string { return k.ScrollUp }, func(k *KeyConfig, v string) { k.ScrollUp = v }},
	{"scroll_down", func(k *KeyConfig) string { return k.ScrollDown }, func(k *KeyConfig, v string) { k.ScrollDown = v }},
	{"page_up", func(k *KeyConfig) string { return k.PageUp }, func(k *KeyConfig, v string) { k.PageUp = v }},
	{"page_down", func(k *KeyConfig) string { return k.PageDown }, func(k *KeyConfig, v string) { k.PageDown = v }},
}

// Migrate brings an older config up to the current defaults in place and
// reports what it changed. It is pure apart from mutating cfg — no file is
// read or written — so the caller owns backup and save ordering.
//
// cfg is the config to rewrite and save. raw is the same file parsed without
// defaults applied ([LoadRawFile]), and is consulted only to tell "the file
// did not have this key" from "the file set it to the current default".
// [Load] fills absent fields from defaultConfig, so without raw a config
// written before help/global_search/contacts_alt existed would look like it
// already had them and the summary would not mention the keys it gained.
// raw may be nil, in which case cfg's own empty fields count as absent.
//
// Per field, in the [keys] table:
//
//   - absent: filled with the current default, and reported so the user can
//     see which keys the file gained.
//   - equal to a known stale default (see staleKeyDefaults): replaced with
//     the current default, because the user inherited it rather than chose
//     it. Comparison is through NormalizeKey, so "CTRL+K" counts.
//   - anything else: left alone. A deliberate customization survives an
//     upgrade even when it collides with something.
//
// It also fills two fields outside [keys] that were introduced later:
// ui.compose_editing and storage.state_file. state_file is written as the
// path the client would otherwise derive, so the location becomes explicit
// rather than implied.
func Migrate(cfg *Config, raw *RawFile) []MigrationChange {
	if cfg == nil {
		return nil
	}
	// What the file actually carried. Falling back to cfg keeps Migrate
	// usable on a hand-built config in tests, where "empty" does mean
	// "absent" because nothing filled defaults in.
	have := cfg
	present := map[string]bool{}
	if raw != nil && raw.Config != nil {
		have = raw.Config
		present = rawKeyPresence(raw)
	} else {
		for _, f := range keyFields {
			present[f.name] = f.get(&cfg.Keys) != ""
		}
	}
	def := defaultConfig()
	var changes []MigrationChange
	for _, f := range keyFields {
		if !migratableKeyFields[f.name] {
			// Inert: parsed and preserved, but never dispatched, so
			// rewriting it cannot change behavior. Left exactly as found.
			continue
		}
		configured := f.get(&have.Keys)
		want := f.get(&def.Keys)
		if want == "" {
			// No current default for this field; nothing to migrate to.
			continue
		}

		absent := !present[f.name]
		switch {
		case absent:
			// The file never had this key — fill it in and say so, even
			// though cfg already holds the default because Load put it
			// there.
		case configured == "":
			// Present but empty: a binding that can never match. Treated
			// as a mistake to repair, not a choice to respect.
		case isStaleKeyDefault(f.name, configured):
			// Inherited from an old example file, not chosen.
			if configured == want {
				continue
			}
		default:
			continue
		}
		f.set(&cfg.Keys, want)
		changes = append(changes, MigrationChange{
			Field: "keys." + f.name, Old: configured, Absent: absent, New: want,
		})
	}

	// Paths are written back exactly as the file had them. Load expands
	// "~/" so the running app gets an absolute path, but persisting that
	// expansion would quietly rewrite the user's config to hardcode this
	// machine's home directory. Expansion stays a runtime concern.
	if raw != nil {
		// A path the file had is kept verbatim; one it lacked is filled
		// from the unexpanded default, so a generated config stays
		// portable rather than hardcoding this machine's home directory.
		if raw.Has("storage", "session_file") {
			cfg.Storage.SessionFile = have.Storage.SessionFile
		} else {
			cfg.Storage.SessionFile = DefaultSessionFile
		}
		if raw.Has("storage", "files_dir") {
			cfg.Storage.FilesDir = have.Storage.FilesDir
		} else {
			cfg.Storage.FilesDir = DefaultFilesDir
		}
		if raw.Has("storage", "state_file") {
			cfg.Storage.StateFile = have.Storage.StateFile
		}
	}

	if !hasField(raw, "ui", "compose_editing", have.UI.ComposeEditing != "") {
		cfg.UI.ComposeEditing = def.UI.ComposeEditing
		changes = append(changes, MigrationChange{
			Field: "ui.compose_editing", Absent: true, New: def.UI.ComposeEditing,
		})
	}

	// Markdown is off by default for new configs (see UIConfig.ParseMarkdown)
	// but on for anyone migrating: they already have a working setup, the
	// feature is worth having, and unlike the default path this reports the
	// change so it is never a silent rewrite of what they type.
	if !hasField(raw, "ui", "parse_markdown", cfg.UI.ParseMarkdown) {
		cfg.UI.ParseMarkdown = true
		changes = append(changes, MigrationChange{
			Field: "ui.parse_markdown", Absent: true, New: "true",
		})
	}

	// state_file is only derived when the file left it out entirely. A
	// config that explicitly sets session_file = "" has opted out of on-disk
	// state, and inventing a path next to a session file that does not exist
	// would override that.
	explicitlyNoSession := raw.Has("storage", "session_file") && have.Storage.SessionFile == ""
	if !hasField(raw, "storage", "state_file", have.Storage.StateFile != "") && !explicitlyNoSession {
		if p := derivedStateFile(have); p != "" {
			cfg.Storage.StateFile = p
			changes = append(changes, MigrationChange{
				Field: "storage.state_file", Absent: true, New: p,
			})
		}
	}

	return changes
}

// hasField reports whether the file contained section.field, falling back to
// the caller's own emptiness check when there is no raw file to consult.
func hasField(raw *RawFile, section, field string, fallback bool) bool {
	if raw == nil {
		return fallback
	}
	return raw.Has(section, field)
}

// RawFile is a config file as written: parsed without defaults, plus the
// structure of what was actually in it.
//
// [Load] applies defaults, which erases the difference between "the file did
// not have this key" and "the file set it to today's default". The migration
// needs that difference to report honestly, and needs the unexpanded path
// strings so it does not rewrite a user's "~/..." into an absolute path.
type RawFile struct {
	// Config is the file parsed with no defaults applied.
	Config *Config
	// sections holds the top-level table names the file contained.
	sections map[string]bool
	// keys holds, per section, the field names the file contained.
	keys map[string]map[string]bool
	// unknown holds "section.key" entries the current schema does not
	// recognize. A rewrite drops them, so the user has to be told.
	unknown []string
}

// Has reports whether the file contained section.field.
func (r *RawFile) Has(section, field string) bool {
	if r == nil {
		return false
	}
	return r.keys[section][field]
}

// Unknown returns the keys the current schema does not recognize, which a
// rewrite will drop. Sorted.
func (r *RawFile) Unknown() []string {
	if r == nil {
		return nil
	}
	return r.unknown
}

// MissingSections returns the config tables the file does not have, which a
// rewrite will add in full. Sorted.
func (r *RawFile) MissingSections() []string {
	if r == nil {
		return nil
	}
	var out []string
	for section := range knownFields() {
		if !r.sections[section] {
			out = append(out, section)
		}
	}
	sort.Strings(out)
	return out
}

// LoadRawFile parses a config file without applying any defaults. See
// [RawFile]; [Load] is what the app itself wants.
func LoadRawFile(path string) (*RawFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Decoded a second time as a plain map, because the struct cannot say
	// which keys were present — an absent string and an empty one both
	// arrive as "".
	var tree map[string]any
	if err := toml.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	known := knownFields()
	raw := &RawFile{
		Config:   &cfg,
		sections: map[string]bool{},
		keys:     map[string]map[string]bool{},
	}
	for section, body := range tree {
		raw.sections[section] = true
		table, ok := body.(map[string]any)
		if !ok {
			raw.unknown = append(raw.unknown, section)
			continue
		}
		raw.keys[section] = map[string]bool{}
		for field := range table {
			raw.keys[section][field] = true
			if !known[section][field] {
				raw.unknown = append(raw.unknown, section+"."+field)
			}
		}
	}
	sort.Strings(raw.unknown)
	return raw, nil
}

// knownFields maps each config table to the fields the current schema
// recognizes, read from the toml struct tags so it cannot drift from Config.
func knownFields() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	cfgType := reflect.TypeOf(Config{})
	for i := range cfgType.NumField() {
		section := cfgType.Field(i).Tag.Get("toml")
		if section == "" {
			continue
		}
		body := cfgType.Field(i).Type
		if body.Kind() != reflect.Struct {
			continue
		}
		fields := map[string]bool{}
		for j := range body.NumField() {
			if tag := body.Field(j).Tag.Get("toml"); tag != "" {
				fields[tag] = true
			}
		}
		out[section] = fields
	}
	return out
}

// rawKeyPresence reports which [keys] fields the file contained.
func rawKeyPresence(raw *RawFile) map[string]bool {
	if raw == nil {
		return nil
	}
	return raw.keys["keys"]
}

// aliasPairs are field pairs that internal/app matches at a single dispatch
// site, e.g. `key.matches(m.keys.contacts, m.keys.contactsAlt)`. Setting both
// to the same key is redundant, not ambiguous — the one action fires either
// way — so it is not worth warning about.
var aliasPairs = [][2]string{
	{"contacts", "contacts_alt"},
	{"search", "global_search"},
}

// DetectKeyCollisions reports [keys] fields that internal/app dispatches on
// and that share a binding, as human-readable lines.
//
// Filling in new fields can create a collision the user never made: someone
// who bound search to "?" gets help = "?" from this migration, and only one
// of the two can win. The migration will not silently rewrite a deliberate
// choice, so the honest thing is to name the clash and let the user decide.
//
// Two limits worth knowing:
//
//   - Only the wired fields are considered. The inert ones (reply,
//     scroll_up, page_up, …) are not dispatched from app.go, so a shared
//     value there means nothing.
//   - Only configurable fields are considered. Plenty of bindings are
//     hardcoded — the alt+1/2/3 focus keys, i/c to compose, h/l and [/] for
//     folders, chatview's n/N and r/e/d, the composer's readline chords —
//     and binding a [keys] field to one of those collides without being
//     reported here. The help overlay shows the real, merged map; this
//     checks only what config.toml can express.
func DetectKeyCollisions(cfg *Config) []string {
	if cfg == nil {
		return nil
	}
	isAlias := func(a, b string) bool {
		for _, p := range aliasPairs {
			if (p[0] == a && p[1] == b) || (p[1] == a && p[0] == b) {
				return true
			}
		}
		return false
	}

	byBinding := map[string][]string{}
	for _, f := range keyFields {
		if !migratableKeyFields[f.name] {
			continue
		}
		v := NormalizeKey(f.get(&cfg.Keys))
		if v == "" {
			continue
		}
		byBinding[v] = append(byBinding[v], f.name)
	}

	var out []string
	for binding, fields := range byBinding {
		if len(fields) < 2 {
			continue
		}
		// An alias pair sharing a key is deliberate redundancy, not a clash.
		if len(fields) == 2 && isAlias(fields[0], fields[1]) {
			continue
		}
		sort.Strings(fields)
		out = append(out, fmt.Sprintf("%q is bound to %s", binding, strings.Join(fields, ", ")))
	}
	sort.Strings(out)
	return out
}

// isStaleKeyDefault reports whether value is one of the retired defaults for
// the named field.
func isStaleKeyDefault(field, value string) bool {
	got := NormalizeKey(value)
	for _, stale := range staleKeyDefaults[field] {
		if got == NormalizeKey(stale) {
			return true
		}
	}
	return false
}

// derivedStateFile returns the state database path the client would use when
// storage.state_file is left empty: alongside the session file. Mirrors
// stateDBPath in internal/telegram, which is the consumer of the value.
func derivedStateFile(cfg *Config) string {
	session := cfg.Storage.SessionFile
	if session == "" {
		// The unexpanded literal, not defaultConfig's expanded copy: this
		// value gets written to disk, and baking in a home directory would
		// make the config non-portable.
		session = DefaultSessionFile
	}
	if session == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(session), "state.db")
}

// SortChanges orders a migration summary by field name, so successive runs
// and successive versions produce comparable output.
func SortChanges(changes []MigrationChange) {
	sort.Slice(changes, func(i, j int) bool { return changes[i].Field < changes[j].Field })
}

// ConfigPath returns the config file the app would load: TELETUI_CONFIG when
// set, otherwise the default location — whether or not it exists. Callers
// that need to know if it exists should stat it.
func ConfigPath() string {
	if p := os.Getenv("TELETUI_CONFIG"); p != "" {
		return p
	}
	return defaultConfigPath()
}

// BackupFile copies path to path+".bak", byte for byte, at mode 0600. The
// migration re-marshals the config through the TOML encoder, which silently
// drops comments and reorders tables; the backup is the only copy of what the
// user actually wrote — and it holds the same api_hash and phone number as
// the original, so it gets the same restrictive mode.
//
// The backup lands beside the *resolved* file. With the usual dotfiles
// layout (~/.config/tele-tui/config.toml symlinked into ~/dotfiles) the
// backup belongs next to the real file in the dotfiles directory, where the
// content it is protecting actually lives — not next to the symlink.
//
// An existing backup is never overwritten. Someone who has already migrated
// once and is migrating again would otherwise lose their real original to a
// copy of the already-migrated file; the second backup gets a timestamp
// suffix instead.
func BackupFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	target := resolveTarget(path)
	backup := target + ".bak"
	if _, err := os.Stat(backup); err == nil {
		backup = fmt.Sprintf("%s.bak.%s", target, time.Now().Format("20060102-150405"))
	}
	if err := writeFilePrivate(backup, data); err != nil {
		return "", err
	}
	return backup, nil
}

// resolveTarget follows symlinks to the real file a write should land on.
//
// Config files are routinely symlinked out of a dotfiles repository. A write
// that replaces the link instead of following it does three bad things at
// once: the link is destroyed, the dotfiles copy silently stops being the
// source of truth, and the real file keeps whatever mode it had — so for
// exactly those users the 0600 tightening would never happen.
//
// Falls back to the path itself when it cannot be resolved: a file that does
// not exist yet is not a symlink, and a directory that cannot be resolved is
// a problem the subsequent write will report properly.
func resolveTarget(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	// The file may not exist yet while its directory is itself a link
	// (a symlinked ~/.config, say). Resolving the directory still puts the
	// temp file on the right filesystem.
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path))
	}
	return path
}

// SaveTo writes the config to an explicit path. Save always writes to the
// default location; migration has to write back to the file it read, which
// TELETUI_CONFIG can move.
func SaveTo(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := marshalConfig(cfg)
	if err != nil {
		return err
	}
	return writeFilePrivate(path, data)
}

// writeFilePrivate writes data to path at mode 0600, atomically, following
// any symlink to the real file.
//
// os.WriteFile would not do: its mode argument applies only when it creates
// the file, so rewriting an existing config.toml that happened to be 0644
// leaves it 0644 — world-readable, holding an api_hash and a phone number.
// Writing a fresh temp file and renaming over the target replaces the mode
// along with the contents, and rules out a truncated config if the write
// fails halfway.
//
// The rename target is the resolved path (see resolveTarget), because rename
// replaces a symlink rather than writing through it. The temp file is created
// beside that resolved target so the rename stays within one filesystem —
// across filesystems it fails outright.
func writeFilePrivate(path string, data []byte) error {
	path = resolveTarget(path)
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup: after a successful rename there is nothing left
	// to remove, and the error from that case is not interesting.
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("setting permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing config: %w", err)
	}
	// Flushed before the rename so a crash cannot leave the new name
	// pointing at a partially written file.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("flushing config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing config: %w", err)
	}
	return nil
}
