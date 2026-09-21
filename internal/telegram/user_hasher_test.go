package telegram

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
)

// The updates manager asks its user hasher, on every private short update,
// whether the sender's access hash is known — and falls back to a full
// getDifference when it is not. The hasher this client hands it must
// therefore see what the peer manager has already learned, under the key
// the peer manager writes it with; the literal prefix in the adapter is
// only correct as long as this test passes.
func TestUserHasherSeesWhatThePeerManagerStored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := openStateStores(path, path)
	if err != nil {
		t.Fatalf("openStateStores: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	mgr := peers.Options{Storage: s.peerStorage()}.Build(nil)
	if err := mgr.Apply(ctx, []tg.UserClass{
		&tg.User{ID: 7, AccessHash: 99},
		&tg.User{ID: 8, AccessHash: 5, Min: true},
	}, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	h := s.userHasher()
	hash, found, err := h.GetUserAccessHash(ctx, 1, 7)
	if err != nil || !found || hash != 99 {
		t.Fatalf("GetUserAccessHash(7) = (%d, %v, %v), want (99, true, nil)", hash, found, err)
	}
	if _, found, _ := h.GetUserAccessHash(ctx, 1, 8); found {
		t.Fatal("a min user must not count as known")
	}
	if _, found, _ := h.GetUserAccessHash(ctx, 1, 9); found {
		t.Fatal("an unseen user must not count as known")
	}
}

// What gotd recovers through getDifference is fed back through
// SetUserAccessHash. It goes under the same key the read side uses, so
// the sender does not force another getDifference next time; the read
// side's key is pinned to the peer manager's by the test above.
func TestUserHasherRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := openStateStores(path, path)
	if err != nil {
		t.Fatalf("openStateStores: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.userHasher().SetUserAccessHash(ctx, 1, 7, 42); err != nil {
		t.Fatalf("SetUserAccessHash: %v", err)
	}
	hash, found, err := s.userHasher().GetUserAccessHash(ctx, 1, 7)
	if err != nil || !found || hash != 42 {
		t.Fatalf("GetUserAccessHash after Set = (%d, %v, %v), want (42, true, nil)", hash, found, err)
	}
}

// Without a state database there is nothing to adapt; gotd must get a
// true nil so it falls back to its in-memory hasher rather than calling
// methods on a typed nil.
func TestUserHasherIsNilWithoutStores(t *testing.T) {
	var s *stateStores
	if s.userHasher() != nil {
		t.Fatal("nil stores must yield a nil hasher interface")
	}
}
