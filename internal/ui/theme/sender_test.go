package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// A sender's colour is an identity cue, so the default ramp must hash every
// person to the colour they have always had. These are SenderColour's
// answers from before a theme could choose its own ramp, pinned by role
// name: a change to the hash, the ramp's order, or its length moves them.
var senderColourPins = map[int64]string{
	0: "cyan", 1: "mauve", 2: "amber", 3: "blue", 4: "cyan", 5: "mauve",
	7: "blue", 42: "amber", 12345: "mauve", -9: "cyan", 1 << 40: "blue",
	777000: "amber", 5_123_456_789: "blue",
}

// defaultRampNames names the default ramp's colours in a palette where
// every role is distinct.
func defaultRampNames(r Roles) map[lipgloss.Color]string {
	return map[lipgloss.Color]string{r.Mauve: "mauve", r.Cyan: "cyan", r.Blue: "blue", r.Amber: "amber"}
}

func TestSenderColoursAreTheOnesPeopleAlwaysHad(t *testing.T) {
	r, _ := MarkerRoles()
	names := defaultRampNames(r)
	for id, want := range senderColourPins {
		if got := names[SenderColour(id, r)]; got != want {
			t.Errorf("user %d is %q, has always been %q", id, got, want)
		}
	}
}
