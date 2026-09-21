package theme

import (
	"reflect"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

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
