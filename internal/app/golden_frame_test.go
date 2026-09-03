package app

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ceesaxp/telegram-cli/internal/ui/golden"
)

// updateGoldens regenerates the fixtures from the renderer instead of
// asserting against them.
//
//	go test ./internal/app -run TestFrameMatchesTheGoldens -update
//
// It is a deliberate, separate action rather than something a failing test
// offers to do for you. A fixture is the drawing the design was signed off
// against; regenerating one is how a copy change lands, and it is also how
// a layout bug becomes the expected output. Every regenerated cell wants
// looking at, which is why this prints the rows it changed.
var updateGoldens = flag.Bool("update", false, "regenerate docs/fixtures from the renderer")

// goldenFrames are the fixtures rendered from the shared scene, with the
// rail's own three at the widths that draw it.
var goldenFrames = []struct {
	fixture string
	w, h    int
	rail    bool
	scene   func() scene
}{
	{"frame-80x24", 80, 24, false, mainScene},
	{"frame-100x30", 100, 30, false, mainScene},
	{"frame-120x40", 120, 40, true, mainScene},
	{"frame-137x29", 137, 29, true, mainScene},
	{"frame-200x60", 200, 60, true, mainScene},
	{"wide-runes-120x40", 120, 40, true, wideScene},
}

// TestFrameMatchesTheGoldens is the design contract: the frame this client
// draws is the frame docs/fixtures holds, cell for cell and byte for byte.
//
// The width half of this has been asserted since the harness landed and
// must never be regenerated away. The CONTENT half is what this adds, and
// it is why the scene beside it is fixed down to the clock: a frame whose
// timestamps move cannot be compared to anything.
func TestFrameMatchesTheGoldens(t *testing.T) {
	dir, err := golden.Dir()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range goldenFrames {
		t.Run(tc.fixture, func(t *testing.T) {
			f, err := golden.Load(filepath.Join(dir, tc.fixture+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			if f.Width != tc.w || f.Height != tc.h {
				t.Fatalf("fixture is %d×%d, the test renders %d×%d",
					f.Width, f.Height, tc.w, tc.h)
			}

			view := buildScene(t, tc.scene(), tc.w, tc.h, tc.rail).View().Content

			if *updateGoldens {
				before := append([]string(nil), f.Lines...)
				if err := f.Update(view); err != nil {
					t.Fatal(err)
				}
				for i := range f.Lines {
					if i < len(before) && before[i] != f.Lines[i] {
						t.Logf("row %d\n  was |%s|\n  now |%s|", i+1, before[i], f.Lines[i])
					}
				}
				return
			}

			for _, d := range f.Compare(view) {
				t.Errorf("%s: %s", tc.fixture, d)
			}
		})
	}
}

// TestTheSceneRendersTheSameTwice is what makes the fixtures assertable at
// all. Every relative label on the frame — the top bar's clock, the chat
// list's "2m", the thread's day dividers — is measured against a clock, and
// a scene that read the real one would render a different frame every
// minute and be regenerated into agreement with itself.
func TestTheSceneRendersTheSameTwice(t *testing.T) {
	for _, tc := range goldenFrames {
		first := buildScene(t, tc.scene(), tc.w, tc.h, tc.rail).View().Content
		second := buildScene(t, tc.scene(), tc.w, tc.h, tc.rail).View().Content
		if first != second {
			t.Fatalf("%s renders differently on a second pass", tc.fixture)
		}
	}
}

// TestTheSceneIsNotTheWallClock. The pin has to actually take: without it
// the frames would still agree with each other within one run and disagree
// with the fixtures on the next.
func TestTheSceneIsNotTheWallClock(t *testing.T) {
	view := buildScene(t, mainScene(), 100, 30, false).View().Content
	if !strings.Contains(view, sceneNow.Format("15:04")) {
		t.Fatalf("the top bar does not show the pinned clock %s", sceneNow.Format("15:04"))
	}
	if strings.Contains(view, time.Now().Format("15:04")) &&
		time.Now().Format("15:04") != sceneNow.Format("15:04") {
		t.Fatal("the top bar shows the wall clock")
	}
}

// TestTheReadmeScreenshotIsAFixture.
//
// The README's screenshot used to be hand-drawn, and had drifted: absolute
// times where the client draws relative ones, a header missing two fields,
// a hint bar missing two counts. A picture of a program that is maintained
// by hand is a picture that goes stale, and the one in the README is the
// first thing anybody sees.
//
// It is frame-80x24 verbatim now, which is asserted against the renderer
// above — so this only has to check that the two have not been allowed to
// disagree.
func TestTheReadmeScreenshotIsAFixture(t *testing.T) {
	dir, err := golden.Dir()
	if err != nil {
		t.Fatal(err)
	}
	f, err := golden.Load(filepath.Join(dir, "frame-80x24.txt"))
	if err != nil {
		t.Fatal(err)
	}

	readme, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	// Trailing blanks are not visible in a fixture and are noise in a
	// markdown block, so the README carries the rows right-trimmed.
	for i, row := range f.Lines {
		want := strings.TrimRight(row, " ")
		if !strings.Contains(string(readme), want) {
			t.Fatalf("README is missing row %d of frame-80x24:\n  %q", i+1, want)
		}
	}
}
