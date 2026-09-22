package notification

// Alert posts one message's notification and plays its sound, and returns
// what the caller must write to the terminal: the notification's sequence,
// the bell, both, or "".
//
// Where the notifier and the player both fall back to the bell, they ring it
// through one limit, the notifier's. The terminal has one bell, and with a
// limit each the two only agree while they are asked at the same instants:
// once one has rung without the other, each rings once an interval on its
// own, and a burst rings twice.
func Alert(n *Notifier, s *SoundPlayer, title, body string) string {
	bell := s.playOrRing(n.bells)
	return n.Notify(title, body) + bell
}
