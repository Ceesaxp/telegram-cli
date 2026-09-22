package app

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/palette"
)

// ArgSpec describes what a command does with the text after its name.
type ArgSpec int

const (
	// ArgNone: the command takes no argument and rejects one rather than
	// silently ignoring it. Silently discarding input is how ":quit now"
	// becomes a bug report about a command that "sometimes does nothing".
	ArgNone ArgSpec = iota
	// ArgOptional: the argument changes behaviour but may be omitted.
	ArgOptional
	// ArgRequired: without it the command cannot run.
	ArgRequired
)

// Command is one entry in the registry.
//
// A single typed table supplies everything about a command — its name,
// argument shape, description, key equivalent, and what it does — so the
// palette, the help card, and any future `:keymap` output all read from the
// same place and cannot drift apart. That is the whole reason this is a table
// rather than a switch in Update.
type Command struct {
	// Name is the command word, without the colon.
	Name string
	// Arg describes the argument, and Placeholder names it for display
	// (e.g. "<query>"). Placeholder must be empty when Arg is ArgNone.
	Arg         ArgSpec
	Placeholder string
	// Description is one line, shown in the palette.
	Description string
	// Key is the equivalent key binding shown right-aligned, so the palette
	// teaches the keymap rather than duplicating it. Empty when there is no
	// single key for the command.
	Key string
	// ArgCandidates lists the values the argument can take, which the
	// palette offers once the command word and a space are typed. Nil for
	// an argument that is free text, or absent.
	ArgCandidates func(m Model) []palette.Arg
	// Run performs the command. It receives the model by value and returns
	// the updated model, matching how Update threads state everywhere else.
	// The returned string is a notice for the user; empty means silent.
	Run func(m Model, arg string) (Model, tea.Cmd, string)
}

// commandRegistry returns the commands available in this build.
//
// Contents are fixed by decision 8: the read-only and local commands ship
// first. **Secret chat and Markdown export are deliberately absent** — they
// are deferred, and a palette entry that cannot run is worse than no entry.
// The server-mutating commands decision 8 authorises (pin/unpin,
// mute/unmute) are not here yet either, because each needs a Telegram
// service this build does not have; see TODO.md.
//
// Keys are the resolved bindings, not hardcoded spellings, so a rebound key
// shows correctly in the palette.
func (m Model) commandRegistry() []Command {
	return []Command{
		{
			Name:        "mark-read",
			Arg:         ArgNone,
			Description: "mark this chat read, keep scroll position",
			Run: func(m Model, _ string) (Model, tea.Cmd, string) {
				chatID := m.chatList.ActiveChatId()
				if chatID == 0 {
					return m, nil, "no chat open"
				}
				return m, m.chatView.MarkReadCmd(), "marked read"
			},
		},
		{
			Name:        "read-mentions",
			Arg:         ArgNone,
			Description: "clear every unread mention in this chat",
			// Guarded on the chat view's chat rather than the chat list's
			// active one: that is the chat the clear goes to, and a jump
			// opens a chat without the list hearing of it.
			// The notice follows the outcome rather than the asking: see
			// chatview.ReadAllMentionsDoneMsg.
			Run: func(m Model, _ string) (Model, tea.Cmd, string) {
				if m.chatView.ChatId() == 0 {
					return m, nil, "no chat open"
				}
				if !m.chatView.CanHaveMentions() {
					return m, nil, "no mentions in this chat"
				}
				cmd := m.chatView.ReadAllMentionsCmd()
				if cmd == nil {
					return m, nil, "could not clear mentions"
				}
				return m, cmd, "clearing mentions..."
			},
		},
		{
			Name:        "search",
			Arg:         ArgOptional,
			Placeholder: "<query>",
			Description: "search across all chats",
			Key:         m.keys.globalSearch,
			Run: func(m Model, arg string) (Model, tea.Cmd, string) {
				m.search.SetVisible(true)
				m.search.SetQuery(arg)
				m.setFocus(PanelSearch)
				return m, nil, ""
			},
		},
		{
			Name:        "keymap",
			Arg:         ArgNone,
			Description: "show the keybinding cheat sheet",
			Key:         m.keys.help,
			Run: func(m Model, _ string) (Model, tea.Cmd, string) {
				m.help.SetVisible(true)
				return m, nil, ""
			},
		},
		{
			Name:          "theme",
			Arg:           ArgOptional,
			Placeholder:   "<name>",
			Description:   "switch the colour theme, and save it",
			ArgCandidates: Model.themeCandidates,
			Run:           Model.runTheme,
		},
		{
			Name:        "reload-config",
			Arg:         ArgNone,
			Description: "re-read config.toml and apply its theme",
			Run:         Model.runReloadConfig,
		},
		{
			Name:        "quit",
			Arg:         ArgNone,
			Description: "quit tele-tui",
			Key:         m.keys.quitBrowsing,
			// Through the same check q uses (decision I-5). This returned
			// tea.Quit unconditionally, and the palette opens from a vi
			// composer in its command state — so the one way out reachable
			// with a draft on screen was the one that never asked about it.
			Run: func(m Model, _ string) (Model, tea.Cmd, string) {
				out, cmd := m.quitConfirming()
				return out.(Model), cmd, ""
			},
		},
	}
}

// paletteItems projects the registry into the palette's display type. The
// projection lives here rather than in the palette so that package stays
// ignorant of what a command is.
func (m Model) paletteItems() []palette.Item {
	cmds := m.commandRegistry()
	items := make([]palette.Item, 0, len(cmds))
	for _, c := range cmds {
		it := palette.Item{
			Name:        c.Name,
			Args:        c.Placeholder,
			Description: c.Description,
			Key:         c.Key,
		}
		if c.ArgCandidates != nil {
			it.Candidates = c.ArgCandidates(m)
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

// openPalette opens the command palette with its items projected afresh, so
// an argument's candidates are read when the palette opens: a theme file
// dropped in since startup is there to choose, and one deleted is not.
func (m *Model) openPalette() {
	m.palette.SetItems(m.paletteItems())
	m.palette.Open()
}

// lookupCommand finds a command by exact name.
func (m Model) lookupCommand(name string) (Command, bool) {
	for _, c := range m.commandRegistry() {
		if c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}

// validateArg checks the argument against the command's spec, returning the
// message to show the user when it does not fit.
//
// The errors name the command and say what was expected, because the palette
// closes on Enter: the notice is the only feedback the user gets, so "unknown
// command" without the name would leave them guessing at a typo.
func validateArg(c Command, arg string) string {
	switch c.Arg {
	case ArgNone:
		if arg != "" {
			return fmt.Sprintf(":%s takes no argument", c.Name)
		}
	case ArgRequired:
		if arg == "" {
			return fmt.Sprintf(":%s needs %s", c.Name, c.Placeholder)
		}
	}
	return ""
}

// runCommandLine parses a typed palette line and executes it.
//
// It is the single entry point from the palette, so parsing, validation, and
// dispatch cannot disagree about what a line means.
func (m Model) runCommandLine(line string) (Model, tea.Cmd, string) {
	name, arg := palette.SplitQuery(line)
	name = strings.TrimPrefix(name, ":")

	if name == "" {
		return m, nil, ""
	}

	c, ok := m.lookupCommand(name)
	if !ok {
		return m, nil, fmt.Sprintf("unknown command :%s", name)
	}
	if problem := validateArg(c, arg); problem != "" {
		return m, nil, problem
	}
	return c.Run(m, arg)
}
