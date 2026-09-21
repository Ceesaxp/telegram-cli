package theme

import (
	"reflect"
	"testing"

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
