package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// EntitiesToMarkdown converts Telegram formatted text entities to markdown.
func EntitiesToMarkdown(text *telegram.FormattedText) string {
	if text == nil {
		return ""
	}

	if len(text.Entities) == 0 {
		return text.Text
	}

	runes := []rune(text.Text)

	// Sort entities by offset.
	entities := make([]*telegram.TextEntity, len(text.Entities))
	copy(entities, text.Entities)
	sort.Slice(entities, func(i, j int) bool {
		return entities[i].Offset < entities[j].Offset
	})

	var b strings.Builder
	lastEnd := int32(0)

	for _, e := range entities {
		// Append text before this entity.
		if e.Offset > lastEnd {
			b.WriteString(string(runes[lastEnd:e.Offset]))
		}

		entityText := string(runes[e.Offset : e.Offset+e.Length])

		switch t := e.Type.(type) {
		case *telegram.TextEntityTypeBold:
			b.WriteString("**")
			b.WriteString(entityText)
			b.WriteString("**")

		case *telegram.TextEntityTypeItalic:
			b.WriteString("*")
			b.WriteString(entityText)
			b.WriteString("*")

		case *telegram.TextEntityTypeUnderline:
			b.WriteString("__")
			b.WriteString(entityText)
			b.WriteString("__")

		case *telegram.TextEntityTypeStrikethrough:
			b.WriteString("~~")
			b.WriteString(entityText)
			b.WriteString("~~")

		case *telegram.TextEntityTypeCode:
			b.WriteString("`")
			b.WriteString(entityText)
			b.WriteString("`")

		case *telegram.TextEntityTypePre:
			b.WriteString("```\n")
			b.WriteString(entityText)
			b.WriteString("\n```")

		case *telegram.TextEntityTypePreCode:
			b.WriteString(fmt.Sprintf("```%s\n", t.Language))
			b.WriteString(entityText)
			b.WriteString("\n```")

		case *telegram.TextEntityTypeTextURL:
			b.WriteString(fmt.Sprintf("[%s](%s)", entityText, t.URL))

		case *telegram.TextEntityTypeURL:
			b.WriteString(entityText)

		case *telegram.TextEntityTypeMention:
			b.WriteString(entityText)

		case *telegram.TextEntityTypeMentionName:
			b.WriteString(fmt.Sprintf("@[%s](user:%d)", entityText, t.UserID))

		case *telegram.TextEntityTypeHashtag:
			b.WriteString(entityText)

		case *telegram.TextEntityTypeBotCommand:
			b.WriteString("`")
			b.WriteString(entityText)
			b.WriteString("`")

		case *telegram.TextEntityTypeEmailAddress:
			b.WriteString(entityText)

		case *telegram.TextEntityTypeSpoiler:
			b.WriteString("||")
			b.WriteString(entityText)
			b.WriteString("||")

		case *telegram.TextEntityTypeBlockQuote:
			lines := strings.Split(entityText, "\n")
			for i, line := range lines {
				b.WriteString("> ")
				b.WriteString(line)
				if i < len(lines)-1 {
					b.WriteString("\n")
				}
			}

		default:
			b.WriteString(entityText)
		}

		lastEnd = e.Offset + e.Length
	}

	// Append remaining text.
	if lastEnd < int32(len(runes)) {
		b.WriteString(string(runes[lastEnd:]))
	}

	return b.String()
}

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
