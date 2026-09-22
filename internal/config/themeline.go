package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// SetThemeLine sets ui.theme in the config file at path to value, and
// touches nothing else in it.
//
// [Save] would not do: it re-encodes the whole file through the TOML
// encoder, which drops every comment and reorders what is left — a price
// -migrate-config pays once, with a backup and a report, and a theme switch
// must not charge on every use. So this edits text:
//
//   - The theme line of the [ui] table has its value replaced and nothing
//     else: indentation, the key as spelled (bare or quoted), the spacing
//     around =, and a comment after the value all stay.
//   - A [ui] table without a theme gets `theme = …` on the line after its
//     header; a file without one gets a [ui] table appended. A file that
//     does not exist is created holding just that, private, as every config
//     write is.
//   - The value is a literal string when it can be one — no ' and no
//     control character — so a Windows path is written as typed, and a
//     basic string with escapes otherwise.
//   - Line endings stay what they were, CRLF included, and so does a
//     missing final newline.
//
// It refuses, and says to edit the file by hand, rather than guess: when ui
// is written as dotted keys or an inline table (where one more line cannot
// set it, and a [ui] table beside them would make the file invalid), when
// ui.theme is anything but a string on a line of [ui], and when the file is
// not TOML at all.
//
// With backup, the file is kept as config.toml.bak before it is written —
// only once it is known there will be a write, so a refusal leaves nothing
// behind. One backup, ever: it is the file as it was before tele-tui first
// edited it, so with a config.toml.bak already there (from an earlier save,
// or from -migrate-config) none is made, timestamped or otherwise; each would
// be one more copy of an api_hash, and none of them the user's own file.
// kept reports whether that file is safe — backed up now, backed up
// already, or never there, when this call creates the file — and holds even
// when the save then fails, so the caller need not ask again. It is false
// for a refusal, and whenever backup is.
//
// The write is atomic, keeps the file's mode, and lands on a symlink's
// target rather than replacing the link. The result is then read back with
// the real loader; if it does not load, or does not say value, the original
// is put back and the error returned. That safety does not depend on the
// backup: the original is the bytes read before the edit.
func SetThemeLine(path, value string, backup bool) (kept bool, err error) {
	original, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return backup, createThemeFile(path, value)
	}
	if err != nil {
		return false, fmt.Errorf("reading config: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("reading config: %w", err)
	}

	edited, err := editThemeLine(original, value)
	if err != nil {
		return false, fmt.Errorf("%s %w — edit it by hand", filepath.Base(path), err)
	}

	backupPath := ""
	if backup {
		if backupPath, err = backupOnce(path, original); err != nil {
			return false, fmt.Errorf("backing up config: %w", err)
		}
		kept = true
	}
	mode := info.Mode().Perm()
	if err := writeFileAtomic(path, edited, mode); err != nil {
		return kept, err
	}
	if err := checkThemeLine(path, value); err != nil {
		if restoreErr := writeFileAtomic(path, original, mode); restoreErr != nil {
			if backupPath == "" {
				return kept, fmt.Errorf("%w, and restoring it failed too (%v)", err, restoreErr)
			}
			return kept, fmt.Errorf("%w, and restoring it failed too (%v): the original is in %s",
				err, restoreErr, backupPath)
		}
		return kept, fmt.Errorf("%w; the file is as it was", err)
	}
	return kept, nil
}

// backupOnce keeps original as config.toml.bak, beside the file a symlink
// resolves to as [BackupFile] puts it, unless a backup is there already —
// then that one is the older file, and it stays. original is the bytes the
// edit starts from, rather than the file read again: what is backed up is
// then what the edit was made to. The path returned is the backup's.
//
// [BackupFile] is -migrate-config's, and keeps its own rule: never
// overwrite, and timestamp a second backup. Once a migration, that is one
// file; once a :theme, it was one a session and more.
func backupOnce(path string, original []byte) (string, error) {
	backup := resolveTarget(path) + ".bak"
	if _, err := os.Lstat(backup); err == nil {
		return backup, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return backup, writeFilePrivate(backup, original)
}

// loadWritten is the loader SetThemeLine reads its own write back with. A
// variable so a test can make the check fail: a real edit that reads back
// wrong is the bug this check exists to contain, and cannot be staged.
var loadWritten = loadFrom

// checkThemeLine reads the config at path back as the app will, and says
// what is wrong when it does not load or its ui.theme is not value.
func checkThemeLine(path, value string) error {
	cfg, err := loadWritten(path)
	if err != nil {
		return fmt.Errorf("the edited config does not load (%v)", err)
	}
	if cfg.UI.Theme != value {
		return fmt.Errorf("the edited config reads ui.theme as %q, not %q", cfg.UI.Theme, value)
	}
	return nil
}

// createThemeFile writes a config holding nothing but the theme, where
// [SaveTo] would, and removes it again if it does not read back.
func createThemeFile(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := writeFilePrivate(path, []byte("[ui]\ntheme = "+tomlString(value)+"\n")); err != nil {
		return err
	}
	if err := checkThemeLine(path, value); err != nil {
		os.Remove(resolveTarget(path))
		return err
	}
	return nil
}

// editThemeLine is data with ui.theme set to value: see [SetThemeLine]. The
// error completes a sentence that starts with the file's name.
func editThemeLine(data []byte, value string) ([]byte, error) {
	site, err := findThemeSite(data)
	if err != nil {
		return nil, err
	}

	eol := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		eol = "\r\n"
	}
	line := "theme = " + tomlString(value)

	var out bytes.Buffer
	switch {
	case site.hasValue:
		// The value's own bytes, and nothing either side of them.
		out.Write(data[:site.valueStart])
		out.WriteString(tomlString(value))
		out.Write(data[site.valueEnd:])
	case site.hasHeader:
		out.Write(data[:site.headerEnd])
		if site.headerEnd == len(data) && !bytes.HasSuffix(data, []byte("\n")) {
			// The header is the last line and nothing ends it: this line
			// is the last one now, and ends the same way.
			out.WriteString(eol + line)
		} else {
			out.WriteString(line + eol)
		}
		out.Write(data[site.headerEnd:])
	default:
		open := len(data) > 0 && !bytes.HasSuffix(data, []byte("\n"))
		out.Write(data)
		if open {
			out.WriteString(eol)
		}
		if len(data) > 0 {
			out.WriteString(eol)
		}
		out.WriteString("[ui]" + eol + line)
		if !open {
			out.WriteString(eol)
		}
	}
	return out.Bytes(), nil
}

// themeSite is where in a config file ui.theme is, or would go.
type themeSite struct {
	// hasValue: [ui] has a theme, and valueStart:valueEnd are the bytes of
	// its value — the quotes included, nothing else.
	hasValue             bool
	valueStart, valueEnd int
	// hasHeader: there is a [ui] table, and its header's line ends just
	// before headerEnd — past the newline, when there is one.
	hasHeader bool
	headerEnd int
}

// findThemeSite finds where ui.theme is written, with a TOML parser rather
// than by matching lines, so a "[ui]" or a "theme =" inside a multi-line
// string is not taken for one. The error is a refusal: ui written some way a
// one-line edit cannot safely change.
//
// Tables and keys are matched whatever their case, because the loader
// matches them so: go-toml fills a struct field from a key that differs from
// its tag only in case, so `[UI]` and `Theme =` are the setting too. Matched
// by case here, a `Theme` line would be missed and a second `theme` added
// beside it, which the loader might read or might not. Two spellings of the
// one table, or of the one key, are TOML's distinct keys and the loader's
// one setting, and which of them wins is not something to guess at.
func findThemeSite(data []byte) (themeSite, error) {
	// The parser below reads syntax only; this also catches a key or a
	// table defined twice, which the loader would reject.
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return themeSite{}, fmt.Errorf("is not valid TOML (%v)", tomlFailure(err))
	}

	site := themeSite{}
	var p unstable.Parser
	p.Reset(data)
	var table []string
	for p.NextExpression() {
		expr := p.Expression()
		switch expr.Kind {
		case unstable.Table, unstable.ArrayTable:
			table = keyParts(expr.Key())
			if !isKey(table[0], "ui") {
				continue
			}
			switch {
			case expr.Kind == unstable.ArrayTable:
				return themeSite{}, errors.New("has an array of [[ui]] tables")
			case len(table) == 1 && site.hasHeader:
				return themeSite{}, errors.New("has more than one [ui] table, told apart only by case")
			case len(table) == 1:
				site.hasHeader = true
				site.headerEnd = lineEnd(data, headerOffset(expr))
			case isKey(table[1], "theme"):
				return themeSite{}, errors.New("sets ui.theme as a table")
			}
		case unstable.KeyValue:
			key := keyParts(expr.Key())
			atRoot, inUI := len(table) == 0, len(table) == 1 && isKey(table[0], "ui")
			switch {
			case atRoot && isKey(key[0], "ui") && len(key) > 1:
				return themeSite{}, errors.New("sets ui with dotted keys, not a [ui] table")
			case atRoot && isKey(key[0], "ui") && expr.Value().Kind == unstable.InlineTable:
				return themeSite{}, errors.New("sets ui as an inline table, not a [ui] table")
			case atRoot && isKey(key[0], "ui"):
				return themeSite{}, errors.New("sets ui to something that is not a table")
			case !inUI || !isKey(key[0], "theme"):
				continue
			case len(key) > 1:
				return themeSite{}, errors.New("sets ui.theme with a dotted key")
			case site.hasValue:
				return themeSite{}, errors.New("sets ui.theme more than once, told apart only by case")
			}
			v := expr.Value()
			if v.Kind != unstable.String {
				return themeSite{}, errors.New("sets ui.theme to something that is not a string")
			}
			site.hasValue = true
			site.valueStart = int(v.Raw.Offset)
			site.valueEnd = site.valueStart + int(v.Raw.Length)
		}
	}
	if err := p.Error(); err != nil {
		return themeSite{}, fmt.Errorf("is not valid TOML (%v)", err)
	}
	return site, nil
}

// isKey reports whether a key part is name as the loader reads it: in any
// case.
func isKey(part, name string) bool { return strings.EqualFold(part, name) }

// keyParts is a key's parts, decoded: `"ui" . theme` is [ui theme].
func keyParts(it unstable.Iterator) []string {
	var parts []string
	for it.Next() {
		parts = append(parts, string(it.Node().Data))
	}
	return parts
}

// headerOffset is where a table header's key starts: on the header's line,
// which is all a caller needs of it.
func headerOffset(table *unstable.Node) int {
	it := table.Key()
	it.Next()
	return int(it.Node().Raw.Offset)
}

// lineEnd is the offset just past the newline ending the line that offset is
// on, or the end of data when no newline does.
func lineEnd(data []byte, offset int) int {
	if i := bytes.IndexByte(data[offset:], '\n'); i >= 0 {
		return offset + i + 1
	}
	return len(data)
}

// tomlString is s as a TOML string: literal when it can be — no ' and no
// control character but tab, which a literal string cannot hold — and basic,
// escaped, otherwise.
func tomlString(s string) string {
	literal := !strings.ContainsFunc(s, func(r rune) bool {
		return r == '\'' || (r < 0x20 && r != '\t') || r == 0x7f
	})
	if literal {
		return "'" + s + "'"
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
