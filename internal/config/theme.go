package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"
)

// The compiled-in palettes. They are the zero-config theme, the only bases a
// theme file may inherit from, and the floor every theme failure degrades to.
const (
	ThemeDark  = "dark"
	ThemeLight = "light"
)

// ThemeForm is which of its forms a [UIConfig.Theme] value took.
type ThemeForm int

const (
	// ThemeFormBuiltin is "dark" or "light": the compiled-in palette.
	ThemeFormBuiltin ThemeForm = iota
	// ThemeFormPath is a value holding a path separator or ending in
	// .toml: a theme file named outright.
	ThemeFormPath
	// ThemeFormStem is any other name: themes/<name>.toml, searched for.
	ThemeFormStem
	// ThemeFormInvalid is a value that is none of those: not a builtin, not
	// path-shaped, and not a plain name either. See [isPlainThemeName].
	ThemeFormInvalid
)

// ThemeResolution is where a [UIConfig.Theme] value points, worked out
// before anything is read. See [ResolveThemeName].
type ThemeResolution struct {
	Form ThemeForm
	// Name is the value trimmed, and lowercased unless it is a path: a
	// path names a file, and on most filesystems case is part of the name.
	// An empty value resolves to [ThemeDark].
	Name string
	// Path is the file a [ThemeFormPath] value names, with "~/" expanded.
	Path string
	// Candidates are the themes/<Name>.toml files, in search order. For a
	// stem they are the search; for a builtin they are the files that would
	// shadow it, which are stat'd only to say so. Nil for an empty value:
	// an older config that never named a theme has shadowed nothing.
	Candidates []string
}

// ResolveThemeName works out where a ui.theme value points, without touching
// the filesystem:
//
//  1. "dark" or "light", in any case, is the builtin. Always — a file of
//     the same name cannot shadow it, because the builtins are what every
//     failure falls back to and what inherit names.
//  2. A value with a path separator or a .toml suffix is a path, resolved
//     like every other path in config.toml: "~/" expanded, anything
//     relative left to the working directory.
//  3. Any other plain name is themes/<name>.toml, searched for in configDir
//     (the loaded config's directory) and then defaultConfigDir, so a
//     TELETUI_CONFIG profile elsewhere still finds the shared collection.
//
// An empty value is dark, and quietly so: it is an older config, not a
// mistake. Unlike the other Resolve functions this one takes directories and
// is not the whole answer: reading what it points to is the loader's job.
func ResolveThemeName(value, configDir, defaultConfigDir string) ThemeResolution {
	trimmed := strings.TrimSpace(value)
	name := strings.ToLower(trimmed)
	switch {
	case name == "":
		return ThemeResolution{Form: ThemeFormBuiltin, Name: ThemeDark}
	case isThemePath(name):
		return ThemeResolution{Form: ThemeFormPath, Name: trimmed, Path: expandPath(trimmed)}
	case name == ThemeDark || name == ThemeLight:
		return ThemeResolution{
			Form:       ThemeFormBuiltin,
			Name:       name,
			Candidates: themeCandidates(name, configDir, defaultConfigDir),
		}
	case !isPlainThemeName(name):
		return ThemeResolution{Form: ThemeFormInvalid, Name: name}
	}
	return ThemeResolution{
		Form:       ThemeFormStem,
		Name:       name,
		Candidates: themeCandidates(name, configDir, defaultConfigDir),
	}
}

// themeCandidates is themes/<name>.toml in each directory, in order, each
// directory once. Join cleans the path, so "/a/" and "/a/." are one
// directory; a symlink to it is not, and finding out would take a stat. An
// empty directory is skipped rather than joined into the working directory.
func themeCandidates(name string, dirs ...string) []string {
	var out []string
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, "themes", name+".toml")
		if !slices.Contains(out, candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

// isThemePath reports whether a ui.theme value names a file rather than a
// theme: it holds a path separator, or it ends in .toml.
func isThemePath(name string) bool {
	return strings.ContainsRune(name, '/') ||
		strings.ContainsRune(name, filepath.Separator) ||
		strings.HasSuffix(name, ".toml")
}

// isPlainThemeName reports whether a stem can be spliced into
// themes/<name>.toml as a file name and nothing else: letters, digits, "-",
// "_" and ".", not starting with ".". That keeps out "..", hidden files, and
// the characters some filesystem gives a meaning to (":" on Windows, NUL
// everywhere) — a whitelist, because a list of the dangerous ones would be a
// list of the ones somebody thought of.
func isPlainThemeName(name string) bool {
	if name == "" || name[0] == '.' {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-_.", r) {
			return false
		}
	}
	return true
}

// ThemeSpec is a theme file as read: the transport between this package,
// which finds and decodes the file, and internal/ui/theme, which alone knows
// the role names and turns a spec into colours. Nothing here is validated
// against the roles.
type ThemeSpec struct {
	// Name is the ui.theme value as resolved: see [ThemeResolution.Name].
	Name string
	// Source is the file the spec was read from.
	Source string
	// Inherit is the builtin the theme starts from, [ThemeDark] or
	// [ThemeLight]: already defaulted and checked here.
	Inherit string
	// Colors and Colors256 are [colors] and [colors256], keys as written
	// and values coerced to strings.
	Colors    map[string]string
	Colors256 map[string]string
	// Ramp is [senders].ramp as written, or nil when the file has none.
	Ramp []string
}

// ThemeSpec is the theme file [Load] read for ui.theme, or nil when the
// palette is a builtin — named, or fallen back to — and [Config.ThemeBuiltin]
// says which.
func (c *Config) ThemeSpec() *ThemeSpec {
	return c.themeSpec
}

// ThemeBuiltin is the builtin palette underneath the theme, [ThemeDark] or
// [ThemeLight]: the whole palette when [Config.ThemeSpec] is nil, and the
// spec's base when it is not.
//
// A Config that [Load] did not build — a test's &Config{}, say — has read
// no file, so it gets the builtin its ui.theme names, and dark otherwise.
func (c *Config) ThemeBuiltin() string {
	if c.themeBuiltin != "" {
		return c.themeBuiltin
	}
	if r := ResolveThemeName(c.UI.Theme, "", ""); r.Form == ThemeFormBuiltin {
		return r.Name
	}
	return ThemeDark
}

// LoadTheme is the theme loader's config half: it resolves a ui.theme
// value, finds and reads the file it names, and says what went wrong, all
// without failing. The result is either a spec, whose base builtin is also
// returned, or no spec and the builtin to draw with — dark whenever a file
// was meant and could not be used, because dark is the floor every theme
// failure lands on.
//
// The directories are parameters rather than looked up here, so a caller
// (and every test) decides which themes/ directories exist — reading
// $XDG_CONFIG_HOME in here would make every test depend on what the
// developer keeps in ~/.config. [Load] passes the loaded config's directory
// and the default one. Validating the spec against the role names is
// internal/ui/theme's job, not this one's.
func LoadTheme(value, configDir, defaultConfigDir string) (spec *ThemeSpec, builtin string, warnings []string) {
	r := ResolveThemeName(value, configDir, defaultConfigDir)
	var path string
	switch r.Form {
	case ThemeFormBuiltin:
		// Candidates is nil for an empty value, so this stats only when
		// the config actually names a builtin.
		if shadow := findShadow(r.Candidates); shadow != "" {
			warnings = []string{shadowedTheme(r, shadow)}
		}
		return nil, r.Name, warnings
	case ThemeFormInvalid:
		return nil, ThemeDark, []string{invalidTheme(r)}
	case ThemeFormPath:
		path = r.Path
	case ThemeFormStem:
		if path = findTheme(r.Candidates); path == "" {
			return nil, ThemeDark, []string{missingTheme(r)}
		}
	}
	spec, warnings = readThemeFile(path)
	if spec == nil {
		return nil, ThemeDark, warnings
	}
	spec.Name = r.Name
	return spec, spec.Inherit, warnings
}

// shadowedTheme is the warning for a themes/dark.toml or light.toml that a
// builtin name will never reach. Builtin names win: the builtins are the
// fallback of every failure and the only thing inherit can name, and a
// fallback a user file can redefine is not a fallback. The path form does
// reach the file, so the warning spells it out.
func shadowedTheme(r ThemeResolution, shadow string) string {
	return fmt.Sprintf("%s is ignored: ui.theme %q always means the builtin; "+
		"to use the file instead, set theme = %q", shadow, r.Name, shadow)
}

// invalidTheme is the warning for a value that is not a plain name and not
// shaped like a path either.
func invalidTheme(r ThemeResolution) string {
	return fmt.Sprintf(`ui.theme %q is not a theme name — a name is letters, digits, "-", "_" and ".", `+
		"and a file is named by a path or a .toml suffix; using %s", r.Name, ThemeDark)
}

// missingTheme is the warning for a stem found in none of its directories.
// It names the builtins too: the likeliest cause is a typo of one of them.
func missingTheme(r ThemeResolution) string {
	dirs := make([]string, len(r.Candidates))
	for i, candidate := range r.Candidates {
		dirs[i] = filepath.Dir(candidate)
	}
	return fmt.Sprintf("ui.theme %q is not %s, %s, or a theme in %s; using %s",
		r.Name, ThemeDark, ThemeLight, strings.Join(dirs, " or "), ThemeDark)
}

// findTheme is the first candidate that is there. "There" includes a
// candidate that cannot be stat'd for any reason but its absence: the
// reader's warning about it is more use than searching on past it.
func findTheme(candidates []string) string {
	for _, path := range candidates {
		if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
			return path
		}
	}
	return ""
}

// findShadow is the first candidate that certainly exists. Stricter than
// [findTheme] on purpose: a search stops at a stat that failed oddly so
// the reader hears why, but a warning that a file is ignored has to name a
// file that is there.
func findShadow(candidates []string) string {
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// maxThemeFileSize caps what is read as a theme. A complete theme is a
// couple of kilobytes; anything near this is not a theme.
const maxThemeFileSize = 64 << 10

// themeFile is the decode shape of a theme file, and the shape is the
// point. [colors] and [colors256] are map[string]any, not map[string]string:
// with the latter, `bg = 235` — an xterm index written the way everybody
// writes one — fails the WHOLE file under go-toml v2. inherit and ramp are
// any for the same reason. Everything is coerced afterwards, one value at a
// time, so a bad value costs that value and nothing else.
type themeFile struct {
	Theme struct {
		Inherit any `toml:"inherit"`
	} `toml:"theme"`
	Colors    map[string]any `toml:"colors"`
	Colors256 map[string]any `toml:"colors256"`
	Senders   struct {
		Ramp any `toml:"ramp"`
	} `toml:"senders"`
}

// readThemeFile reads one theme file into a spec. It never fails: a file
// that cannot be used at all is a nil spec and one warning, and the caller
// falls back to dark; anything less costs only the values concerned.
//
// The file is stat'd before it is opened. Opening a fifo — or /dev/stdin
// under a terminal — blocks until something writes to it, which at startup
// is never, and a directory would fail the read with something confusing.
// There is no permission check: unlike the session file, a theme holds no
// secrets.
func readThemeFile(path string) (*ThemeSpec, []string) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, []string{unreadableTheme(path, err)}
	}
	if !info.Mode().IsRegular() {
		return nil, []string{fmt.Sprintf("theme file %s is not a regular file; using %s", path, ThemeDark)}
	}
	if info.Size() > maxThemeFileSize {
		return nil, []string{fmt.Sprintf("theme file %s is %d bytes, over the 64 KiB a theme may be; using %s",
			path, info.Size(), ThemeDark)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []string{unreadableTheme(path, err)}
	}
	return decodeThemeFile(path, data)
}

// decodeThemeFile turns a theme file's bytes into a spec, coercing each
// value on its own. Only a file that is not TOML at all fails whole.
func decodeThemeFile(source string, data []byte) (*ThemeSpec, []string) {
	var raw themeFile
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, []string{fmt.Sprintf("theme file %s: %s; using %s",
			source, tomlFailure(err), ThemeDark)}
	}
	spec := &ThemeSpec{Source: source}
	var warnings, w []string
	spec.Inherit, w = themeInherit(source, raw.Theme.Inherit)
	warnings = append(warnings, w...)
	spec.Colors, w = coerceColors(source, "colors", raw.Colors)
	warnings = append(warnings, w...)
	spec.Colors256, w = coerceColors(source, "colors256", raw.Colors256)
	warnings = append(warnings, w...)
	spec.Ramp, w = coerceRamp(source, raw.Senders.Ramp)
	warnings = append(warnings, w...)
	// A theme with nothing in it is its base, which is well defined and
	// almost certainly not what its author meant. Judged on what was
	// written, not on what survived coercion: a dropped value has already
	// had its warning.
	if len(raw.Colors) == 0 && len(raw.Colors256) == 0 && raw.Senders.Ramp == nil {
		warnings = append(warnings, fmt.Sprintf(
			"theme file %s defines no colours, so it is %s unchanged", source, spec.Inherit))
	}
	return spec, warnings
}

// unreadableTheme is the warning for a theme file the OS would not give us.
// The path is said once: a *fs.PathError would say it again, after "stat".
func unreadableTheme(path string, err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	return fmt.Sprintf("theme file %s: %v; using %s", path, err, ThemeDark)
}

// tomlFailure describes a parse error with the position go-toml knows,
// when it knows one. A syntax error is a *toml.DecodeError and carries a
// line and column; a duplicate key or table is a plain error that carries
// neither, only the name of what was duplicated — so errors.As alone would
// lose that message rather than just the position.
func tomlFailure(err error) string {
	msg := strings.TrimPrefix(err.Error(), "toml: ")
	var decodeErr *toml.DecodeError
	if errors.As(err, &decodeErr) {
		line, column := decodeErr.Position()
		return fmt.Sprintf("line %d, column %d: %s", line, column, msg)
	}
	return msg
}

// themeInherit is [theme].inherit checked: absent or empty is dark, "dark"
// and "light" in any case are themselves, and anything else is dark with a
// warning. Only a builtin can be a base — theme-file chains would cost cycle
// detection and buy little — and a builtin is the one base known to be
// complete.
func themeInherit(source string, v any) (string, []string) {
	s, isString := v.(string)
	name := strings.ToLower(strings.TrimSpace(s))
	switch {
	case v == nil || (isString && name == ""):
		return ThemeDark, nil
	case isString && (name == ThemeDark || name == ThemeLight):
		return name, nil
	}
	return ThemeDark, []string{fmt.Sprintf(
		"theme file %s: inherit = %s is not %q or %q — only a builtin can be inherited; using %s",
		source, tomlValue(v), ThemeDark, ThemeLight, ThemeDark)}
}

// tomlValue shows a decoded value roughly as it was written: a string
// quoted, anything else as Go prints it.
func tomlValue(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(v)
}

// coerceColors turns a decoded colour table into strings: a string as it
// is, an integer as its digits, and anything else dropped with a warning so
// that one role inherits instead of the whole file failing. Keys are walked
// in order so the warnings are too.
func coerceColors(source, table string, raw map[string]any) (map[string]string, []string) {
	out := make(map[string]string, len(raw))
	var warnings []string
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		switch v := raw[key].(type) {
		case string:
			out[key] = v
		case int64:
			out[key] = strconv.FormatInt(v, 10)
		default:
			warnings = append(warnings, fmt.Sprintf(
				"theme file %s: %s.%s is %s, not a string or an integer; it inherits instead",
				source, table, key, tomlKind(v)))
		}
	}
	return out, warnings
}

// coerceRamp is [senders].ramp as a list of strings, nil when absent. An
// entry that is not a string is passed on as its text: whether an entry
// names a role is the converter's call, and it answers a bad one with the
// default ramp, whole. Dropping the entry here would make the same mistake
// a shorter ramp instead — everybody's colour moves either way.
func coerceRamp(source string, v any) ([]string, []string) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, []string{fmt.Sprintf(
			"theme file %s: senders.ramp is %s, not a list of role names; ignored",
			source, tomlKind(v))}
	}
	out := make([]string, len(list))
	for i, entry := range list {
		out[i] = fmt.Sprint(entry)
	}
	return out, nil
}

// tomlKind names a decoded TOML value the way a theme author would.
func tomlKind(v any) string {
	switch v.(type) {
	case string:
		return "a string"
	case int64:
		return "an integer"
	case float64:
		return "a float"
	case bool:
		return "a boolean"
	case []any:
		return "an array"
	case map[string]any:
		return "a table"
	default:
		return "a date or time"
	}
}
