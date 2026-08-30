package render

import (
	"sort"
	"strings"

	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// EntitiesToANSI converts Telegram formatted text directly to ANSI escape codes.
func EntitiesToANSI(text *telegram.FormattedText) string {
	if text == nil {
		return ""
	}

	if len(text.Entities) == 0 {
		return text.Text
	}

	runes := []rune(text.Text)
	entities := make([]*telegram.TextEntity, len(text.Entities))
	copy(entities, text.Entities)
	sort.Slice(entities, func(i, j int) bool {
		return entities[i].Offset < entities[j].Offset
	})

	var b strings.Builder
	lastEnd := int32(0)

	for _, e := range entities {
		if e.Offset > lastEnd {
			b.WriteString(string(runes[lastEnd:e.Offset]))
		}

		entityText := string(runes[e.Offset : e.Offset+e.Length])

		switch e.Type.(type) {
		case *telegram.TextEntityTypeBold:
			b.WriteString("\033[1m")
			b.WriteString(entityText)
			b.WriteString("\033[22m")
		case *telegram.TextEntityTypeItalic:
			b.WriteString("\033[3m")
			b.WriteString(entityText)
			b.WriteString("\033[23m")
		case *telegram.TextEntityTypeUnderline:
			b.WriteString("\033[4m")
			b.WriteString(entityText)
			b.WriteString("\033[24m")
		case *telegram.TextEntityTypeStrikethrough:
			b.WriteString("\033[9m")
			b.WriteString(entityText)
			b.WriteString("\033[29m")
		case *telegram.TextEntityTypeCode, *telegram.TextEntityTypePre:
			b.WriteString("\033[7m") // inverse
			b.WriteString(entityText)
			b.WriteString("\033[27m")
		default:
			b.WriteString(entityText)
		}

		lastEnd = e.Offset + e.Length
	}

	if lastEnd < int32(len(runes)) {
		b.WriteString(string(runes[lastEnd:]))
	}

	return b.String()
}
