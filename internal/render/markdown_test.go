package render

import (
	"strings"
	"testing"
	"time"

	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

func newTestRenderer() *MessageRenderer {
	return NewMessageRenderer(theme.DarkTheme())
}

func textMessage(text string, entities []*telegram.TextEntity) *telegram.Message {
	return &telegram.Message{
		ID:       1,
		ChatID:   1,
		SenderID: &telegram.MessageSenderUser{UserID: 42},
		Date:     1700000000,
		Content: &telegram.MessageText{
			Text: &telegram.FormattedText{
				Text:     text,
				Entities: entities,
			},
		},
	}
}

// maxLineWidth returns the widest rendered (ANSI-aware) line width in s.
func maxLineWidth(s string) int {
	max := 0
	for _, line := range strings.Split(s, "\n") {
		if w := ansi.StringWidth(line); w > max {
			max = w
		}
	}
	return max
}

func TestRenderMessage_WrapsLongSingleToken(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	longToken := strings.Repeat("x", 500)
	msg := textMessage(longToken, nil)
	msg.SenderID = nil // no sender header line, keeps the line-count math exact

	const maxWidth = 60
	bubbleW := bubbleWidth(maxWidth)
	innerW := contentInnerWidth(bubbleW)

	// isOwn=false: own messages are right-padded to the full panel
	// maxWidth for alignment (by design), which would make a bubbleW
	// bound fail even for correctly-wrapped content. isOwn=false renders
	// the bubble at its natural width, so bubbleW is the real bound.
	bubble := r.RenderMessage(msg, st, false, false, maxWidth)

	// Bound against the real per-line bubble width (border+padding
	// included), not the outer panel maxWidth — maxWidth is ~35% wider
	// than bubbleW (bubbleW = 65% of maxWidth), so asserting against
	// maxWidth would pass even for a bubble that never wraps at all.
	if got := maxLineWidth(bubble); got > bubbleW {
		t.Fatalf("rendered line width %d exceeds bubble width %d\nbubble:\n%s", got, bubbleW, bubble)
	}

	totalLines := strings.Count(bubble, "\n") + 1
	contentLines := totalLines - 3 // top border + bottom border + 1-line footer
	minExpectedLines := (500 + innerW - 1) / innerW
	if contentLines < minExpectedLines {
		t.Fatalf("expected at least %d content lines wrapping 500 chars at inner width %d, got %d (total lines %d)\nbubble:\n%s",
			minExpectedLines, innerW, contentLines, totalLines, bubble)
	}

	// Regression check: real wrapping must PRESERVE every character. The
	// pre-fix code relied solely on lipgloss's MaxWidth, which truncates a
	// line to the target width instead of wrapping it — that would leave
	// only ~innerW of the 500 'x' characters in the output. This assertion
	// fails under that old behavior and passes only with real wrapping.
	stripped := ansi.Strip(bubble)
	if got := strings.Count(stripped, "x"); got != 500 {
		t.Fatalf("expected all 500 'x' characters to survive wrapping, found %d (consistent with MaxWidth-only truncation dropping the rest)", got)
	}
}

func TestRenderMessage_CJKAndEmojiNoPanic(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	text := strings.Repeat("你好世界🎉こんにちは😀漢字テスト\n", 10)
	msg := textMessage(text, nil)

	const maxWidth = 40
	bubbleW := bubbleWidth(maxWidth)

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("RenderMessage panicked on CJK/emoji input: %v", rec)
		}
	}()

	// isOwn=false for the same reason as TestRenderMessage_WrapsLongSingleToken:
	// own-message right-padding stretches lines to maxWidth by design, which
	// would make a bubbleW bound fail on correct output.
	bubble := r.RenderMessage(msg, st, false, false, maxWidth)
	if bubble == "" {
		t.Fatal("expected non-empty rendered bubble")
	}
	if got := maxLineWidth(bubble); got > bubbleW {
		t.Fatalf("rendered line width %d exceeds bubble width %d", got, bubbleW)
	}
}

// TestRenderMessage_PhotoArtNotReflowed verifies that pre-rendered photo
// art (as produced by renderPhoto/imgRend) is cropped, never word-wrapped.
// ansi.Wrap treats a wide row with no spaces as one long "word" and will
// hard-wrap it across multiple lines, scrambling a block-art grid whose
// rows must stay at fixed column positions. RenderMessage must route art
// around the text wrapper and rely on lipgloss's MaxWidth to crop it
// instead.
func TestRenderMessage_PhotoArtNotReflowed(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	const rowWidth = 55 // wider than the bubble's inner width computed below
	rows := []string{
		"ROW1:" + strings.Repeat("#", rowWidth-5),
		"ROW2:" + strings.Repeat("#", rowWidth-5),
		"ROW3:" + strings.Repeat("#", rowWidth-5),
	}
	fakeArt := strings.Join(rows, "\n")

	const fileID = "fake-photo-file"
	// Prime the renderer's image cache directly so renderPhoto takes the
	// cache-hit path and returns our fake art without touching the
	// filesystem or a real image decoder.
	r.imgCache.Set("img:"+fileID, fakeArt)

	msg := &telegram.Message{
		ID:       1,
		ChatID:   1,
		SenderID: nil, // no sender header line, keeps the line-count math exact
		Date:     1700000000,
		Content: &telegram.MessagePhoto{
			Photo: &telegram.Photo{
				Sizes: []*telegram.PhotoSize{
					{
						Width:  rowWidth,
						Height: len(rows),
						File:   &telegram.File{ID: fileID, Downloaded: true, Path: "/fake/path.jpg"},
					},
				},
			},
		},
	}

	const maxWidth = 54 // bubbleWidth(54)=35, contentInnerWidth(35)=31 < rowWidth
	bubbleW := bubbleWidth(maxWidth)
	innerW := contentInnerWidth(bubbleW)
	if innerW >= rowWidth {
		t.Fatalf("test setup broken: innerW %d must be < rowWidth %d to exercise cropping", innerW, rowWidth)
	}

	// isOwn=false (see TestRenderMessage_WrapsLongSingleToken for why: own
	// messages are right-padded to maxWidth by design). No caption, no
	// sender/forwarded/reply header: the bubble is exactly
	// top-border + N art rows + 1-line footer + bottom-border.
	bubble := r.RenderMessage(msg, st, false, false, maxWidth)

	wantTotalLines := 2 + len(rows) + 1 // border top/bottom + art rows + footer
	gotTotalLines := strings.Count(bubble, "\n") + 1
	if gotTotalLines != wantTotalLines {
		t.Fatalf("expected %d total lines (art rows preserved 1:1), got %d — art was likely reflowed instead of cropped\nbubble:\n%s",
			wantTotalLines, gotTotalLines, ansi.Strip(bubble))
	}

	// Cropping must still respect the bubble width.
	if got := maxLineWidth(bubble); got > bubbleW {
		t.Fatalf("rendered line width %d exceeds bubble width %d after cropping", got, bubbleW)
	}

	// Each row marker must still start its own line, intact.
	stripped := ansi.Strip(bubble)
	for _, want := range []string{"ROW1:", "ROW2:", "ROW3:"} {
		found := false
		for _, line := range strings.Split(stripped, "\n") {
			if strings.Contains(line, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("row marker %q not found intact on its own line; art appears to have been reflowed\nbubble:\n%s", want, stripped)
		}
	}
}

func TestRenderMessage_EntityFormattingSurvivesWrapping(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	text := "start " + strings.Repeat("bold ", 40) + "end"
	entities := []*telegram.TextEntity{
		{
			Offset: 6,
			Length: int32(len([]rune(strings.Repeat("bold ", 40)))) - 1, // exclude trailing space
			Type:   &telegram.TextEntityTypeBold{},
		},
	}
	msg := textMessage(text, entities)

	const maxWidth = 50
	bubble := r.RenderMessage(msg, st, false, false, maxWidth)

	if !strings.Contains(bubble, "\033[1m") {
		t.Fatalf("expected bold escape code \\033[1m to survive wrapping, got:\n%s", bubble)
	}
	if !strings.Contains(bubble, "\033[22m") {
		t.Fatalf("expected bold-reset escape code \\033[22m to survive wrapping, got:\n%s", bubble)
	}
}

func TestFormatTimestampSmart(t *testing.T) {
	now := time.Now().Local()

	// Construct times relative to "now" so the test is independent of the
	// timezone/date it happens to run in.
	todayNoon := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	// Guard against a test run right around midnight shifting the day.
	if todayNoon.Day() != now.Day() {
		todayNoon = now
	}

	threeDaysAgo := todayNoon.AddDate(0, 0, -3)
	tenDaysAgo := todayNoon.AddDate(0, 0, -10)

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"today", todayNoon, todayNoon.Format("15:04")},
		{"three days ago", threeDaysAgo, threeDaysAgo.Format("Mon 15:04")},
		{"ten days ago", tenDaysAgo, tenDaysAgo.Format("2006-01-02")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatTimestampSmart(int32(tc.t.Unix()))
			if got != tc.want {
				t.Errorf("FormatTimestampSmart(%v) = %q, want %q", tc.t, got, tc.want)
			}
		})
	}
}

// TestGlamourStyleNameNeverQueriesTerminal is a regression test for a bug
// where glamour.WithAutoStyle() (used to build the glamour renderer) queries
// the terminal's background color via an OSC 10/11 escape sequence and reads
// the reply from stdin. Under Bubble Tea's raw-mode input loop, that reply
// has nowhere to go but into the app as literal keystrokes — it was
// observed leaking into the composer as text like ";1rgb:2020/2020/2020" on
// chat open (when the renderer/glamour instance is (re)built).
//
// glamourStyleName must always resolve to one of glamour's static style
// names ("dark"/"light", i.e. glamourstyles.DarkStyle/LightStyle), and must
// never return glamourstyles.AutoStyle ("auto"), which is what triggers the
// terminal query inside glamour.WithStandardStyle.
func TestGlamourStyleNameNeverQueriesTerminal(t *testing.T) {
	cases := []struct {
		name string
		th   *theme.Theme
		want string
	}{
		{"dark theme", theme.DarkTheme(), glamourstyles.DarkStyle},
		{"light theme", theme.LightTheme(), glamourstyles.LightStyle},
		{"nil theme falls back to dark", nil, glamourstyles.DarkStyle},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := glamourStyleName(tc.th)
			if got == glamourstyles.AutoStyle {
				t.Fatalf("glamourStyleName returned %q (auto) — this triggers a live terminal background-color query, which leaks raw OSC response bytes into bubbletea's stdin as keystrokes", got)
			}
			if got != tc.want {
				t.Errorf("glamourStyleName(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestNewMessageRendererDoesNotUseAutoStyle guards the actual construction
// path: NewMessageRenderer (and ensureGlamourWidth's rebuild) must build the
// glamour.TermRenderer via glamour.WithStandardStyle(glamourStyleName(...)),
// never glamour.WithAutoStyle(). There's no public way to introspect a built
// *glamour.TermRenderer's style choice, so this just re-asserts (a) building
// succeeds with the deterministic style for both themes, and (b) rendering
// simple markdown produces non-empty, deterministic-looking output — i.e.
// the renderer is usable without ever having asked the terminal anything.
func TestNewMessageRendererDoesNotUseAutoStyle(t *testing.T) {
	for _, th := range []*theme.Theme{theme.DarkTheme(), theme.LightTheme()} {
		r := NewMessageRenderer(th)
		if r.glamour == nil {
			t.Fatal("expected glamour renderer to be constructed")
		}
		out, err := r.glamour.Render("**bold**")
		if err != nil {
			t.Fatalf("glamour.Render failed: %v", err)
		}
		if strings.TrimSpace(out) == "" {
			t.Fatal("expected non-empty rendered markdown output")
		}
	}
}
