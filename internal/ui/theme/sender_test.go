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

// The ramp-taking form over the default ramp — or over no ramp, which is
// what a component that was never handed one has — is SenderColour exactly.
func TestTheDefaultRampColoursEveryoneAsBefore(t *testing.T) {
	r, _ := MarkerRoles()
	names := defaultRampNames(r)
	for id, want := range senderColourPins {
		if got := names[SenderColourFrom(id, DefaultSenderRamp(r), r)]; got != want {
			t.Errorf("user %d is %q on the default ramp, has always been %q", id, got, want)
		}
	}
	for id := int64(-100); id < 1000; id++ {
		want := SenderColour(id, r)
		if got := SenderColourFrom(id, DefaultSenderRamp(r), r); got != want {
			t.Fatalf("user %d: default ramp gives %q, SenderColour %q", id, got, want)
		}
		if got := SenderColourFrom(id, nil, r); got != want {
			t.Fatalf("user %d: no ramp gives %q, SenderColour %q", id, got, want)
		}
	}
}

// A theme's own ramp is hashed into the same way, spreading people across
// whatever it names; a one-colour ramp is legal and colours everyone alike.
func TestACustomRampIsHashedInto(t *testing.T) {
	r, _ := MarkerRoles()
	seen := map[lipgloss.Color]int{}
	for id := int64(1000); id < 1064; id++ {
		seen[SenderColourFrom(id, []lipgloss.Color{r.Green, r.Red}, r)]++
	}
	if len(seen) != 2 || seen[r.Green] == 0 || seen[r.Red] == 0 {
		t.Errorf("64 consecutive IDs over a green-and-red ramp gave %v", seen)
	}
	for id := int64(1000); id < 1064; id++ {
		if got := SenderColourFrom(id, []lipgloss.Color{r.Green}, r); got != r.Green {
			t.Fatalf("user %d on a one-colour ramp is %q, not that colour", id, got)
		}
	}
}

// The colour is looked up for every sender on every line drawn, so neither
// form may allocate: not the default ramp, and not a theme's.
func TestASenderColourAllocatesNothing(t *testing.T) {
	r, _ := MarkerRoles()
	ramp := DefaultSenderRamp(r)
	var sink lipgloss.Color
	if n := testing.AllocsPerRun(100, func() { sink = SenderColour(12345, r) }); n != 0 {
		t.Errorf("SenderColour allocates %v times per call", n)
	}
	if n := testing.AllocsPerRun(100, func() { sink = SenderColourFrom(12345, ramp, r) }); n != 0 {
		t.Errorf("SenderColourFrom allocates %v times per call", n)
	}
	_ = sink
}
