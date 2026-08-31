package theme

import "github.com/charmbracelet/lipgloss"

// The overlays' shared vocabulary.
//
// Six components — the palette, the help card, search, dialogs, auth and
// contacts — each drew from the pre-2.0 [Theme] and its hard-coded bright
// blue 39 and green 42. That made every overlay a different palette from the
// frame underneath it, and it made "what colour is a title" a question with
// six answers.
//
// These are the answers. They are functions on [Roles] rather than a second
// struct of pre-built styles, which is what [Theme] was and why it drifted:
// a table of styles has to be constructed somewhere, so it acquires a
// lifecycle, a constructor, and a copy in every component that holds one.
// A function of the palette has none of that and cannot go stale.
//
// Layout is deliberately absent. Padding, width and alignment belong to the
// component that knows what it is drawing; only the colours are shared,
// because only the colours have to agree.

// OverlayFrame is the box an overlay sits in: a panel surface inside a
// border, over whatever it covers.
func OverlayFrame(r Roles) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(r.Border).
		BorderBackground(r.Panel).
		Background(r.Panel).
		Foreground(r.Fg)
}

// OverlayTitle is the name of the thing an overlay is showing.
func OverlayTitle(r Roles) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(r.Bright).Background(r.Panel).Bold(true)
}

// OverlayBody is ordinary copy inside an overlay.
func OverlayBody(r Roles) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(r.Fg).Background(r.Panel)
}

// OverlayMuted is secondary copy: hints, counts, the keys a surface honours.
func OverlayMuted(r Roles) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(r.Dim).Background(r.Panel)
}

// OverlayKey is a key name inside a hint — cyan, the sole accent, so the
// thing to press is the thing that stands out.
func OverlayKey(r Roles) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(r.Cyan).Background(r.Panel)
}

// OverlaySelected is the highlighted row or button.
//
// A background rather than a brighter foreground, for the reason the top
// bar's active folder needed one: a row can contain a colour emoji, and a
// colour emoji ignores the foreground it is given.
func OverlaySelected(r Roles) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(r.Bright).Background(r.Sel).Bold(true)
}

// OverlayInput is a text field an overlay is collecting into.
func OverlayInput(r Roles) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(r.Fg).Background(r.Sel)
}

// OverlayError and OverlaySuccess are the two outcomes a surface reports.
func OverlayError(r Roles) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(r.Red).Background(r.Panel)
}

func OverlaySuccess(r Roles) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(r.Green).Background(r.Panel)
}
