package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/palette"
)

// runningThemeName is the name of the theme New draws cfg in, as
// config.ResolveThemeName spells it — a builtin, a theme file's stem, or the
// path a file was named by — or "" when ui.theme named one that could not be
// used, and dark is on screen in its place.
func runningThemeName(cfg *config.Config) string {
	if r := config.ResolveThemeName(cfg.UI.Theme, "", ""); r.Form == config.ThemeFormBuiltin {
		return r.Name
	}
	if spec := cfg.ThemeSpec(); spec != nil {
		return spec.Name
	}
	return ""
}

// currentTheme is the theme on screen by name: dark stands in for a theme
// that could not be used.
func (m Model) currentTheme() string {
	if m.themeName == "" {
		return config.ThemeDark
	}
	return m.themeName
}

// themeCandidates are the themes :theme can switch to, for the palette to
// list: the builtins, then every themes/*.toml in the directories config.Load
// searched, the one on screen marked.
func (m Model) themeCandidates() []palette.Arg {
	entries := config.ListThemes(m.config.ThemeDirs())
	args := make([]palette.Arg, 0, len(entries))
	for _, e := range entries {
		where := "built in"
		if e.Kind == config.ThemeKindFile {
			where = homeRelative(e.Dir)
		}
		args = append(args, palette.Arg{Value: e.Name, Description: where, Current: e.Name == m.themeName})
	}
	return args
}

// homeRelative is path with the home directory written as ~: the palette row
// is 60 cells, and the home prefix is the part that says nothing.
func homeRelative(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	home = strings.TrimSuffix(home, string(filepath.Separator))
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}

// runTheme is :theme. With no argument it says which theme is on. With one
// it applies the theme the argument names, on the spot, and saves it as
// ui.theme — by rewriting that one line of config.toml — so the next start
// draws what is on screen now. The first save of a session backs the file
// up; see configSaved.
//
// A name that names nothing usable changes nothing and writes nothing: see
// applyTheme. Nor does the theme already on, which is what Enter on the
// palette's highlighted row is until somebody moves it.
func (m Model) runTheme(arg string) (Model, tea.Cmd, string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		if m.themeName == "" {
			return m, nil, fmt.Sprintf("theme: %s (ui.theme %q could not be used)",
				config.ThemeDark, m.config.UI.Theme)
		}
		return m, nil, "theme: " + m.themeName
	}

	configDir, defaultDir := m.config.ThemeDirs()
	if name := config.ResolveThemeName(arg, configDir, defaultDir).Name; name == m.themeName {
		return m, nil, "theme: " + name + " (unchanged)"
	}

	name, warnings, err := m.applyTheme(arg)
	if err != nil {
		return m, nil, fmt.Sprintf("⚠ %v; keeping %s", err, m.currentTheme())
	}

	// The running config says what is running, saved or not.
	m.config.UI.Theme = name
	var report commandReport
	if err := config.SetThemeLine(m.config.Path(), name, !m.configSaved); err != nil {
		report.warn(fmt.Sprintf("theme: %s (not saved: %v)", name, err))
	} else {
		m.configSaved = true
		report.say("theme: " + name)
	}
	report.warnings(warnings)
	return m, nil, report.String()
}

// applyTheme draws the app in the theme value names, as ui.theme would name
// it, and returns that name and what the theme's two halves warned about:
// the file's, from config.LoadTheme, and the palette's, from applyThemeSpec.
//
// Unless value names nothing usable — no such theme, not a name, a file that
// is not a theme. The loader's answer to that is dark, which is right at
// startup, where there is nothing else to draw, and wrong here, where the
// theme on screen is working. So nothing is applied, and the error is the
// loader's first warning, without the fallback it announces.
func (m *Model) applyTheme(value string) (name string, warnings []string, err error) {
	configDir, defaultDir := m.config.ThemeDirs()
	r := config.ResolveThemeName(value, configDir, defaultDir)
	spec, builtin, warnings := config.LoadTheme(value, configDir, defaultDir)
	if spec == nil && r.Form != config.ThemeFormBuiltin {
		reason := fmt.Sprintf("%q is not a theme", r.Name)
		if len(warnings) > 0 {
			reason = strings.TrimSuffix(warnings[0], "; using "+config.ThemeDark)
		}
		return r.Name, nil, errors.New(reason)
	}
	warnings = append(warnings, m.applyThemeSpec(spec, builtin)...)
	m.themeName = r.Name
	return r.Name, warnings, nil
}

// commandReport is a command's notice, built a clause at a time. The
// clauses are joined by " · " on the one line the hint bar has, and the line
// is marked ⚠ — which is what makes m.notify draw it as a problem — when
// any clause is one.
type commandReport struct {
	clauses []string
	problem bool
}

// say adds a clause that is news, not a problem.
func (r *commandReport) say(clause string) { r.clauses = append(r.clauses, clause) }

// warn adds a clause that is a problem.
func (r *commandReport) warn(clause string) {
	r.say(clause)
	r.problem = true
}

// warnings adds a count of ws and the first of them: there is one line to
// say it on, and the first is usually the one that explains the rest.
func (r *commandReport) warnings(ws []string) {
	switch len(ws) {
	case 0:
	case 1:
		r.warn("1 warning: " + ws[0])
	default:
		r.warn(fmt.Sprintf("%d warnings: %s", len(ws), ws[0]))
	}
}

func (r commandReport) String() string {
	line := strings.Join(r.clauses, " · ")
	if r.problem {
		return "⚠ " + line
	}
	return line
}
