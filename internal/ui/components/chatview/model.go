package chatview

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/media"
	"github.com/imtaqin/telegram-cli/internal/render"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
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

// bubbleEntry is one cached rendered message bubble together with the
// inputs it was rendered from. RenderMessage is deterministic for a given
// (message, isOwn, width) with two exceptions, both covered here:
//
//   - the footer timestamp comes from render.FormatTimestamp, which is
//     *now*-dependent at day granularity ("15:04" today, "Yesterday 15:04",
//     "Mon 15:04", "Jan 02", "2006-01-02"). The formatted stamp is stored
//     alongside the bubble and compared on lookup, so a bubble rendered
//     before midnight is re-rendered the first time it is looked up after
//     the label changes. There is no sub-day ("2m ago") component in the
//     bubble footer, so no per-frame churn.
//   - the sender name (store.Users) and photo art (store.Files) can change
//     as async fetches land. Those are handled by explicit invalidation
//     from Update, not by re-checking on every lookup.
type bubbleEntry struct {
	width    int
	isOwn    bool
	stamp    string
	rendered string
	lines    int
}

// bubbleCache maps message ID -> rendered bubble. It is held by pointer in
// Model so that the value copies bubbletea makes of the model (Update has a
// value receiver) all share one cache.
type bubbleCache struct {
	entries map[int64]bubbleEntry
}

func newBubbleCache() *bubbleCache {
	return &bubbleCache{entries: make(map[int64]bubbleEntry)}
}

func (c *bubbleCache) get(id int64) (bubbleEntry, bool) {
	if c == nil {
		return bubbleEntry{}, false
	}
	e, ok := c.entries[id]
	return e, ok
}

func (c *bubbleCache) put(id int64, e bubbleEntry) {
	if c == nil {
		return
	}
	c.entries[id] = e
}

// invalidate drops the cached bubbles of the given message IDs.
func (c *bubbleCache) invalidate(ids ...int64) {
	if c == nil {
		return
	}
	for _, id := range ids {
		delete(c.entries, id)
	}
}

// clear empties the cache in place (in place, so every value copy of the
// model observes it).
func (c *bubbleCache) clear() {
	if c == nil {
		return
	}
	clear(c.entries)
}

func (c *bubbleCache) len() int {
	if c == nil {
		return 0
	}
	return len(c.entries)
}

type Model struct {
	store    *store.Store
	tg       *telegram.Client
	theme    *theme.Theme
	renderer *render.MessageRenderer
	cache    *bubbleCache

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
	loading      bool
	loadStatus   string // honest stage label, e.g. "Loading messages..."
	notice       string // transient notice shown in the header
	myUserId     int64
	mediaStatus  string

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
}

func New(s *store.Store, tg *telegram.Client, th *theme.Theme) Model {
	input := widgets.NewTextArea()
	input.Focused = true
	return Model{
		store:              s,
		tg:                 tg,
		theme:              th,
		renderer:           render.NewMessageRenderer(th),
		cache:              newBubbleCache(),
		searchInput:        input,
		autoDownloadPhotos: true,
	}
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

// bubble returns the rendered bubble for a message and the number of
// terminal lines it occupies, rendering only on a cache miss or when the
// cached entry is stale (different width, own-ness, or timestamp label).
//
// Messages with ID 0 are unconfirmed outgoing sends: they are never cached
// because their ID is not yet unique and their footer shows a pending mark
// that flips once the server assigns the real ID.
func (m Model) bubble(msg *telegram.Message) (string, int) {
	if msg == nil {
		return "", 0
	}
	isOwn := isOwnMessage(msg, m.myUserId)
	stamp := render.FormatTimestamp(msg.Date)

	if msg.ID != 0 {
		if e, ok := m.cache.get(msg.ID); ok && e.width == m.width && e.isOwn == isOwn && e.stamp == stamp {
			return e.rendered, e.lines
		}
	}

	rendered := m.renderer.RenderMessage(msg, m.store, isOwn, false, m.width)
	lines := strings.Count(rendered, "\n") + 1
	if msg.ID != 0 {
		m.cache.put(msg.ID, bubbleEntry{
			width:    m.width,
			isOwn:    isOwn,
			stamp:    stamp,
			rendered: rendered,
			lines:    lines,
		})
	}
	return rendered, lines
}

// renderedBubbles returns every message's cached bubble plus its line
// count, in store order (oldest first).
func (m Model) renderedBubbles(msgs []*telegram.Message) ([]string, []int) {
	bubbles := make([]string, len(msgs))
	counts := make([]int, len(msgs))
	for i, msg := range msgs {
		bubbles[i], counts[i] = m.bubble(msg)
	}
	return bubbles, counts
}

// lineCounts is renderedBubbles when only the line index is needed. It
// serves every hit from the cache once the bubbles have been drawn, so
// scrolling no longer re-renders the history on each keypress.
func (m Model) lineCounts() []int {
	msgs := m.store.Messages.Get(m.chatID)
	counts := make([]int, len(msgs))
	for i, msg := range msgs {
		_, counts[i] = m.bubble(msg)
	}
	return counts
}

// totalRenderedLines sums per-message line counts, i.e. the total number
// of lines View() draws for the whole loaded history (bubbles are joined
// with a single "\n", so the total is the exact sum).
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
	m.clearSearch()
	m.cache.clear()

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
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
		return
	}
	if maxOffset := m.maxScrollOffset(); m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
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

	// Panel-local keys are matched on msg.String() rather than through
	// app's keyPress helper, which is fine only because none of them is
	// alt-modified: String() returns Key.Text when the terminal reported
	// any, so a Kitty-protocol alt binding on macOS would arrive as its
	// composed character ("¡" for alt+1) and never match here. See the
	// keyPress doc comment in internal/app/keymap.go before adding an
	// alt-modified binding to this switch — it needs Keystroke(), not
	// String().
	key := msg.String()

	switch key {
	case "up", "k", "down", "j", "G", "end", "g", "home",
		"ctrl+u", "ctrl+d", "pgup", "pgdown":
		// Scrolling by hand takes over from a jump still waiting to settle.
		m.pendingJumpID = 0
	}

	switch key {
	case "up", "k":
		m.scrollOffset += 3
		if cmd := m.clampScrollUp(); cmd != nil {
			return m, cmd
		}
	case "pgup":
		m.scrollOffset += m.pageStep()
		if cmd := m.clampScrollUp(); cmd != nil {
			return m, cmd
		}
	case "down", "j":
		m.scrollOffset -= 3
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
	case "pgdown":
		m.scrollOffset -= m.pageStep()
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
	case "G", "end":
		m.scrollOffset = 0
	case "g", "home":
		m.scrollOffset = m.maxScrollOffset()
	case "ctrl+u":
		m.scrollOffset += m.height
		if maxOffset := m.maxScrollOffset(); m.scrollOffset > maxOffset {
			m.scrollOffset = maxOffset
		}
	case "ctrl+d":
		m.scrollOffset -= m.height
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}

	case "esc":
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

	case "ctrl+f":
		m.OpenFind()
		return m, nil

	case "n":
		if len(m.searchHits) == 0 {
			return m, nil
		}
		cmd := m.jumpToHit(m.searchIdx + 1)
		return m, cmd
	case "N":
		if len(m.searchHits) == 0 {
			return m, nil
		}
		cmd := m.jumpToHit(m.searchIdx - 1)
		return m, cmd

	case "r":
		return m, m.messageAction("reply")
	case "e":
		return m, m.messageAction("edit")
	case "d":
		return m, m.messageAction("delete")

	// Enter: play/open media of the bottom-visible message
	case "enter":
		return m, m.playMedia()

	// 'o' also opens media
	case "o":
		return m, m.playMedia()

	// 's' saves/downloads file
	case "s":
		return m, m.downloadFile()
	}

	// Any key that moved the viewport may have brought photos whose
	// thumbnails were skipped by the open-time prefetch cap into view.
	switch key {
	case "up", "k", "down", "j", "G", "end", "g", "home",
		"ctrl+u", "ctrl+d", "pgup", "pgdown":
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
	oldest := m.store.Messages.OldestMessageId(m.chatID)
	if oldest != 0 && !m.loading && m.tg != nil {
		m.loading = true
		m.loadStatus = "Loading older messages..."
		return m.loadHistoryCmd(m.gen, m.chatID, oldest)
	}
	return nil
}

// getTargetMessage returns the message the scroll position is currently
// "on" — used by the r/e/d/enter/o/s action keys. View() shows lines
// [total-bodyH-scrollOffset : total-scrollOffset) of the full rendered
// history (oldest message first), so scrollOffset counts rendered lines up
// from the bottom (newest) message. Walk messages newest-to-oldest,
// consuming each one's rendered line count, until scrollOffset is used up —
// that message is the target. Line counts come from the bubble cache, so
// this costs no extra rendering.
func (m Model) getTargetMessage() *telegram.Message {
	msgs := m.store.Messages.Get(m.chatID)
	if len(msgs) == 0 {
		return nil
	}
	if m.scrollOffset <= 0 {
		return msgs[len(msgs)-1]
	}

	counts := m.lineCounts()
	remaining := m.scrollOffset
	for i := len(msgs) - 1; i >= 0; i-- {
		remaining -= counts[i]
		if remaining < 0 {
			return msgs[i]
		}
	}
	return msgs[0]
}

func (m Model) messageAction(action string) tea.Cmd {
	msg := m.getTargetMessage()
	if msg == nil {
		return nil
	}
	if action == "edit" && !isOwnMessage(msg, m.myUserId) {
		return nil
	}
	return func() tea.Msg {
		return MessageActionMsg{Action: action, ChatId: m.chatID, MessageId: msg.ID}
	}
}

// playMedia downloads and plays the media in the target message.
func (m Model) playMedia() tea.Cmd {
	msg := m.getTargetMessage()
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
	msg := m.getTargetMessage()
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
		key, name = fileKey(bestPhotoSizeFile(c.Photo)), "photo"
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
			key, name = fileKey(c.VoiceNote.File), "voice"
		}
	default:
		return nil
	}
	if key == "" {
		return nil
	}

	return func() tea.Msg {
		file, err := m.tg.DownloadFileSync(key)
		if err != nil {
			return MediaPlayMsg{Status: "error", Info: fmt.Sprintf("Download failed: %v", err)}
		}
		return MediaPlayMsg{Status: "downloaded", Info: fmt.Sprintf("💾 Saved %s → %s", name, file.Path)}
	}
}

func (m Model) downloadAndPlay(key string, mediaType string, statusMsg string) tea.Cmd {
	if key == "" {
		return nil
	}
	voice, video := m.voice, m.video
	return func() tea.Msg {
		file, err := m.tg.DownloadFileSync(key)
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
	return func() tea.Msg {
		file, err := m.tg.DownloadFileSync(key)
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
			Foreground(lipgloss.Color("244")).
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
			Foreground(lipgloss.Color("244")).
			Align(lipgloss.Center, lipgloss.Center).
			Render(label)
		if statusLine != "" {
			return header + "\n" + statusLine + "\n" + body
		}
		return header + "\n" + body
	}

	bubbles, counts := m.renderedBubbles(messages)
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

	visible := sliceLines(bubbles, counts, start, end)
	for len(visible) < bodyH {
		visible = append([]string{""}, visible...)
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

// sliceLines returns lines [start, end) of the history that bubbles/counts
// describe, splitting only the bubbles that actually intersect the window
// (counts is the exact per-bubble line index, so the rest are skipped by
// arithmetic alone).
func sliceLines(bubbles []string, counts []int, start, end int) []string {
	if start >= end {
		return nil
	}
	var out []string
	pos := 0
	for i, bubble := range bubbles {
		n := counts[i]
		if pos+n <= start {
			pos += n
			continue
		}
		if pos >= end {
			break
		}
		lines := strings.Split(bubble, "\n")
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

// renderHeader draws the one-line chat header: title, plus whatever
// transient context is worth a few columns. It is hard-truncated to the
// panel width — lipgloss would otherwise WRAP a long title (or a long
// notice) onto a second row and push the body one line off the bottom.
func (m Model) renderHeader() string {
	text := "  " + m.chatTitle
	if m.metaBusy {
		// Deliberately tiny: the messages are readable already, this only
		// says that names/thumbnails are still filling in.
		text += " ⟳"
	}
	if m.mediaStatus != "" {
		text += "  │  " + m.mediaStatus
	}
	if m.notice != "" {
		text += "  │  " + m.notice
	}
	if hint := m.mediaHint(); hint != "" {
		text += "  │  " + hint
	}
	// Truncate by CELLS, not runes: a CJK title is one rune but two cells
	// wide, so a rune-count cut still overflows and lipgloss then *wraps*
	// the header onto extra rows, pushing the body off the bottom.
	// Style.MaxWidth wraps too — ansi.Truncate is the tool that genuinely
	// clips a single line, and it is ANSI- and wide-rune-aware. Same fix
	// as internal/ui/components/statusbar.
	//
	// The budget is the panel width minus the style's own padding: this
	// header is padded, and content filling the full width would push the
	// rendered block to width+padding and wrap right back.
	style := m.theme.ChatViewHeader
	if inner := m.width - style.GetHorizontalFrameSize(); inner > 0 {
		text = ansi.Truncate(text, inner, "")
	}
	return style.Width(m.width).Render(text)
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
	style := lipgloss.NewStyle().Foreground(m.theme.TextMuted)
	if inner := m.width - style.GetHorizontalFrameSize(); inner > 0 {
		line = ansi.Truncate(line, inner, "")
	}
	return style.Width(m.width).Render(line)
}
