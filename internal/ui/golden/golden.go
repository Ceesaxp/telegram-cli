// Package golden loads and asserts against the cell-exact terminal
// fixtures in docs/fixtures — the acceptance artifact for TUI 2.0's frame
// integrity and column alignment (docs/tui-2.0.md, decision 11).
//
// The fixtures exist before the renderer does. That is the point: they turn
// the frame and grid work from "does this look right" into red/green, so a
// row that is one cell too wide fails a test instead of shearing a panel in
// someone's terminal.
//
// Two assertions, deliberately reported separately, because they have
// different lifetimes:
//
//   - Display width. Every rendered row must be exactly the frame width.
//     This must pass from the first commit and must never be regenerated
//     away — a width diff is a layout bug.
//   - Byte equality against the fixture. This is the design contract.
//     Expect to regenerate a fixture when copy changes; never when geometry
//     does.
//
// [Fixture.Compare] returns both kinds tagged by [DiffKind] so a caller can
// gate on the first while tolerating churn in the second.
//
// This package deliberately does not import "testing": it returns values and
// errors so it can also back a regeneration command. Callers in _test.go
// files turn a non-empty []Diff into t.Errorf.
package golden

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
)

// ruleRune is the box-drawing rune used for the delimiter lines that fence
// the frame inside a fixture file. It is not part of the frame.
const ruleRune = '┄'

// headerRe matches the first comment line of a fixture, which carries the
// authoritative dimensions, e.g.
//
//	# frame-80x24.txt  (80×24)
//
// Note the multiplication sign (U+00D7) in the dimensions, where the file
// name uses a plain ASCII "x". Both are parsed and cross-checked in [Load]:
// a fixture whose header and file name disagree is corrupt, and silently
// trusting either one would hide it.
var headerRe = regexp.MustCompile(`^#\s*(\S+?)\.txt\s+\((\d+)×(\d+)\)\s*$`)

// nameRe extracts the dimensions encoded in a fixture's file name.
var nameRe = regexp.MustCompile(`^(.*)-(\d+)x(\d+)$`)

// Fixture is one parsed golden file: a frame of exactly Height lines, each
// exactly Width display cells wide.
type Fixture struct {
	// Name is the file name without its extension, e.g. "frame-80x24".
	Name string
	// Path is the file the fixture was read from.
	Path string
	// Width and Height are the frame's dimensions in cells and rows.
	Width, Height int
	// Lines is the frame itself, with the comment header and the two rule
	// lines removed. Fixtures carry geometry, not colour, so these are
	// plain text with no escape sequences.
	Lines []string
}

// DiffKind classifies a mismatch. Callers gate on this: DiffWidth,
// DiffRowCount, and DiffControl are always bugs in the renderer, while
// DiffContent can legitimately mean the fixture needs regenerating after a
// copy change.
type DiffKind int

const (
	// DiffRowCount means the rendered output had the wrong number of rows.
	DiffRowCount DiffKind = iota
	// DiffWidth means a row's display width was not the frame width. This
	// is the assertion that must pass from day one.
	DiffWidth
	// DiffContent means a row's text did not match the fixture.
	DiffContent
	// DiffControl means a row contained a control character. Tabs and
	// carriage returns make "display width" meaningless — a tab's width
	// depends on the terminal's tab stops, not on the string — so they are
	// rejected outright rather than measured.
	DiffControl
)

func (k DiffKind) String() string {
	switch k {
	case DiffRowCount:
		return "row-count"
	case DiffWidth:
		return "width"
	case DiffContent:
		return "content"
	case DiffControl:
		return "control-char"
	}
	return "unknown"
}

// Diff is a single mismatch between rendered output and a fixture.
type Diff struct {
	Kind DiffKind
	// Row is the 1-indexed frame row, or 0 for a whole-frame problem such
	// as DiffRowCount.
	Row int
	// Got and Want are the rendered and expected rows, ANSI already
	// stripped. Both are empty for DiffRowCount.
	Got, Want string
	// GotWidth and WantWidth are display widths in cells.
	GotWidth, WantWidth int
}

func (d Diff) String() string {
	switch d.Kind {
	case DiffRowCount:
		return fmt.Sprintf("row count = %d, want %d", d.GotWidth, d.WantWidth)
	case DiffWidth:
		return fmt.Sprintf("row %d: display width = %d, want %d\n  got: %q",
			d.Row, d.GotWidth, d.WantWidth, d.Got)
	case DiffControl:
		return fmt.Sprintf("row %d: contains a control character (tabs and CR have no "+
			"well-defined display width)\n  got: %q", d.Row, d.Got)
	case DiffContent:
		return fmt.Sprintf("row %d:\n  got:  %q\n  want: %q", d.Row, d.Got, d.Want)
	}
	return fmt.Sprintf("row %d: unknown diff", d.Row)
}

// Error lets a Diff be used where an error is expected.
func (d Diff) Error() string { return d.String() }

// Width returns the display width of s in terminal cells.
//
// This deliberately delegates to [cell.Width] rather than measuring
// independently: the harness must agree with the renderer it judges, and
// two measurement implementations would eventually disagree about some
// grapheme and make a real shear look like a passing test.
func Width(s string) int { return cell.Width(s) }

// StripANSI removes escape sequences, leaving the plain text a fixture
// stores. Rendered output is styled; fixtures are not, so every comparison
// goes through this first.
func StripANSI(s string) string { return ansi.Strip(s) }

// Dir returns the absolute path of docs/fixtures, located by walking up
// from the working directory to the module root. Tests run with their
// package directory as the working directory, so a relative path would
// differ per package; this keeps every caller writing golden.Dir().
func Dir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "docs", "fixtures"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("golden: no go.mod found above %s", wd)
		}
		dir = parent
	}
}

// Load reads and parses a single fixture file.
func Load(path string) (*Fixture, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(path, string(raw))
}

// LoadAll reads every .txt fixture in dir, sorted by name for deterministic
// test output.
func LoadAll(dir string) ([]*Fixture, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	out := make([]*Fixture, 0, len(paths))
	for _, p := range paths {
		f, err := Load(p)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("golden: no fixtures found in %s", dir)
	}
	return out, nil
}

// parse turns a fixture file's contents into a Fixture. The format is
// fixed and strict — two comment lines, a rule, exactly Height frame rows,
// a closing rule — because a lenient parser would silently accept a
// fixture that had lost a row, and a missing row is precisely the kind of
// corruption these files exist to detect.
func parse(path, raw string) (*Fixture, error) {
	name := strings.TrimSuffix(filepath.Base(path), ".txt")

	lines := strings.Split(raw, "\n")
	// Files end with a trailing newline, which Split turns into a final
	// empty element. Drop it so it is not mistaken for a frame row.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 4 {
		return nil, fmt.Errorf("golden: %s: too short to be a fixture (%d lines)", path, len(lines))
	}

	m := headerRe.FindStringSubmatch(lines[0])
	if m == nil {
		return nil, fmt.Errorf("golden: %s: first line is not a dimension header: %q", path, lines[0])
	}
	headerName := m[1]
	width, _ := strconv.Atoi(m[2])
	height, _ := strconv.Atoi(m[3])

	if headerName != name {
		return nil, fmt.Errorf("golden: %s: header names %q but the file is %q", path, headerName, name)
	}
	if err := checkNameDimensions(path, name, width, height); err != nil {
		return nil, err
	}

	// Skip the remaining comment lines, then the opening rule.
	i := 1
	for i < len(lines) && strings.HasPrefix(lines[i], "#") {
		i++
	}
	if i >= len(lines) || !isRule(lines[i]) {
		return nil, fmt.Errorf("golden: %s: expected a %q rule after the header, got %q",
			path, string(ruleRune), lineAt(lines, i))
	}
	i++

	if len(lines)-i < height+1 {
		return nil, fmt.Errorf("golden: %s: header declares %d rows but only %d lines remain",
			path, height, len(lines)-i)
	}
	frame := lines[i : i+height]
	i += height

	if !isRule(lines[i]) {
		return nil, fmt.Errorf("golden: %s: expected a closing rule after %d rows, got %q",
			path, height, lines[i])
	}
	if rest := lines[i+1:]; len(rest) != 0 {
		return nil, fmt.Errorf("golden: %s: %d unexpected line(s) after the closing rule",
			path, len(rest))
	}

	return &Fixture{
		Name:   name,
		Path:   path,
		Width:  width,
		Height: height,
		Lines:  frame,
	}, nil
}

// checkNameDimensions cross-checks the dimensions in the file name against
// the ones in the header. Fixtures are regenerated by tooling; a rename
// that forgets the header (or vice versa) would otherwise let a 120-column
// frame be asserted at 100 columns and pass.
func checkNameDimensions(path, name string, width, height int) error {
	m := nameRe.FindStringSubmatch(name)
	if m == nil {
		// A fixture that does not encode dimensions in its name is
		// allowed; the header is authoritative.
		return nil
	}
	nw, _ := strconv.Atoi(m[2])
	nh, _ := strconv.Atoi(m[3])
	if nw != width || nh != height {
		return fmt.Errorf("golden: %s: file name says %dx%d but header says %d×%d",
			path, nw, nh, width, height)
	}
	return nil
}

func isRule(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != ruleRune {
			return false
		}
	}
	return true
}

func lineAt(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return "<end of file>"
	}
	return lines[i]
}

// Validate checks a fixture against its own header: the right number of
// rows, every row exactly Width cells, and no control characters.
//
// This guards the fixtures themselves, which are hand-maintained until the
// renderer can regenerate them. It is what catches a row padded with the
// wrong width rule — the ZWJ-emoji defect found on first review.
func (f *Fixture) Validate() []Diff {
	var diffs []Diff
	if len(f.Lines) != f.Height {
		diffs = append(diffs, Diff{
			Kind: DiffRowCount, GotWidth: len(f.Lines), WantWidth: f.Height,
		})
	}
	for i, line := range f.Lines {
		diffs = append(diffs, checkRow(i+1, line, f.Width)...)
	}
	return diffs
}

// Compare checks rendered output against the fixture. rendered may carry
// ANSI styling and may end with a trailing newline; both are normalised
// away first, since fixtures store geometry rather than colour.
//
// Every row is checked for width even when the row count is wrong, so a
// frame that lost one row still reports which of the remaining rows are
// mis-sized instead of hiding them behind a single count failure.
func (f *Fixture) Compare(rendered string) []Diff {
	got := SplitLines(rendered)

	var diffs []Diff
	if len(got) != f.Height {
		diffs = append(diffs, Diff{
			Kind: DiffRowCount, GotWidth: len(got), WantWidth: f.Height,
		})
	}

	for i, line := range got {
		row := i + 1
		rowDiffs := checkRow(row, line, f.Width)
		diffs = append(diffs, rowDiffs...)

		// A control character invalidates the text comparison the same
		// way it invalidates the width measurement, so it reports once
		// and nothing else is said about that row. A width mismatch does
		// not suppress the content check: those are independent problems
		// and seeing both is useful.
		if hasKind(rowDiffs, DiffControl) {
			continue
		}
		if i < len(f.Lines) && line != f.Lines[i] {
			diffs = append(diffs, Diff{
				Kind: DiffContent, Row: row,
				Got: line, Want: f.Lines[i],
				GotWidth: Width(line), WantWidth: Width(f.Lines[i]),
			})
		}
	}
	return diffs
}

func hasKind(diffs []Diff, kind DiffKind) bool {
	for _, d := range diffs {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

// checkRow applies the per-row invariants shared by Validate and Compare.
func checkRow(row int, line string, want int) []Diff {
	if i := strings.IndexFunc(line, isControl); i >= 0 {
		// Report and stop: a control character makes the width
		// measurement below meaningless, so reporting both would just be
		// a confusing second failure for one cause.
		return []Diff{{Kind: DiffControl, Row: row, Got: line}}
	}
	if w := Width(line); w != want {
		return []Diff{{
			Kind: DiffWidth, Row: row,
			Got: line, GotWidth: w, WantWidth: want,
		}}
	}
	return nil
}

func isControl(r rune) bool {
	// Newlines are handled by the line split before this runs, so any
	// remaining control rune — tab, CR, vertical tab — is a bug.
	return r != '\n' && unicode.IsControl(r)
}

// SplitLines normalises rendered output into frame rows: ANSI stripped, a
// single trailing newline tolerated. A View() that ends with a newline and
// one that does not must not disagree about how many rows it drew.
func SplitLines(rendered string) []string {
	rendered = StripANSI(rendered)
	rendered = strings.TrimSuffix(rendered, "\n")
	if rendered == "" {
		return nil
	}
	return strings.Split(rendered, "\n")
}

// Bytes renders the fixture back into its on-disk form, header and rules
// included. Load(Bytes()) round-trips.
func (f *Fixture) Bytes() []byte {
	rule := strings.Repeat(string(ruleRune), f.Width)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s.txt  (%d×%d)\n", f.Name, f.Width, f.Height)
	fmt.Fprintf(&b, "# every line below is exactly %d display cells wide\n", f.Width)
	b.WriteString(rule + "\n")
	for _, l := range f.Lines {
		b.WriteString(l + "\n")
	}
	b.WriteString(rule + "\n")
	return []byte(b.String())
}

// Update replaces the fixture's frame with rendered output and writes it
// back to disk. It backs the -update flag that regenerates goldens once a
// renderer exists.
//
// It refuses to write output that is not self-consistent — wrong row count,
// a mis-sized row, a stray control character — because the whole value of
// these files is that every line is exactly the frame width. A regeneration
// that laundered a layout bug into the expected output would destroy that.
// Height is taken from the fixture, not from the rendered output, so
// -update cannot quietly resize a frame either.
func (f *Fixture) Update(rendered string) error {
	lines := SplitLines(rendered)

	candidate := &Fixture{
		Name: f.Name, Path: f.Path,
		Width: f.Width, Height: f.Height,
		Lines: lines,
	}
	if diffs := candidate.Validate(); len(diffs) != 0 {
		return fmt.Errorf("golden: refusing to update %s: rendered output is not "+
			"cell-exact (%d problem(s)); first: %s", f.Path, len(diffs), diffs[0])
	}

	if err := os.WriteFile(f.Path, candidate.Bytes(), 0o644); err != nil {
		return err
	}
	f.Lines = lines
	return nil
}
