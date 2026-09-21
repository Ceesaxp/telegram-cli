package theme

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/charmbracelet/lipgloss"
)

// RolesForSpec is the palette for a theme: the builtin named by builtin,
// with whatever the spec sets drawn over it. A nil spec is the builtin
// alone, so this is the one place the app's palette is chosen whether or
// not a theme file is involved.
func RolesForSpec(spec *config.ThemeSpec, builtin string, trueColor bool) (Roles, []lipgloss.Color, []string) {
	r := RolesFor(builtin, trueColor)
	if spec == nil {
		return r, nil, nil
	}
	colors, warnings := tableRoles(spec.Source, "colors", spec.Colors, hexForm)
	_, w := tableRoles(spec.Source, "colors256", spec.Colors256, xtermForm)
	warnings = append(warnings, w...)
	v := reflect.ValueOf(&r).Elem()
	for i, value := range colors {
		v.Field(i).SetString(value)
	}
	return r, nil, warnings
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
