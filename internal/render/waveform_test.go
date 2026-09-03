package render

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/charmbracelet/x/ansi"
)

// speech is a plausible voice note: a few hundred samples with peaks in
// them, which is the shape the bar has to survive being squeezed into two
// dozen cells.
func speech(n int) []byte {
	samples := make([]byte, n)
	for i := range samples {
		samples[i] = byte((i * 7) % 32)
	}
	return samples
}

func TestWaveformBarIsExactlyAsWideAsAsked(t *testing.T) {
	for _, samples := range [][]byte{speech(300), speech(24), speech(5), {0}} {
		for width := 1; width <= 40; width++ {
			bar := waveformBar(samples, width)
			if got := cell.Width(bar); got != width {
				t.Fatalf("%d samples at width %d: bar is %d cells: %q",
					len(samples), width, got, bar)
			}
		}
	}
}

// TestWaveformBarTakesThePeaks, not the mean. A voice note is a few hundred
// samples squeezed into two dozen cells, and averaging flattens speech into
// an even grey band.
func TestWaveformBarTakesThePeaks(t *testing.T) {
	// One loud sample in a hundred quiet ones. Averaged it disappears, and
	// it sits in the MIDDLE of the cell that covers it, so a bar that read
	// one sample per cell would miss it too.
	samples := make([]byte, 100)
	samples[53] = 31

	bar := waveformBar(samples, waveformCells)
	if !strings.Contains(bar, "█") {
		t.Fatalf("the peak was averaged away: %q", bar)
	}
	if strings.Count(bar, "█") != 1 {
		t.Fatalf("one loud sample became %d loud cells: %q", strings.Count(bar, "█"), bar)
	}
}

// TestAQuietSampleStillDrawsABlock. The bar is a shape with a baseline, and
// a gap in the middle of it reads as the end of the bar rather than as a
// quiet moment.
func TestAQuietSampleStillDrawsABlock(t *testing.T) {
	bar := waveformBar(make([]byte, 24), waveformCells)
	if strings.Contains(bar, " ") {
		t.Fatalf("silence drew a gap: %q", bar)
	}
	if cell.Width(bar) != waveformCells {
		t.Fatalf("bar is %d cells: %q", cell.Width(bar), bar)
	}
}

// TestWaveformBarStretchesWhenThereAreFewerSamplesThanCells rather than
// dropping to silence for the cells it has nothing for.
func TestWaveformBarStretchesWhenThereAreFewerSamplesThanCells(t *testing.T) {
	bar := waveformBar([]byte{31, 31, 31}, waveformCells)
	if strings.Count(bar, "█") != waveformCells {
		t.Fatalf("three loud samples over %d cells drew %q", waveformCells, bar)
	}
}

// TestWaveformBarSpansItsRange: the quietest sample draws the shortest
// block and the loudest the tallest, so the bar uses the height it has.
func TestWaveformBarSpansItsRange(t *testing.T) {
	bar := waveformBar([]byte{0, 31}, 2)
	blocks := []rune(waveformBlocks)
	want := string(blocks[0]) + string(blocks[len(blocks)-1])
	if bar != want {
		t.Fatalf("bar = %q, want %q", bar, want)
	}
}

// TestWaveformBarClampsOutOfRangeSamples. Nothing in the decoder can
// produce one, but the bar indexes an array with it.
func TestWaveformBarClampsOutOfRangeSamples(t *testing.T) {
	bar := waveformBar([]byte{255, 200, 32}, 3)
	if cell.Width(bar) != 3 {
		t.Fatalf("bar = %q", bar)
	}
	if strings.Count(bar, "█") != 3 {
		t.Fatalf("an over-range sample did not clamp to the tallest block: %q", bar)
	}
}

func TestNoSamplesDrawNoBar(t *testing.T) {
	if got := waveformBar(nil, waveformCells); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if got := waveformBar(speech(24), 0); got != "" {
		t.Fatalf("at zero width, got %q, want empty", got)
	}
}

// TestAVoiceNoteWithAWaveformIsOneRow. The bar is the card's subject, and a
// three-row frame around it says less than the bar already does.
func TestAVoiceNoteWithAWaveformIsOneRow(t *testing.T) {
	card, ok := mediaCardFor(&telegram.MessageVoiceNote{
		VoiceNote: &telegram.VoiceNote{
			Duration: 47,
			File:     &telegram.File{ID: "v", Size: 34_000},
			Waveform: speech(300),
		},
	})
	if !ok {
		t.Fatal("a voice note must produce a card")
	}

	for _, width := range []int{40, 60, 100, 140} {
		lines := card.render(testRoles(), width)
		if len(lines) != 1 {
			t.Fatalf("at width %d the voice note took %d rows", width, len(lines))
		}
		plain := ansi.Strip(lines[0])
		if !strings.ContainsAny(plain, waveformBlocks) {
			t.Fatalf("at width %d there is no waveform: %q", width, plain)
		}
		if !strings.Contains(plain, "0:47") {
			t.Fatalf("at width %d the duration is missing: %q", width, plain)
		}
		if strings.Contains(plain, "voice note") {
			t.Fatalf("at width %d the bar was labelled as well as drawn: %q", width, plain)
		}
	}
}

// TestAVoiceNoteFitsAndClosesItsStyles at every width a pane can be, with
// and without amplitudes — the two forms are different code paths.
func TestAVoiceNoteFitsAndClosesItsStyles(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	messages := map[string]telegram.MessageContent{
		"with a waveform": &telegram.MessageVoiceNote{VoiceNote: &telegram.VoiceNote{
			Duration: 47, File: &telegram.File{ID: "v", Size: 34_000}, Waveform: speech(300),
		}},
		"without one": &telegram.MessageVoiceNote{VoiceNote: &telegram.VoiceNote{
			Duration: 12, File: &telegram.File{ID: "v"},
		}},
	}

	for width := 1; width <= 140; width++ {
		for name, content := range messages {
			msg := &telegram.Message{ID: 1, Content: content}
			for i, line := range r.RenderBody(msg, st, BodyOptions{Width: width}) {
				if got := cell.Width(line); got > width {
					t.Fatalf("%s at width %d: line %d is %d cells: %q",
						name, width, i, got, ansi.Strip(line))
				}
				if open := cell.OpenStyle(line); open != "" {
					t.Fatalf("%s at width %d: line %d leaves %q open", name, width, i, open)
				}
			}
		}
	}
}

// TestANarrowVoiceNoteKeepsItsDuration. The bar shortens; the duration is
// the fact a reader cannot recover from anything else on the row.
func TestANarrowVoiceNoteKeepsItsDuration(t *testing.T) {
	card, _ := mediaCardFor(&telegram.MessageVoiceNote{
		VoiceNote: &telegram.VoiceNote{Duration: 47, Waveform: speech(300)},
	})
	for width := 10; width <= 40; width++ {
		plain := ansi.Strip(card.render(testRoles(), width)[0])
		if !strings.Contains(plain, "0:47") {
			t.Fatalf("at width %d: %q", width, plain)
		}
	}
}
