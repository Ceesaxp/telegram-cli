package layout

import "testing"

// TestGoldenFrameGeometry pins Compute against the region widths measured
// from docs/fixtures. These are not invented numbers: each row is the rule
// column positions read out of the corresponding golden file, which is the
// acceptance artifact for the frame.
func TestGoldenFrameGeometry(t *testing.T) {
	tests := []struct {
		name               string
		w, h               int
		list, thread, rail int
		topBar, hintBar    bool
		bodyHeight         int
	}{
		{"frame-80x24", 80, 24, 30, 49, 0, true, true, 22},
		{"frame-100x30", 100, 30, 38, 61, 0, true, true, 28},
		{"frame-120x40", 120, 40, 38, 50, 30, true, true, 38},
		{"frame-137x29", 137, 29, 38, 67, 30, true, true, 27},
		{"frame-200x60", 200, 60, 38, 130, 30, true, true, 58},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := Compute(tc.w, tc.h, true)

			if l.ChatListWidth != tc.list {
				t.Errorf("ChatListWidth = %d, want %d", l.ChatListWidth, tc.list)
			}
			if l.ThreadWidth != tc.thread {
				t.Errorf("ThreadWidth = %d, want %d", l.ThreadWidth, tc.thread)
			}
			if l.RailWidth != tc.rail {
				t.Errorf("RailWidth = %d, want %d", l.RailWidth, tc.rail)
			}
			if l.TopBar != tc.topBar || l.HintBar != tc.hintBar {
				t.Errorf("chrome = (top %v, hint %v), want (%v, %v)",
					l.TopBar, l.HintBar, tc.topBar, tc.hintBar)
			}
			if l.BodyHeight != tc.bodyHeight {
				t.Errorf("BodyHeight = %d, want %d", l.BodyHeight, tc.bodyHeight)
			}
		})
	}
}

// TestRegionsAlwaysSumToWidth is the invariant that keeps the frame from
// shearing: whatever the thresholds do, the columns plus their rules must
// account for every cell of the terminal. A frame one cell short is exactly
// the bug the goldens exist to catch, and it is cheaper to catch here.
func TestRegionsAlwaysSumToWidth(t *testing.T) {
	for w := 1; w <= 400; w++ {
		for _, h := range []int{1, 8, 12, 19, 20, 24, 60} {
			for _, rail := range []bool{false, true} {
				l := Compute(w, h, rail)
				if got := l.TotalWidth(); got != w {
					t.Fatalf("Compute(%d, %d, rail=%v): regions sum to %d, want %d "+
						"(list %d, thread %d, rail %d)",
						w, h, rail, got, w, l.ChatListWidth, l.ThreadWidth, l.RailWidth)
				}
			}
		}
	}
}

// TestThreadNeverGoesNegative: the thread absorbs the remainder, so a
// threshold mistake shows up here as a negative width rather than as a panic
// deep inside a renderer.
func TestThreadNeverGoesNegative(t *testing.T) {
	for w := 1; w <= 400; w++ {
		for _, rail := range []bool{false, true} {
			if l := Compute(w, 24, rail); l.ThreadWidth < 0 {
				t.Fatalf("Compute(%d, 24, rail=%v): ThreadWidth = %d", w, rail, l.ThreadWidth)
			}
		}
	}
}

// TestWidthPrecedence encodes the rule that the thread is what survives:
// the rail goes first, then the chat list's extra cells, then the second
// column. Crossing each boundary must give the thread more, never less.
func TestWidthPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		w          int
		list, rail int
	}{
		{"rail fits", 118, ChatListWide, RailWidth},
		{"one cell too narrow for the rail", 117, ChatListWide, 0},
		{"wide list still fits", 90, ChatListWide, 0},
		{"one cell too narrow for the wide list", 89, ChatListNarrow, 0},
		{"two panels still fit", 72, ChatListNarrow, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := Compute(tc.w, 24, true)
			if l.SinglePanel {
				t.Fatal("unexpectedly single-panel")
			}
			if l.ChatListWidth != tc.list {
				t.Errorf("ChatListWidth = %d, want %d", l.ChatListWidth, tc.list)
			}
			if l.RailWidth != tc.rail {
				t.Errorf("RailWidth = %d, want %d", l.RailWidth, tc.rail)
			}
		})
	}

	// Dropping the rail must hand its cells to the thread, not lose them.
	wide, narrow := Compute(118, 24, true), Compute(117, 24, true)
	if narrow.ThreadWidth <= wide.ThreadWidth {
		t.Errorf("thread got %d cells at 117 but %d at 118; dropping the rail "+
			"must widen the thread", narrow.ThreadWidth, wide.ThreadWidth)
	}
}

func TestSinglePanelBelow72(t *testing.T) {
	l := Compute(71, 24, true)
	if !l.SinglePanel {
		t.Fatal("71 columns should be single-panel")
	}
	if l.RailWidth != 0 {
		t.Errorf("RailWidth = %d in single-panel mode, want 0", l.RailWidth)
	}
	if l.ChatListWidth != 71 || l.ThreadWidth != 71 {
		t.Errorf("single panel regions = (%d, %d), want both 71",
			l.ChatListWidth, l.ThreadWidth)
	}
}

// TestRailPreferenceIsHonoured: width can force the rail off, but never on.
// A user who turned it off does not get it back by widening the terminal.
func TestRailPreferenceIsHonoured(t *testing.T) {
	if l := Compute(200, 60, false); l.RailWidth != 0 {
		t.Errorf("rail shown at 200 columns despite being disabled")
	}
	if l := Compute(200, 60, true); l.RailWidth != RailWidth {
		t.Errorf("rail hidden at 200 columns despite being enabled")
	}
	// Disabled: those cells go to the thread.
	on, off := Compute(200, 60, true), Compute(200, 60, false)
	if off.ThreadWidth != on.ThreadWidth+RailWidth+RuleWidth {
		t.Errorf("disabling the rail gave the thread %d cells, want %d",
			off.ThreadWidth-on.ThreadWidth, RailWidth+RuleWidth)
	}
}

// TestHeightPrecedence covers the three vertical regimes and, more
// importantly, that BodyHeight accounts for exactly the chrome rows drawn.
func TestHeightPrecedence(t *testing.T) {
	tests := []struct {
		name            string
		h               int
		topBar, hintBar bool
		inlineOnly      bool
		body            int
	}{
		{"full frame", 24, true, true, false, 22},
		{"exactly full", 20, true, true, false, 18},
		{"hint bar dropped", 19, true, false, true, 18},
		{"still has a top bar", 12, true, false, true, 11},
		{"thread only", 11, false, false, true, 11},
		{"absurdly short", 1, false, false, true, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := Compute(100, tc.h, true)
			if l.TopBar != tc.topBar || l.HintBar != tc.hintBar {
				t.Errorf("chrome = (top %v, hint %v), want (%v, %v)",
					l.TopBar, l.HintBar, tc.topBar, tc.hintBar)
			}
			if l.InlineComposerOnly != tc.inlineOnly {
				t.Errorf("InlineComposerOnly = %v, want %v",
					l.InlineComposerOnly, tc.inlineOnly)
			}
			if l.BodyHeight != tc.body {
				t.Errorf("BodyHeight = %d, want %d", l.BodyHeight, tc.body)
			}
		})
	}
}

// TestBodyHeightMatchesChromeExactly: every row is either chrome or body, so
// the accounting has to be exact for any height at all.
func TestBodyHeightMatchesChromeExactly(t *testing.T) {
	for h := 1; h <= 120; h++ {
		l := Compute(100, h, true)
		chrome := 0
		if l.TopBar {
			chrome++
		}
		if l.HintBar {
			chrome++
		}
		if l.BodyHeight+chrome != h {
			t.Fatalf("height %d: body %d + chrome %d != %d",
				h, l.BodyHeight, chrome, h)
		}
	}
}
