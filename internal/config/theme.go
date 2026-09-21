package config

import (
	"path/filepath"
	"slices"
	"strings"
	"unicode"
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
