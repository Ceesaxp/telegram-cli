package telegram

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gotd/td/telegram/peers"
	bolt "go.etcd.io/bbolt"
)

// peerStore opens a fresh cache on a temp file.
func peerStore(t *testing.T) (*boltPeerStorage, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := openStateStores(path, path)
	if err != nil {
		t.Fatalf("openStateStores: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s.peers, path
}

// writeCount is how many transactions the file has committed. bbolt keeps
// the number in its own stats, which makes "did this touch the disk" a
// question with an exact answer rather than a benchmark to squint at.
func writeCount(s *boltPeerStorage) int64 {
	stats := s.db.Stats().TxStats
	return stats.GetWrite()
}

func readCount(s *boltPeerStorage) int {
	stats := s.db.Stats()
	return stats.TxN
}

func key(id int64) peers.Key {
	return peers.Key{Prefix: "user", ID: id}
}

// TestApplyingHashesAlreadyStoredWritesNothing.
//
// The overwhelmingly common case: every dialog page, history page and
// incoming update re-applies entities the cache already has. It used to pay
// one fsync'd transaction per entity for the privilege of writing the same
// eight bytes back.
func TestApplyingHashesAlreadyStoredWritesNothing(t *testing.T) {
	s, _ := peerStore(t)
	ctx := context.Background()

	for i := range int64(200) {
		if err := s.Save(ctx, key(i), peers.Value{AccessHash: i * 7}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	before := writeCount(s)
	for i := range int64(200) {
		if err := s.Save(ctx, key(i), peers.Value{AccessHash: i * 7}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := writeCount(s) - before; got != 0 {
		t.Errorf("re-applying 200 unchanged hashes performed %d writes, want 0", got)
	}
}

// TestApplyingPhoneMappingsAlreadyStoredWritesNothing. The same rule for the
// other bucket: GetContacts re-applies the whole contact list, and a phone
// number does not change when it is looked up twice.
func TestApplyingPhoneMappingsAlreadyStoredWritesNothing(t *testing.T) {
	s, _ := peerStore(t)
	ctx := context.Background()

	phone := func(i int64) string { return fmt.Sprintf("+1555000%04d", i) }

	for i := range int64(100) {
		if err := s.SavePhone(ctx, phone(i), key(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	before := writeCount(s)
	for i := range int64(100) {
		if err := s.SavePhone(ctx, phone(i), key(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := writeCount(s) - before; got != 0 {
		t.Errorf("re-applying 100 unchanged phone mappings performed %d writes, want 0", got)
	}

	// And a phone that moves to another account still lands.
	if err := s.SavePhone(ctx, phone(0), key(999)); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	k, _, _, err := s.FindPhone(ctx, phone(0))
	if err != nil {
		t.Fatal(err)
	}
	if k.ID != 999 {
		t.Errorf("a reassigned phone resolves to %d, want 999", k.ID)
	}
}

// TestApplyingNewHashesDoesNotCostOneTransactionEach.
//
// A hundred-dialog page used to put a hundred fsyncs between the fetch and
// the chat list painting. What is measured is bbolt's write count, which
// counts PAGE writes rather than commits — a single commit is several — so
// the claim is tested as the proportion it actually is: two hundred entities
// must cost about what one costs, not two hundred times it.
func TestApplyingNewHashesDoesNotCostOneTransactionEach(t *testing.T) {
	saveN := func(t *testing.T, n int64) int64 {
		t.Helper()
		s, _ := peerStore(t)
		ctx := context.Background()

		before := writeCount(s)
		for i := range n {
			if err := s.Save(ctx, key(i), peers.Value{AccessHash: i + 1}); err != nil {
				t.Fatal(err)
			}
		}
		// Nothing has reached the disk yet: the burst is still arriving.
		if got := writeCount(s) - before; got != 0 {
			t.Errorf("%d saves wrote %d times before any flush, want 0", n, got)
		}
		if err := s.Flush(); err != nil {
			t.Fatal(err)
		}
		return writeCount(s) - before
	}

	one := saveN(t, 1)
	if one == 0 {
		t.Fatal("a flush of one new hash wrote nothing at all")
	}
	many := saveN(t, 200)

	// Generous on purpose: two hundred entries need more PAGES than one,
	// and that is not what this is about. One transaction per entity would
	// be two orders of magnitude away from here.
	if many > one*4 {
		t.Errorf("200 new hashes cost %d writes against %d for a single one — "+
			"that is per-entity, not per-burst", many, one)
	}
}

// TestAHashSurvivesTheProcess. The point of the file.
func TestAHashSurvivesTheProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	ctx := context.Background()

	first, err := openStateStores(path, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.peers.Save(ctx, key(7), peers.Value{AccessHash: 4242}); err != nil {
		t.Fatal(err)
	}
	if err := first.peers.SavePhone(ctx, "+15550001111", key(7)); err != nil {
		t.Fatal(err)
	}
	// Close, not Flush: closing is what a real run relies on.
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := openStateStores(path, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	value, found, err := second.peers.Find(ctx, key(7))
	if err != nil || !found {
		t.Fatalf("Find after reopen: found=%v err=%v", found, err)
	}
	if value.AccessHash != 4242 {
		t.Errorf("access hash = %d, want 4242", value.AccessHash)
	}

	k, v, found, err := second.peers.FindPhone(ctx, "+15550001111")
	if err != nil || !found {
		t.Fatalf("FindPhone after reopen: found=%v err=%v", found, err)
	}
	if k.ID != 7 || v.AccessHash != 4242 {
		t.Errorf("FindPhone = %+v %+v, want user 7 with hash 4242", k, v)
	}
}

// TestReadsDoNotTouchTheDisk. gotd consults Find on the update path, so a
// read that opened a transaction would put the disk between an incoming
// message and the screen.
func TestReadsDoNotTouchTheDisk(t *testing.T) {
	s, _ := peerStore(t)
	ctx := context.Background()

	if err := s.Save(ctx, key(1), peers.Value{AccessHash: 99}); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	before := readCount(s)
	for range 500 {
		if _, _, err := s.Find(ctx, key(1)); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := s.FindPhone(ctx, "+15550001111"); err != nil {
			t.Fatal(err)
		}
	}
	if got := readCount(s) - before; got != 0 {
		t.Errorf("1000 lookups opened %d read transactions, want 0", got)
	}
}

// TestAChangedHashIsNotLostToTheCache. The early return is on the VALUE, not
// on the key: an access hash that Telegram rotated must reach the disk.
func TestAChangedHashIsNotLostToTheCache(t *testing.T) {
	s, _ := peerStore(t)
	ctx := context.Background()

	for _, hash := range []int64{1, 2} {
		if err := s.Save(ctx, key(5), peers.Value{AccessHash: hash}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	got, found, err := s.Find(ctx, key(5))
	if err != nil || !found {
		t.Fatalf("Find: found=%v err=%v", found, err)
	}
	if got.AccessHash != 2 {
		t.Errorf("access hash = %d, want the newer 2", got.AccessHash)
	}
}

// TestTheTimerFlushesWithoutBeingAsked. Nothing calls Flush in the running
// client until shutdown; the burst has to land on its own.
func TestTheTimerFlushesWithoutBeingAsked(t *testing.T) {
	s, _ := peerStore(t)
	ctx := context.Background()

	before := writeCount(s)
	if err := s.Save(ctx, key(11), peers.Value{AccessHash: 11}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for writeCount(s) == before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if writeCount(s) == before {
		t.Fatal("the pending write never reached the disk on its own")
	}
}

// TestABurstArmsOneTimer, not one per entity.
//
// White-box on purpose: the difference is invisible from the outside,
// because a stray timer finds the pending set empty and returns without
// doing anything. What it costs is two hundred timers and two hundred
// goroutine wake-ups per dialog page, which is the same shape of waste this
// whole change is about — just in the runtime rather than on the disk.
func TestABurstArmsOneTimer(t *testing.T) {
	s, _ := peerStore(t)
	ctx := context.Background()

	if err := s.Save(ctx, key(1), peers.Value{AccessHash: 1}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	armed := s.flush
	s.mu.Unlock()
	if armed == nil {
		t.Fatal("the first pending write armed no timer at all")
	}

	for i := range int64(200) {
		if err := s.Save(ctx, key(i+2), peers.Value{AccessHash: i + 2}); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Lock()
	still := s.flush
	s.mu.Unlock()
	if still != armed {
		t.Error("a second save armed another timer; a dialog page would arm two hundred")
	}
}

// TestClosingIsFinal.
//
// One timer per burst, not one per save. The guard that arms it only when
// none is pending is what makes Close able to stop it: without that, every
// Save overwrites the handle, Close stops only the last one, and the strays
// fire afterwards against a database that is no longer open. Nothing breaks,
// but the log fills with failures from writes nobody is waiting for — which
// is exactly the kind of noise that trains people to ignore the log.
func TestClosingIsFinal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	stores, err := openStateStores(path, path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for i := range int64(20) {
		if err := stores.peers.Save(ctx, key(i), peers.Value{AccessHash: i + 1}); err != nil {
			t.Fatal(err)
		}
	}

	var complaints bytes.Buffer
	log.SetOutput(&complaints)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	if err := stores.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Well past the flush delay: anything still armed has fired by now.
	time.Sleep(3 * peerFlushDelay)

	if complaints.Len() != 0 {
		t.Errorf("a flush ran after Close and failed:\n%s", complaints.String())
	}
}

// TestRebindingToAnotherAccountEmptiesTheCacheItself.
//
// bindOwner drops the buckets when a session file has been re-authorised as
// somebody else, because access hashes are per-account and serving stale
// ones yields PEER_ID_INVALID. Now that the MAP is authoritative, dropping
// only the buckets would keep serving them out of memory — the same bug,
// one layer up.
func TestRebindingToAnotherAccountEmptiesTheCacheItself(t *testing.T) {
	s, _ := peerStore(t)
	ctx := context.Background()

	if _, err := s.bindOwner(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, key(3), peers.Value{AccessHash: 777}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePhone(ctx, "+15550002222", key(3)); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	dropped, err := s.bindOwner(ctx, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if !dropped {
		t.Fatal("rebinding to a different account did not report a drop")
	}

	if _, found, _ := s.Find(ctx, key(3)); found {
		t.Error("the previous account's access hash is still being served from memory")
	}
	// The KEY, not just found: FindPhone reports found from the VALUE, so
	// clearing only the hashes would make it false while the phone mapping
	// itself lived on.
	k, _, found, _ := s.FindPhone(ctx, "+15550002222")
	if found {
		t.Error("the previous account's phone mapping is still being served from memory")
	}
	if k.ID != 0 {
		t.Errorf("the phone still resolves to user %d from the previous account", k.ID)
	}
}

// TestAFailedFlushKeepsTheWritesForTheNextOne. They are a cache, but losing
// them silently on a transient write error would mean a peer re-learned for
// the rest of the session with nothing said.
func TestAFailedFlushKeepsTheWritesForTheNextOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	stores, err := openStateStores(path, path)
	if err != nil {
		t.Fatal(err)
	}
	s := stores.peers
	ctx := context.Background()

	if err := s.Save(ctx, key(42), peers.Value{AccessHash: 4242}); err != nil {
		t.Fatal(err)
	}
	// Close the file underneath it: every write now fails.
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err == nil {
		t.Fatal("flushing to a closed database reported success")
	}

	s.mu.Lock()
	pending := len(s.pending)
	s.mu.Unlock()
	if pending == 0 {
		t.Error("a failed flush discarded the writes instead of keeping them")
	}
}

// TestClosingFlushesWhatIsPending — and reports a real error rather than
// swallowing it.
func TestClosingFlushesWhatIsPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := openStateStores(path, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.peers.Save(context.Background(), key(1), peers.Value{AccessHash: 5}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read the raw file, so this is about bytes rather than about the map.
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	found := false
	if err := db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(_ []byte, root *bolt.Bucket) error {
			b := root.Bucket(peerBucket)
			if b == nil {
				return nil
			}
			return b.ForEach(func(_, v []byte) error {
				if hash, ok := decodeInt64(v); ok && hash == 5 {
					found = true
				}
				return nil
			})
		})
	}); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("Close did not write the pending hash")
	}
}

// BenchmarkPeerSaves documents the before and the after, in one run.
//
// The unit is one Apply of a two-hundred-entity response: a dialog page, a
// contact list, a supergroup's members. The issue measured 200 sequential
// saves at 95ms on tmpfs and called that a lower bound, since a real fsync
// is slower than tmpfs's.
//
//	per-save-transaction   what it used to do: one commit per entity
//	new-batched            the same entities, one commit
//	unchanged              re-applying what the cache already has
//
// The last is the one that runs most: every dialog page, history page and
// incoming update re-applies entities already known.
func BenchmarkPeerSaves(b *testing.B) {
	const entities = 200

	open := func(b *testing.B) *stateStores {
		b.Helper()
		s, err := openStateStores(filepath.Join(b.TempDir(), "state.db"), "bench")
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { s.Close() })
		return s
	}

	b.Run("per-save-transaction", func(b *testing.B) {
		s := open(b)
		ctx := context.Background()
		b.ResetTimer()
		for n := range int64(b.N) {
			for i := range int64(entities) {
				if err := s.peers.Save(ctx, key(i), peers.Value{AccessHash: i + n*entities}); err != nil {
					b.Fatal(err)
				}
				// One commit per entity, which is what this replaced.
				if err := s.peers.Flush(); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("new-batched", func(b *testing.B) {
		s := open(b)
		ctx := context.Background()
		b.ResetTimer()
		for n := range int64(b.N) {
			for i := range int64(entities) {
				if err := s.peers.Save(ctx, key(i), peers.Value{AccessHash: i + n*entities}); err != nil {
					b.Fatal(err)
				}
			}
			if err := s.peers.Flush(); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("unchanged", func(b *testing.B) {
		s := open(b)
		ctx := context.Background()
		// Warm, so this measures the steady state rather than the one
		// iteration that had to write.
		for i := range int64(entities) {
			if err := s.peers.Save(ctx, key(i), peers.Value{AccessHash: i}); err != nil {
				b.Fatal(err)
			}
		}
		if err := s.peers.Flush(); err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for range b.N {
			for i := range int64(entities) {
				if err := s.peers.Save(ctx, key(i), peers.Value{AccessHash: i}); err != nil {
					b.Fatal(err)
				}
			}
			if err := s.peers.Flush(); err != nil {
				b.Fatal(err)
			}
		}
	})
}
