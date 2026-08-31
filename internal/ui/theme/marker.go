package theme

import (
	"fmt"
	"reflect"

	"github.com/charmbracelet/lipgloss"
)

// MarkerRoles is a palette in which every role is a colour nothing could
// arrive at by accident — #0100xx, one per field, in declaration order — and
// a lookup from the "r;g;b" a terminal sequence carries back to the role's
// name.
//
// It answers one question: did this component draw ONLY the palette it was
// given? Rendering through it and checking every colour on screen against the
// map fails both ways a component gets that wrong — ignoring the palette
// (drawing some default instead) and reaching past it for a literal.
//
// Exported from the production package for the same reason [OpenStyle] and
// [cell.PaintedWidth] are: the invariant belongs to every component, so the
// tool for asserting it cannot live inside one component's test package. It
// is built by reflection deliberately — a hand-written table would need
// updating whenever Roles gains a field, and the field it forgot would be the
// one nobody was checking.
func MarkerRoles() (Roles, map[string]string) {
	var r Roles
	v := reflect.ValueOf(&r).Elem()
	byColour := make(map[string]string, v.NumField())

	for i := range v.NumField() {
		hex := fmt.Sprintf("#0100%02x", i+1)
		v.Field(i).Set(reflect.ValueOf(lipgloss.Color(hex)))
		byColour[fmt.Sprintf("1;0;%d", i+1)] = v.Type().Field(i).Name
	}
	return r, byColour
}
