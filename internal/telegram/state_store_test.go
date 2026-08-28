package telegram

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gotd/td/telegram/peers"

	"github.com/imtaqin/telegram-cli/internal/config"
)

func cfgWith(session, state string) *config.Config {
	return &config.Config{
		Storage: config.StorageConfig{
			SessionFile: session,
			StateFile:   state,
		},
	}
}

func TestStateDBTarget(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *config.Config
		noUpdates bool
		wantPath  string
		wantOpen  bool
	}{
		{
			name:     "default next to session file",
			cfg:      cfgWith(filepath.Join("/data", "tele", "session.json"), ""),
			wantPath: filepath.Join("/data", "tele", "state.db"),
			wantOpen: true,
		},
		{
			name:     "explicit override wins",
			cfg:      cfgWith(filepath.Join("/data", "tele", "session.json"), filepath.Join("/elsewhere", "u.db")),
			wantPath: filepath.Join("/elsewhere", "u.db"),
			wantOpen: true,
		},
		{
			// RPC-only clients share the data directory with the TUI and
			// must never take the exclusive bbolt lock.
			name:      "no-updates client never opens",
			cfg:       cfgWith(filepath.Join("/data", "tele", "session.json"), ""),
			noUpdates: true,
			wantOpen:  false,
		},
		{
			name:      "no-updates client ignores explicit override too",
			cfg:       cfgWith("", filepath.Join("/elsewhere", "u.db")),
			noUpdates: true,
			wantOpen:  false,
		},
		{
			name:     "no derivable location",
			cfg:      cfgWith("", ""),
			wantOpen: false,
		},
		{
			name:     "nil config",
			cfg:      nil,
			wantOpen: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, open := stateDBTarget(tt.cfg, tt.noUpdates)
			if open != tt.wantOpen {
				t.Fatalf("open = %v, want %v", open, tt.wantOpen)
			}
			if open && path != tt.wantPath {
				t.Fatalf("path = %q, want %q", path, tt.wantPath)
			}
			if !open && path != "" {
				t.Fatalf("path = %q, want empty when not opening", path)
			}
		})
	}
}

func TestNilStateStoresFallsBackToInMemory(t *testing.T) {
	var s *stateStores
	if got := s.stateStorage(); got != nil {
		t.Errorf("stateStorage() = %v, want nil so gotd uses its in-memory default", got)
	}
	if got := s.peerStorage(); got != nil {
		t.Errorf("peerStorage() = %v, want nil so gotd uses its in-memory default", got)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

func TestOpenStateStoresFreshFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := openStateStores(path, path)
	if err != nil {
		t.Fatalf("openStateStores: %v", err)
	}
	defer s.Close()

	if s.stateStorage() == nil || s.peerStorage() == nil {
		t.Fatal("expected both storages to be non-nil")
	}

	// First run contract: the updates manager treats (found == false,
	// err == nil) as "no state yet" and fetches the current state from
	// the server, which is exactly today's behaviour.
	state, found, err := s.stateStorage().GetState(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetState on empty storage: %v", err)
	}
	if found {
		t.Fatalf("GetState found = true on empty storage (state %+v)", state)
	}
}

func TestOpenStateStoresLockHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")

	first, err := openStateStores(path, path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	defer first.Close()

	// bbolt's lock is exclusive; the second open must give up quickly
	// rather than block the caller forever.
	const timeout = 100 * time.Millisecond
	start := time.Now()
	second, err := openStateStoresTimeout(path, path, timeout)
	elapsed := time.Since(start)
	if err == nil {
		second.Close()
		t.Fatal("second open succeeded, want a lock timeout error")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("second open took %s, want it to fail fast", elapsed)
	}
}

func TestOpenStateStoresCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	garbage := make([]byte, 8192)
	for i := range garbage {
		garbage[i] = 0xAB
	}
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := openStateStoresTimeout(path, path, 100*time.Millisecond)
	if err == nil {
		s.Close()
		t.Fatal("open of a corrupt file succeeded, want an error so the caller falls back")
	}
}

func TestBoltPeerStorageRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")

	s, err := openStateStores(path, path)
	if err != nil {
		t.Fatalf("openStateStores: %v", err)
	}

	ps := s.peerStorage()
	key := peers.Key{Prefix: "users_", ID: -1002233445566}
	val := peers.Value{AccessHash: -8877665544332211}

	if _, found, err := ps.Find(ctx, key); err != nil || found {
		t.Fatalf("Find on empty storage = (found %v, err %v), want (false, nil)", found, err)
	}
	if err := ps.Save(ctx, key, val); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := ps.SavePhone(ctx, "15551234567", key); err != nil {
		t.Fatalf("SavePhone: %v", err)
	}
	if err := ps.SaveContactsHash(ctx, 1234567890123); err != nil {
		t.Fatalf("SaveContactsHash: %v", err)
	}

	// Reopen to prove the data actually survives the process.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err = openStateStores(path, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	ps = s.peerStorage()

	got, found, err := ps.Find(ctx, key)
	if err != nil || !found {
		t.Fatalf("Find after reopen = (found %v, err %v), want (true, nil)", found, err)
	}
	if got != val {
		t.Fatalf("Find = %+v, want %+v", got, val)
	}

	gotKey, gotVal, found, err := ps.FindPhone(ctx, "15551234567")
	if err != nil || !found {
		t.Fatalf("FindPhone = (found %v, err %v), want (true, nil)", found, err)
	}
	if gotKey != key || gotVal != val {
		t.Fatalf("FindPhone = (%+v, %+v), want (%+v, %+v)", gotKey, gotVal, key, val)
	}

	if _, _, found, err := ps.FindPhone(ctx, "999"); err != nil || found {
		t.Fatalf("FindPhone(unknown) = (found %v, err %v), want (false, nil)", found, err)
	}

	hash, err := ps.GetContactsHash(ctx)
	if err != nil {
		t.Fatalf("GetContactsHash: %v", err)
	}
	if hash != 1234567890123 {
		t.Fatalf("GetContactsHash = %d, want 1234567890123", hash)
	}
}

func TestBoltPeerStorageEmptyContactsHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := openStateStores(path, path)
	if err != nil {
		t.Fatalf("openStateStores: %v", err)
	}
	defer s.Close()

	// A missing hash must read as 0 so contacts.getContacts returns the
	// full list instead of contactsNotModified.
	hash, err := s.peerStorage().GetContactsHash(context.Background())
	if err != nil {
		t.Fatalf("GetContactsHash: %v", err)
	}
	if hash != 0 {
		t.Fatalf("GetContactsHash = %d, want 0", hash)
	}
}

func TestPeerKeyCodec(t *testing.T) {
	for _, key := range []peers.Key{
		{Prefix: "users_", ID: 1},
		{Prefix: "channel_", ID: -1001234567890},
		{Prefix: "chats_", ID: 0},
		{Prefix: "", ID: 9223372036854775807},
	} {
		got, ok := decodePeerKey(encodePeerKey(key))
		if !ok {
			t.Fatalf("decodePeerKey(%+v) not ok", key)
		}
		if got != key {
			t.Fatalf("round trip = %+v, want %+v", got, key)
		}
	}

	if _, ok := decodePeerKey([]byte("short")); ok {
		t.Fatal("decodePeerKey of a truncated value reported ok")
	}
}

func TestStateIdentity(t *testing.T) {
	dbPath := filepath.Join("/data", "tele", "state.db")

	if got := stateIdentity(cfgWith("/data/tele/session.json", ""), dbPath); got != "/data/tele/session.json" {
		t.Errorf("stateIdentity = %q, want the session file", got)
	}
	// Without a session file the database path is the only stable identity.
	if got := stateIdentity(cfgWith("", "/elsewhere/u.db"), dbPath); got != dbPath {
		t.Errorf("stateIdentity = %q, want %q", got, dbPath)
	}
	if got := stateIdentity(nil, dbPath); got != dbPath {
		t.Errorf("stateIdentity(nil) = %q, want %q", got, dbPath)
	}
}

func TestPeerNamespaceIsPerSessionFile(t *testing.T) {
	a := string(peerNamespace("/data/tele/session.json"))
	b := string(peerNamespace("/data/tele/session-mcp.json"))
	if a == b {
		t.Fatal("different session files produced the same namespace")
	}
	// Stable across restarts, and insensitive to path spelling.
	if a != string(peerNamespace("/data/tele/./session.json")) {
		t.Fatal("namespace is not stable under path cleaning")
	}
	// Must not collide with contrib's 16-byte user-ID state buckets.
	if len(a) != len("peerns_")+32 {
		t.Fatalf("namespace %q has unexpected length %d", a, len(a))
	}
}

// Two accounts sharing one state.db must not clobber each other's access
// hashes: the hash is only valid for the account that observed it.
func TestPeerStorageNamespaceIsolation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	sessionA := filepath.Join(dir, "session.json")
	sessionB := filepath.Join(dir, "session-mcp.json")

	key := peers.Key{Prefix: "channel_", ID: 1001}
	valA := peers.Value{AccessHash: 111}
	valB := peers.Value{AccessHash: 222}

	// bbolt's lock is exclusive, so the accounts take turns — which is
	// exactly how the TUI and telegram-api interleave in practice.
	a, err := openStateStores(dbPath, sessionA)
	if err != nil {
		t.Fatalf("open for A: %v", err)
	}
	if err := a.peerStorage().Save(ctx, key, valA); err != nil {
		t.Fatalf("A save: %v", err)
	}
	if err := a.peerStorage().SavePhone(ctx, "15551110000", key); err != nil {
		t.Fatalf("A save phone: %v", err)
	}
	if err := a.peerStorage().SaveContactsHash(ctx, 777); err != nil {
		t.Fatalf("A save contacts hash: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("A close: %v", err)
	}

	b, err := openStateStores(dbPath, sessionB)
	if err != nil {
		t.Fatalf("open for B: %v", err)
	}
	// B starts cold even though A already cached this peer.
	if _, found, err := b.peerStorage().Find(ctx, key); err != nil || found {
		t.Fatalf("B Find = (found %v, err %v), want (false, nil) — namespaces leaked", found, err)
	}
	if _, _, found, err := b.peerStorage().FindPhone(ctx, "15551110000"); err != nil || found {
		t.Fatalf("B FindPhone = (found %v, err %v), want (false, nil)", found, err)
	}
	if hash, err := b.peerStorage().GetContactsHash(ctx); err != nil || hash != 0 {
		t.Fatalf("B GetContactsHash = (%d, %v), want (0, nil)", hash, err)
	}
	if err := b.peerStorage().Save(ctx, key, valB); err != nil {
		t.Fatalf("B save: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("B close: %v", err)
	}

	// A's hash survived B's write to the same peer ID.
	a, err = openStateStores(dbPath, sessionA)
	if err != nil {
		t.Fatalf("reopen for A: %v", err)
	}
	defer a.Close()

	got, found, err := a.peerStorage().Find(ctx, key)
	if err != nil || !found {
		t.Fatalf("A Find after B = (found %v, err %v), want (true, nil)", found, err)
	}
	if got != valA {
		t.Fatalf("A Find = %+v, want %+v — B clobbered A", got, valA)
	}
	if hash, err := a.peerStorage().GetContactsHash(ctx); err != nil || hash != 777 {
		t.Fatalf("A GetContactsHash after B = (%d, %v), want (777, nil)", hash, err)
	}
}

// Re-authorizing as a different account reuses the same session file, so
// the session-derived namespace alone cannot tell the accounts apart. The
// post-auth owner check must drop the stale cache.
func TestBindOwnerDropsForeignNamespace(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	session := filepath.Join(dir, "session.json")

	key := peers.Key{Prefix: "users_", ID: 55}

	s, err := openStateStores(dbPath, session)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	dropped, err := s.bindOwner(ctx, 1000)
	if err != nil {
		t.Fatalf("first bindOwner: %v", err)
	}
	if dropped {
		t.Fatal("first bindOwner dropped a fresh namespace")
	}
	if err := s.peerStorage().Save(ctx, key, peers.Value{AccessHash: 9}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Same account again: cache must be preserved.
	if dropped, err := s.bindOwner(ctx, 1000); err != nil || dropped {
		t.Fatalf("rebind same owner = (dropped %v, err %v), want (false, nil)", dropped, err)
	}
	if _, found, err := s.peerStorage().Find(ctx, key); err != nil || !found {
		t.Fatalf("Find after rebind = (found %v, err %v), want (true, nil)", found, err)
	}

	// Different account on the same session file: cache must be dropped.
	dropped, err = s.bindOwner(ctx, 2000)
	if err != nil {
		t.Fatalf("rebind new owner: %v", err)
	}
	if !dropped {
		t.Fatal("rebind with a different owner did not drop the namespace")
	}
	if _, found, err := s.peerStorage().Find(ctx, key); err != nil || found {
		t.Fatalf("Find after drop = (found %v, err %v), want (false, nil)", found, err)
	}

	// The new owner is recorded, so the next start does not drop again.
	if dropped, err := s.bindOwner(ctx, 2000); err != nil || dropped {
		t.Fatalf("rebind after drop = (dropped %v, err %v), want (false, nil)", dropped, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestBindOwnerWithoutDatabase(t *testing.T) {
	var s *stateStores
	dropped, err := s.bindOwner(context.Background(), 1)
	if err != nil || dropped {
		t.Fatalf("bindOwner on nil stores = (%v, %v), want (false, nil)", dropped, err)
	}
}

// FindPhone reports the resolved key even when the value is missing,
// matching peers.InmemoryStorage.
func TestFindPhoneReturnsKeyWithoutValue(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := openStateStores(path, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	key := peers.Key{Prefix: "users_", ID: 7}
	if err := s.peerStorage().SavePhone(ctx, "15550000000", key); err != nil {
		t.Fatalf("SavePhone: %v", err)
	}

	gotKey, _, found, err := s.peerStorage().FindPhone(ctx, "15550000000")
	if err != nil {
		t.Fatalf("FindPhone: %v", err)
	}
	if found {
		t.Fatal("FindPhone found a value that was never saved")
	}
	if gotKey != key {
		t.Fatalf("FindPhone key = %+v, want %+v", gotKey, key)
	}
}
