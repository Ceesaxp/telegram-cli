package golden

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- The fixtures themselves -------------------------------------------
//
// Until a renderer exists to regenerate them, docs/fixtures is
// hand-maintained, which means it can drift the same way a hand-written doc
// can. These tests are the safety net: they are what caught (and now
// prevent) the ZWJ padding defect found when the fixtures were first
// reviewed.

// TestFixturesAreCellExact is the load-bearing test in this file. Every
// fixture must satisfy its own header: the declared number of rows, every
// row exactly the declared width, no control characters anywhere.
//
// A failure here means a golden is wrong, not that a renderer is wrong —
// which is why it is worth having before any renderer exists.
func TestFixturesAreCellExact(t *testing.T) {
	for _, f := range loadFixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			for _, d := range f.Validate() {
				t.Errorf("%s", d)
			}
		})
	}
}

// TestFixtureSetIsComplete pins the fixture set named by decision 11. A
// fixture that is deleted or renamed without updating the design record
// would otherwise silently stop being asserted — the frame sizes are an
// acceptance contract, not an arbitrary sample.
func TestFixtureSetIsComplete(t *testing.T) {
	want := map[string]struct{ w, h int }{
		"frame-80x24":       {80, 24},
		"frame-100x30":      {100, 30},
		"frame-120x40":      {120, 40},
		"frame-137x29":      {137, 29},
		"frame-200x60":      {200, 60},
		"wide-runes-120x40": {120, 40},
		"blocks-100x52":     {100, 52},
	}

	got := map[string]struct{ w, h int }{}
	for _, f := range loadFixtures(t) {
		got[f.Name] = struct{ w, h int }{f.Width, f.Height}
	}

	for name, dim := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("fixture %q is missing; decision 11 names it as an acceptance artifact", name)
			continue
		}
		if g != dim {
			t.Errorf("fixture %q is %dx%d, want %dx%d", name, g.w, g.h, dim.w, dim.h)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected fixture %q: add it to this list and to docs/tui-2.0.md, "+
				"or delete it", name)
		}
	}
}

// TestZWJFamilyEmojiIsTwoCells guards the specific measurement mistake that
// produced the only defect found in the original fixtures: the row holding
// this emoji had been padded as if it were 8 cells wide, the width you get
// by summing runewidth.RuneWidth over its four runes plus joiners.
//
// Every grapheme-cluster-aware measurement says 2. If this test ever fails,
// the layout code is about to shear on emoji and no amount of regenerating
// fixtures will fix it.
func TestZWJFamilyEmojiIsTwoCells(t *testing.T) {
	const family = "\U0001F468‍\U0001F469‍\U0001F467‍\U0001F466"
	if got := Width(family); got != 2 {
		t.Errorf("Width(ZWJ family emoji) = %d, want 2 — the layout must measure "+
			"grapheme clusters, never sum per-rune widths", got)
	}
}

// --- Parsing ------------------------------------------------------------

func TestParseRoundTrips(t *testing.T) {
	for _, f := range loadFixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			reparsed, err := parse(f.Path, string(f.Bytes()))
			if err != nil {
				t.Fatalf("re-parsing our own output failed: %v", err)
			}
			if reparsed.Width != f.Width || reparsed.Height != f.Height {
				t.Errorf("round-trip changed dimensions: %dx%d -> %dx%d",
					f.Width, f.Height, reparsed.Width, reparsed.Height)
			}
			if strings.Join(reparsed.Lines, "\n") != strings.Join(f.Lines, "\n") {
				t.Error("round-trip changed the frame content")
			}
		})
	}
}

// TestParseRejectsCorruptFixtures covers the failure modes a lenient parser
// would wave through. A fixture that has quietly lost a row is the worst of
// them: the frame would still look plausible, and every assertion built on
// it would be measuring the wrong thing.
func TestParseRejectsCorruptFixtures(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string // substring the error should mention
	}{
		{
			name: "no header",
			body: "not a header\n" + rule(4) + "\nabcd\n" + rule(4) + "\n",
			want: "not a dimension header",
		},
		{
			name: "header name disagrees with file name",
			body: "# other.txt  (4×1)\n# x\n" + rule(4) + "\nabcd\n" + rule(4) + "\n",
			want: "header names",
		},
		{
			name: "file name dimensions disagree with header",
			body: "# frame-9x9.txt  (4×1)\n# x\n" + rule(4) + "\nabcd\n" + rule(4) + "\n",
			want: "file name says",
		},
		{
			name: "missing opening rule",
			body: "# frame-4x1.txt  (4×1)\n# x\nabcd\n" + rule(4) + "\n",
			want: "expected a",
		},
		{
			name: "lost a row",
			body: "# frame-4x2.txt  (4×2)\n# x\n" + rule(4) + "\nabcd\n" + rule(4) + "\n",
			want: "declares 2 rows",
		},
		{
			name: "missing closing rule",
			body: "# frame-4x1.txt  (4×1)\n# x\n" + rule(4) + "\nabcd\nefgh\n",
			want: "expected a closing rule",
		},
		{
			name: "trailing junk after the closing rule",
			body: "# frame-4x1.txt  (4×1)\n# x\n" + rule(4) + "\nabcd\n" + rule(4) + "\nstray\n",
			want: "unexpected line",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name := "frame-4x1.txt"
			if strings.Contains(tc.body, "frame-4x2") {
				name = "frame-4x2.txt"
			}
			if strings.Contains(tc.body, "frame-9x9") {
				name = "frame-9x9.txt"
			}
			_, err := parse(name, tc.body)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// --- Comparison ---------------------------------------------------------

// fix builds a small in-memory fixture for the comparison tests.
func fix(t *testing.T, width int, lines ...string) *Fixture {
	t.Helper()
	return &Fixture{
		Name:  fmt.Sprintf("frame-%dx%d", width, len(lines)),
		Path:  "in-memory",
		Width: width, Height: len(lines),
		Lines: lines,
	}
}

func TestCompareAcceptsExactMatch(t *testing.T) {
	f := fix(t, 4, "abcd", "efgh")
	if diffs := f.Compare("abcd\nefgh"); len(diffs) != 0 {
		t.Errorf("expected no diffs, got %v", diffs)
	}
}

// TestCompareStripsANSI is the reason fixtures can stay plain text: real
// rendered output is styled, and the goldens carry geometry only.
func TestCompareStripsANSI(t *testing.T) {
	f := fix(t, 4, "abcd")
	styled := "\x1b[38;5;73mabcd\x1b[0m"
	if diffs := f.Compare(styled); len(diffs) != 0 {
		t.Errorf("styled output that matches after stripping should pass, got %v", diffs)
	}
}

// TestCompareToleratesTrailingNewline: a View() that ends its last row with
// a newline and one that does not must not disagree about row count.
func TestCompareToleratesTrailingNewline(t *testing.T) {
	f := fix(t, 4, "abcd")
	for _, in := range []string{"abcd", "abcd\n"} {
		if diffs := f.Compare(in); len(diffs) != 0 {
			t.Errorf("Compare(%q) = %v, want no diffs", in, diffs)
		}
	}
}

func TestCompareDetectsWidth(t *testing.T) {
	f := fix(t, 4, "abcd")
	diffs := f.Compare("abc") // one cell short
	if len(diffs) == 0 {
		t.Fatal("expected a width diff")
	}
	if diffs[0].Kind != DiffWidth {
		t.Errorf("Kind = %v, want %v", diffs[0].Kind, DiffWidth)
	}
	if diffs[0].GotWidth != 3 || diffs[0].WantWidth != 4 {
		t.Errorf("widths = %d/%d, want 3/4", diffs[0].GotWidth, diffs[0].WantWidth)
	}
}

// TestCompareDetectsWideRuneOverflow is the failure this whole harness
// exists to catch: text that is the right number of *runes* but the wrong
// number of *cells*. Four CJK ideographs are 4 runes and 8 cells.
func TestCompareDetectsWideRuneOverflow(t *testing.T) {
	f := fix(t, 4, "abcd")
	diffs := f.Compare("四字熟語")
	if len(diffs) == 0 || diffs[0].Kind != DiffWidth {
		t.Fatalf("expected a width diff for 4 wide runes in a 4-cell frame, got %v", diffs)
	}
	if diffs[0].GotWidth != 8 {
		t.Errorf("GotWidth = %d, want 8", diffs[0].GotWidth)
	}
}

// TestCompareAcceptsWideRunesThatFitExactly is the positive half of the
// rune-vs-cell distinction: two CJK ideographs are 2 runes but exactly 4
// cells, so they legitimately fill a 4-cell frame. A harness that counted
// runes would reject this row and accept the 4-rune one above — precisely
// backwards.
func TestCompareAcceptsWideRunesThatFitExactly(t *testing.T) {
	f := fix(t, 4, "四字")
	if diffs := f.Compare("四字"); len(diffs) != 0 {
		t.Errorf("2 wide runes exactly fill a 4-cell frame, got %v", diffs)
	}
}

func TestCompareDetectsRowCount(t *testing.T) {
	f := fix(t, 4, "abcd", "efgh")
	diffs := f.Compare("abcd")
	if len(diffs) == 0 || diffs[0].Kind != DiffRowCount {
		t.Fatalf("expected a row-count diff, got %v", diffs)
	}
}

// TestCompareChecksRemainingRowsAfterRowCountMismatch: a frame that lost a
// row should still report which of its surviving rows are mis-sized,
// instead of hiding them behind the count failure.
func TestCompareChecksRemainingRowsAfterRowCountMismatch(t *testing.T) {
	f := fix(t, 4, "abcd", "efgh")
	diffs := f.Compare("abc") // one row, and that row is too narrow

	var sawCount, sawWidth bool
	for _, d := range diffs {
		switch d.Kind {
		case DiffRowCount:
			sawCount = true
		case DiffWidth:
			sawWidth = true
		}
	}
	if !sawCount || !sawWidth {
		t.Errorf("want both a row-count and a width diff, got %v", diffs)
	}
}

func TestCompareDetectsContent(t *testing.T) {
	f := fix(t, 4, "abcd")
	diffs := f.Compare("wxyz") // right width, wrong text
	if len(diffs) != 1 {
		t.Fatalf("expected exactly one diff, got %v", diffs)
	}
	if diffs[0].Kind != DiffContent {
		t.Errorf("Kind = %v, want %v", diffs[0].Kind, DiffContent)
	}
}

// TestCompareRejectsControlCharacters: a tab has no fixed display width —
// it depends on the terminal's tab stops — so measuring a row containing
// one is meaningless. It is rejected rather than measured, and it suppresses
// the width diff for that row so one cause produces one failure.
func TestCompareRejectsControlCharacters(t *testing.T) {
	f := fix(t, 4, "abcd")
	diffs := f.Compare("ab\tc")
	if len(diffs) != 1 {
		t.Fatalf("expected exactly one diff, got %v", diffs)
	}
	if diffs[0].Kind != DiffControl {
		t.Errorf("Kind = %v, want %v", diffs[0].Kind, DiffControl)
	}
}

// --- Update -------------------------------------------------------------

// TestUpdateWritesCellExactOutput checks the happy path of the -update flag
// that will regenerate goldens once a renderer exists.
func TestUpdateWritesCellExactOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frame-4x2.txt")
	f := &Fixture{Name: "frame-4x2", Path: path, Width: 4, Height: 2,
		Lines: []string{"abcd", "efgh"}}
	if err := os.WriteFile(path, f.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := f.Update("wxyz\n1234"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Update: %v", err)
	}
	if got := strings.Join(reloaded.Lines, "|"); got != "wxyz|1234" {
		t.Errorf("lines = %q, want %q", got, "wxyz|1234")
	}
}

// TestUpdateRefusesNonCellExactOutput is the guard that keeps -update from
// laundering a layout bug into the expected output. The entire value of
// these files is that every line is exactly the frame width; a regeneration
// that accepted a mis-sized row would quietly destroy it.
func TestUpdateRefusesNonCellExactOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frame-4x1.txt")
	f := &Fixture{Name: "frame-4x1", Path: path, Width: 4, Height: 1,
		Lines: []string{"abcd"}}
	if err := os.WriteFile(path, f.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"too narrow":   "abc",
		"too wide":     "abcde",
		"wrong rows":   "abcd\nefgh",
		"control char": "ab\tc",
		// 3 runes, 6 cells, in a 4-cell frame. Note that "四字" would be
		// *accepted* here — 2 runes but exactly 4 cells — which is the
		// whole point of measuring cells rather than runes.
		"wide runes": "四字熟",
	}
	for name, rendered := range cases {
		t.Run(name, func(t *testing.T) {
			if err := f.Update(rendered); err == nil {
				t.Fatal("expected Update to refuse, got nil")
			}
			// The file must be untouched.
			reloaded, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if strings.Join(reloaded.Lines, "|") != "abcd" {
				t.Errorf("refused update still modified the file: %q", reloaded.Lines)
			}
		})
	}
}

// --- Helpers ------------------------------------------------------------

func TestDirResolves(t *testing.T) {
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("Dir() = %q, which does not exist: %v", dir, err)
	}
	if filepath.Base(dir) != "fixtures" {
		t.Errorf("Dir() = %q, want it to end in docs/fixtures", dir)
	}
}

func TestSplitLinesOnEmptyInput(t *testing.T) {
	for _, in := range []string{"", "\n"} {
		if got := SplitLines(in); len(got) != 0 {
			t.Errorf("SplitLines(%q) = %v, want empty", in, got)
		}
	}
}

func loadFixtures(t *testing.T) []*Fixture {
	t.Helper()
	dir, err := Dir()
	if err != nil {
		t.Fatalf("locating fixtures: %v", err)
	}
	fixtures, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("loading fixtures: %v", err)
	}
	return fixtures
}

func rule(n int) string { return strings.Repeat(string(ruleRune), n) }
