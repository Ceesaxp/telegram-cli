package theme

import (
	"reflect"
	"testing"
)

// A live theme switch is checked by drawing in one marker palette, switching
// to another, and finding none of the first on screen. That only works if the
// two cannot be mistaken for each other: no colour in both, and each one a
// whole palette in its own right.
func TestTheSecondMarkerPaletteSharesNoColourWithTheFirst(t *testing.T) {
	first, firstKnown := MarkerRoles()
	second, secondKnown := SecondMarkerRoles()

	fields := reflect.TypeOf(Roles{}).NumField()
	if len(secondKnown) != fields {
		t.Fatalf("the second marker map names %d colours, want one per role (%d)",
			len(secondKnown), fields)
	}
	for rgb, role := range secondKnown {
		if other, ok := firstKnown[rgb]; ok {
			t.Errorf("rgb(%s) is %s in the second marker palette and %s in the first",
				rgb, role, other)
		}
	}

	a, b := reflect.ValueOf(first), reflect.ValueOf(second)
	for i := range fields {
		name := a.Type().Field(i).Name
		if a.Field(i).Interface() == b.Field(i).Interface() {
			t.Errorf("%s is %v in both marker palettes", name, a.Field(i))
		}
	}
}
