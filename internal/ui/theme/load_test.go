package theme

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/charmbracelet/lipgloss"
)

// A theme file names roles in snake_case, derived from the field names, so
// the keys a reader writes are the names the code uses.
func TestRoleKeysAreTheFieldNamesInSnakeCase(t *testing.T) {
	roles := reflect.TypeFor[Roles]()
	for key, field := range map[string]string{
		"bg":        "Bg",
		"cur_line":  "CurLine",
		"rule_soft": "RuleSoft",
		"fg":        "Fg",
		"bright":    "Bright",
		"cyan":      "Cyan",
		"red":       "Red",
	} {
		i, ok := roleKeys[key]
		if !ok {
			t.Errorf("%q is not a theme key; want it to name Roles.%s", key, field)
			continue
		}
		if got := roles.Field(i).Name; got != field {
			t.Errorf("%q names Roles.%s, want Roles.%s", key, got, field)
		}
	}
}

// Every role is a key, which holds only while every field of Roles is a
// lipgloss.Color. This is the pin on that invariant: a field of any other
// type — a []lipgloss.Color sender ramp is the obvious candidate — would
// silently not be a key here and would panic MarkerRoles, taking the
// palette test for every component down with it.
func TestEveryRolesFieldIsAThemeKey(t *testing.T) {
	roles := reflect.TypeFor[Roles]()
	colour := reflect.TypeFor[lipgloss.Color]()
	for i := range roles.NumField() {
		if f := roles.Field(i); f.Type != colour {
			t.Errorf("Roles.%s is a %s, not a lipgloss.Color. Every field of Roles "+
				"must be one colour: MarkerRoles sets them all by reflection and would "+
				"panic, and a theme file could not name it. Keep it outside Roles.",
				f.Name, f.Type)
		}
	}
	if len(roleKeys) != roles.NumField() {
		t.Errorf("Roles has %d fields but a theme file can set %d of them",
			roles.NumField(), len(roleKeys))
	}
}

// The pin above is only as good as themeKeys' refusal of a field that is not
// a colour, so that is tested on a struct built to break the rule.
func TestAFieldThatIsNotAColourIsNotAKey(t *testing.T) {
	type mixed struct {
		Fg   lipgloss.Color
		Ramp []lipgloss.Color
	}
	keys := themeKeys(reflect.TypeFor[mixed]())
	if _, ok := keys["ramp"]; ok {
		t.Error("a []lipgloss.Color field became a theme key")
	}
	if len(keys) != 1 {
		t.Errorf("got keys %v, want only fg", keys)
	}
}

// spec builds a theme spec the way internal/config hands one over: never-nil
// tables, keys as written.
func spec(colors, colors256 map[string]string) *config.ThemeSpec {
	if colors == nil {
		colors = map[string]string{}
	}
	if colors256 == nil {
		colors256 = map[string]string{}
	}
	return &config.ThemeSpec{
		Name: "test", Source: "themes/test.toml", Inherit: config.ThemeDark,
		Colors: colors, Colors256: colors256,
	}
}

// A theme names the roles it changes; every other role is its base's.
func TestASpecChangesOnlyTheRolesItNames(t *testing.T) {
	got, _, warnings := RolesForSpec(spec(map[string]string{"cyan": "#8ec07c"}, nil),
		config.ThemeDark, true)

	if string(got.Cyan) != "#8ec07c" {
		t.Errorf("cyan is %q, want the theme's #8ec07c", got.Cyan)
	}
	base := DarkRoles(true)
	got.Cyan = base.Cyan
	if got != base {
		t.Errorf("roles the theme does not name moved off the dark base:\n got %+v\nwant %+v", got, base)
	}
	if len(warnings) != 0 {
		t.Errorf("a clean theme warned: %q", warnings)
	}
}

// inherit = "light" is drawn over the light palette, not dark with a few
// roles moved.
func TestASpecOverLightStartsFromLight(t *testing.T) {
	got, _, _ := RolesForSpec(spec(map[string]string{"cyan": "#8ec07c"}, nil),
		config.ThemeLight, true)

	base := LightRoles(true)
	got.Cyan = base.Cyan
	if got != base {
		t.Errorf("a theme inheriting light is not light underneath:\n got %+v\nwant %+v", got, base)
	}
}

// Keys fold to lower case: Cyan and CYAN are cyan, as a reader writing
// the key from the field name would expect.
func TestKeysIgnoreCase(t *testing.T) {
	got, _, warnings := RolesForSpec(spec(map[string]string{
		"Cyan": "#8ec07c", "CUR_LINE": "#32302f",
	}, nil), config.ThemeDark, true)

	if string(got.Cyan) != "#8ec07c" {
		t.Errorf("Cyan did not set cyan: got %q", got.Cyan)
	}
	if string(got.CurLine) != "#32302f" {
		t.Errorf("CUR_LINE did not set cur_line: got %q", got.CurLine)
	}
	if len(warnings) != 0 {
		t.Errorf("a key in another case warned: %q", warnings)
	}
}

// Two keys that fold to one role are a mistake worth a warning, and which
// one wins must not depend on map order: the first in sorted order, every
// run.
func TestTwoKeysForOneRoleWarnAndTheFirstWins(t *testing.T) {
	for range 20 {
		got, _, warnings := RolesForSpec(spec(map[string]string{
			"cyan": "#222222", "Cyan": "#111111", "CYAN": "#333333",
		}, nil), config.ThemeDark, true)

		if string(got.Cyan) != "#333333" {
			t.Fatalf("cyan is %q, want %q from CYAN, the first key in sorted order",
				got.Cyan, "#333333")
		}
		if len(warnings) != 2 {
			t.Fatalf("got %d warnings, want one for each key that lost: %q", len(warnings), warnings)
		}
		for _, w := range warnings {
			if !strings.Contains(w, "themes/test.toml") || !strings.Contains(w, "CYAN") {
				t.Errorf("warning does not name the file and the key that won: %q", w)
			}
		}
	}
}

// A key that is no role — a typo, or a role from a newer build — is
// ignored with a warning rather than failing the theme.
func TestAnUnknownKeyWarnsAndIsIgnored(t *testing.T) {
	got, _, warnings := RolesForSpec(spec(map[string]string{
		"cyan": "#8ec07c", "accent": "#ff0000",
	}, map[string]string{"backgrnd": "235"}), config.ThemeDark, true)

	if string(got.Cyan) != "#8ec07c" {
		t.Errorf("an unknown key cost a known one: cyan is %q", got.Cyan)
	}
	want := []string{"colors.accent", "colors256.backgrnd"}
	if len(warnings) != len(want) {
		t.Fatalf("got %d warnings, want %d: %q", len(warnings), len(want), warnings)
	}
	for i, key := range want {
		if !strings.Contains(warnings[i], "themes/test.toml") || !strings.Contains(warnings[i], key) {
			t.Errorf("warning %d does not name the file and %s: %q", i, key, warnings[i])
		}
	}
}

// A value lipgloss cannot draw renders as no colour at all — for bg, an
// unpainted surface — so [colors] is checked strictly at load: #rrggbb or
// #rgb, and anything else is warned about and inherits.
func TestAMalformedHexWarnsAndInherits(t *testing.T) {
	base := DarkRoles(true)
	for _, value := range []string{
		"", "8ec07c", "#8ec07", "#8ec07c0", "#12", "#ggg", "#8ec07g", "235", " #8ec07c", "red",
	} {
		t.Run(value, func(t *testing.T) {
			got, _, warnings := RolesForSpec(spec(map[string]string{"bg": value}, nil),
				config.ThemeDark, true)

			if got.Bg != base.Bg {
				t.Errorf("bg = %q became %q; want it to inherit dark's", value, got.Bg)
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0], "colors.bg") ||
				!strings.Contains(warnings[0], "themes/test.toml") {
				t.Errorf("want one warning naming the file and colors.bg, got %q", warnings)
			}
		})
	}
}

// Both hex forms lipgloss draws are accepted, in either case.
func TestBothHexFormsAreColours(t *testing.T) {
	for _, value := range []string{"#8ec07c", "#8EC07C", "#abc", "#ABC"} {
		got, _, warnings := RolesForSpec(spec(map[string]string{"bg": value}, nil),
			config.ThemeDark, true)
		if string(got.Bg) != value || len(warnings) != 0 {
			t.Errorf("bg = %q gave %q with warnings %q; want it as written, no warning",
				value, got.Bg, warnings)
		}
	}
}

// [colors256] is the decimal range 0–255 and nothing else: termenv writes an
// index past 255 into the escape sequence as it is.
func TestAMalformedXtermValueWarnsAndInherits(t *testing.T) {
	base := DarkRoles(false)
	for _, value := range []string{"", "256", "1000", "-1", "+7", "0x10", " 7", "#1d2021", "seven"} {
		t.Run(value, func(t *testing.T) {
			got, _, warnings := RolesForSpec(spec(nil, map[string]string{"bg": value}),
				config.ThemeDark, false)

			if got.Bg != base.Bg {
				t.Errorf("colors256 bg = %q became %q; want dark's own", value, got.Bg)
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0], "colors256.bg") ||
				!strings.Contains(warnings[0], "themes/test.toml") {
				t.Errorf("want one warning naming the file and colors256.bg, got %q", warnings)
			}
		})
	}
}

// No spec is the builtin alone: the path every config that names no theme
// file takes, so it must be exactly what RolesFor always gave.
func TestNoSpecIsTheBuiltin(t *testing.T) {
	for _, builtin := range []string{config.ThemeDark, config.ThemeLight} {
		for _, trueColor := range []bool{true, false} {
			got, _, warnings := RolesForSpec(nil, builtin, trueColor)
			if got != RolesFor(builtin, trueColor) || len(warnings) != 0 {
				t.Errorf("%s (truecolour %v) with no spec is not the builtin, or warned: %q",
					builtin, trueColor, warnings)
			}
		}
	}
}

// The fixture goes through the config half first, as a user's theme does,
// so the two halves are tested meeting and not only apart.
func TestAThemeFileIsDrawnOverItsBase(t *testing.T) {
	spec, builtin, warnings := config.LoadTheme("testdata/over-light.toml", "", "")
	if spec == nil || len(warnings) != 0 {
		t.Fatalf("the fixture did not load cleanly: spec %v, warnings %q", spec, warnings)
	}
	if builtin != config.ThemeLight {
		t.Fatalf("the fixture's base is %q, want light", builtin)
	}

	got, _, warnings := RolesForSpec(spec, builtin, true)
	if len(warnings) != 0 {
		t.Errorf("the fixture warned: %q", warnings)
	}
	if string(got.Cyan) != "#8ec07c" || string(got.Red) != "#fb4934" {
		t.Errorf("cyan %q and red %q, want the fixture's #8ec07c and #fb4934", got.Cyan, got.Red)
	}
	base := LightRoles(true)
	got.Cyan, got.Red = base.Cyan, base.Red
	if got != base {
		t.Errorf("the rest is not light:\n got %+v\nwant %+v", got, base)
	}
}

// Colour depth, per role, in the order docs/theming.md gives it. On a
// truecolour terminal the hex, the theme's or the base's; on any other, the
// theme's hand-picked [colors256], else its hex quantised, else the base's
// own hand-picked 256 value.
func TestEachRoleResolvesToTheTerminalsDepth(t *testing.T) {
	theme := spec(
		map[string]string{"bg": "#1d2021", "cyan": "#8ec07c"},
		map[string]string{"bg": "235", "red": "160"},
	)
	tests := []struct {
		name      string
		trueColor bool
		role      func(Roles) lipgloss.Color
		want      string
	}{
		{"truecolour takes the theme's hex", true, func(r Roles) lipgloss.Color { return r.Bg }, "#1d2021"},
		{"truecolour ignores [colors256]", true, func(r Roles) lipgloss.Color { return r.Red }, string(darkHex.Red)},
		{"truecolour falls back to the base hex", true, func(r Roles) lipgloss.Color { return r.Fg }, string(darkHex.Fg)},
		{"256 takes [colors256] over the hex", false, func(r Roles) lipgloss.Color { return r.Bg }, "235"},
		{"256 takes [colors256] alone", false, func(r Roles) lipgloss.Color { return r.Red }, "160"},
		{"256 quantises a hex with no [colors256]", false, func(r Roles) lipgloss.Color { return r.Cyan }, "108"},
		{"256 falls back to the base's hand-picked value", false, func(r Roles) lipgloss.Color { return r.Fg }, string(dark256.Fg)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, warnings := RolesForSpec(theme, config.ThemeDark, tt.trueColor)
			if string(tt.role(got)) != tt.want {
				t.Errorf("got %q, want %q", tt.role(got), tt.want)
			}
			if len(warnings) != 0 {
				t.Errorf("warned: %q", warnings)
			}
		})
	}
}

// The builtin 256 column is hand-picked, never generated from the hex, and
// this is where that stops being a claim: dark's fg is #c9ced4, which
// termenv quantises to 188, and the table says 252. So a theme that writes
// dark's own fg hex gets 188 on a 256-colour terminal, while a theme that
// leaves fg alone keeps the hand-picked 252.
func TestTheBuiltin256ColumnIsHandPickedNotQuantised(t *testing.T) {
	if string(darkHex.Fg) != "#c9ced4" || string(dark256.Fg) != "252" {
		t.Fatalf("precondition: dark fg is %q / %q, want #c9ced4 / 252", darkHex.Fg, dark256.Fg)
	}
	if got, ok := quantise("#c9ced4"); !ok || got != "188" {
		t.Fatalf("termenv quantises #c9ced4 to %q (ok %v); the pin expects 188", got, ok)
	}

	written, _, _ := RolesForSpec(spec(map[string]string{"fg": "#c9ced4"}, nil), config.ThemeDark, false)
	if string(written.Fg) != "188" {
		t.Errorf("a theme writing fg = #c9ced4 got %q on 256 colours, want the quantised 188", written.Fg)
	}
	untouched, _, _ := RolesForSpec(spec(map[string]string{"cyan": "#8ec07c"}, nil), config.ThemeDark, false)
	if string(untouched.Fg) != "252" {
		t.Errorf("a theme leaving fg alone got %q on 256 colours, want the hand-picked 252", untouched.Fg)
	}
}

// termenv answers a colour it cannot parse with nil, and a nil colour draws
// as nothing. quantise refuses it rather than passing that on.
func TestQuantiseRefusesWhatTermenvCannotConvert(t *testing.T) {
	for _, junk := range []string{"", "#", "#zzzzzz", "#12", "nonsense"} {
		if got, ok := quantise(junk); ok {
			t.Errorf("quantise(%q) = %q, want a refusal", junk, got)
		}
	}
}
