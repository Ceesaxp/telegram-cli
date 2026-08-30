package app

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/composer"
)

// TestReadmeKeymapMatchesHelpSections is the drift test standing in for
// generating README.md's key tables from helpSections(). The keymap has
// four descriptions — keymap.go's prose table, helpSections(), the status
// bar strip, and the README — and the first three are built from the same
// resolvedKeys, so the README is the only one that can silently fall
// behind. It did, twice: it documented quick-type for a release after
// quick-type was removed, and it listed "h/l: folders" after h/l became
// panel movement.
//
// # What it pins
//
// The KEY SET, in both directions. A binding added to helpSections() and
// not written down fails; a README row naming a key that no longer appears
// anywhere in helpSections() fails.
//
// # What it does not pin
//
//   - Descriptions. The two can word the same key differently, and should
//     — the README has room to explain, the card does not.
//   - Which panel a key ACTS on, only which table it is listed under.
//     Section placement is checked (see below), so a key documented in
//     the wrong table fails — but swapping two keys' Actions inside one
//     table does not, because the Action column is prose and prose is
//     what this test deliberately cannot read. Only a human or a
//     behavioral test catches "h and l described back to front".
//   - Keys that never reach helpSections(). One area is out of scope for
//     that reason and is skipped by name: the "Help overlay" subsection.
//     Those keys (?/esc/q to close, j/k, pgup/pgdown, g/G to scroll) are
//     handled inside help.Model's own Update, never pass through
//     helpSections, and would otherwise have to be exempted one by one.
//     Everything else in the section is in scope, including both composer
//     editing modes — the model is built twice, once per mode, because
//     helpSections only ever returns the active one.
//
// # Section placement
//
// Comparing key SETS alone was too weak: it would not have caught its own
// motivating example, because a key moving between tables keeps the set
// identical. So each key also carries the set of sections it appears in —
// global / chatlist / chatview / composer / overlays — and those are
// compared too. A key documented under Chat list that the card puts under
// Chat view fails. sectionPlacementAllowed carries the handful of
// deliberate divergences, each with its reason; an entry that stops being
// necessary fails as well, so the allow-list cannot quietly absorb real
// drift.
//
// # How it reads the README
//
// Only the first column of tables whose header cell is exactly "Key",
// inside the "## Keybindings" section, and within those only text in
// `backticks`. That is what makes it survive prose churn: the paragraphs,
// the parenthetical "(or Home / End)" synonyms, the config-field notes in
// the Action column, the terminal-settings table and the [keys] "Wired?"
// table are all invisible to it. Rewriting the surrounding prose cannot
// break this test; only changing a documented key can.
func TestReadmeKeymapMatchesHelpSections(t *testing.T) {
	documentedIn, tables := readmeKeymapTokens(t)
	advertisedIn := helpSectionTokens(t)
	documented, advertised := flatten(documentedIn), flatten(advertisedIn)

	// A rename of the heading, or a table losing its "Key" header, would
	// otherwise make this test pass by comparing nothing at all.
	if tables < 6 {
		t.Fatalf("parsed only %d key tables from README.md — has the "+
			"\"## Keybindings\" section moved or been reformatted?", tables)
	}
	if len(documented) < 50 {
		t.Fatalf("parsed only %d keys from README.md; expected the whole "+
			"keymap, so the parse is probably broken rather than the docs", len(documented))
	}

	for key := range advertised {
		if documented[key] {
			continue
		}
		if _, exempt := onlyInHelpSections[key]; exempt {
			continue
		}
		t.Errorf("%q is advertised by helpSections() but is NOT documented "+
			"in README.md's keybinding tables — add a row for it (or, if it "+
			"is deliberately undocumented, an entry in onlyInHelpSections)", key)
	}

	for key := range documented {
		if advertised[key] {
			continue
		}
		if _, ok := onlyInReadme[key]; ok {
			continue
		}
		t.Errorf("%q is documented in README.md's keybinding tables but is "+
			"NOT advertised by helpSections() — the binding is gone, so "+
			"delete the README row (or, if the key is real but component-"+
			"owned, add an entry to onlyInReadme)", key)
	}

	// Section placement, for the keys both sides agree exist.
	for key := range advertised {
		if !documented[key] {
			continue // already reported above
		}
		want, got := advertisedIn[key], documentedIn[key]
		if sameSections(want, got) {
			if reason, allowed := sectionPlacementAllowed[key]; allowed {
				t.Errorf("sectionPlacementAllowed[%q] (%s) is stale — the "+
					"sections agree now; delete the entry", key, reason)
			}
			continue
		}
		if _, allowed := sectionPlacementAllowed[key]; allowed {
			continue
		}
		t.Errorf("%q is documented under README section(s) %v but the help "+
			"card puts it under %v — one of the two has it in the wrong "+
			"table (or add an entry to sectionPlacementAllowed with the "+
			"reason the difference is deliberate)",
			key, sectionList(got), sectionList(want))
	}

	// The exemptions are a liability, not a feature: an entry that has
	// become unnecessary should be deleted rather than left to hide a
	// future mismatch.
	for key, reason := range onlyInHelpSections {
		if documented[key] {
			t.Errorf("onlyInHelpSections[%q] (%s) is stale — the key is "+
				"documented in the README now; delete the exemption", key, reason)
		}
	}
	for key, reason := range onlyInReadme {
		if advertised[key] {
			t.Errorf("onlyInReadme[%q] (%s) is stale — the key is advertised "+
				"by helpSections() now; delete the exemption", key, reason)
		}
	}
}

// sectionPlacementAllowed lists keys the README files under a different
// heading than the help card groups them under, on purpose.
var sectionPlacementAllowed = map[string]string{
	"q": "the README lists it once under Global with a scope note (\"chat " +
		"list / chat view only\") as well as in both browsing tables; the " +
		"card has no room for the note and repeats the row instead",
	"esc": "the README spells out what Esc does in the chat view (drop the " +
		"find results first) where the card leaves it to the global row",
	"up": "arrow synonym for k in the browsing panels and the overlays, " +
		"where the card names only the vi spelling — but NOT in the " +
		"command palette, which has no vi spelling to name: every " +
		"printable goes into the query, so the arrows are the only way " +
		"to move and both surfaces have to say so",
	"down": "arrow synonym for j; see \"up\"",
}

// flatten collapses a key -> sections index into the plain key set.
func flatten(index map[string]map[string]bool) map[string]bool {
	out := make(map[string]bool, len(index))
	for key := range index {
		out[key] = true
	}
	return out
}

func sameSections(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// sectionList renders a section set for a failure message.
func sectionList(s map[string]bool) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// onlyInHelpSections lists keys the help card names that the README writes
// as prose rather than as a `code span`, so the parser cannot see them.
var onlyInHelpSections = map[string]string{
	"click": "the README writes it as \"click a chat\" / \"click a folder tab\"",
}

// onlyInReadme lists keys the README documents that the help card
// deliberately does not name.
var onlyInReadme = map[string]string{}

// helpSectionTokens indexes every key the help overlay names by the
// canonical section it appears in. Built once per composer editing mode,
// because helpSections only ever returns the section for the mode that is
// active.
func helpSectionTokens(t *testing.T) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	for _, mode := range []composer.EditingMode{composer.ModeEmacs, composer.ModeVi} {
		cfg := &config.Config{}
		m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
		m.composer.SetEditingMode(mode)
		for _, sec := range m.helpSections() {
			section := canonicalSection(sec.Title)
			if section == "" {
				t.Fatalf("help section %q has no canonical name — teach "+
					"canonicalSection about it", sec.Title)
			}
			for _, b := range sec.Bindings {
				// Split on the exact " / " joiner every row is built
				// with, never on a bare "/" — "/" is itself a binding,
				// and splitting on it would drop the search key from
				// both sides at once and quietly stop covering it.
				for _, key := range strings.Split(b.Keys, " / ") {
					k := normalizeKeyToken(key)
					if k == "" || k == unboundKey {
						// unboundKey is the card saying "no key reaches
						// this action", not a key to document.
						continue
					}
					if out[k] == nil {
						out[k] = map[string]bool{}
					}
					out[k][section] = true
				}
			}
		}
	}
	return out
}

// canonicalSection maps a help-overlay section title, and a README "###"
// heading, onto the one name both are compared under. Returning "" from
// the README side means "not a keymap section" (the macOS notes, the
// [keys] table, the clipboard section); from the help side it is a
// programming error, since every help section is a keymap section.
func canonicalSection(title string) string {
	switch {
	case strings.HasPrefix(title, "Global"):
		return "global"
	case strings.HasPrefix(title, "Chat list"):
		return "chatlist"
	case strings.HasPrefix(title, "Chat view"):
		return "chatview"
	case strings.HasPrefix(title, "Composer"):
		// "Composer", "Composer editing modes", "Composer (emacs
		// editing)" and "Composer (vi editing)" are all one section
		// here: the split is by editing mode, not by panel, and the
		// README documents both modes under the same heading.
		return "composer"
	case strings.HasPrefix(title, "Overlays"):
		return "overlays"
	case strings.HasPrefix(title, "Command palette"):
		return "palette"
	}
	return ""
}

// codeSpan matches a markdown `code span`.
var codeSpan = regexp.MustCompile("`([^`]+)`")

// doubleCodeSpan matches Markdown's escape hatch for a code span that
// contains a backtick: ``` “ ` “ ```. It is the only way to write a
// literal backtick as code, so the rail's toggle has no other spelling —
// and the single-span pattern above reads it as a space, which is how a
// documented key can look undocumented.
var doubleCodeSpan = regexp.MustCompile("``\\s*(.+?)\\s*``")

// digitRange collapses the README's "`1`–`9`" idiom (any dash) into the
// single "1-9" token the help card uses for the folder jump.
var digitRange = regexp.MustCompile("`1`\\s*[-–—]\\s*`9`")

// readmeKeymapTokens returns every key documented in README.md's keybinding
// tables, plus the number of tables it parsed. See the test's doc comment
// for the parsing rules and why they are the ones that survive editing.
func readmeKeymapTokens(t *testing.T) (map[string]map[string]bool, int) {
	t.Helper()

	// Located relative to this source file: `go test` runs with the
	// package directory as its working directory, but that is a promise
	// about the test binary, not about the repository layout.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test's own source file")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	out := map[string]map[string]bool{}
	tables := 0
	inSection := false
	skipping := false
	inTable := false
	section := ""

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			inSection = trimmed == "## Keybindings"
			skipping = false
			inTable = false
			continue
		}
		if !inSection {
			continue
		}
		// The one out-of-scope subsection: help.Model owns those keys and
		// they never pass through helpSections.
		if strings.HasPrefix(trimmed, "### ") {
			heading := strings.TrimPrefix(trimmed, "### ")
			skipping = strings.HasPrefix(heading, "Help overlay")
			section = canonicalSection(heading)
			inTable = false
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			inTable = false
			continue
		}

		cell := firstCell(trimmed)
		// A table is a key table only if it says so in its header. That is
		// what keeps the terminal-settings table, the [keys] "Wired?"
		// table and the compose_editing table out without naming them.
		if cell == "Key" {
			if !skipping && section == "" {
				t.Errorf("a key table appears under a heading this test " +
					"cannot attribute to a keymap section — teach " +
					"canonicalSection about it, or the keys in it go " +
					"unchecked")
			}
			inTable = !skipping && section != ""
			if inTable {
				tables++
			}
			continue
		}
		if !inTable || isSeparatorRow(trimmed) {
			continue
		}

		cell = digitRange.ReplaceAllString(cell, "`1-9`")

		// Double-backtick spans first, and removed from the cell before the
		// single-span pass: leaving them in means matching their inner
		// backticks as the delimiters of a span that is not there.
		var spans [][]string
		cell = doubleCodeSpan.ReplaceAllStringFunc(cell, func(match string) string {
			spans = append(spans, doubleCodeSpan.FindStringSubmatch(match))
			return " "
		})
		spans = append(spans, codeSpan.FindAllStringSubmatch(cell, -1)...)

		// One code span is one key: the README always backticks each key
		// separately, including in runs like "`h`/`l`/`j`/`k`".
		for _, span := range spans {
			k := normalizeKeyToken(span[1])
			if k == "" {
				continue
			}
			if out[k] == nil {
				out[k] = map[string]bool{}
			}
			out[k][section] = true
		}
	}
	return out, tables
}

// firstCell returns the first cell of a markdown table row.
func firstCell(row string) string {
	cells := strings.Split(strings.Trim(row, "|"), "|")
	if len(cells) == 0 {
		return ""
	}
	return strings.TrimSpace(cells[0])
}

// isSeparatorRow reports whether a table row is the |---|---| rule.
func isSeparatorRow(row string) bool {
	return strings.Trim(row, "|-: \t") == ""
}

// normalizeKeyToken puts a key from either source into one spelling.
//
// Case is folded for named keys ("Ctrl+C", "PgDn", "Shift+Enter") but NOT
// for single characters, where case is the binding: G is not g, N is not
// n, and A/O/D are their own vi commands.
func normalizeKeyToken(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if alias, ok := keyTokenAliases[key]; ok {
		return alias
	}
	if len([]rune(key)) > 1 {
		key = strings.ToLower(key)
	}
	if alias, ok := keyTokenAliases[key]; ok {
		return alias
	}
	return key
}

// keyTokenAliases maps the spellings the README uses to the ones
// Key.Keystroke() — and therefore helpSections — produces.
var keyTokenAliases = map[string]string{
	"↓": "down", "↑": "up", "←": "left", "→": "right",
	"pgdn": "pgdown", "pagedown": "pgdown", "pageup": "pgup",
	"return": "enter", "escape": "esc",
}
