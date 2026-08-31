package chatview

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/keys"
	"github.com/imtaqin/telegram-cli/internal/media"
	"github.com/imtaqin/telegram-cli/internal/render"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/sigil"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
	"github.com/imtaqin/telegram-cli/internal/ui/widgets"
)

const (
	// metaFetchConcurrency bounds the parallel per-sender/per-photo
	// network calls a single meta-fetch stage may have in flight.
	metaFetchConcurrency = 4

	// maxTargetPages is how many extra history pages OpenChatAt walks
	// backwards looking for a jump target before giving up.
	maxTargetPages = 3

	// photoPrefetchLimit caps how many photo thumbnails are downloaded
	// eagerly when a chat opens. A 50-message page of a media-heavy chat
	// can hold dozens of photos; downloading all of them keeps the meta
	// pipeline (and its status line) busy for seconds after the text is
	// already on screen. Everything past this cap loads lazily, when the
	// user scrolls near it or opens it with enter/o.
	photoPrefetchLimit = 10

	// photoLazyMargin is how many rendered lines above and below the
	// visible window still count as "near enough to be worth fetching"
	// when a scroll triggers a lazy thumbnail load.
	photoLazyMargin = 20

	// senderPriorityWindow is how many of the newest messages of a page
	// have their unknown senders resolved in the first sender stage. The
	// senders that only appear further back trail in a second stage, so
	// the names the user is actually looking at land first.
	senderPriorityWindow = 20

	// searchResultLimit is how many hits an in-chat search asks for.
	searchResultLimit = 20
)

// gridEntry is one message's rendered grid lines together with the inputs
// they were rendered from. Rendering is deterministic for a given message
// at a given width, with the exceptions listed here — each is compared on
// lookup so a stale entry is re-rendered rather than silently reused:
//
//   - the day divider label is *now*-dependent ("TODAY" becomes
//     "YESTERDAY" at midnight), and which message carries a divider
//     depends on the message before it, so both the label and the previous
//     message's date are part of the key.
//   - an outgoing message's delivery mark changes when the chat's
//     last-read-outbox marker moves.
//   - the sender name (store.Users) and photo art (store.Files) change as
//     async fetches land. Those are handled by explicit invalidation from
//     Update, not by re-checking on every lookup.
//
// Selection is deliberately NOT part of the key: entries are always the
// unselected rendering, and View re-renders the one selected message on top
// of them. Caching per selection state would double the cache and, worse,
// make the line index depend on where the cursor is — and the line index is
// what every scroll, jump and hit-test in this package is built on.
type gridEntry struct {
	width    int
	isOwn    bool
	dayLabel string
	prevDate int32
	unread   bool
	state    sendState
	lines    []string
}

// gridCache maps message ID -> rendered grid lines. It is held by pointer in
// Model so that the value copies bubbletea makes of the model (Update has a
// value receiver) all share one cache.
type gridCache struct {
	entries map[int64]gridEntry
}

func newGridCache() *gridCache {
	return &gridCache{entries: make(map[int64]gridEntry)}
}

func (c *gridCache) get(id int64) (gridEntry, bool) {
	if c == nil {
		return gridEntry{}, false
	}
	e, ok := c.entries[id]
	return e, ok
}

func (c *gridCache) put(id int64, e gridEntry) {
	if c == nil {
		return
	}
	c.entries[id] = e
}

// invalidate drops the cached lines of the given message IDs.
func (c *gridCache) invalidate(ids ...int64) {
	if c == nil {
		return
	}
	for _, id := range ids {
		delete(c.entries, id)
	}
}

// clear empties the cache in place (in place, so every value copy of the
// model observes it).
func (c *gridCache) clear() {
	if c == nil {
		return
	}
	clear(c.entries)
}

func (c *gridCache) len() int {
	if c == nil {
		return 0
	}
	return len(c.entries)
}

type Model struct {
	store    *store.Store
	tg       *telegram.Client
	renderer *render.MessageRenderer
	cache    *gridCache

	voice *media.VoicePlayer
	video *media.VideoPlayer

	// autoDownloadPhotos defaults to true in New so tests that skip
	// ApplyMedia keep the historical open-chat prefetch. ApplyMedia
	// overwrites it from config.
	autoDownloadPhotos  bool
	autoDownloadLimitMB int

	width        int
	height       int
	focused      bool
	chatID       int64
	chatTitle    string
	scrollOffset int

	// cursorID is the message the action keys (reply, edit, delete,
	// enter/o, s) act on, held as an identity rather than recomputed from
	// the scroll position on every read.
	//
	// It used to be derived: "the message containing the last visible
	// line". That is a position, and positions move on their own here —
	// a photo thumbnail landing turns a one-line placeholder into twenty
	// lines of art, and the message under the old rule silently changed
	// between the user reading the screen and pressing r. An identity
	// cannot drift that way.
	//
	// The cursor moves two ways. } and { move it a message at a time and
	// PIN it (see cursorPinned); otherwise it is anchored back into the
	// visible window by syncCursor whenever the viewport moves. What the
	// anchoring buys is stickiness — while the message stays on screen,
	// the target stays that message no matter what re-renders around it.
	//
	// Zero means "not anchored yet"; the next read or sync adopts the
	// newest visible message.
	cursorID int64

	// downloadDir is where s copies a file to. Empty means saving is
	// refused rather than guessed at — see saveInto.
	downloadDir string

	// pendingCount is the vi count prefix typed but not yet spent: the 9
	// in "9{". Zero means none. See count.go.
	pendingCount int

	// cursorPinned is whether the user placed the cursor themselves.
	//
	// It exists because of the tail rule below: at the bottom of the
	// history the cursor IS the newest message and follows arrivals, which
	// is what a live chat needs — r has to reply to the message that just
	// came in, not to whichever one the cursor was resting on. That rule
	// would also swallow a deliberate } or {, so an explicit motion sets
	// this and the tail rule stands down until the reader says otherwise
	// (G, or opening another chat).
	cursorPinned bool

	// roles is the TUI 2.0 semantic palette the grid draws with. New
	// installs a default so a Model that never has SetRoles called still
	// renders — this panel's own tests construct it directly, and a
	// component whose output depends on the host remembering a setter is
	// a component with two behaviours.
	roles theme.Roles

	// unreadFromID is the first message that was unread when the chat
	// opened, and unreadCount how many there were. The divider is drawn
	// from these rather than from the live marker so that it STAYS where
	// the reader found it while they read past it — a divider that
	// retreats as you read never tells you where you were.
	// unreadAfterID is the last-read-inbox marker as it stood at open
	// time; unreadFromID is the first loaded message past it, resolved
	// once history arrives.
	unreadAfterID int64
	unreadFromID  int64
	unreadCount   int

	// revealedID is the message whose spoilers are currently open. Zero
	// means none, which is the state every chat opens in and the state a
	// cursor move returns to: a spoiler that stayed open after the reader
	// scrolled away would be revealed to whoever looks at the screen next,
	// which is the one thing a spoiler exists to prevent.
	revealedID int64

	// typing is the set of user IDs currently composing in the open chat.
	// The thread owns this because TUI 2.0 draws the indicator as the
	// bottom row of the scroller, aligned with the message grid, rather
	// than as a line in a status bar.
	typing      []int64
	loading     bool
	loadStatus  string // honest stage label, e.g. "Loading messages..."
	notice      string // transient notice shown in the header
	myUserId    int64
	mediaStatus string

	// metaBusy is the trailing meta pipeline (sender names, photo
	// thumbnails). It runs *after* first paint and must never take a
	// body line: it only shows as a small glyph in the header, because
	// the messages are already readable while it runs.
	metaBusy bool

	// gen is bumped on every OpenChatAt. Async results carry the
	// generation they were started for; Update drops anything stale, so
	// switching chats cancels the previous chat's pending work.
	gen         int
	targetMsgID int64 // jump target being searched for, 0 when none
	targetPages int   // extra history pages already walked for the target

	// pendingJumpID is a jump target that has already been scrolled to but
	// whose position is not final yet: the meta stages still to run for
	// this generation can change bubble heights (most sharply a photo,
	// which grows from a one-line placeholder to multi-line art), which
	// would push the target off screen. The jump is re-applied when the
	// LAST meta stage of its generation completes.
	pendingJumpID int64

	// pendingMeta accumulates the messages of every page loaded since the
	// last meta stage ran, so pages fetched while hunting for a jump
	// target still get their senders resolved.
	pendingMeta []*telegram.Message

	// blurred tracks terminal focus (tea.FocusMsg/tea.BlurMsg). The zero
	// value means focused, so behaviour is unchanged when the program
	// never enables focus reporting.
	blurred       bool
	pendingReadID int64 // newest message that arrived while blurred

	// In-chat search (ctrl+f). searchActive means the input line under
	// the header owns every keypress; searchHits are the message IDs of
	// the last completed search, newest first, cycled with n/N.
	searchActive bool
	searchInput  widgets.TextArea
	searchQuery  string
	searchHits   []int64
	searchIdx    int

	// keys holds the resolved (defaulted, collision-checked) bindings
	// handleKey consults for reply/edit/delete/scroll/page. See SetKeys.
	// New sets this to the built-in defaults, so a Model that never has
	// SetKeys called again behaves exactly as it did before chatview became
	// configurable.
	keys resolvedKeys

	// reservedKeys holds extra keys claimed outside this package (the
	// app-level surface: quit, panel focus, tab, i/c, h/l panel movement,
	// /, ?, and so on), set via SetReservedKeys. nil until that is called,
	// in which case SetKeys treats the app-level surface as empty — this
	// package's original, pre-reservation behavior.
	reservedKeys map[string]bool
}

func New(s *store.Store, tg *telegram.Client, r theme.Roles) Model {
	input := widgets.NewTextArea()
	input.Focused = true
	m := Model{
		store:              s,
		tg:                 tg,
		renderer:           render.NewMessageRenderer(),
		cache:              newGridCache(),
		searchInput:        input,
		autoDownloadPhotos: true,
	}
	// The palette reaches the renderer too: the grid draws the gutter and
	// the body draws the message, and they have to agree about what amber
	// is.
	m.roles = r
	m.renderer.SetRoles(r)
	m.SetKeys(Keys{})
	return m
}

// ApplyUI applies the [ui] settings the renderer needs: the inline-image
// policy and whether links carry OSC 8. The rail preference is the host's
// own business.
func (m *Model) ApplyUI(cfg config.UIConfig) {
	m.renderer.SetInlineImages(cfg.InlineImages)
	m.renderer.SetHyperlinks(hyperlinksEnabled(cfg.Hyperlinks))
	m.cache.clear()
}

// hyperlinksEnabled folds the ui.hyperlinks policy and the terminal's
// capability into the single boolean the renderer wants.
//
// Resolved here rather than in the renderer so the terminal is consulted
// once, at config time, and never during a frame — and so "auto" has exactly
// one meaning in the codebase.
func hyperlinksEnabled(policy string) bool {
	switch config.ResolveHyperlinks(policy) {
	case config.HyperlinksAlways:
		return true
	case config.HyperlinksNever:
		return false
	default:
		return theme.SupportsHyperlinks()
	}
}

// ApplyStorage takes the [storage] settings this panel needs: where `s`
// saves to. The media cache is the client's business, not this panel's.
func (m *Model) ApplyStorage(cfg config.StorageConfig) {
	m.downloadDir = cfg.DownloadDir
}

// ApplyMedia applies [media] config: image protocol and bubble size,
// external players, and photo auto-download. Voice notes download on
// play regardless of AutoDownloadVoice; they are not eagerly prefetched.
func (m *Model) ApplyMedia(cfg config.MediaConfig) {
	m.autoDownloadPhotos = cfg.AutoDownloadPhotos
	m.autoDownloadLimitMB = cfg.AutoDownloadLimitMB
	m.voice = media.NewVoicePlayer(cfg.VoicePlayer)
	m.video = media.NewVideoPlayer(cfg.VideoPlayer)
	if m.renderer != nil {
		m.renderer.SetImageProtocol(
			media.ResolveProtocol(cfg.ImageProtocol),
			cfg.MaxImageWidth,
			cfg.MaxImageHeight,
		)
	}
}

// ChatId is the chat currently open in this panel, 0 when none.
func (m Model) ChatId() int64 { return m.chatID }

// SearchActive reports whether the in-chat search input is open. While it
// is, every key belongs to the input: the host must route input events to
// this panel without consuming them first (esc, quick-type, etc.).
func (m Model) SearchActive() bool { return m.searchActive }

// HasSearchResults reports whether a completed in-chat search still holds
// hits, i.e. whether n/N are meaningful.
func (m Model) HasSearchResults() bool { return len(m.searchHits) > 0 }

// SetSize resizes the view. A width change invalidates every cached
// bubble, since bubbles are laid out for a specific panel width.
func (m *Model) SetSize(w, h int) {
	if w != m.width {
		m.cache.clear()
	}
	m.width = w
	m.height = h
}

func (m *Model) SetFocused(focused bool) { m.focused = focused }
func (m *Model) SetMyUserId(id int64)    { m.myUserId = id }

// SetReservedKeys tells chatview which keys are already claimed by
// bindings outside this package — every app-level key (quit, panel focus,
// tab, i/c into the composer, h/l panel movement, /, ?, and so on) — so
// SetKeys's collision resolution can refuse to accept a configured
// mnemonic that would silently shadow one of them. Without this, e.g.
// reply = "q" would be accepted, advertised on the help card as Reply, and
// quit the app the moment it fired, because chatview had no way to know
// app.go had already claimed "q".
//
// Call order: SetReservedKeys must be called before SetKeys, since SetKeys
// resolves and freezes the claimed-key set immediately when it runs.
// Calling SetKeys without ever calling SetReservedKeys first is safe: the
// app-level surface is simply treated as empty, which is this package's
// original, pre-reservation behavior.
func (m *Model) SetReservedKeys(reserved []string) {
	m.reservedKeys = make(map[string]bool, len(reserved))
	for _, r := range reserved {
		if key := normalizeChatViewKey(r); key != "" {
			m.reservedKeys[key] = true
		}
	}
}

// Keys is the subset of config.KeyConfig that chatview consults directly.
// chatview cannot import internal/config or internal/app (app imports this
// package), so the caller — internal/app, in New() — is responsible for
// reading config.toml's [keys] table, running each field through
// config.NormalizeKey, and passing the result here via SetKeys.
//
// A zero/empty field means "keep the built-in default", so a Model that
// never has SetKeys called behaves exactly as it did before chatview became
// configurable (New calls SetKeys(Keys{}) itself for exactly this reason).
//
// Reply/Edit/Delete are mnemonics, not motions: a configured value REPLACES
// the corresponding hardcoded letter ("r"/"e"/"d") rather than adding to it,
// so rebinding e.g. reply away from "r" also frees "r" for something else.
//
// ScrollUp/ScrollDown/PageUp/PageDown are motions: a configured value is
// ADDED alongside the always-on hardcoded spellings (arrows, k/j,
// pgup/pgdown) rather than replacing them. Configuration only ever adds a
// motion binding, it never removes one — matching vi/lazygit's expectation
// that hjkl and the arrows always move, regardless of what else is bound.
type Keys struct {
	Reply, Edit, Delete  string
	ScrollUp, ScrollDown string
	PageUp, PageDown     string
}

// resolvedKeys is the fully-resolved (defaulted, collision-checked) view of
// Keys that handleKey's switch matches against. See SetKeys.
type resolvedKeys struct {
	// reply/edit/delete are the single active spelling for each mnemonic:
	// either the configured key or, absent one (or on a collision), the
	// built-in letter.
	reply, edit, delete string

	// The motion fields are an *additional* spelling to accept alongside the
	// fixed hardcoded ones baked into handleKey's switch; "" means no extra
	// binding was configured (or it collided with something and was
	// dropped).
	scrollUpExtra, scrollDownExtra string
	pageUpExtra, pageDownExtra     string
}

// chatViewFixedKeys is every key handleKey matches unconditionally, outside
// of the configurable Keys fields: the scroll/page motions' hardcoded
// spellings (which stay active no matter what ScrollUp/ScrollDown/PageUp/
// PageDown are configured to — see Keys's doc comment) plus the remaining
// keys this wave does not make configurable (g/G/home/end/ctrl+u/ctrl+d/
// esc/ctrl+f/n/N/enter/o/s/x, and phase 8's y/M/space). This is chatview's
// own reserved surface;
// SetKeys additionally claims whatever SetReservedKeys was given for the
// app-level surface. Together they seed the "claimed" set no configured
// binding may shadow.
func chatViewFixedKeys() map[string]bool {
	return map[string]bool{
		"up": true, "k": true, "down": true, "j": true,
		"pgup": true, "pgdown": true,
		"g": true, "G": true, "home": true, "end": true,
		"ctrl+u": true, "ctrl+d": true,
		"esc": true, "ctrl+f": true,
		"n": true, "N": true,
		"enter": true, "o": true, "s": true, "x": true,
		// Phase 8's message actions. space is spelled as the config
		// vocabulary spells it (config.NormalizeKey maps "spacebar" and " "
		// onto it), so a user reading this list and their config file see
		// the same word.
		"y": true, "M": true, "space": true,
		// Message-wise cursor motion. See cursor.go for why } and { rather
		// than something with a letter in it.
		"}": true, "{": true,
	}
}

// SetKeys resolves and stores the bindings handleKey consults. See Keys's
// doc comment for the add-vs-replace rule between mnemonics and motions,
// and SetReservedKeys's doc comment for the app-level surface this also
// protects — call that first if the caller has one.
//
// Resolution runs in three passes over a single "claimed" set, so that
// explicit configuration always outranks a built-in default regardless of
// field order, and a collision never produces a silent double-bind:
//
//  1. Claim chatViewFixedKeys() and every key from SetReservedKeys. These
//     are claimed unconditionally, before anything from k is considered.
//  2. For Reply, Edit, and Delete in that order, if the field was
//     EXPLICITLY configured (non-empty), accept it as that mnemonic's
//     binding and claim it — unless it is already claimed, in which case
//     the configured value is rejected outright (not partially honored)
//     and the field is treated as unconfigured for pass 3.
//  3. For any of Reply/Edit/Delete that did not get an accepted binding in
//     pass 2 (unconfigured, or rejected), try its built-in letter
//     ("r"/"e"/"d"). If that letter is itself already claimed — by a fixed
//     key, a reserved key, or another field's pass-2 binding — the action
//     is UNREACHABLE this call: it is left as "" rather than bound anyway,
//     and ActiveKeys reports the same "" so a caller can advertise it
//     honestly as unbound instead of lying about which key fires it. This
//     never happens with an unconfigured Keys{}, since r/e/d never collide
//     with each other or with anything in chatViewFixedKeys().
//
// Only after mnemonic resolution is finished are the motion fields
// (ScrollUp/ScrollDown/PageUp/PageDown) considered, purely additively: a
// configured extra spelling is accepted only if it is not already claimed
// by anything above, and dropped (not bound anywhere) otherwise. The
// hardcoded motion bases (up/k, down/j, pgup, pgdown) are already in
// chatViewFixedKeys and stay live no matter what.
//
// Two collisions this resolves that a naive single-pass, field-order
// resolver got wrong:
//
//   - reply = "e" (edit_message left at its default "e") used to leave
//     both reply and edit bound to "e", with edit silently unreachable
//     because reply's switch case came first — and nothing reported it.
//     Now edit resolves to "" (unreachable) instead of double-binding.
//   - edit_message = "r" (reply left at its default "r") used to be
//     silently discarded, because reply's default claimed "r" before
//     edit_message's explicit config was ever considered. Now pass 2 runs
//     every explicit config before pass 3 tries any default, so the
//     explicit edit_message = "r" wins and reply falls back to "" instead
//     (there is no other letter for it to try).
func (m *Model) SetKeys(k Keys) {
	// Pass 1: the app-level and chatview-level surfaces are claimed
	// unconditionally, before any of k is considered.
	claimed := chatViewFixedKeys()
	for r := range m.reservedKeys {
		claimed[r] = true
	}

	// Pass 2: accept every EXPLICITLY configured mnemonic, field order,
	// claiming as we go. A configured key already claimed is rejected
	// outright; the field falls through to pass 3 as if unconfigured.
	acceptConfigured := func(configured string) (value string, ok bool) {
		if configured == "" {
			return "", false
		}
		key := normalizeChatViewKey(configured)
		if claimed[key] {
			return "", false
		}
		claimed[key] = true
		return key, true
	}
	reply, replyOK := acceptConfigured(k.Reply)
	edit, editOK := acceptConfigured(k.Edit)
	deleteKey, deleteOK := acceptConfigured(k.Delete)

	// Pass 3: anything without an accepted config tries its built-in
	// letter. If that too is claimed, the action is left unreachable ("")
	// rather than silently sharing a key that already does something else.
	tryDefault := func(def string) string {
		if claimed[def] {
			return ""
		}
		claimed[def] = true
		return def
	}
	if !replyOK {
		reply = tryDefault("r")
	}
	if !editOK {
		edit = tryDefault("e")
	}
	if !deleteOK {
		deleteKey = tryDefault("d")
	}

	// Motions: purely additive on top of the now-final claimed set (fixed
	// + reserved + resolved mnemonics).
	motionExtra := func(configured string) string {
		if configured == "" {
			return ""
		}
		key := normalizeChatViewKey(configured)
		if claimed[key] {
			return ""
		}
		claimed[key] = true
		return key
	}
	scrollUpExtra := motionExtra(k.ScrollUp)
	scrollDownExtra := motionExtra(k.ScrollDown)
	pageUpExtra := motionExtra(k.PageUp)
	pageDownExtra := motionExtra(k.PageDown)

	m.keys = resolvedKeys{
		reply:  reply,
		edit:   edit,
		delete: deleteKey,

		scrollUpExtra:   scrollUpExtra,
		scrollDownExtra: scrollDownExtra,
		pageUpExtra:     pageUpExtra,
		pageDownExtra:   pageDownExtra,
	}
}

// ActiveKeys reports what handleKey actually matches right now, after
// SetKeys's defaulting and collision resolution — not the raw Keys a caller
// last passed in. This is the source of truth for anything outside this
// package that advertises a chatview binding (the "?" help card, a status
// bar hint, and so on): it exists precisely so nothing has to reimplement
// SetKeys's collision rule to stay honest. A caller that instead re-derived
// a "resolved" reply/scroll_up/etc. by re-running the config through its
// own defaulting logic could end up advertising a binding that collided and
// was dropped — e.g. showing "j" for reply after a user sets reply = "j",
// when the panel still (correctly) matches "r". Always read this instead.
//
// Reply/Edit/Delete are returned as the single spelling that is actually
// live — the configured value if it was accepted, the built-in letter if
// it fell back to that, or "" if SetKeys's pass 3 found the action
// UNREACHABLE (its configured value collided and its built-in letter was
// also already claimed by something else). A caller rendering a help card
// should show "" as "unbound", not as an empty/absent row that looks like
// an oversight.
//
// ScrollUp/ScrollDown/PageUp/PageDown are returned as the *extra* spelling
// that was accepted on top of the always-on built-ins (up/k, down/j, pgup,
// pgdown) — empty when none was configured, or when the configured one
// collided and was dropped. The built-ins themselves are not repeated here;
// a caller advertising chatview's scroll/page bindings documents those
// separately (see internal/app/keymap.go's prose table) and only needs this
// for the additional spelling, if any.
func (m Model) ActiveKeys() Keys {
	return Keys{
		Reply:  m.keys.reply,
		Edit:   m.keys.edit,
		Delete: m.keys.delete,

		ScrollUp:   m.keys.scrollUpExtra,
		ScrollDown: m.keys.scrollDownExtra,
		PageUp:     m.keys.pageUpExtra,
		PageDown:   m.keys.pageDownExtra,
	}
}

// normalizeChatViewKey trims and lowercases a configured key the same way
// config.NormalizeKey does, so a caller that (against SetKeys's doc
// comment) forgets to run it through config.NormalizeKey first still gets
// case-insensitive matching rather than a binding that silently never
// fires.
func normalizeChatViewKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// statusLineVisible reports whether View() draws the one-line strip under
// the header — either the blocking load stage or the search input. The
// trailing meta pipeline deliberately does NOT put a line here: it runs
// after first paint, and taking a body line back and forth would make the
// history jump under the reader.
func (m Model) statusLineVisible() bool {
	return m.loading || m.searchActive
}

// bodyHeight is the number of terminal lines View() gives to the message
// body: everything but the header and the optional status/search line.
func (m Model) bodyHeight() int {
	h := m.height - 1
	if m.statusLineVisible() {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

// resolveUnreadDivider finds the first loaded message past the open-time
// read marker and pins the divider there.
//
// It runs on every page load rather than once, because the first page of a
// chat with a long unread run does not necessarily contain the boundary:
// the divider appears as soon as paging backwards reaches it, and never
// moves afterwards.
//
// Own messages cannot start the unread run — you have read what you sent —
// so the marker walks past them.
func (m *Model) resolveUnreadDivider() {
	if m.unreadAfterID == 0 || m.unreadFromID != 0 {
		return
	}
	for _, msg := range m.store.Messages.Get(m.chatID) {
		if msg.ID > m.unreadAfterID && !isOwnMessage(msg, m.myUserId) {
			m.unreadFromID = msg.ID
			return
		}
	}
}

// gridBlock returns a message's rendered grid lines, drawing them only on a
// cache miss or when the cached entry is stale.
//
// prev is the message before it in the history, which is what decides
// whether this one carries a day divider — dividers belong to the message
// under them rather than being separate history entries, so the scroll
// index stays exactly one count per message.
//
// Messages with ID 0 are unconfirmed outgoing sends: they are never cached
// because their ID is not yet unique and their delivery mark flips once the
// server assigns the real one.
func (m Model) gridBlock(msg, prev *telegram.Message) []string {
	if msg == nil {
		return nil
	}
	isOwn := isOwnMessage(msg, m.myUserId)
	dayLabel := render.FormatDayLabel(msg.Date)
	state := m.sendStateFor(msg)
	unread := m.unreadFromID != 0 && msg.ID == m.unreadFromID
	prevDate := int32(0)
	if prev != nil {
		prevDate = prev.Date
	}

	if msg.ID != 0 {
		if e, ok := m.cache.get(msg.ID); ok &&
			e.width == m.width && e.isOwn == isOwn && e.dayLabel == dayLabel &&
			e.prevDate == prevDate && e.unread == unread && e.state == state {
			return e.lines
		}
	}

	lines := m.gridMessageLines(msg, prev, false)
	if msg.ID != 0 {
		m.cache.put(msg.ID, gridEntry{
			width:    m.width,
			isOwn:    isOwn,
			dayLabel: dayLabel,
			prevDate: prevDate,
			unread:   unread,
			state:    state,
			lines:    lines,
		})
	}
	return lines
}

// renderedMessages returns every message's grid lines plus its line count,
// in store order (oldest first).
func (m Model) renderedMessages(msgs []*telegram.Message) ([][]string, []int) {
	blocks := make([][]string, len(msgs))
	counts := make([]int, len(msgs))
	var prev *telegram.Message
	for i, msg := range msgs {
		blocks[i] = m.gridBlock(msg, prev)
		counts[i] = len(blocks[i])
		prev = msg
	}
	return blocks, counts
}

// lineCounts is renderedMessages when only the line index is needed. It
// serves every hit from the cache once the messages have been drawn, so
// scrolling no longer re-renders the history on each keypress.
func (m Model) lineCounts() []int {
	_, counts := m.renderedMessages(m.store.Messages.Get(m.chatID))
	return counts
}

// totalRenderedLines sums per-message line counts, i.e. the total number
// of lines View() draws for the whole loaded history.
func totalRenderedLines(counts []int) int {
	total := 0
	for _, c := range counts {
		total += c
	}
	return total
}

// maxScrollOffset is the largest scrollOffset that still shows content:
// the oldest message's first line sitting at the top of the body.
func (m Model) maxScrollOffset() int {
	max := totalRenderedLines(m.lineCounts()) - m.bodyHeight()
	if max < 0 {
		return 0
	}
	return max
}

// ScrollByLines scrolls the message view by n lines (positive = up/older).
func (m *Model) ScrollByLines(n int) {
	// Scrolling by hand takes over from a jump still waiting to settle.
	m.pendingJumpID = 0
	m.scrollOffset += n
	m.clampScroll()
}

// LazyMediaCmd starts the thumbnail download for any photo that the
// current scroll position brought near the viewport but that the
// open-time prefetch cap skipped. The keyboard scroll handlers call it
// themselves; it is exported for the host's mouse-wheel path, which
// drives ScrollByLines and would otherwise never trigger a lazy load.
func (m *Model) LazyMediaCmd() tea.Cmd { return m.lazyPhotoCmd() }

// MarkReadCmd marks the open chat read up to its newest loaded message,
// without moving the scroll position — the point of an explicit mark-read is
// to clear the badge while you keep reading where you are.
//
// Unlike the automatic read receipts in Update, this does NOT wait for
// terminal focus. Those are inferred from "the user is looking at it", which
// is only true when the terminal has focus; this one was asked for
// explicitly, so deferring it would just make the command look broken.
//
// Returns nil when there is nothing to mark, so a caller can treat a nil Cmd
// as "no chat open or no messages loaded".
func (m *Model) MarkReadCmd() tea.Cmd {
	if m.chatID == 0 {
		return nil
	}
	msgs := m.store.Messages.Get(m.chatID)
	if len(msgs) == 0 {
		return nil
	}

	chatID, msgID := m.chatID, msgs[len(msgs)-1].ID
	tg := m.tg
	if tg == nil {
		return nil
	}

	// A pending blurred-focus receipt is now redundant: this marks at least
	// as far. Clearing it stops FocusMsg re-sending an older message ID.
	m.pendingReadID = 0

	return func() tea.Msg {
		tg.ViewMessages(chatID, []int64{msgID})
		return nil
	}
}

// OpenChat opens a chat scrolled to its newest message.
func (m *Model) OpenChat(chatID int64, title string) tea.Cmd {
	return m.OpenChatAt(chatID, title, 0)
}

// OpenChatAt opens a chat and, once history has loaded, scrolls so that
// targetMsgID is visible (roughly centred). targetMsgID 0 means "newest
// message at the bottom", i.e. plain OpenChat behaviour. If the target is
// not in the first page, up to maxTargetPages further pages are fetched
// backwards; if it is still not found the view settles at the oldest
// loaded message and a notice is shown in the header.
func (m *Model) OpenChatAt(chatID int64, title string, targetMsgID int64) tea.Cmd {
	m.gen++
	m.chatID = chatID
	m.chatTitle = title
	m.scrollOffset = 0
	m.cursorID = 0 // re-anchors to this chat's newest message on first paint
	m.cursorPinned = false
	m.pendingCount = 0
	m.revealedID = 0
	m.loading = true
	m.loadStatus = "Loading messages..."
	m.mediaStatus = ""
	m.notice = ""
	m.targetMsgID = targetMsgID
	m.targetPages = 0
	m.pendingJumpID = 0
	m.pendingMeta = nil
	m.pendingReadID = 0
	m.metaBusy = false
	m.typing = nil
	m.clearSearch()
	m.cache.clear()

	// Snapshot where the unread run starts, once, here. The live marker
	// moves as read receipts are sent; this one must not, or the divider
	// walks down the screen ahead of the reader and they never see the
	// boundary they opened the chat to find. It is resolved to a message
	// ID by historyLoadedMsg, once there is history to resolve it against.
	m.unreadFromID = 0
	m.unreadCount = 0
	if entry, ok := m.store.Chats.Get(chatID); ok && entry.Chat != nil {
		m.unreadCount = int(entry.UnreadCount)
		m.unreadAfterID = entry.Chat.LastReadInboxMessageID
	} else {
		m.unreadAfterID = 0
	}

	gen, tg := m.gen, m.tg
	return tea.Batch(
		m.loadHistoryCmd(gen, chatID, 0),
		func() tea.Msg { tg.OpenChat(chatID); return nil },
	)
}

type historyLoadedMsg struct {
	gen      int
	chatID   int64
	messages []*telegram.Message
	err      error
}

// metaWork is the trailing meta pipeline still owed for a page: the photo
// thumbnails to prefetch and the sender IDs that did not make the priority
// window. It rides along on the stage messages so each stage knows what to
// hand to the next one, which keeps the pipeline a single linear chain
// with exactly one "last stage" (where a pending jump settles).
type metaWork struct {
	photos  []*telegram.Message
	senders []int64
}

func (w metaWork) empty() bool { return len(w.photos) == 0 && len(w.senders) == 0 }

// sendersFetchedMsg reports the end of one sender-resolution stage.
// userIDs are the users actually fetched (their bubbles need re-render);
// work is what the pipeline still owes.
type sendersFetchedMsg struct {
	gen     int
	chatID  int64
	userIDs []int64
	work    metaWork
}

// photosFetchedMsg reports the end of a photo-thumbnail stage. msgIDs
// are the photo messages whose art may now have changed.
type photosFetchedMsg struct {
	gen    int
	chatID int64
	msgIDs []int64
	work   metaWork
}

type messageFetchedMsg struct {
	chatID  int64
	message *telegram.Message
}

func (m *Model) loadHistoryCmd(gen int, chatID int64, fromMsgId int64) tea.Cmd {
	tg := m.tg
	return func() tea.Msg {
		msgs, err := tg.GetChatHistory(chatID, fromMsgId, 0, 50)
		if err != nil {
			return historyLoadedMsg{gen: gen, chatID: chatID, err: err}
		}
		return historyLoadedMsg{gen: gen, chatID: chatID, messages: msgs}
	}
}

// senderTargets splits the unknown senders of a page into the ones worth
// resolving first — those who sent one of the newest priorityWindow
// messages, i.e. what the reader is looking at — and the rest, which
// trail. Both lists are newest-message-first.
func senderTargets(msgs []*telegram.Message, st *store.Store, priorityWindow int) (priority, trailing []int64) {
	seen := make(map[int64]bool)
	for i := len(msgs) - 1; i >= 0; i-- {
		sender, ok := msgs[i].SenderID.(*telegram.MessageSenderUser)
		if !ok || seen[sender.UserID] {
			continue
		}
		seen[sender.UserID] = true
		if st != nil {
			if _, exists := st.Users.Get(sender.UserID); exists {
				continue
			}
		}
		if len(msgs)-1-i < priorityWindow {
			priority = append(priority, sender.UserID)
		} else {
			trailing = append(trailing, sender.UserID)
		}
	}
	return priority, trailing
}

// fetchSendersCmd resolves the given user IDs with at most
// metaFetchConcurrency requests in flight, then hands `work` on.
func (m Model) fetchSendersCmd(gen int, chatID int64, wanted []int64, work metaWork) tea.Cmd {
	tg, st := m.tg, m.store
	return func() tea.Msg {
		var (
			mu      sync.Mutex
			wg      sync.WaitGroup
			fetched []int64
		)
		sem := make(chan struct{}, metaFetchConcurrency)
		for _, id := range wanted {
			wg.Add(1)
			go func(id int64) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				user, err := tg.GetUser(id)
				if err != nil || user == nil {
					return
				}
				st.Users.Set(user)
				mu.Lock()
				fetched = append(fetched, id)
				mu.Unlock()
			}(id)
		}
		wg.Wait()

		return sendersFetchedMsg{gen: gen, chatID: chatID, userIDs: fetched, work: work}
	}
}

// fetchPhotosCmd downloads the thumbnail of each photo message, again
// with at most metaFetchConcurrency downloads in flight.
func (m Model) fetchPhotosCmd(gen int, chatID int64, msgs []*telegram.Message, work metaWork) tea.Cmd {
	tg, st := m.tg, m.store
	maxBytes := m.photoMaxBytes()
	return func() tea.Msg {
		order, wanted := photoDownloadTargets(msgs, maxBytes)

		var (
			mu   sync.Mutex
			wg   sync.WaitGroup
			done []int64
		)
		sem := make(chan struct{}, metaFetchConcurrency)
		for _, fileID := range order {
			wg.Add(1)
			go func(fileID string, msgIDs []int64) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				file, err := tg.DownloadFileSync(fileID)
				if err != nil || file == nil {
					return
				}
				st.Files.Update(file)
				mu.Lock()
				done = append(done, msgIDs...)
				mu.Unlock()
			}(fileID, wanted[fileID])
		}
		wg.Wait()

		return photosFetchedMsg{gen: gen, chatID: chatID, msgIDs: done, work: work}
	}
}

// nextMetaCmd advances the trailing meta pipeline one stage: photos
// first (they change bubble heights, so the sooner the layout settles the
// better), then the trailing senders. It returns nil when the pipeline is
// exhausted, which is what marks the last stage.
func (m *Model) nextMetaCmd(work metaWork) tea.Cmd {
	if work.empty() {
		return nil
	}
	if len(work.photos) > 0 {
		photos := work.photos
		work.photos = nil
		return m.fetchPhotosCmd(m.gen, m.chatID, photos, work)
	}
	senders := work.senders
	work.senders = nil
	return m.fetchSendersCmd(m.gen, m.chatID, senders, work)
}

// finishMeta ends the trailing pipeline: the layout is final, so a jump
// that was waiting for it can settle.
func (m *Model) finishMeta() {
	m.metaBusy = false
	m.settleJump()
	m.clampScroll()
}

// photoDownloadTargets returns the thumbnails a page still needs, keyed by
// FILE rather than by message: the same photo can appear twice in one page
// (e.g. a forward of a message already in the page). The file registry
// coalesces concurrent DownloadFileSync calls for one key; grouping here
// still avoids duplicate work on the UI side. Each file is downloaded once
// and its result fanned out to every message that shows it. order preserves
// page order so downloads start top-down.
func photoDownloadTargets(msgs []*telegram.Message, maxBytes int64) (order []string, wanted map[string][]int64) {
	wanted = make(map[string][]int64)
	for _, msg := range msgs {
		photo, ok := msg.Content.(*telegram.MessagePhoto)
		if !ok || photo.Photo == nil || len(photo.Photo.Sizes) == 0 {
			continue
		}
		target := thumbnailSize(photo.Photo)
		if target == nil || target.File == nil || target.File.Downloaded {
			continue
		}
		if fileExceedsLimit(target.File, maxBytes) {
			continue
		}
		id := target.File.ID
		if _, seen := wanted[id]; !seen {
			order = append(order, id)
		}
		wanted[id] = append(wanted[id], msg.ID)
	}
	return order, wanted
}

// thumbnailSize picks the largest photo size no wider than 320px, skipping
// sizes with no downloadable file behind them (stripped/cached sizes are
// dropped upstream, but a nil File must never reach a download call).
func thumbnailSize(photo *telegram.Photo) *telegram.PhotoSize {
	if photo == nil || len(photo.Sizes) == 0 {
		return nil
	}
	var target *telegram.PhotoSize
	for _, sz := range photo.Sizes {
		if sz == nil || sz.File == nil || sz.File.ID == "" {
			continue
		}
		if target == nil {
			target = sz
			continue
		}
		if sz.Width <= 320 && sz.Width > target.Width {
			target = sz
		}
	}
	return target
}

// bestPhotoSize picks the largest downloadable size of a photo — the one
// worth handing to an external viewer. Sizes without a registered file are
// skipped rather than dereferenced, and the largest is chosen by area
// rather than by position, so an out-of-order Sizes slice cannot hand back
// a thumbnail (or nil) where the full-size image was meant.
func bestPhotoSize(photo *telegram.Photo) *telegram.PhotoSize {
	if photo == nil {
		return nil
	}
	var best *telegram.PhotoSize
	for _, sz := range photo.Sizes {
		if sz == nil || sz.File == nil || sz.File.ID == "" {
			continue
		}
		if best == nil || sz.Width*sz.Height > best.Width*best.Height {
			best = sz
		}
	}
	return best
}

// needsThumbnail reports whether a message is a photo whose thumbnail is
// not on disk yet. The store is the authority: DownloadFileSync returns a
// fresh File value rather than mutating the one hanging off the message,
// so msg.…File.Downloaded stays false for the whole session and cannot be
// used on its own to decide what still needs fetching.
func needsThumbnail(msg *telegram.Message, st *store.Store, maxBytes int64) bool {
	photo, ok := msg.Content.(*telegram.MessagePhoto)
	if !ok {
		return false
	}
	target := thumbnailSize(photo.Photo)
	if target == nil || target.File == nil || target.File.Downloaded {
		return false
	}
	if fileExceedsLimit(target.File, maxBytes) {
		return false
	}
	if st != nil && st.Files.IsComplete(target.File.ID) {
		return false
	}
	return true
}

// fileExceedsLimit reports whether f is known to be larger than maxBytes.
// Size 0 is treated as unknown and allowed. maxBytes <= 0 means no limit.
func fileExceedsLimit(f *telegram.File, maxBytes int64) bool {
	if f == nil || maxBytes <= 0 || f.Size <= 0 {
		return false
	}
	return f.Size > maxBytes
}

func (m Model) photoMaxBytes() int64 {
	if m.autoDownloadLimitMB <= 0 {
		return 0
	}
	return int64(m.autoDownloadLimitMB) << 20
}

// photoPrefetchTargets is the open-chat photo prefetch, gated on
// AutoDownloadPhotos. Tests that never call ApplyMedia keep the default
// (enabled) via New.
func (m Model) photoPrefetchTargets(msgs []*telegram.Message) []*telegram.Message {
	if !m.autoDownloadPhotos {
		return nil
	}
	return recentPhotoTargets(msgs, m.store, photoPrefetchLimit, m.photoMaxBytes())
}

// recentPhotoTargets returns at most limit photo messages that still need
// a thumbnail, preferring the newest — the ones the reader lands on when
// the chat opens at the bottom. The result stays in page order (oldest
// first) so downloads still start with the topmost of the chosen set.
func recentPhotoTargets(msgs []*telegram.Message, st *store.Store, limit int, maxBytes int64) []*telegram.Message {
	if limit <= 0 {
		return nil
	}
	var out []*telegram.Message
	for i := len(msgs) - 1; i >= 0 && len(out) < limit; i-- {
		if needsThumbnail(msgs[i], st, maxBytes) {
			out = append(out, msgs[i])
		}
	}
	// Collected newest-first; hand back oldest-first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// visiblePhotoTargets returns the photo messages whose rendered lines fall
// inside the visible window, widened by margin lines on both sides, and
// which still need a thumbnail. This is the lazy half of the prefetch cap:
// photos older than photoPrefetchLimit download when scrolled near, using
// the very same line index the scroll handlers already maintain.
func (m Model) visiblePhotoTargets(margin int) []*telegram.Message {
	msgs := m.store.Messages.Get(m.chatID)
	if len(msgs) == 0 {
		return nil
	}
	counts := m.lineCounts()
	total := totalRenderedLines(counts)

	end := total - m.scrollOffset
	if end > total {
		end = total
	}
	if end < 0 {
		end = 0
	}
	start := end - m.bodyHeight()

	lo, hi := start-margin, end+margin

	var out []*telegram.Message
	pos := 0
	for i, msg := range msgs {
		first, last := pos, pos+counts[i]
		pos = last
		if last <= lo || first >= hi {
			continue
		}
		if needsThumbnail(msg, m.store, m.photoMaxBytes()) {
			out = append(out, msg)
			if len(out) >= photoPrefetchLimit {
				break
			}
		}
	}
	return out
}

// lazyPhotoCmd starts a thumbnail download for the photos near the current
// scroll position, unless the meta pipeline is already busy (which also
// keeps a held-down scroll key from firing one command per repeat).
func (m *Model) lazyPhotoCmd() tea.Cmd {
	if !m.autoDownloadPhotos || m.tg == nil || m.metaBusy || m.chatID == 0 {
		return nil
	}
	targets := m.visiblePhotoTargets(photoLazyMargin)
	if len(targets) == 0 {
		return nil
	}
	m.metaBusy = true
	return m.fetchPhotosCmd(m.gen, m.chatID, targets, metaWork{})
}

// invalidateBySender drops cached bubbles of the open chat's messages sent
// by any of the given users: the sender name is part of the bubble, so a
// name that only became known now changes the render.
func (m Model) invalidateBySender(userIDs []int64) {
	if len(userIDs) == 0 {
		return
	}
	ids := make(map[int64]struct{}, len(userIDs))
	for _, id := range userIDs {
		ids[id] = struct{}{}
	}
	for _, msg := range m.store.Messages.Get(m.chatID) {
		if sender, ok := msg.SenderID.(*telegram.MessageSenderUser); ok {
			if _, hit := ids[sender.UserID]; hit {
				m.cache.invalidate(msg.ID)
			}
		}
	}
}

// invalidateByFile drops cached bubbles of photo messages that reference
// the given file: photo art is rendered from the downloaded file.
func (m Model) invalidateByFile(fileID string) {
	if fileID == "" {
		return
	}
	for _, msg := range m.store.Messages.Get(m.chatID) {
		photo, ok := msg.Content.(*telegram.MessagePhoto)
		if !ok || photo.Photo == nil {
			continue
		}
		for _, sz := range photo.Photo.Sizes {
			if sz.File != nil && sz.File.ID == fileID {
				m.cache.invalidate(msg.ID)
				break
			}
		}
	}
}

// hasMessage reports whether a message is in the open chat's loaded set.
func (m Model) hasMessage(id int64) bool {
	for _, msg := range m.store.Messages.Get(m.chatID) {
		if msg.ID == id {
			return true
		}
	}
	return false
}

// scrollToMessage positions the target message inside the body, roughly
// centred, using the cached line index. It reports whether the message was
// found.
func (m *Model) scrollToMessage(id int64) bool {
	msgs := m.store.Messages.Get(m.chatID)
	counts := m.lineCounts()
	total := totalRenderedLines(counts)
	bodyH := m.bodyHeight()

	startLine, msgLines := 0, 0
	pos, found := 0, false
	for i, msg := range msgs {
		if msg.ID == id {
			startLine, msgLines, found = pos, counts[i], true
			break
		}
		pos += counts[i]
	}
	if !found {
		return false
	}

	// Top line of the window that centres the message vertically.
	winStart := startLine - (bodyH-msgLines)/2
	if winStart < 0 {
		winStart = 0
	}
	maxStart := total - bodyH
	if maxStart < 0 {
		maxStart = 0
	}
	if winStart > maxStart {
		winStart = maxStart
	}

	// scrollOffset counts lines from the bottom of the history.
	m.scrollOffset = total - bodyH - winStart
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	// A jump is the one movement that says which message it is about, so
	// it sets the cursor outright: land on a search hit and r replies to
	// the hit, not to whatever happens to sit at the edge of the window.
	m.setCursor(id)
	return true
}

// settleJump re-applies a pending jump target, then forgets it. Called
// when the last meta stage of the target's generation finishes, so the
// message stays visible after late-arriving sender names and photo art
// have changed the bubble heights around it.
func (m *Model) settleJump() {
	if m.pendingJumpID == 0 {
		return
	}
	m.scrollToMessage(m.pendingJumpID)
	m.pendingJumpID = 0
}

// clampScroll re-clamps the scroll position against the current line
// index. Needed whenever the history shrinks (deletions): without it
// scrollOffset stays past the end and View draws a blank body until the
// next scroll keypress.
func (m *Model) clampScroll() {
	switch {
	case m.scrollOffset < 0:
		m.scrollOffset = 0
	default:
		if maxOffset := m.maxScrollOffset(); m.scrollOffset > maxOffset {
			m.scrollOffset = maxOffset
		}
	}
	m.syncCursor()
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case historyLoadedMsg:
		// Generation guard first (chat switched), chatID as a second guard.
		if msg.gen != m.gen || msg.chatID != m.chatID {
			return m, nil
		}
		if msg.err != nil {
			m.loading = false
			m.loadStatus = ""
			m.targetMsgID = 0
			m.pendingJumpID = 0
			m.notice = "could not load messages"
			return m, nil
		}
		if len(msg.messages) == 0 {
			m.loading = false
			m.loadStatus = ""
			if m.targetMsgID != 0 {
				m.targetMsgID = 0
				m.notice = "message not in loaded history"
				m.scrollOffset = m.maxScrollOffset()
			}
			return m, nil
		}

		reversed := make([]*telegram.Message, len(msg.messages))
		for i, v := range msg.messages {
			reversed[len(msg.messages)-1-i] = v
		}
		m.store.Messages.Prepend(m.chatID, reversed)
		m.pendingMeta = append(m.pendingMeta, reversed...)
		m.resolveUnreadDivider()

		if m.targetMsgID != 0 {
			switch {
			case m.hasMessage(m.targetMsgID):
				m.scrollToMessage(m.targetMsgID)
				// Hold on to it: the sender/photo stages still to come
				// change bubble heights, so the jump is re-applied when
				// the last of them lands.
				m.pendingJumpID = m.targetMsgID
				m.targetMsgID = 0
			case m.targetPages < maxTargetPages:
				// Keep paging backwards for the target, then resolve the
				// senders of everything fetched so far.
				m.targetPages++
				m.loadStatus = "Searching for message..."
				if oldest := m.store.Messages.OldestMessageId(m.chatID); oldest != 0 {
					return m, m.loadHistoryCmd(m.gen, m.chatID, oldest)
				}
				fallthrough
			default:
				m.targetMsgID = 0
				m.notice = "message not in loaded history"
				m.scrollOffset = m.maxScrollOffset()
			}
		}

		// FIRST PAINT. The text of the page is in the store and the next
		// View() draws it; everything below is trailing meta that must
		// never hold the body back or take a line away from it.
		m.loading = false
		m.loadStatus = ""

		page := m.pendingMeta
		m.pendingMeta = nil

		priority, trailing := senderTargets(page, m.store, senderPriorityWindow)
		work := metaWork{
			photos:  m.photoPrefetchTargets(page),
			senders: trailing,
		}
		if len(priority) > 0 {
			m.metaBusy = true
			return m, m.fetchSendersCmd(m.gen, m.chatID, priority, work)
		}
		if cmd := m.nextMetaCmd(work); cmd != nil {
			m.metaBusy = true
			return m, cmd
		}
		m.metaBusy = false
		m.settleJump()
		return m, nil

	case sendersFetchedMsg:
		if msg.gen != m.gen || msg.chatID != m.chatID {
			return m, nil
		}
		m.invalidateBySender(msg.userIDs)
		if cmd := m.nextMetaCmd(msg.work); cmd != nil {
			return m, cmd
		}
		m.finishMeta()
		return m, nil

	case photosFetchedMsg:
		if msg.gen != m.gen || msg.chatID != m.chatID {
			return m, nil
		}
		m.cache.invalidate(msg.msgIDs...)
		if cmd := m.nextMetaCmd(msg.work); cmd != nil {
			return m, cmd
		}
		// Last stage for this generation: photo bubbles just grew from a
		// one-line placeholder to multi-line art, so re-apply the jump.
		m.finishMeta()

	case searchResultsMsg:
		return m.handleSearchResults(msg)

	case telegram.NewMessageMsg:
		if msg.Message.ChatID == m.chatID {
			m.store.Messages.Append(m.chatID, msg.Message)
			m.cache.invalidate(msg.Message.ID)

			// Only claim the message was read when the terminal actually
			// has focus; otherwise remember it and catch up on FocusMsg.
			if m.blurred {
				if msg.Message.ID > m.pendingReadID {
					m.pendingReadID = msg.Message.ID
				}
				return m, nil
			}
			chatID, msgID := m.chatID, msg.Message.ID
			tg := m.tg
			return m, func() tea.Msg {
				tg.ViewMessages(chatID, []int64{msgID})
				return nil
			}
		}

	case telegram.ChatActionMsg:
		// The typing indicator is a row of the thread grid in TUI 2.0, so
		// the thread is what tracks who is typing. Actions for other chats
		// are dropped rather than accumulated: nothing shows them, and a
		// map keyed by chat would grow for the life of the session.
		if msg.ChatId == m.chatID && msg.UserId != 0 {
			m.typing = applyChatAction(m.typing, msg)
		}

	case tea.FocusMsg:
		m.blurred = false
		if m.chatID != 0 && m.pendingReadID != 0 {
			chatID, msgID := m.chatID, m.pendingReadID
			m.pendingReadID = 0
			tg := m.tg
			return m, func() tea.Msg {
				tg.ViewMessages(chatID, []int64{msgID})
				return nil
			}
		}

	case tea.BlurMsg:
		m.blurred = true

	case telegram.MessageEditedMsg:
		if msg.ChatId == m.chatID {
			tg := m.tg
			return m, func() tea.Msg {
				fetched, _ := tg.GetMessage(msg.ChatId, msg.MessageId)
				if fetched != nil {
					return messageFetchedMsg{chatID: msg.ChatId, message: fetched}
				}
				return nil
			}
		}

	case telegram.MessageDeletedMsg:
		if msg.ChatId == 0 {
			// Non-channel deletions carry no peer — remove from all chats.
			m.store.Messages.DeleteFromAll(msg.MessageIds)
			m.cache.invalidate(msg.MessageIds...)
			// A deleted message can never be scrolled to: leaving it among
			// the hits makes n burn a three-page backwards hunt and then
			// report "message not in loaded history", every time around.
			m.pruneSearchHits(msg.MessageIds)
			// The history just got shorter: re-clamp now, or View draws a
			// blank body until the next scroll keypress.
			m.clampScroll()
		} else if msg.ChatId == m.chatID {
			m.store.Messages.Delete(m.chatID, msg.MessageIds)
			m.cache.invalidate(msg.MessageIds...)
			m.pruneSearchHits(msg.MessageIds)
			m.clampScroll()
		}

	case telegram.MessageSendSucceededMsg:
		if msg.Message.ChatID == m.chatID {
			m.store.Messages.ReplaceMessageId(m.chatID, msg.OldMessageId, msg.Message)
			m.cache.invalidate(msg.OldMessageId, msg.Message.ID)
		}

	case messageFetchedMsg:
		if msg.chatID == m.chatID && msg.message != nil {
			m.store.Messages.UpdateMessage(m.chatID, msg.message.ID, msg.message)
			m.cache.invalidate(msg.message.ID)
		}

	case telegram.FileUpdateMsg:
		if msg.File != nil {
			m.store.Files.Update(msg.File)
			m.invalidateByFile(msg.File.ID)
		}

	case MediaPlayMsg:
		m.mediaStatus = msg.Info

	case tea.KeyPressMsg:
		if m.focused {
			return m.handleKey(msg)
		}
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	// While the search input is open it owns every key.
	if m.searchActive {
		return m.handleSearchKey(msg)
	}

	// kp carries both spellings of the key event (Keystroke() and String())
	// so a configured, possibly-modified binding (e.g. reply = "ctrl+r")
	// matches correctly even on terminals where String() alone would be
	// wrong for a modified key. See internal/keys.Press's doc comment for
	// the full Kitty-protocol rationale. Bare, unmodified letters/arrows/
	// ctrl-combos match identically to before — Matches falls back to
	// String() exactly when nothing is modified.
	kp := keys.NewPress(msg)

	// A digit is a count prefix, not a command. Taken before the motion
	// switch so no binding has to know about it, and before the isScroll
	// test so a digit does not cancel a pending jump.
	if m.countDigit(msg.String()) {
		return m, nil
	}

	isScroll := kp.Matches(
		"up", "k", m.keys.scrollUpExtra,
		"down", "j", m.keys.scrollDownExtra,
		"G", "end", "g", "home",
		"ctrl+u", "ctrl+d",
		"pgup", m.keys.pageUpExtra,
		"pgdown", m.keys.pageDownExtra,
	)
	if isScroll {
		// Scrolling by hand takes over from a jump still waiting to settle.
		m.pendingJumpID = 0
	}

	// Spent here, once, whatever the key turns out to be: a count is
	// attached to the motion that follows it and does not survive into the
	// next one. count is 1 when none was typed.
	count := m.takeCount()

	switch {
	case kp.Matches("up", "k", m.keys.scrollUpExtra):
		m.scrollOffset += 3 * count
		if cmd := m.clampScrollUp(); cmd != nil {
			return m, cmd
		}
	case kp.Matches("pgup", m.keys.pageUpExtra):
		m.scrollOffset += m.pageStep() * count
		if cmd := m.clampScrollUp(); cmd != nil {
			return m, cmd
		}
	case kp.Matches("down", "j", m.keys.scrollDownExtra):
		m.scrollOffset -= 3 * count
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
	case kp.Matches("pgdown", m.keys.pageDownExtra):
		m.scrollOffset -= m.pageStep() * count
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
	case kp.Matches("}"):
		m.moveCursor(count)
		return m, m.lazyPhotoCmd()
	case kp.Matches("{"):
		m.moveCursor(-count)
		return m, m.lazyPhotoCmd()
	case kp.Matches("G", "end"):
		m.scrollOffset = 0
		// Back to the bottom is "stop holding my place": from here the
		// cursor follows arrivals again.
		m.unpinCursor()
	case kp.Matches("g", "home"):
		m.scrollOffset = m.maxScrollOffset()
	case kp.Matches("ctrl+u"):
		m.scrollOffset += m.height * count
		if maxOffset := m.maxScrollOffset(); m.scrollOffset > maxOffset {
			m.scrollOffset = maxOffset
		}
	case kp.Matches("ctrl+d"):
		m.scrollOffset -= m.height * count
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}

	case kp.Matches("esc"):
		// Vim-style: the first esc after a search drops the held hits, so
		// n/N stop being claimed by this panel and the host can go back to
		// quick-typing words that start with n/N. A second esc is the
		// host's plain "back" again.
		if len(m.searchHits) > 0 {
			m.searchHits = nil
			m.searchIdx = 0
			m.searchQuery = ""
			m.notice = ""
		}
		return m, nil

	case kp.Matches("ctrl+f"):
		m.OpenFind()
		return m, nil

	case kp.Matches("n"):
		if len(m.searchHits) == 0 {
			return m, nil
		}
		cmd := m.jumpToHit(m.searchIdx + 1)
		return m, cmd
	case kp.Matches("N"):
		if len(m.searchHits) == 0 {
			return m, nil
		}
		cmd := m.jumpToHit(m.searchIdx - 1)
		return m, cmd

	case kp.Matches(m.keys.reply):
		return m, m.messageAction("reply")
	case kp.Matches(m.keys.edit):
		return m, m.messageAction("edit")
	case kp.Matches(m.keys.delete):
		return m, m.messageAction("delete")

	// Enter opens the cursored message's media. A photo raises the overlay;
	// everything else keeps going to the platform, because a video or a
	// document has no in-terminal form this client can draw.
	case kp.Matches("enter"):
		if cmd := m.OverlayPhotoCmd(); cmd != nil {
			return m, cmd
		}
		return m, m.playMedia()

	// 'o' opens externally, always — it is the way past the overlay for a
	// photo the terminal draws badly, and the overlay's own hint row
	// advertises it.
	case kp.Matches("o"):
		return m, m.playMedia()

	// 's' saves/downloads file
	case kp.Matches("s"):
		return m, m.downloadFile()

	// 'y' yanks the message's text to the system clipboard. A nil Cmd means
	// there was nothing to copy, and the host says so rather than leaving
	// the press to look like it worked.
	case kp.Matches("y"):
		if cmd := m.YankCmd(); cmd != nil {
			return m, cmd
		}
		return m, func() tea.Msg { return YankMsg{} }

	// space plays the cursored voice note.
	case kp.Matches("space", " "):
		return m, m.PlayVoiceCmd()

	// 'M' marks the chat read without moving. The whole point is to clear
	// the badge while staying where you are reading, so it deliberately
	// does not scroll and does not touch the unread divider — that stays
	// put until the buffer is left.
	case kp.Matches("M"):
		return m, m.MarkReadCmd()

	// 'x' opens the spoilers in the message under the cursor, and closes
	// them again. A toggle rather than a one-way reveal: having looked, the
	// reader may well want the screen safe again, and there is otherwise no
	// way back short of leaving the chat.
	case kp.Matches("x"):
		if msg := m.cursorMessage(); msg != nil && msg.ID != 0 {
			if m.revealedID == msg.ID {
				m.revealedID = 0
			} else {
				m.revealedID = msg.ID
			}
		}
		return m, nil
	}

	// Any key that moved the viewport may have brought photos whose
	// thumbnails were skipped by the open-time prefetch cap into view,
	// and may have carried the cursor off the visible window.
	if isScroll {
		m.syncCursor()
		cmd := m.lazyPhotoCmd()
		return m, cmd
	}
	return m, nil
}

// pageStep is how far pgup/pgdown move: one body-height minus a line of
// overlap, so the reader keeps a line of context across the jump.
func (m Model) pageStep() int {
	step := m.bodyHeight() - 1
	if step < 1 {
		step = 1
	}
	return step
}

// clampScrollUp clamps an upward scroll against the loaded history and,
// when it lands on the oldest loaded message, pulls in the next older
// page. It returns that load command, or nil when nothing had to be
// loaded (the caller then falls through to the lazy-thumbnail check).
func (m *Model) clampScrollUp() tea.Cmd {
	maxOffset := m.maxScrollOffset()
	if m.scrollOffset <= maxOffset {
		return nil
	}
	// At the top of what is loaded: pull in an older page.
	m.scrollOffset = maxOffset
	m.syncCursor()
	oldest := m.store.Messages.OldestMessageId(m.chatID)
	if oldest != 0 && !m.loading && m.tg != nil {
		m.loading = true
		m.loadStatus = "Loading older messages..."
		return m.loadHistoryCmd(m.gen, m.chatID, oldest)
	}
	return nil
}

// visibleMessages returns the index range [first, last] of the messages
// that have at least one line inside the body window, and whether any do.
//
// View() shows lines [total-bodyH-scrollOffset : total-scrollOffset) of the
// full rendered history (oldest message first), so scrollOffset counts
// rendered lines up from the bottom (newest) message. Line counts come from
// the render cache, so this costs no extra rendering.
func (m Model) visibleMessages() (first, last int, ok bool) {
	msgs := m.store.Messages.Get(m.chatID)
	if len(msgs) == 0 {
		return 0, 0, false
	}

	counts := m.lineCounts()
	total := totalRenderedLines(counts)
	end := total - m.scrollOffset
	if end > total {
		end = total
	}
	start := end - m.bodyHeight()
	if start < 0 {
		start = 0
	}
	if end <= start {
		// Scrolled past the top of the history: the window shows the
		// oldest message and nothing else can be acted on.
		return 0, 0, true
	}

	first, last, found := 0, 0, false
	pos := 0
	for i, c := range counts {
		if pos < end && pos+c > start {
			if !found {
				first, found = i, true
			}
			last = i
		}
		pos += c
	}
	if !found {
		return 0, 0, true
	}
	return first, last, true
}

// cursorMessage resolves cursorID against the loaded history, falling back
// to the newest visible message when the cursor has not been anchored yet
// or its message is gone (deleted, or dropped by a chat switch).
//
// Reads never mutate — syncCursor owns the anchoring — so a value copy of
// the model cannot silently disagree with the one Update is holding.
func (m Model) cursorMessage() *telegram.Message {
	msgs := m.store.Messages.Get(m.chatID)
	if len(msgs) == 0 {
		return nil
	}
	// Tail mode is resolved here as well as in syncCursor, so a message
	// arriving between two keypresses cannot leave a stale identity
	// behind: at the bottom of the history the cursor IS the newest
	// message, with nothing stored to go out of date.
	//
	// Unless the reader placed it. A cursor moved with } or { is theirs
	// until they give it back, even at the bottom of the history — the
	// whole point of moving it there is to act on something other than
	// the newest message.
	if m.scrollOffset <= 0 && !m.cursorPinned {
		return msgs[len(msgs)-1]
	}
	if m.cursorID != 0 {
		for _, msg := range msgs {
			if msg.ID == m.cursorID {
				return msg
			}
		}
	}
	_, last, ok := m.visibleMessages()
	if !ok {
		return nil
	}
	return msgs[last]
}

// syncCursor anchors the cursor back into the visible window: it is left
// alone while its message is on screen, and clamped to the nearest visible
// message when a scroll has carried it off.
//
// Clamping to the *nearest* end rather than to a fixed one is what makes
// the two directions feel the same: scrolling towards older messages leaves
// the cursor riding the bottom of the window, scrolling back towards newer
// leaves it riding the top, and in both cases it is the message the reader
// last had their eye on.
//
// Pinned to the bottom (scrollOffset 0) the cursor is simply the newest
// message, and it follows each arrival. Stickiness there would be a bug,
// not a feature: sitting in a live chat, r has to reply to the message that
// just came in, not to whichever one the cursor was resting on when the
// conversation moved. Scrolling up leaves tail mode, and from that point
// the cursor holds its message.
func (m *Model) syncCursor() {
	msgs := m.store.Messages.Get(m.chatID)
	if len(msgs) == 0 {
		m.cursorID = 0
		return
	}
	if m.scrollOffset <= 0 && !m.cursorPinned {
		m.setCursor(msgs[len(msgs)-1].ID)
		return
	}
	first, last, ok := m.visibleMessages()
	if !ok {
		m.cursorID = 0
		return
	}

	if m.cursorID != 0 {
		for i, msg := range msgs {
			if msg.ID != m.cursorID {
				continue
			}
			switch {
			case i < first:
				m.setCursor(msgs[first].ID)
				// Scrolling the pinned message off the screen is the
				// reader letting go of it: the cursor is a position again
				// from here, and reaching the bottom resumes following
				// arrivals.
				m.cursorPinned = false
			case i > last:
				m.setCursor(msgs[last].ID)
				m.cursorPinned = false
			}
			return
		}
	}
	m.setCursor(msgs[last].ID)
}

// setCursor moves the cursor and closes any spoilers it leaves behind.
func (m *Model) setCursor(id int64) {
	if m.cursorID != id {
		m.revealedID = 0
	}
	m.cursorID = id
}

func (m Model) messageAction(action string) tea.Cmd {
	msg := m.cursorMessage()
	if msg == nil {
		return nil
	}
	if action == "edit" && !isOwnMessage(msg, m.myUserId) {
		// Telegram allows editing only your own messages, and a refusal
		// that says nothing is indistinguishable from a key that does not
		// work — which is how "e is not working either" gets reported for
		// a rule the client is right to enforce.
		return func() tea.Msg {
			return MediaPlayMsg{Status: "error", Info: "⚠ you can only edit your own messages"}
		}
	}
	return func() tea.Msg {
		return MessageActionMsg{Action: action, ChatId: m.chatID, MessageId: msg.ID}
	}
}

// playMedia downloads and plays the media in the target message.
func (m Model) playMedia() tea.Cmd {
	msg := m.cursorMessage()
	if msg == nil || msg.Content == nil {
		return nil
	}

	switch c := msg.Content.(type) {
	case *telegram.MessageVoiceNote:
		if c.VoiceNote != nil {
			return m.downloadAndPlay(fileKey(c.VoiceNote.File), "voice", "🎤 Playing voice...")
		}

	case *telegram.MessageAudio:
		if c.Audio != nil {
			return m.downloadAndPlay(fileKey(c.Audio.File), "audio", fmt.Sprintf("🎵 Playing %s...", c.Audio.Title))
		}

	case *telegram.MessageVideoNote:
		if c.VideoNote != nil {
			return m.downloadAndPlay(fileKey(c.VideoNote.File), "video", "📹 Playing video note...")
		}

	case *telegram.MessageVideo:
		if c.Video != nil {
			return m.downloadAndPlay(fileKey(c.Video.File), "video", "🎥 Opening video...")
		}

	case *telegram.MessageAnimation:
		if c.Animation != nil {
			return m.downloadAndPlay(fileKey(c.Animation.File), "video", "🎬 Opening GIF...")
		}

	case *telegram.MessageDocument:
		if c.Document != nil {
			return m.downloadAndOpen(fileKey(c.Document.File), fmt.Sprintf("📎 Opening %s...", c.Document.FileName))
		}

	case *telegram.MessagePhoto:
		// The largest size with a registered file — not blindly the last
		// entry of Sizes, whose File may be nil.
		return m.downloadAndOpen(fileKey(bestPhotoSizeFile(c.Photo)), "🖼 Opening photo...")

	case *telegram.MessageSticker:
		if c.Sticker != nil {
			return m.downloadAndOpen(fileKey(c.Sticker.File), "Opening sticker...")
		}
	}

	return nil
}

// fileKey is the download key of a file, or "" when there is nothing to
// download. Every media action goes through it, so a media message that
// arrived without a registered file is a no-op instead of a panic.
func fileKey(f *telegram.File) string {
	if f == nil {
		return ""
	}
	return f.ID
}

// bestPhotoSizeFile is bestPhotoSize's file, or nil.
func bestPhotoSizeFile(photo *telegram.Photo) *telegram.File {
	if sz := bestPhotoSize(photo); sz != nil {
		return sz.File
	}
	return nil
}

// downloadFile saves the file from the target message.
func (m Model) downloadFile() tea.Cmd {
	msg := m.cursorMessage()
	if msg == nil || msg.Content == nil {
		return nil
	}

	var key string
	var name string

	switch c := msg.Content.(type) {
	case *telegram.MessageDocument:
		if c.Document != nil {
			key, name = fileKey(c.Document.File), c.Document.FileName
		}
	case *telegram.MessagePhoto:
		// Photos carry no filename over the wire, so one is made from the
		// message id — unique, sortable, and recognisable as coming from
		// here, which "photo" was not.
		key, name = fileKey(bestPhotoSizeFile(c.Photo)),
			fmt.Sprintf("telegram-photo-%d.jpg", msg.ID)
	case *telegram.MessageVideo:
		if c.Video != nil {
			key, name = fileKey(c.Video.File), c.Video.FileName
		}
	case *telegram.MessageAudio:
		if c.Audio != nil {
			key, name = fileKey(c.Audio.File), c.Audio.FileName
		}
	case *telegram.MessageVoiceNote:
		if c.VoiceNote != nil {
			key, name = fileKey(c.VoiceNote.File),
				fmt.Sprintf("telegram-voice-%d.ogg", msg.ID)
		}
	default:
		return nil
	}
	if key == "" {
		return nil
	}

	tg, dir := m.tg, m.downloadDir
	return func() tea.Msg {
		if tg == nil {
			return MediaPlayMsg{Status: "error", Info: "not connected"}
		}
		file, err := tg.DownloadFileSync(key)
		if err != nil {
			return MediaPlayMsg{Status: "error", Info: fmt.Sprintf("⚠ download failed: %v", err)}
		}
		// The download landed in the CACHE, under a server-side id. Saving
		// is the copy out of it, under a name the sender chose and into a
		// directory the reader will look in.
		saved, err := saveInto(dir, name, file.Path)
		if err != nil {
			return MediaPlayMsg{Status: "error", Info: fmt.Sprintf("⚠ save failed: %v", err)}
		}
		return MediaPlayMsg{Status: "saved", Info: "💾 saved to " + saved}
	}
}

func (m Model) downloadAndPlay(key string, mediaType string, statusMsg string) tea.Cmd {
	if key == "" {
		return nil
	}
	voice, video, tg := m.voice, m.video, m.tg
	return func() tea.Msg {
		if tg == nil {
			return MediaPlayMsg{Status: "error", Info: "not connected"}
		}
		file, err := tg.DownloadFileSync(key)
		if err != nil {
			return MediaPlayMsg{Status: "error", Info: fmt.Sprintf("Download error: %v", err)}
		}

		path := file.Path
		switch mediaType {
		case "voice", "audio":
			if err := playVoice(voice, path); err != nil {
				return MediaPlayMsg{Status: "error", Info: fmt.Sprintf("Play error: %v", err)}
			}
		case "video":
			if err := playVideo(video, path); err != nil {
				return MediaPlayMsg{Status: "error", Info: fmt.Sprintf("Play error: %v", err)}
			}
		}

		return MediaPlayMsg{Status: "playing", Info: statusMsg}
	}
}

func playVoice(voice *media.VoicePlayer, path string) error {
	if voice != nil {
		return voice.Play(path)
	}
	var cmd *exec.Cmd
	if _, err := exec.LookPath("mpv"); err == nil {
		cmd = exec.Command("mpv", "--no-video", "--really-quiet", path)
	} else if _, err := exec.LookPath("ffplay"); err == nil {
		cmd = exec.Command("ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", path)
	} else {
		cmd = defaultOpenCmd(path)
	}
	if cmd == nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func playVideo(video *media.VideoPlayer, path string) error {
	if video != nil {
		return video.Play(path)
	}
	var cmd *exec.Cmd
	if _, err := exec.LookPath("mpv"); err == nil {
		cmd = exec.Command("mpv", path)
	} else {
		cmd = defaultOpenCmd(path)
	}
	if cmd == nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func (m Model) downloadAndOpen(key string, statusMsg string) tea.Cmd {
	if key == "" {
		return nil
	}
	tg := m.tg
	return func() tea.Msg {
		if tg == nil {
			return MediaPlayMsg{Status: "error", Info: "not connected"}
		}
		file, err := tg.DownloadFileSync(key)
		if err != nil {
			return MediaPlayMsg{Status: "error", Info: fmt.Sprintf("Download error: %v", err)}
		}

		cmd := defaultOpenCmd(file.Path)
		if cmd != nil {
			cmd.Start()
			go cmd.Wait()
		}

		return MediaPlayMsg{Status: "opened", Info: statusMsg}
	}
}

func defaultOpenCmd(path string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path)
	case "windows":
		return exec.Command("cmd", "/c", "start", path)
	default:
		return exec.Command("xdg-open", path)
	}
}

func isOwnMessage(msg *telegram.Message, myUserId int64) bool {
	if s, ok := msg.SenderID.(*telegram.MessageSenderUser); ok {
		return s.UserID == myUserId
	}
	return false
}

func (m Model) View() string {
	if m.chatID == 0 {
		return lipgloss.NewStyle().
			Width(m.width).Height(m.height).
			Foreground(m.roles.Dim).
			Align(lipgloss.Center, lipgloss.Center).
			Render("Select a chat\n\nTab to switch panels")
	}

	header := m.renderHeader()

	// One honest line under the header: the search input when it is open,
	// otherwise the blocking load stage. The trailing meta pipeline never
	// appears here — by the time it runs the messages are already drawn,
	// and taking a body line for it would shift the history under the
	// reader twice. It gets a glyph in the header instead.
	statusLine := ""
	switch {
	case m.searchActive:
		statusLine = m.renderSearchLine()
	case m.loading:
		statusLine = m.renderStatusLine()
	}

	bodyH := m.bodyHeight()
	messages := m.store.Messages.Get(m.chatID)

	if len(messages) == 0 {
		label := "No messages"
		if m.loading {
			label = m.loadStatus
		}
		body := lipgloss.NewStyle().
			Width(m.width).Height(bodyH).
			Foreground(m.roles.Dim).
			Align(lipgloss.Center, lipgloss.Center).
			Render(label)
		if statusLine != "" {
			return header + "\n" + statusLine + "\n" + body
		}
		return header + "\n" + body
	}

	blocks, counts := m.renderedMessages(messages)

	// The cursor is resolved AFTER the line index, and the selected block
	// is re-rendered on top of it rather than being cached in its selected
	// form. Selection changes colour, never height — so the index, and
	// every scroll and jump built on it, stays independent of where the
	// cursor happens to be. cursorMessage reads that same index, which is
	// why the order here matters rather than being a style choice.
	if cursor := m.cursorMessage(); cursor != nil {
		for i, msg := range messages {
			if msg != cursor {
				continue
			}
			var prev *telegram.Message
			if i > 0 {
				prev = messages[i-1]
			}
			blocks[i] = m.gridMessageLines(msg, prev, true)
			break
		}
	}

	total := totalRenderedLines(counts)

	end := total - m.scrollOffset
	if end > total {
		end = total
	}
	if end < 0 {
		end = 0
	}
	start := end - bodyH
	if start < 0 {
		start = 0
	}

	visible := sliceLines(blocks, counts, start, end)
	if typing := m.gridTypingRow(); typing != "" {
		visible = append(visible, typing)
	}
	// Pad the top with full-width blanks rather than empty strings: every
	// row this panel emits is exactly the pane width, so a caller can
	// overlay or background it without discovering that some rows are
	// shorter than others.
	blank := strings.Repeat(" ", max(m.width, 0))
	for len(visible) < bodyH {
		visible = append([]string{blank}, visible...)
	}
	if len(visible) > bodyH {
		visible = visible[len(visible)-bodyH:]
	}

	body := strings.Join(visible, "\n")
	if statusLine != "" {
		return header + "\n" + statusLine + "\n" + body
	}
	return header + "\n" + body
}

// sliceLines returns lines [start, end) of the history that blocks/counts
// describe, cutting only the blocks that actually intersect the window
// (counts is the exact per-message line index, so the rest are skipped by
// arithmetic alone).
func sliceLines(blocks [][]string, counts []int, start, end int) []string {
	if start >= end {
		return nil
	}
	var out []string
	pos := 0
	for i, lines := range blocks {
		n := counts[i]
		if pos+n <= start {
			pos += n
			continue
		}
		if pos >= end {
			break
		}
		lo, hi := 0, n
		if start > pos {
			lo = start - pos
		}
		if end < pos+n {
			hi = end - pos
		}
		if lo < 0 {
			lo = 0
		}
		if hi > len(lines) {
			hi = len(lines)
		}
		if lo < hi {
			out = append(out, lines[lo:hi]...)
		}
		pos += n
	}
	return out
}

// renderHeader draws the one-line thread header (docs/tui-2.0.md, "Thread
// grid"): sigil, bright title, ghost separator, dim subtitle on the left;
// the scroll position on the right.
//
// The right group is measured and claimed FIRST, and only the subtitle is
// elided. That order is the whole design of the row: "where am I in this
// history" is fixed-width and always true, while the subtitle is the part a
// reader can lose without losing their place. Budgeting the left side first
// would drop the position off a narrow pane, which is exactly the cell you
// cannot do without.
//
// Everything is cut by CELLS, not runes: a CJK title is one rune and two
// cells, and a rune-count cut overflows, at which point lipgloss WRAPS the
// header onto a second row and pushes the body off the bottom.
func (m Model) renderHeader() string {
	r := m.roles

	mark, markColour := "@", r.Blue
	kind := ""
	if entry, ok := m.store.Chats.Get(m.chatID); ok && entry.Chat != nil {
		// Saved Messages is the chat whose ID is your own user ID —
		// Telegram models it as a private chat with yourself.
		saved := m.myUserId != 0 && entry.Chat.ID == m.myUserId
		mark, markColour = sigil.For(entry.Chat.Type, saved, r)
		kind = sigil.Kind(entry.Chat.Type, saved)
	}

	// The right group: line position, and the meta glyph when names and
	// thumbnails are still filling in. The glyph is deliberately tiny —
	// the messages are already readable, it only says more is coming.
	// A pending count goes ahead of the position, in the one place on this
	// row that already reports state. Without it a typed digit is
	// indistinguishable from a key the thread ignores — which is what a
	// digit WAS here until the count prefix existed.
	right := m.headerPosition()
	if n := m.countLabel(); n != "" {
		right = n + "  " + right
	}
	if m.metaBusy {
		right += " ⟳"
	}
	right = " " + right + " "
	rightW := cell.Width(right)

	// The subtitle carries whatever is most worth knowing about this
	// thread right now. A transient — a media status, a search position, a
	// media affordance — outranks the standing description, because the
	// standing description is still true a second later and the transient
	// is the thing that just changed.
	subtitle := kind
	switch {
	case m.mediaStatus != "":
		subtitle = m.mediaStatus
	case m.notice != "":
		subtitle = m.notice
	case m.mediaHint() != "":
		subtitle = m.mediaHint()
	}

	title := m.chatTitle
	if title == "" {
		title = "—"
	}

	// " " + sigil + " " + title, then " │ " + subtitle.
	const lead = 3
	titleW := m.width - lead - rightW
	if titleW < 1 {
		titleW = 1
	}
	title = cell.Truncate(title, titleW)

	line := " " +
		lipgloss.NewStyle().Foreground(markColour).Render(mark) + " " +
		lipgloss.NewStyle().Foreground(r.Bright).Bold(true).Render(title)

	if subtitle != "" {
		subW := m.width - cell.Width(line) - 3 - rightW
		if subW > 0 {
			line += lipgloss.NewStyle().Foreground(r.Ghost).Render(" │ ") +
				lipgloss.NewStyle().Foreground(r.Dim).Render(cell.Truncate(subtitle, subW))
		}
	}

	pad := m.width - cell.Width(line) - rightW
	if pad < 0 {
		pad = 0
	}
	line += strings.Repeat(" ", pad) +
		lipgloss.NewStyle().Foreground(r.Faint).Render(right)

	// The header is panel, and the thread column's surface is bg, so this
	// one does paint: it is a real exception rather than a repeat.
	return cell.Fill(r.Panel, line, m.width)
}

// headerPosition is the "ln 214/214" cell: the last visible rendered line
// and the total, which is what tells a reader whether there is more history
// below them.
//
// Lines rather than messages, because lines are what the scroll position
// actually is — a message count would jump by one while the screen moved by
// twenty.
func (m Model) headerPosition() string {
	total := totalRenderedLines(m.lineCounts())
	if total == 0 {
		return "ln 0/0"
	}
	at := total - m.scrollOffset
	if at > total {
		at = total
	}
	if at < 0 {
		at = 0
	}
	return "ln " + strconv.Itoa(at) + "/" + strconv.Itoa(total)
}

// renderStatusLine draws the current loading stage label. It is one line
// tall — bodyHeight accounts for exactly that.
func (m Model) renderStatusLine() string {
	label := m.loadStatus
	if label == "" {
		label = "Working..."
	}
	line := " ⟳ " + label
	// Cell-accurate clip, not a rune-count cut and not Style.MaxWidth:
	// both let wide glyphs overflow and wrap, and this must stay exactly
	// one line tall — that is what bodyHeight subtracts for it. The label
	// is ASCII today, but it is assembled from stage strings that may not
	// stay that way.
	style := lipgloss.NewStyle().Foreground(m.roles.Dim)
	if inner := m.width - style.GetHorizontalFrameSize(); inner > 0 {
		line = cell.Clamp(line, inner)
	}
	return style.Width(m.width).Render(line)
}
