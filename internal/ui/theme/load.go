package theme

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// RolesForSpec is the palette for a theme, and the ramp its senders' names
// are coloured from: the builtin named by builtin — [config.Config.ThemeBuiltin],
// which for a theme file is the base it inherits — with whatever the spec
// sets drawn over it. A nil spec is the builtin alone, so this is the one
// place the app's palette is chosen, theme file or not.
//
// Nothing in a spec is fatal. What cannot be used is left out, its role
// inherits, and the warnings say so, each naming the theme file. They do
// not depend on trueColor: a broken [colors256] is broken on any terminal.
func RolesForSpec(spec *config.ThemeSpec, builtin string, trueColor bool) (Roles, []lipgloss.Color, []string) {
	r := RolesFor(builtin, trueColor)
	if spec == nil {
		return r, DefaultSenderRamp(r), nil
	}
	hex, warnings := tableRoles(spec.Source, "colors", spec.Colors, hexForm)
	xterm, w := tableRoles(spec.Source, "colors256", spec.Colors256, xtermForm)
	warnings = append(warnings, w...)
	v := reflect.ValueOf(&r).Elem()
	for i := range v.NumField() {
		if value, ok := atDepth(i, hex, xterm, trueColor); ok {
			v.Field(i).SetString(value)
		}
	}
	ramp, w := senderRamp(spec.Source, spec.Ramp, r)
	return r, ramp, append(warnings, w...)
}

// senderRamp is [senders].ramp as colours, resolved against the theme's
// finished roles, so a ramp naming cyan gets the theme's cyan. Absent is the
// default ramp. A ramp naming anything that is not a role, or nothing, is
// the default ramp with a warning — all of it, because a ramp with an entry
// dropped is a different length and everybody hashes to a new colour
// anyway. One entry is legal: one colour for everybody is a taste.
func senderRamp(source string, names []string, r Roles) ([]lipgloss.Color, []string) {
	if names == nil {
		return DefaultSenderRamp(r), nil
	}
	if len(names) == 0 {
		return DefaultSenderRamp(r), []string{fmt.Sprintf(
			"theme file %s: senders.ramp is empty; using the default ramp", source)}
	}
	v := reflect.ValueOf(r)
	ramp := make([]lipgloss.Color, len(names))
	for n, name := range names {
		i, ok := roleKeys[strings.ToLower(name)]
		if !ok {
			return DefaultSenderRamp(r), []string{fmt.Sprintf(
				"theme file %s: senders.ramp names %q, which is not a role; using the default ramp",
				source, name)}
		}
		ramp[n] = lipgloss.Color(v.Field(i).String())
	}
	return ramp, nil
}

// CheckSpec is what [RolesForSpec] would warn about the spec, without a
// palette to keep. main prints startup warnings before the app is built and
// the palette with it, so this is how a theme's warnings reach the same
// print as the config's rather than arriving after it, into the void.
func CheckSpec(spec *config.ThemeSpec, builtin string) []string {
	_, _, warnings := RolesForSpec(spec, builtin, true)
	return warnings
}

// atDepth is the theme's own value for role i at the terminal's colour
// depth, or false when it has none and the base's value stands.
//
// On truecolour that is the theme's hex. On anything less it is the
// theme's hand-picked [colors256] value, else its hex quantised, and only
// then the base's — whose 256 column is hand-picked too, which is why a
// theme's hex is never quantised over a role the theme did not set.
func atDepth(i int, hex, xterm map[int]string, trueColor bool) (string, bool) {
	if trueColor {
		value, ok := hex[i]
		return value, ok
	}
	if value, ok := xterm[i]; ok {
		return value, true
	}
	if value, ok := hex[i]; ok {
		return quantise(value)
	}
	return "", false
}

// quantise is a hex colour as the nearest xterm-256 index, by termenv's own
// conversion: the one lipgloss would make at render time anyway, made here
// so the result is fixed at load and can be tested. Hand-writing a nearest
// colour search would only be a second opinion on the same question.
//
// False when termenv cannot convert it. A hex that passed isHexColour never
// fails, but termenv answers junk with a nil colour, and a nil drawn is an
// unpainted surface; the role keeps its base value instead.
func quantise(hex string) (string, bool) {
	c, ok := termenv.ANSI256.Convert(termenv.RGBColor(hex)).(termenv.ANSI256Color)
	if !ok {
		return "", false
	}
	return strconv.Itoa(int(c)), true
}

// A colourForm is what the values of one colour table have to look like.
type colourForm struct {
	what  string // for the warning: what the value is not
	valid func(string) bool
}

var (
	hexForm   = colourForm{"a #rrggbb or #rgb colour", isHexColour}
	xtermForm = colourForm{"an xterm colour from 0 to 255", isXtermColour}
)

// tableRoles folds one colour table onto the roles it names, by field
// index. Keys fold to lower case and are walked in sorted order, so when two
// fold to one role the same one wins every run — the first — and the rest
// are warned about. A value not in the table's form is warned about and
// left out, so its role inherits.
func tableRoles(source, table string, raw map[string]string, form colourForm) (map[int]string, []string) {
	out := make(map[int]string, len(raw))
	claimedBy := make(map[int]string, len(raw))
	var warnings []string
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		i, ok := roleKeys[strings.ToLower(key)]
		if !ok {
			warnings = append(warnings, fmt.Sprintf(
				"theme file %s: %s.%s is not a role; ignored", source, table, key))
			continue
		}
		if first, taken := claimedBy[i]; taken {
			warnings = append(warnings, fmt.Sprintf(
				"theme file %s: %s.%s and %s.%s are the same role; using %s.%s",
				source, table, first, table, key, table, first))
			continue
		}
		claimedBy[i] = key
		if !form.valid(raw[key]) {
			warnings = append(warnings, fmt.Sprintf(
				"theme file %s: %s.%s = %q is not %s; ignored",
				source, table, key, raw[key], form.what))
			continue
		}
		out[i] = raw[key]
	}
	return out, warnings
}

// isHexColour is the check lipgloss does not make. It draws an invalid
// colour as no colour at all — not a wrong one, zero escape bytes — which
// for bg or panel is a surface left unpainted, so a value is refused here
// rather than discovered on screen.
func isHexColour(s string) bool {
	if len(s) != 4 && len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// isXtermColour is a decimal index into the 256-colour table. The range
// check is not optional: termenv writes an out-of-range index into the
// escape sequence as it is.
func isXtermColour(s string) bool {
	if len(s) == 0 || len(s) > 3 {
		return false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	return n <= 255
}

// roleKeys maps each key a theme file may set to the index of the [Roles]
// field it names: CurLine is cur_line, RuleSoft is rule_soft.
//
// Derived by reflection, like [MarkerRoles], so a new role is a new legal
// key the moment it is declared, with nothing to register. That only works
// because every field of Roles is a lipgloss.Color — it is also what lets
// MarkerRoles set them all — so a field of any other type is left out here,
// and the count pin in load_test.go fails loudly about it. Anything that is
// not one colour per role, the sender ramp above all, lives outside Roles.
var roleKeys = themeKeys(reflect.TypeFor[Roles]())

// themeKeys is the key-to-field map for a struct of colours. Separate from
// roleKeys so the rule it applies can be tested on a struct that breaks it.
func themeKeys(t reflect.Type) map[string]int {
	colour := reflect.TypeFor[lipgloss.Color]()
	keys := make(map[string]int, t.NumField())
	for i := range t.NumField() {
		if f := t.Field(i); f.Type == colour {
			keys[snakeCase(f.Name)] = i
		}
	}
	return keys
}

// snakeCase is a field name as a theme key: an underscore before every
// capital but the first, then lower case. Mechanical on purpose — a reader
// who knows one key can guess the rest.
func snakeCase(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
