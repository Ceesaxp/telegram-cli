package render

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// previewDescriptionLines is how much of a page's description is drawn.
//
// From the design record: "Link previews use a cyan left rule, host, title,
// and at most two description lines." A description is a paragraph written
// for a browser, and letting it run means one link pushing the messages
// around it off the screen.
const previewDescriptionLines = 2

// renderWebPage draws a link preview: a cyan left rule, the host, the
// title, and at most two lines of the description.
//
// The rule is what makes the preview read as a quotation of the page rather
// than as more of the sender's message — the same device the blockquote
// uses, in the accent colour rather than the ghost one, because these are
// somebody's words about a link and not somebody's words in the chat.
func renderWebPage(page *telegram.WebPage, roles theme.Roles, width int) []string {
	if page == nil || width < 1 {
		return nil
	}

	const rule = "│ "
	blockW := min(width, maxBlockWidth)
	inner := max(blockW-cell.Width(rule), 1)

	prefix := lipgloss.NewStyle().Foreground(roles.Cyan).Render(rule)
	host := lipgloss.NewStyle().Foreground(roles.Cyan)
	title := lipgloss.NewStyle().Foreground(roles.Bright).Bold(true)
	body := lipgloss.NewStyle().Foreground(roles.Dim)

	var out []string
	add := func(text string, style lipgloss.Style, maxLines int) {
		if text == "" {
			return
		}
		lines := cell.WrapLines(text, inner)
		if maxLines > 0 && len(lines) > maxLines {
			lines = lines[:maxLines]
		}
		for _, line := range lines {
			out = append(out, ruledLine(prefix, line, style, blockW))
		}
	}

	add(previewHost(page), host, 1)
	add(page.Title, title, 2)
	add(flattenDescription(page.Description), body, previewDescriptionLines)
	return out
}

// previewHost is the name of the site the link points at: what Telegram
// calls it, or failing that the host of the URL as Telegram displays it.
func previewHost(page *telegram.WebPage) string {
	if page.SiteName != "" {
		return page.SiteName
	}
	return page.DisplayURL
}

// flattenDescription turns a page description's own line breaks into
// spaces.
//
// A description arrives wrapped for whatever column the page author had in
// mind. Kept, those breaks make the two-line budget land after a dozen
// words; flattened, the two lines are two lines of this pane's width.
func flattenDescription(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
