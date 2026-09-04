package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Telegram      TelegramConfig     `toml:"telegram"`
	Storage       StorageConfig      `toml:"storage"`
	UI            UIConfig           `toml:"ui"`
	Media         MediaConfig        `toml:"media"`
	Notifications NotificationConfig `toml:"notifications"`
	Keys          KeyConfig          `toml:"keys"`
}

type TelegramConfig struct {
	APIID   int32  `toml:"api_id"`
	APIHash string `toml:"api_hash"`
	Phone   string `toml:"phone"`
}

type StorageConfig struct {
	SessionFile string `toml:"session_file"`

	// FilesDir is the media CACHE: where downloads land so a photo drawn
	// twice is fetched once. It is not where "save this" saves to — see
	// DownloadDir — and a user who set files_dir expecting the latter got
	// a cache directory full of files with server-side names.
	FilesDir string `toml:"files_dir"`

	// DownloadDir is where `s` puts a copy, under the sender's own
	// filename. Defaults to the platform download folder, because that is
	// where a person looks for a thing they just saved.
	DownloadDir string `toml:"download_dir"`
	// StateFile is the bbolt database holding the update-sequence state
	// (pts/qts/seq/date) and the peer access-hash cache, so updates that
	// arrived while the app was offline can be recovered on the next start.
	// Empty (the default) means "state.db" next to SessionFile.
	StateFile string `toml:"state_file"`
}

type UIConfig struct {
	Theme string `toml:"theme"`

	// TimestampFormat and DateFormat are READ BACK AND WRITTEN OUT, and
	// nothing consults them. Every time on screen has a fixed form chosen
	// for the column it sits in: the thread's clock is 15:04 because the
	// grid gives it five cells and puts the date in a day divider, the
	// chat list is relative because a chat list is read for recency, and a
	// day divider names the day. A Go layout string cannot express those,
	// and one that overrode all three would break the column widths the
	// frame is built on.
	//
	// Kept so an existing config round-trips rather than losing keys on
	// -migrate-config. Marked here, and in config.example.toml, so nobody
	// spends an afternoon finding out they do nothing.
	TimestampFormat string `toml:"timestamp_format"`
	DateFormat      string `toml:"date_format"`

	// InlineImages governs WHERE a photo is drawn:
	// [InlineImagesNever], [InlineImagesOnOpen] (the default), or
	// [InlineImagesAlways]. Only the last puts art in the thread, and only
	// bounded — see render.inlineArtRows for why the bound is not
	// negotiable.
	InlineImages string `toml:"inline_images"`

	// Hyperlinks governs OSC 8 terminal hyperlinks on links in a message:
	// [HyperlinksAuto] (the default), [HyperlinksNever], or
	// [HyperlinksAlways].
	Hyperlinks string `toml:"hyperlinks"`

	// EmojiWidth declares how this terminal draws emoji sequences that
	// have a composition rule: [EmojiWidthAuto] (the default),
	// [EmojiWidthComposed], or [EmojiWidthSeparate]. It is a declaration
	// because it cannot be detected — see internal/ui/cell for why.
	EmojiWidth string `toml:"emoji_width"`

	// Rail shows the right-hand context rail — pinned message, members,
	// shared files — on a terminal wide enough for it. Off by default: it
	// costs 30 columns, and they come out of the thread.
	Rail bool `toml:"rail"`
	// ComposeEditing selects the composer's line-editing keymap:
	// [ComposeEditingEmacs], [ComposeEditingVi], or [ComposeEditingAuto]
	// (the default) to infer it from $VISUAL/$EDITOR. Resolve it with
	// [ResolveComposeEditing]; never read the raw value.
	ComposeEditing string `toml:"compose_editing"`
	// ParseMarkdown enables the Telegram Desktop markdown subset in
	// outgoing messages and captions (**bold**, __italic__, `code`,
	// ```pre```, ~~strike~~, ||spoiler||, [text](url)).
	//
	// Defaults to FALSE: what you typed is what gets sent. The composer has
	// no preview, so with parsing on silently by default the first time a
	// user notices is when a message has already left — and the syntax
	// overlaps with things people paste verbatim. __init__ arrives as
	// init, a snippet full of ** loses it, a table of || collapses. Opting
	// in means knowing that transformation happens.
	//
	// -migrate-config turns it on for existing configs, where it reports
	// the change, so upgraders get the feature but are told about it.
	ParseMarkdown bool `toml:"parse_markdown"`
}

// Default storage locations, in the portable "~/" form. defaultConfig
// expands them for the running app; -migrate-config writes these literals so
// a generated config stays portable instead of hardcoding one machine's home
// directory.
const (
	DefaultSessionFile = "~/.local/share/tele-tui/session.json"
	DefaultFilesDir    = "~/.local/share/tele-tui/files"
	DefaultDownloadDir = "~/Downloads"
)

// Inline-image policies for [UIConfig.InlineImages].
const (
	// InlineImagesNever shows the metadata card in the thread and hands
	// the picture to the platform viewer on Enter. The right answer over a
	// slow link, and on a terminal whose image support is a guess.
	InlineImagesNever = "never"
	// InlineImagesOnOpen shows the card in the thread and draws the picture
	// full-pane when the reader OPENS it. The default, and the name is
	// literal: "on open" is when the art appears, not a condition under
	// which it appears in the history.
	InlineImagesOnOpen = "on_open"
	// InlineImagesAlways also draws an eight-row preview in the thread,
	// which is the only setting that puts art in the history at all.
	InlineImagesAlways = "always"
)

// Hyperlink policies for [UIConfig.Hyperlinks].
const (
	// HyperlinksAuto emits OSC 8 only on terminals known to understand it
	// (theme.SupportsHyperlinks). The default, and an allowlist: a
	// terminal that prints the sequence instead of acting on it puts a URL
	// in the middle of somebody's message.
	HyperlinksAuto = "auto"
	// HyperlinksNever never emits them. Links stay cyan and underlined,
	// which is the affordance; OSC 8 only adds the click.
	HyperlinksNever = "never"
	// HyperlinksAlways emits them regardless — for a terminal the
	// allowlist does not know, or for tmux with allow-passthrough on,
	// which cannot be detected from the environment.
	HyperlinksAlways = "always"
)

// Delivery methods for [NotificationConfig.Method].
//
// The strings are defined here, beside the other policy fields, so this
// package stays free of the one that implements them — internal/notification
// parses the same three values.
const (
	// NotifyMethodAuto asks the terminal where it is known to understand
	// the sequence and the system otherwise. The default.
	NotifyMethodAuto = "auto"
	// NotifyMethodTerminal always asks the terminal, for one the allowlist
	// does not know. A terminal that does not understand it prints it.
	NotifyMethodTerminal = "terminal"
	// NotifyMethodSystem always uses the platform notifier: notify-send on
	// Linux, osascript on macOS — which posts as Script Editor, because a
	// command-line binary has no bundle of its own to post from.
	NotifyMethodSystem = "system"
)

// Emoji-width declarations for [UIConfig.EmojiWidth].
//
// This is one setting rather than two because the terminals that get it
// wrong get it wrong consistently: one that honours U+FE0F also composes
// ZWJ sequences and flags. What it cannot be is inferred — the widths differ
// in opposite directions, so no single "narrow" or "wide" describes them.
const (
	// EmojiWidthAuto measures with the Unicode tables and reserves a cell
	// on top for every composition rule, so an over-reservation shows as a
	// gap rather than as a row overwriting its neighbour. The default, and
	// the only value that is a guess.
	EmojiWidthAuto = "auto"
	// EmojiWidthComposed says this terminal applies every composition
	// rule. The tables are then right and nothing is reserved on top —
	// which is what closes the gap between the folder tabs and the clock.
	EmojiWidthComposed = "composed"
	// EmojiWidthSeparate says it applies none of them: U+FE0F is ignored
	// and joined or paired sequences are drawn as their parts.
	EmojiWidthSeparate = "separate"
)

// ResolveEmojiWidth normalises [UIConfig.EmojiWidth], falling back to the
// default for an empty or unrecognised value — a typo here should cost the
// user the setting, not the client.
func ResolveEmojiWidth(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case EmojiWidthComposed:
		return EmojiWidthComposed
	case EmojiWidthSeparate:
		return EmojiWidthSeparate
	default:
		return EmojiWidthAuto
	}
}

// ResolveHyperlinks normalises [UIConfig.Hyperlinks] to one of the three
// policies, treating anything unrecognised as the default rather than
// failing: a typo in a cosmetic setting should not stop the client starting.
func ResolveHyperlinks(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case HyperlinksNever:
		return HyperlinksNever
	case HyperlinksAlways:
		return HyperlinksAlways
	default:
		return HyperlinksAuto
	}
}

// ResolveInlineImages normalises [UIConfig.InlineImages], falling back to
// the default for an empty or unrecognised value.
//
// Unrecognised falls back rather than failing: a typo in this field should
// cost the user the setting, not the client.
func ResolveInlineImages(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case InlineImagesNever:
		return InlineImagesNever
	case InlineImagesAlways:
		return InlineImagesAlways
	default:
		return InlineImagesOnOpen
	}
}

// Line-editing keymaps for [UIConfig.ComposeEditing].
const (
	// ComposeEditingEmacs is the readline keymap (ctrl+a/e/b/f/k/u/w/d).
	ComposeEditingEmacs = "emacs"
	// ComposeEditingVi is the modal vi keymap.
	ComposeEditingVi = "vi"
	// ComposeEditingAuto infers the keymap from the user's $EDITOR.
	ComposeEditingAuto = "auto"
)

// ResolveComposeEditing turns a configured [UIConfig.ComposeEditing] value
// into a concrete [ComposeEditingEmacs] or [ComposeEditingVi].
//
// An explicit "emacs" or "vi" wins. Everything else — "auto", empty (an
// older config.toml predating the field), or an unrecognized value — infers
// the keymap from $VISUAL, falling back to $EDITOR: if the editor's command
// name contains "vi" (vi, vim, nvim, gvim, view) the answer is vi, otherwise
// emacs. That also makes emacs the answer when no editor is set, matching
// the shell convention that readline bindings are the default.
//
// An unrecognized value is treated as "auto" rather than rejected, so a typo
// in config.toml degrades to a sensible keymap instead of breaking startup.
func ResolveComposeEditing(setting string) string {
	switch strings.ToLower(strings.TrimSpace(setting)) {
	case ComposeEditingEmacs:
		return ComposeEditingEmacs
	case ComposeEditingVi:
		return ComposeEditingVi
	}

	editor := os.Getenv("VISUAL")
	if strings.TrimSpace(editor) == "" {
		editor = os.Getenv("EDITOR")
	}
	// $EDITOR often carries arguments ("nvim -u NONE") and a path
	// ("/usr/local/bin/vim"); only the command name is meaningful here.
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		return ComposeEditingEmacs
	}
	if strings.Contains(strings.ToLower(filepath.Base(fields[0])), "vi") {
		return ComposeEditingVi
	}
	return ComposeEditingEmacs
}

type MediaConfig struct {
	ImageProtocol       string `toml:"image_protocol"`
	MaxImageWidth       int    `toml:"max_image_width"`
	MaxImageHeight      int    `toml:"max_image_height"`
	VoicePlayer         string `toml:"voice_player"`
	VideoPlayer         string `toml:"video_player"`
	AutoDownloadPhotos  bool   `toml:"auto_download_photos"`
	AutoDownloadLimitMB int    `toml:"auto_download_limit_mb"`

	// AutoDownloadVoice is read back and written out, and nothing consults
	// it. Voice notes are never prefetched: one is fetched when you press
	// space on it, which is the only moment anybody wants the bytes, and
	// turning that off would mean a key that does nothing. Kept for config
	// round-tripping — see TimestampFormat for the same reasoning at
	// length.
	AutoDownloadVoice bool `toml:"auto_download_voice"`
}

type NotificationConfig struct {
	Enabled     bool `toml:"enabled"`
	Sound       bool `toml:"sound"`
	ShowPreview bool `toml:"show_preview"`

	// Method is who posts the notification: "auto" (the default — the
	// terminal where it is known to understand the sequence, the system
	// otherwise), "terminal", or "system". See internal/notification for
	// why the terminal is usually the right answer, and why macOS labels
	// the system path "Script Editor".
	Method string `toml:"method"`
}

// KeyConfig lists user-configurable key bindings. Not every field is
// currently consulted:
//
//   - Wired, dispatched by internal/app itself (which normalizes the value
//     via [NormalizeKey] and falls back to its built-in default when
//     empty): Quit, QuitBrowsing, FocusChatList, FocusChatView,
//     FocusComposer, Search, GlobalSearch, Contacts, ContactsAlt, Help,
//     NextFolder, PrevFolder, NextChat, PrevChat. Note
//     FocusChatList/View/Composer are wired *in addition to* their
//     hardcoded alt+1/2/3 shortcuts, which always work regardless of
//     configuration.
//   - Wired, resolved by internal/app and handed to the chat view, which
//     implements them: Reply, EditMessage, DeleteMessage, ScrollUp,
//     ScrollDown, PageUp, PageDown. Same normalization and defaulting; the
//     difference is only which layer matches the key. Two rules apply
//     there and nowhere else. Reply/EditMessage/DeleteMessage are
//     mnemonics: a value REPLACES the built-in r/e/d. ScrollUp/ScrollDown/
//     PageUp/PageDown are motions: a value is ADDED to the built-in j/k,
//     the arrows and pgup/pgdown, which always keep working. And a value
//     that collides with a key the chat view already claims is dropped
//     rather than allowed to shadow it. The chat view's remaining keys
//     (g/G, ctrl+u/ctrl+d, n/N, ctrl+f, enter/o/s) are hardcoded there, as
//     are the chat list's arrows and digits and the composer's line
//     editing — see the keymap table in internal/app/keymap.go.
//   - Unwired (parsed and preserved on save, but not consulted anywhere —
//     kept so existing config.toml files round-trip cleanly; a value here
//     is silently inert): Forward. There is no forward-a-message feature
//     to bind it to; the field exists only so config files that set it
//     still load.
//
// A component-dispatched field set to a key internal/app claims first is
// not merely shadowed — it is dead, because app-level dispatch runs before
// the focused panel sees the event. reply = "q" used to be accepted,
// advertised on the help card as Reply, and quit the application when
// pressed. Two things now prevent that: the chat view is told what the app
// has claimed (keys.AppReserved) and refuses such a binding, keeping its
// built-in letter; and [DetectKeyCollisions] reports it, so the refusal is
// explained rather than silent.
//
// Wired bindings are matched before the focused panel sees the key, so a
// binding here shadows that key in the chat list and chat view. It does not
// shadow it in the composer: typing is only ever entered deliberately (i, c,
// Tab, the focus keys, or a click), and once the composer has focus almost
// nothing is claimed at app level — see the exception list in the keymap
// table in internal/app/keymap.go. A bare printable is therefore a
// reasonable binding here, though a modifier still reads more clearly.
//
// # macOS: Alt bindings and the Option key
//
// The default alt+… bindings only reach the app if the terminal reports
// Option as a modifier. Terminals differ:
//
//   - Ghostty: macos-option-as-alt = true (default "false" on macOS —
//     confirmed in field testing to be why alt bindings work in kitty but
//     not in a stock Ghostty).
//   - Terminal.app: Settings → Profiles → Keyboard → "Use Option as Meta
//     key" (off by default).
//   - iTerm2: Settings → Profiles → Keys → Left/Right Option key → "Esc+".
//   - kitty/WezTerm/Alacritty report Option as Alt by default.
//
// While Option is not reported as a modifier, macOS composes the character
// itself and the terminal sends only that: Option+1 arrives as a bare "¡"
// with no modifier bit, indistinguishable from the user typing "¡". This is
// not something the Kitty keyboard protocol fixes — the composition happens
// before the terminal builds the key event — and no amount of key matching
// can recover the binding.
//
// So every alt binding has an alt-free alternative: f1/f2/f3 for panel
// focus, f4 for contacts (ContactsAlt), ctrl+g for global search
// (GlobalSearch), and "[" / "]", the left/right arrows or the 1-9 jump for
// the folder tabs while the chat list is focused. (Bare h/l used to be that
// fallback for the folder tabs; they now move between panels, which is what
// left/right means in a two-column layout — see internal/app/keymap.go.)
// Rebinding here works too — prefer ctrl+… or a function key.
type KeyConfig struct {
	Quit string `toml:"quit"`
	// QuitBrowsing quits from the chat list and the chat view only, where
	// a bare letter cannot be mistaken for typing — the composer owns
	// printables and never sees it. Quit (and the hardcoded
	// ctrl+q) work from everywhere including the composer. Default "q".
	//
	// An unsent draft or a pending attachment turns it into a confirm
	// rather than an immediate exit, so a single keystroke cannot discard
	// a message being written.
	QuitBrowsing  string `toml:"quit_browsing"`
	FocusChatList string `toml:"focus_chat_list"`
	FocusChatView string `toml:"focus_chat_view"`
	FocusComposer string `toml:"focus_composer"`
	Search        string `toml:"search"`
	Contacts      string `toml:"contacts"`
	// ContactsAlt is a second, alt-free binding for the contacts overlay,
	// so the overlay stays reachable on terminals that cannot report Alt
	// (see the macOS notes below). Default f4.
	ContactsAlt string `toml:"contacts_alt"`
	// Help opens the keybinding overlay. Default "?".
	Help string `toml:"help"`
	// GlobalSearch searches every chat. Search ("/") does the same from
	// every panel except the chat view, where vi convention makes "/" mean
	// "find in this buffer"; GlobalSearch is the panel-independent binding.
	// Default ctrl+g.
	GlobalSearch  string `toml:"global_search"`
	NextChat      string `toml:"next_chat"`
	PrevChat      string `toml:"prev_chat"`
	Reply         string `toml:"reply"`
	EditMessage   string `toml:"edit_message"`
	DeleteMessage string `toml:"delete_message"`
	Forward       string `toml:"forward"`
	ScrollUp      string `toml:"scroll_up"`
	ScrollDown    string `toml:"scroll_down"`
	PageUp        string `toml:"page_up"`
	PageDown      string `toml:"page_down"`
	// NextFolder/PrevFolder cycle the chat list's folder tabs.
	NextFolder string `toml:"next_folder"`
	PrevFolder string `toml:"prev_folder"`
}

// keyModAliases maps the modifier spellings a user might write in
// config.toml to the canonical spelling bubbletea's Key.Keystroke() emits.
// Notably "option"/"opt" (the macOS name for Alt) normalize to "alt".
var keyModAliases = map[string]string{
	"ctrl":    "ctrl",
	"control": "ctrl",
	"ctl":     "ctrl",
	"alt":     "alt",
	"opt":     "alt",
	"option":  "alt",
	"shift":   "shift",
	"meta":    "meta",
	"hyper":   "hyper",
	"super":   "super",
	"win":     "super",
	"cmd":     "super",
	"command": "super",
}

// keyModOrder is the order Key.Keystroke() prints modifiers in. A configured
// binding is re-sorted into this order so that e.g. "shift+alt+a" matches the
// "alt+shift+a" a real key event produces.
var keyModOrder = []string{"ctrl", "alt", "shift", "meta", "hyper", "super"}

// keyNameAliases maps common spellings of non-printable keys to the names
// bubbletea uses.
var keyNameAliases = map[string]string{
	"escape":    "esc",
	"return":    "enter",
	"ret":       "enter",
	"del":       "delete",
	"ins":       "insert",
	"pageup":    "pgup",
	"page_up":   "pgup",
	"pgdn":      "pgdown",
	"pagedown":  "pgdown",
	"page_down": "pgdown",
	"spacebar":  "space",
	"bs":        "backspace",
}

// NormalizeKey canonicalizes a user-configured key string to the form
// produced by bubbletea's Key.Keystroke(): lowercased, with modifier and key
// aliases resolved and modifiers emitted in Keystroke's fixed order
// (ctrl, alt, shift, meta, hyper, super). An empty input returns empty, so
// callers can detect "not configured" and fall back to a built-in default.
//
// Examples: "ALT+L" -> "alt+l", "Option+1" -> "alt+1", "shift+ctrl+a" ->
// "ctrl+shift+a", "Escape" -> "esc", "ctrl++" -> "ctrl++".
//
// Anything that is not a recognized modifier terminates the modifier prefix
// and is taken (together with the rest of the string) as the key name, so a
// literal "+" binding survives intact.
func NormalizeKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}

	parts := strings.Split(s, "+")
	seen := map[string]bool{}
	i := 0
	for ; i < len(parts)-1; i++ {
		mod, ok := keyModAliases[strings.TrimSpace(parts[i])]
		if !ok {
			break
		}
		seen[mod] = true
	}

	key := strings.Join(parts[i:], "+")
	if alias, ok := keyNameAliases[key]; ok {
		key = alias
	}

	var sb strings.Builder
	for _, mod := range keyModOrder {
		if seen[mod] {
			sb.WriteString(mod)
			sb.WriteByte('+')
		}
	}
	sb.WriteString(key)
	return sb.String()
}

// DefaultQuitKey is what [ResolveQuitKey] falls back to. It is a chord on
// purpose: quit is matched before every focus gate, so a bare letter here is
// a letter that cannot be typed anywhere in the client.
const DefaultQuitKey = "ctrl+q"

// namedPrintableKeys are keys whose NAME is longer than the character they
// produce. There is one: pressing space types a space, and a composer that
// cannot take a space is not a composer.
//
// A set rather than a special case, because the question it answers —
// "would binding this shadow a character somebody types?" — is the same one
// [IsBarePrintableKey] asks of the single-rune spellings, and the answer for
// space is the same yes. [NormalizeKey] folds "spacebar" onto this name, so
// both spellings are covered by the one entry; a literal " " is trimmed to
// the empty string there and falls back to the default anyway.
var namedPrintableKeys = map[string]bool{"space": true}

// IsBarePrintableKey reports whether a NORMALIZED binding types a character
// when it is pressed: a single unmodified printable ("x", "?", "+"), or
// "space". A chord ("ctrl+x"), a named key that produces no text ("esc",
// "f1", "pgup") and the empty string are all false.
//
// It answers one question: would binding this shadow a character somebody
// types? Only the bindings matched ahead of the composer have to ask it.
func IsBarePrintableKey(key string) bool {
	if namedPrintableKeys[key] {
		return true
	}
	if utf8.RuneCountInString(key) != 1 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(key)
	return unicode.IsGraphic(r) && !unicode.IsSpace(r)
}

// ResolveQuitKey is the binding [KeyConfig.Quit] resolves to, and whether the
// configured value was refused (decision I-13).
//
// quit is the one field where a bare printable is not merely unwise but
// broken: it is matched before every focus gate, so quit = "x" meant that
// pressing x while writing a message quit the application instead of typing
// an x. Nothing rejected that — [DetectKeyCollisions] only compares bindings
// against each other, never against "is this a character someone types" — so
// the documented advice was to avoid it, which is not the same thing as it
// not happening.
//
// The refusal keeps the default rather than leaving quit unbound: a client
// with no way out is worse than one that ignored a line of config, and
// [StartupWarnings] says which it did.
func ResolveQuitKey(configured string) (key string, refused bool) {
	normalized := NormalizeKey(configured)
	if normalized == "" {
		return DefaultQuitKey, false
	}
	if IsBarePrintableKey(normalized) {
		return DefaultQuitKey, true
	}
	return normalized, false
}

// StartupWarnings is everything about the [keys] table worth telling the
// user before the TUI takes the screen: a refused quit binding, and
// bindings that cannot all work.
//
// At startup, not only under -migrate-config (decision I-13). A warning
// somebody sees only if they happen to run a migration is a warning about a
// client they are already using with the broken binding in it.
func StartupWarnings(cfg *Config) []string {
	if cfg == nil {
		return nil
	}
	var out []string
	if key, refused := ResolveQuitKey(cfg.Keys.Quit); refused {
		out = append(out, fmt.Sprintf(
			"%q cannot be keys.quit — quit is matched before the composer sees a "+
				"key, so a bare printable would be untypable in a message; using %s",
			NormalizeKey(cfg.Keys.Quit), key))
	}
	return append(out, DetectKeyCollisions(cfg)...)
}

func Load() (*Config, error) {
	cfg := defaultConfig()

	configPath := findConfigPath()
	if configPath == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.Storage.SessionFile = expandPath(cfg.Storage.SessionFile)
	cfg.Storage.FilesDir = expandPath(cfg.Storage.FilesDir)
	cfg.Storage.DownloadDir = expandPath(cfg.Storage.DownloadDir)
	cfg.Storage.StateFile = expandPath(cfg.Storage.StateFile)

	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Storage: StorageConfig{
			SessionFile: expandPath(DefaultSessionFile),
			FilesDir:    expandPath(DefaultFilesDir),
			DownloadDir: expandPath(DefaultDownloadDir),
		},
		UI: UIConfig{
			ComposeEditing:  ComposeEditingAuto,
			Theme:           "dark",
			TimestampFormat: "15:04",
			DateFormat:      "2006-01-02",
			InlineImages:    InlineImagesOnOpen,
			Hyperlinks:      HyperlinksAuto,
			EmojiWidth:      EmojiWidthAuto,
			Rail:            false,
			ParseMarkdown:   false,
		},
		Media: MediaConfig{
			ImageProtocol:       "auto",
			MaxImageWidth:       40,
			MaxImageHeight:      20,
			VoicePlayer:         "mpv",
			VideoPlayer:         "mpv",
			AutoDownloadPhotos:  true,
			AutoDownloadVoice:   true,
			AutoDownloadLimitMB: 10,
		},
		Notifications: NotificationConfig{
			Enabled:     true,
			Sound:       false,
			ShowPreview: true,
			Method:      NotifyMethodAuto,
		},
		Keys: KeyConfig{
			Quit:          "ctrl+q",
			QuitBrowsing:  "q",
			FocusChatList: "f1",
			FocusChatView: "f2",
			FocusComposer: "f3",
			Search:        "/",
			Contacts:      "alt+c",
			ContactsAlt:   "f4",
			GlobalSearch:  "ctrl+g",
			Help:          "?",
			NextChat:      "alt+j",
			PrevChat:      "alt+k",
			Reply:         "r",
			EditMessage:   "e",
			DeleteMessage: "d",
			Forward:       "f",
			ScrollUp:      "k",
			ScrollDown:    "j",
			PageUp:        "pgup",
			PageDown:      "pgdown",
			NextFolder:    "alt+l",
			PrevFolder:    "alt+h",
		},
	}
}

// Save writes the config to the path the app will read back: $TELETUI_CONFIG
// when set, otherwise the default location. See [ConfigPath].
//
// Writing to the default location unconditionally would break the first-run
// setup wizard under $TELETUI_CONFIG: the credentials it collects would land
// in a file [Load] never looks at, and the next launch would ask for them
// again.
func Save(cfg *Config) error {
	return SaveTo(ConfigPath(), cfg)
}

// marshalConfig encodes a config as TOML.
func marshalConfig(cfg *Config) ([]byte, error) {
	data, err := toml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}
	return data, nil
}

func defaultConfigPath() string {
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		home, _ := os.UserHomeDir()
		xdgConfig = filepath.Join(home, ".config")
	}
	return filepath.Join(xdgConfig, "tele-tui", "config.toml")
}

// findConfigPath returns the config file to read, or "" when there is none.
// [ConfigPath] is the same resolution for writing, where a file that does not
// exist yet is the normal case rather than a miss.
func findConfigPath() string {
	if p := os.Getenv("TELETUI_CONFIG"); p != "" {
		return p
	}

	path := defaultConfigPath()
	if _, err := os.Stat(path); err == nil {
		return path
	}

	return ""
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
