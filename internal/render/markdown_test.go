package render

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
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
	lines := r.RenderBody(msg, st, BodyOptions{Width: bodyW})

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

	lines := r.RenderBody(msg, st, BodyOptions{Width: bodyW})
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
	lines := r.RenderBody(msg, st, BodyOptions{Width: bodyW})

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
// must open its style on EVERY line it covers and close it at the end of
// each one.
//
// Opening once and closing at the very end looks correct in a single-column
// dump and is wrong on screen: a terminal does not reset at a newline, and
// the rows of a panel are not adjacent — whatever a body line leaves open
// bleeds through its trailing padding, across the panel rule, and into the
// next column. That is the bug this asserts against, and it is invisible to
// any test that only checks the joined output.
func TestRenderBodyKeepsEntityStylesAcrossWraps(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	body := strings.Repeat("bold ", 40)
	msg := textMessage("start "+body+"end", []*telegram.TextEntity{{
		Offset: 6,
		Length: int32(len([]rune(body))) - 1,
		Type:   &telegram.TextEntityTypeBold{},
	}})

	lines := r.RenderBody(msg, st, BodyOptions{Width: 31})
	if len(lines) < 4 {
		t.Fatalf("test needs several wrapped lines, got %d", len(lines))
	}

	for i, line := range lines {
		if !strings.Contains(ansi.Strip(line), "bold") {
			continue
		}
		if !strings.Contains(line, "\x1b[1;") && !strings.Contains(line, "\x1b[1m") {
			t.Errorf("line %d carries bold text but never opens the style: %q", i, line)
		}
		if open := cell.OpenStyle(line); open != "" {
			t.Errorf("line %d leaves %q open past the end of the row: %q", i, open, line)
		}
	}
}

// TestRenderBodyLeavesNoStyleOpen is the same guarantee stated for every
// style this renderer emits, not just bold: no body line may end with an
// escape sequence still in effect.
func TestRenderBodyLeavesNoStyleOpen(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	long := strings.Repeat("word ", 30)
	cases := map[string]*telegram.Message{
		"link":  entityMessage(long, &telegram.TextEntityTypeTextURL{URL: "https://example.com"}),
		"code":  entityMessage(long, &telegram.TextEntityTypeCode{}),
		"quote": entityMessage(long, &telegram.TextEntityTypeBlockQuote{}),
		"pre":   entityMessage(long, &telegram.TextEntityTypePreCode{Language: "go"}),
		"spoil": entityMessage(long, &telegram.TextEntityTypeSpoiler{}),
	}

	for name, msg := range cases {
		for i, line := range r.RenderBody(msg, st, BodyOptions{Width: 31}) {
			if !strings.Contains(line, "\x1b[") {
				continue
			}
			if open := cell.OpenStyle(line); open != "" {
				t.Errorf("%s: line %d leaves %q open: %q", name, i, open, line)
			}
		}
	}
}

// entityMessage is a message whose whole text carries one entity.
func entityMessage(text string, kind telegram.TextEntityType) *telegram.Message {
	return textMessage(text, []*telegram.TextEntity{{
		Offset: 0,
		Length: int32(len([]rune(text))),
		Type:   kind,
	}})
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
		lines := r.RenderBody(msg, st, BodyOptions{Width: 31})
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
	got := ansi.Strip(strings.Join(r.RenderBody(msg, st, BodyOptions{Width: 40}), "\n"))
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
