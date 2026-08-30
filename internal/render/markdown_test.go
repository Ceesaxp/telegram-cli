package render

import (
	"strings"
	"testing"
	"time"

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

// TestRenderBodyWrapsLongSingleToken: an unbroken 500-character token has
// to be hard-broken across body lines, not truncated. Relying on a MaxWidth
// style would leave one line's worth of it on screen and silently drop the
// rest, which is how a pasted URL loses its tail.
func TestRenderBodyWrapsLongSingleToken(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	msg := textMessage(strings.Repeat("x", 500), nil)

	const bodyW = 31
	lines := r.RenderBody(msg, st, bodyW)

	for i, line := range lines {
		if got := ansi.StringWidth(line); got > bodyW {
			t.Fatalf("line %d is %d cells wide, body is %d: %q", i, got, bodyW, line)
		}
	}
	if want := (500 + bodyW - 1) / bodyW; len(lines) < want {
		t.Fatalf("expected at least %d lines wrapping 500 chars at width %d, got %d", want, bodyW, len(lines))
	}
	if got := strings.Count(ansi.Strip(strings.Join(lines, "")), "x"); got != 500 {
		t.Fatalf("expected all 500 characters to survive wrapping, found %d", got)
	}
}

// TestRenderBodyCJKAndEmoji: wide runes are measured in cells, so a line of
// them fills the body without overflowing it. A rune-count wrap would put
// twice the width on screen and shear the grid.
func TestRenderBodyCJKAndEmoji(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	msg := textMessage(strings.Repeat("你好世界🎉こんにちは😀漢字テスト\n", 10), nil)

	const bodyW = 20
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("RenderBody panicked on CJK/emoji input: %v", rec)
		}
	}()

	lines := r.RenderBody(msg, st, bodyW)
	if len(lines) == 0 {
		t.Fatal("expected rendered body lines")
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > bodyW {
			t.Fatalf("line %d is %d cells wide, body is %d", i, got, bodyW)
		}
	}
}

// TestRenderBodyDoesNotReflowArt: pre-rendered image art is a grid of cells
// at fixed column positions. A word wrapper sees a wide row with no spaces
// as one long token and hard-breaks it, turning a picture into noise — so
// art is cropped instead, one output line per art row.
func TestRenderBodyDoesNotReflowArt(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	const rowWidth = 55
	rows := []string{
		"ROW1:" + strings.Repeat("#", rowWidth-5),
		"ROW2:" + strings.Repeat("#", rowWidth-5),
		"ROW3:" + strings.Repeat("#", rowWidth-5),
	}

	const fileID = "fake-photo-file"
	// Prime the image cache directly so renderPhoto takes the cache-hit
	// path and never touches the filesystem or an image decoder.
	r.imgCache.Set("img:"+fileID, strings.Join(rows, "\n"))

	msg := &telegram.Message{
		ID:     1,
		ChatID: 1,
		Date:   1700000000,
		Content: &telegram.MessagePhoto{
			Photo: &telegram.Photo{
				Sizes: []*telegram.PhotoSize{{
					Width:  rowWidth,
					Height: len(rows),
					File:   &telegram.File{ID: fileID, Downloaded: true, Path: "/fake/path.jpg"},
				}},
			},
		},
	}

	const bodyW = 31 // narrower than rowWidth, so cropping is exercised
	lines := r.RenderBody(msg, st, bodyW)

	if len(lines) != len(rows) {
		t.Fatalf("expected %d lines (one per art row), got %d: %q", len(rows), len(lines), lines)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > bodyW {
			t.Fatalf("art line %d is %d cells wide, body is %d", i, got, bodyW)
		}
		if want := rows[i][:5]; !strings.HasPrefix(ansi.Strip(line), want) {
			t.Fatalf("art line %d does not start with %q: %q", i, want, ansi.Strip(line))
		}
	}
}

// TestRenderBodyKeepsEntityStylesAcrossWraps: a bold run spanning a wrap
// point must be bold on both lines. A wrapper that is not ANSI-aware either
// drops the style or leaks it to the end of the message.
func TestRenderBodyKeepsEntityStylesAcrossWraps(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	body := strings.Repeat("bold ", 40)
	msg := textMessage("start "+body+"end", []*telegram.TextEntity{{
		Offset: 6,
		Length: int32(len([]rune(body))) - 1,
		Type:   &telegram.TextEntityTypeBold{},
	}})

	lines := r.RenderBody(msg, st, 31)
	if len(lines) < 4 {
		t.Fatalf("test needs several wrapped lines, got %d", len(lines))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "\033[1m") {
		t.Fatalf("bold escape did not survive wrapping:\n%s", joined)
	}
	if !strings.Contains(joined, "\033[22m") {
		t.Fatalf("bold-reset escape did not survive wrapping:\n%s", joined)
	}
}

// TestRenderBodyIsNeverEmpty: a message with nothing renderable still
// occupies exactly one line. Zero lines would make the scroll index
// disagree with what is on screen, and every jump built on it would land
// one message off.
func TestRenderBodyIsNeverEmpty(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	for name, msg := range map[string]*telegram.Message{
		"empty text":  textMessage("", nil),
		"nil content": {ID: 1, ChatID: 1, Date: 1700000000},
	} {
		lines := r.RenderBody(msg, st, 31)
		if len(lines) != 1 {
			t.Fatalf("%s: expected exactly one line, got %d: %q", name, len(lines), lines)
		}
	}
}

// TestRenderBodyTakesTextAsWritten: Telegram sends formatting as entity
// ranges, so the body is built from those and never round-tripped through a
// Markdown renderer. A user who types an asterisk gets an asterisk.
func TestRenderBodyTakesTextAsWritten(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	msg := textMessage("2 * 3 _ 4 # five", nil)
	got := ansi.Strip(strings.Join(r.RenderBody(msg, st, 40), "\n"))
	if got != "2 * 3 _ 4 # five" {
		t.Fatalf("text was reinterpreted as Markdown: %q", got)
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
