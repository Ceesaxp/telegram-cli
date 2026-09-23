package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// liveSettings are the settings :reload-config applies to the running app.
// Everything else it finds changed waits for a restart, and says so.
var liveSettings = map[string]bool{"ui.theme": true}

// runReloadConfig is :reload-config. It reads config.toml again, from the
// file config.Load read, and applies its theme the way :theme applies one —
// but saves nothing, since the file is where the theme came from. Every
// other setting that changed is named, by its key in the file, as needing a
// restart.
//
// Those keep their running values: the app goes on behaving as the config
// it started with, plus the theme, rather than as half of each — a keymap
// that is half rebound is worse than one that is not rebound yet. A file
// that does not load changes nothing at all.
func (m Model) runReloadConfig(_ string) (Model, tea.Cmd, string) {
	fresh, err := m.config.Reload()
	if err != nil {
		return m, nil, fmt.Sprintf("⚠ config not reloaded: %v", err)
	}

	var report commandReport
	report.say("config reloaded")
	name, warnings, err := m.applyTheme(fresh.UI.Theme)
	if err != nil {
		report.warn(fmt.Sprintf("%v; keeping %s", err, m.currentTheme()))
	} else {
		m.config.UI.Theme = fresh.UI.Theme
		report.say("theme: " + name)
	}
	if restart := restartSettings(m.config, fresh); len(restart) > 0 {
		report.say("restart to apply: " + strings.Join(restart, ", "))
	}
	report.warnings(warnings)
	return m, nil, report.String()
}

// restartSettings are the settings fresh changes that the running app does
// not take on until it starts again: every one config.ChangedSettings finds,
// less the ones applied live.
func restartSettings(running, fresh *config.Config) []string {
	var out []string
	for _, key := range config.ChangedSettings(running, fresh) {
		if !liveSettings[key] {
			out = append(out, key)
		}
	}
	return out
}
