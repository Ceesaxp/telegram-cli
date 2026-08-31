package palette

import "testing"

// One spelling per action. The palette took the arrows AND ctrl+p/ctrl+n,
// which is two ways to do one thing on a list that is never more than a few
// entries tall — and a second set of bindings to keep documented, tested and
// in agreement with the first.
//
// The arrows are the ones that stay: divergence 9 already settled that this
// is a text surface and so cannot take j/k, which makes the arrows the
// deliberate choice rather than the leftover one.
func TestTheEmacsChordsDoNotNavigate(t *testing.T) {
	const (
		up    = "\x1b[A"
		down  = "\x1b[B"
		ctrlP = "\x10"
		ctrlN = "\x0e"
	)

	for _, tc := range []struct{ name, chord string }{
		{"ctrl+p", ctrlP},
		{"ctrl+n", ctrlN},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := openPalette(t)

			// Off both ends first. At index 0 an "up" is clamped and at the
			// last entry a "down" is, so a chord tested from either end is
			// inert whether or not it is bound — which is exactly how this
			// test passed against a palette that still had the chord.
			m, _ = m.Update(decodeKey(t, down))
			if m.cursor == 0 {
				t.Fatal("precondition: the list is too short to move within")
			}
			before := m.cursor

			m, _ = m.Update(decodeKey(t, tc.chord))
			if m.cursor != before {
				t.Errorf("%s moved the selection from %d to %d",
					tc.name, before, m.cursor)
			}
		})
	}
}

// The control case: the arrows still do, in both directions.
func TestTheArrowsNavigate(t *testing.T) {
	m := openPalette(t)
	before := m.cursor

	m, _ = m.Update(decodeKey(t, "\x1b[B"))
	if m.cursor == before {
		t.Fatalf("down did not move the selection off %d", before)
	}
	down := m.cursor

	m, _ = m.Update(decodeKey(t, "\x1b[A"))
	if m.cursor == down {
		t.Errorf("up did not move the selection back off %d", down)
	}
}
