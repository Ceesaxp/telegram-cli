// Package layout computes the TUI 2.0 frame's region budget: how many cells
// wide each column is, how many rows the body gets, and which chrome rows are
// shown at all.
//
// It is deliberately pure arithmetic over (width, height, rail preference) —
// no styling, no components, no I/O — so the responsive rules can be tested
// exhaustively and the goldens in docs/fixtures can be checked against them
// without rendering anything.
//
// The rules are docs/tui-2.0.md, "Frame and responsive layout", and were
// confirmed against the golden fixtures at 80x24, 100x30, 120x40, 137x29,
// and 200x60.
package layout

// Fixed region widths, in cells. The chat list and rail are fixed rather
// than proportional because they hold columnar data whose budget is designed
// (a 38-cell chat row, a 30-cell rail row), while the thread is the region
// that should absorb whatever is left.
const (
	// ChatListWide is the chat list's width when there is room for it.
	ChatListWide = 38
	// ChatListNarrow is the reduced width used before dropping to a single
	// panel. Giving up 8 cells here buys the thread 8.
	ChatListNarrow = 30
	// RailWidth is the context rail's width when shown.
	RailWidth = 30
	// RuleWidth is the one-cell vertical rule between two regions.
	RuleWidth = 1
	// InlineComposerHeight is the composer's default single row.
	InlineComposerHeight = 1
)

// Width thresholds. See [Compute] for the precedence these encode.
const (
	// MinRailWidth is the narrowest terminal that still shows the rail:
	// 38 + 1 + 48 + 1 + 30. Below it the thread would be squeezed harder
	// than the rail is worth.
	MinRailWidth = 118
	// MinWideListWidth is the narrowest terminal that keeps the chat list
	// at its full width.
	MinWideListWidth = 90
	// MinTwoPanelWidth is the narrowest terminal that shows two panels at
	// all. Below it, one panel owns the screen and Tab swaps them.
	MinTwoPanelWidth = 72
)

// Height thresholds.
const (
	// MinFullHeight is the shortest terminal that renders the whole frame,
	// hint bar included.
	MinFullHeight = 20
	// MinTopBarHeight is the shortest terminal that keeps the top bar. Below
	// it only the thread and its inline composer are drawn.
	MinTopBarHeight = 12
)

// Layout is the computed frame budget. Every width is in cells and every
// height in rows; a zero width means the region is not drawn at all.
type Layout struct {
	Width, Height int

	// ChatListWidth, ThreadWidth, and RailWidth are the three columns.
	// Exactly one RuleWidth separates each pair of adjacent non-zero
	// regions, and the three plus their rules always sum to Width.
	ChatListWidth int
	ThreadWidth   int
	RailWidth     int

	// TopBar and HintBar report whether those single chrome rows are drawn.
	TopBar  bool
	HintBar bool

	// BodyHeight is the rows between the chrome bars — everything the
	// columns get to divide up.
	BodyHeight int

	// ChatListHeight is the rows the chat list column gets: the whole body,
	// since its filter header and footer are drawn inside that budget.
	ChatListHeight int

	// ComposerHeight is the rows reserved at the foot of the THREAD column
	// (not the chat list — the golden frames put the chat list's own footer
	// beside the composer, not above it). One row inline; the eight-row
	// expanded form is refused when InlineComposerOnly is set.
	ComposerHeight int

	// ThreadHeight is what is left of the body for the thread's header and
	// scroller once the composer has taken its rows.
	ThreadHeight int

	// SinglePanel means the terminal is too narrow for two columns: one
	// panel owns the full width and Tab swaps which.
	SinglePanel bool

	// InlineComposerOnly means the composer may not expand to its eight-row
	// form, because there are not enough rows to give it.
	InlineComposerOnly bool
}

// Compute returns the frame budget for a terminal of the given size.
//
// Width precedence — the thread is the region that must survive, so the
// order in which space is given up is: the rail first, then the chat list's
// extra 8 cells, then the second column entirely.
//
//	>= 118   38 / rule / flex / rule / 30
//	90..117  38 / rule / flex
//	72..89   30 / rule / flex
//	<  72    one panel, full width
//
// railEnabled is the user's explicit preference (ui.rail / the grave-accent
// toggle). Width can force the rail off, but never on: a user who turned it
// off does not get it back by widening the terminal.
//
// Height precedence:
//
//	>= 20    full frame
//	12..19   top bar kept, hint bar dropped, composer forced inline
//	<  12    thread and its inline composer only
func Compute(width, height, composerRows int, railEnabled bool) Layout {
	l := Layout{Width: width, Height: height}

	// --- Vertical ---------------------------------------------------
	switch {
	case height >= MinFullHeight:
		l.TopBar, l.HintBar = true, true
	case height >= MinTopBarHeight:
		l.TopBar, l.HintBar = true, false
		l.InlineComposerOnly = true
	default:
		l.TopBar, l.HintBar = false, false
		l.InlineComposerOnly = true
	}

	l.BodyHeight = height
	if l.TopBar {
		l.BodyHeight--
	}
	if l.HintBar {
		l.BodyHeight--
	}
	if l.BodyHeight < 0 {
		l.BodyHeight = 0
	}

	l.ChatListHeight = l.BodyHeight
	// The composer keeps its row even in a very short terminal: a client you
	// cannot type into is worse than one with no room to read.
	//
	// composerRows is what the composer asked for — one for the inline row,
	// more for a reply bar, an attachment chip, or the expanded form. It is
	// taken from the caller rather than derived here because only the
	// composer knows what it is about to draw, and a layout that guessed
	// would leave a hole under it or push the thread off the bottom.
	l.ComposerHeight = max(composerRows, InlineComposerHeight)
	if l.InlineComposerOnly {
		// Too few rows to give the expanded form its eight: the thread would
		// have nothing left. The composer is told, and stays inline.
		l.ComposerHeight = InlineComposerHeight
	}
	if l.ComposerHeight > l.BodyHeight {
		l.ComposerHeight = l.BodyHeight
	}
	l.ThreadHeight = l.BodyHeight - l.ComposerHeight

	// --- Horizontal -------------------------------------------------
	if width < MinTwoPanelWidth {
		l.SinglePanel = true
		l.ChatListWidth = width
		l.ThreadWidth = width
		return l
	}

	list := ChatListWide
	if width < MinWideListWidth {
		list = ChatListNarrow
	}

	rail := 0
	if railEnabled && width >= MinRailWidth {
		rail = RailWidth
	}

	rules := RuleWidth // between list and thread
	if rail > 0 {
		rules += RuleWidth // between thread and rail
	}

	l.ChatListWidth = list
	l.RailWidth = rail
	l.ThreadWidth = width - list - rail - rules

	return l
}

// TotalWidth returns what the regions and their rules actually sum to. It
// exists so tests and the frame assembler can assert the invariant directly
// rather than re-deriving it: if this ever differs from Layout.Width, some
// row is going to be the wrong length and the frame will shear.
func (l Layout) TotalWidth() int {
	if l.SinglePanel {
		return l.ChatListWidth
	}
	w := l.ChatListWidth + RuleWidth + l.ThreadWidth
	if l.RailWidth > 0 {
		w += RuleWidth + l.RailWidth
	}
	return w
}
