package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	contribbolt "github.com/gotd/contrib/bbolt"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/updates"
	bolt "go.etcd.io/bbolt"

	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// Sub-buckets of a peer namespace (see peerNamespace). contrib's
// updates.StateStorage keys its own top-level buckets by the 16-byte
// encoded user ID, and namespace buckets are named "peerns_"+32 hex
// chars, so the two cannot collide and everything fits in one file.
var (
	peerBucket      = []byte("peers")
	peerPhoneBucket = []byte("peer_phones")
	peerMetaBucket  = []byte("peer_meta")
)

var (
	contactsHashKey = []byte("contacts_hash")
	ownerIDKey      = []byte("owner_id")
)

// peerNamespace derives the top-level bucket that isolates one account's
// peer cache from every other account's.
//
// Access hashes are per-requesting-account: the hash account A holds for a
// mutual contact is not valid for account B. All binaries resolve the same
// state.db, and telegram-api/telegram-mcp deliberately support logging in
// as a different account via their own session file, so a single flat peer
// bucket would let B overwrite A's hashes and leave A issuing InputPeers
// that fail with PEER_ID_INVALID until state.db was deleted.
//
// The namespace is derived from the session file rather than the user ID
// because peers.Storage is constructed before authorization and its
// interface carries no user ID, whereas accounts map 1:1 onto session
// files and the path is known at construction time. Account identity is
// additionally confirmed post-auth by bindOwner.
func peerNamespace(identity string) []byte {
	if abs, err := filepath.Abs(identity); err == nil {
		identity = abs
	}
	sum := sha256.Sum256([]byte(filepath.Clean(identity)))
	return []byte("peerns_" + hex.EncodeToString(sum[:16]))
}

// stateDBOpenTimeout bounds how long we wait for the bbolt file lock.
// The lock is exclusive and other processes sharing the data directory
// (telegram-api, telegram-mcp) may hold it, so a held lock must fail
// fast and let the caller fall back to in-memory state.
const stateDBOpenTimeout = time.Second

// stateDBPath returns the location of the persistent state database, or
// "" when no sensible location can be derived from the config.
func stateDBPath(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if p := cfg.Storage.StateFile; p != "" {
		return p
	}
	if cfg.Storage.SessionFile == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cfg.Storage.SessionFile), "state.db")
}

// stateDBTarget decides whether this client should open the persistent
// state database and, if so, where.
//
// No-updates clients never open it: they have no update sequence to
// persist, and they routinely run concurrently with the TUI over the same
// data directory — taking the exclusive bbolt lock would starve the one
// process that actually needs it.
func stateDBTarget(cfg *config.Config, noUpdates bool) (path string, open bool) {
	if noUpdates {
		return "", false
	}
	p := stateDBPath(cfg)
	return p, p != ""
}

// stateStores bundles the persistent stores backed by a single bbolt file:
// the update-sequence state (pts/qts/seq/date, so updates missed while
// offline are recovered via updates.getDifference) and the peer access-hash
// cache (so peers are not re-learned from scratch every session).
//
// A nil *stateStores is valid and yields nil storages, which makes gotd
// fall back to its in-memory defaults.
type stateStores struct {
	db    *bolt.DB
	state updates.StateStorage
	peers *boltPeerStorage
}

// stateIdentity is the account identity that selects the peer namespace:
// the session file, which maps 1:1 onto an account. Falls back to the
// database path when no session file is configured.
func stateIdentity(cfg *config.Config, dbPath string) string {
	if cfg != nil && cfg.Storage.SessionFile != "" {
		return cfg.Storage.SessionFile
	}
	return dbPath
}

// openStateStores opens (creating if needed) the state database at path.
// identity selects the peer namespace within it — the session file of the
// account this client will authorize as; see peerNamespace.
func openStateStores(path, identity string) (*stateStores, error) {
	return openStateStoresTimeout(path, identity, stateDBOpenTimeout)
}

func openStateStoresTimeout(path, identity string, timeout time.Duration) (*stateStores, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: timeout})
	if err != nil {
		return nil, fmt.Errorf("open state db %s: %w", path, err)
	}

	ps, err := newBoltPeerStorage(db, peerNamespace(identity))
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init peer storage %s: %w", path, err)
	}

	return &stateStores{
		db:    db,
		state: contribbolt.NewStateStorage(db),
		peers: ps,
	}, nil
}

// stateStorage returns the updates.StateStorage, or nil for in-memory.
func (s *stateStores) stateStorage() updates.StateStorage {
	if s == nil {
		return nil
	}
	return s.state
}

// peerStorage returns the peers.Storage, or nil for in-memory. The
// explicit nil checks matter: returning a typed nil here would defeat
// peers.Options.setDefaults and panic on first use.
func (s *stateStores) peerStorage() peers.Storage {
	if s == nil || s.peers == nil {
		return nil
	}
	return s.peers
}

// bindOwner confirms the peer namespace belongs to userID, dropping it if
// it was left behind by a different account on the same session file.
// Reports whether such a drop happened. No-op without a database.
func (s *stateStores) bindOwner(ctx context.Context, userID int64) (bool, error) {
	if s == nil || s.peers == nil {
		return false, nil
	}
	return s.peers.bindOwner(ctx, userID)
}

// Close releases the database and its file lock. Nil-safe.
func (s *stateStores) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	// The peer cache holds writes back to coalesce them, so it has to be
	// caught up before the file it writes to goes away.
	var flushErr error
	if s.peers != nil {
		flushErr = s.peers.Close()
	}
	if err := s.db.Close(); err != nil {
		return err
	}
	return flushErr
}

// boltPeerStorage is a peers.Storage backed by bbolt, so access hashes
// survive across runs. contrib has no peers.Storage implementation — its
// storage.PeerStorage is a different, resolver-oriented abstraction — so
// this small one lives here and shares the state database.
//
// All data lives under the ns bucket, which isolates one account from
// another (see peerNamespace).
// boltPeerStorage is the peer access-hash cache: a map that a bbolt file
// outlives the process for.
//
// The map is authoritative. Everything is loaded at open, every write goes
// through it, and reads never touch the disk — so Find and FindPhone cost
// nothing, which matters because gotd consults them on the update path.
//
// Writes are COALESCED rather than immediate. gotd's peers.Manager.Apply
// calls Save once per user and once per chat in every response it is handed,
// synchronously, on the goroutine doing the fetching: a hundred-dialog page
// used to pay a hundred fsync'd transactions before the chat list could
// paint. Now an unchanged hash — overwhelmingly the common case — costs
// nothing at all, and changed ones accumulate until the burst stops and go
// out together.
//
// Losing the tail of that on a crash is acceptable and always was: these are
// hashes the server will hand out again, which is why bindOwner can drop the
// whole namespace without ceremony and why the in-memory mode gotd falls
// back to keeps none of them.
type boltPeerStorage struct {
	db *bolt.DB
	ns []byte

	mu     sync.Mutex
	hashes map[string]int64  // encoded peer key -> access hash
	phones map[string]string // phone -> encoded peer key

	// pending are the entries the map has and the file does not.
	pending       map[string]int64
	pendingPhones map[string]string

	flush  *time.Timer
	closed bool

	// flushMu serialises flushes and is what Close waits on.
	//
	// Flush detaches the pending set under mu and then commits WITHOUT it,
	// because a bbolt commit fsyncs and holding mu across one would stall
	// every Find on the update path. That leaves a window where a batch is
	// in flight and belongs to nobody: Close could see an empty pending
	// set, report success, and let the database be closed under a write
	// that had already started. Holding this for the whole of a flush —
	// including the commit — is what makes a successful Close mean every
	// detached batch has landed.
	flushMu sync.Mutex

	// sealed is set by Close under flushMu. A flush that finds it does
	// nothing: the database is about to be closed, or already is.
	sealed bool

	// beforeCommit is a test seam, called after the pending set has been
	// detached and before it is written. It exists because the window this
	// mutex closes is otherwise only reachable by luck.
	beforeCommit func()
}

// peerFlushDelay is how long a changed hash waits for company.
//
// Long enough that one Apply of a dialog page, a member list or a contact
// list becomes one transaction; short enough that a crash loses nothing a
// reader would notice. Both ends of that are forgiving — the cost of being
// wrong is a cache miss.
const peerFlushDelay = 250 * time.Millisecond

var _ peers.Storage = (*boltPeerStorage)(nil)

var peerSubBuckets = [][]byte{peerBucket, peerPhoneBucket, peerMetaBucket}

func newBoltPeerStorage(db *bolt.DB, ns []byte) (*boltPeerStorage, error) {
	// Create the buckets up front so reads never have to deal with a
	// missing bucket, and so an unwritable database fails here, at open
	// time, where the caller can still fall back to in-memory.
	err := db.Update(func(tx *bolt.Tx) error {
		root, err := tx.CreateBucketIfNotExists(ns)
		if err != nil {
			return err
		}
		for _, name := range peerSubBuckets {
			if _, err := root.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	store := &boltPeerStorage{
		db:            db,
		ns:            ns,
		hashes:        map[string]int64{},
		phones:        map[string]string{},
		pending:       map[string]int64{},
		pendingPhones: map[string]string{},
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

// load reads the namespace into memory, once, so the read path never has to
// go back for it. A peer entry is a key and eight bytes; an account with
// thousands of them is a map measured in tens of kilobytes.
func (s *boltPeerStorage) load() error {
	return s.db.View(func(tx *bolt.Tx) error {
		if b := s.read(tx, peerBucket); b != nil {
			if err := b.ForEach(func(k, v []byte) error {
				if hash, ok := decodeInt64(v); ok {
					s.hashes[string(k)] = hash
				}
				return nil
			}); err != nil {
				return err
			}
		}
		if b := s.read(tx, peerPhoneBucket); b != nil {
			return b.ForEach(func(k, v []byte) error {
				s.phones[string(k)] = string(v)
				return nil
			})
		}
		return nil
	})
}

// scheduleLocked arms the flush timer. Called with mu held.
func (s *boltPeerStorage) scheduleLocked() {
	if s.closed || s.flush != nil {
		return
	}
	s.flush = time.AfterFunc(peerFlushDelay, func() {
		s.mu.Lock()
		s.flush = nil
		s.mu.Unlock()
		if err := s.Flush(); err != nil {
			log.Printf("state db: peer cache flush: %s", err)
		}
	})
}

// Flush writes everything the map has and the file does not, in one
// transaction.
//
// The pending set is taken under the lock and written outside it: a bbolt
// commit fsyncs, and holding the mutex across it would stall every Find on
// the update path — which is the cost this whole change exists to remove.
func (s *boltPeerStorage) Flush() error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	if s.sealed {
		return nil
	}
	return s.writePending()
}

// writePending is the body of a flush. Callers hold flushMu.
func (s *boltPeerStorage) writePending() error {
	s.mu.Lock()
	hashes, phones := s.pending, s.pendingPhones
	if len(hashes) == 0 && len(phones) == 0 {
		s.mu.Unlock()
		return nil
	}
	s.pending, s.pendingPhones = map[string]int64{}, map[string]string{}
	before := s.beforeCommit
	s.mu.Unlock()

	if before != nil {
		before()
	}

	err := s.db.Update(func(tx *bolt.Tx) error {
		if len(hashes) > 0 {
			b, err := s.write(tx, peerBucket)
			if err != nil {
				return err
			}
			for key, hash := range hashes {
				if err := b.Put([]byte(key), encodeInt64(hash)); err != nil {
					return err
				}
			}
		}
		if len(phones) > 0 {
			b, err := s.write(tx, peerPhoneBucket)
			if err != nil {
				return err
			}
			for phone, key := range phones {
				if err := b.Put([]byte(phone), []byte(key)); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		// Put them back so the next flush retries, but never over a newer
		// value: what is in pending now was written after these were taken.
		s.mu.Lock()
		for key, hash := range hashes {
			if _, newer := s.pending[key]; !newer {
				s.pending[key] = hash
			}
		}
		for phone, key := range phones {
			if _, newer := s.pendingPhones[phone]; !newer {
				s.pendingPhones[phone] = key
			}
		}
		s.scheduleLocked()
		s.mu.Unlock()
	}
	return err
}

// Close stops the timer and writes what is left. After it, the map still
// answers reads — the caller is about to close the database, and a Find
// racing that must not reach it.
func (s *boltPeerStorage) Close() error {
	s.mu.Lock()
	s.closed = true
	if s.flush != nil {
		s.flush.Stop()
		s.flush = nil
	}
	s.mu.Unlock()

	// Taking flushMu is the wait: a flush already in flight holds it across
	// its commit, so this blocks until that batch has landed rather than
	// finding an empty pending set and declaring success over the top of it.
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	err := s.writePending()
	s.sealed = true
	return err
}

// read returns a sub-bucket of this storage's namespace, or nil.
func (s *boltPeerStorage) read(tx *bolt.Tx, name []byte) *bolt.Bucket {
	root := tx.Bucket(s.ns)
	if root == nil {
		return nil
	}
	return root.Bucket(name)
}

// write returns a sub-bucket of this storage's namespace, creating the
// namespace and the sub-bucket if they are missing.
func (s *boltPeerStorage) write(tx *bolt.Tx, name []byte) (*bolt.Bucket, error) {
	root, err := tx.CreateBucketIfNotExists(s.ns)
	if err != nil {
		return nil, err
	}
	return root.CreateBucketIfNotExists(name)
}

// bindOwner records which account owns this namespace and reports whether
// the namespace had to be dropped because it belonged to someone else.
//
// The namespace is keyed by session file, and re-authorizing as a
// different account reuses the same session file — gotd overwrites the
// session in place and neither peers.Manager nor our client resets the
// peer store on such a change. This check, run once the self ID is known,
// closes that gap: a mismatch wipes the stale hashes rather than serving
// account A's hashes to account B. Dropping is always safe, since the
// cache is rebuilt on demand.
func (s *boltPeerStorage) bindOwner(_ context.Context, userID int64) (dropped bool, err error) {
	err = s.db.Update(func(tx *bolt.Tx) error {
		meta, err := s.write(tx, peerMetaBucket)
		if err != nil {
			return err
		}

		prev, ok := decodeInt64(meta.Get(ownerIDKey))
		if ok && prev == userID {
			return nil
		}

		if ok {
			// The MAP is dropped with the buckets. It is the authoritative
			// copy now, so leaving it would serve account A's hashes to
			// account B out of memory — which is the exact thing this
			// check exists to prevent, one layer up from where it used to
			// be able to happen.
			s.mu.Lock()
			s.hashes = map[string]int64{}
			s.phones = map[string]string{}
			s.pending = map[string]int64{}
			s.pendingPhones = map[string]string{}
			s.mu.Unlock()

			root := tx.Bucket(s.ns)
			for _, name := range peerSubBuckets {
				if root.Bucket(name) == nil {
					continue
				}
				if err := root.DeleteBucket(name); err != nil {
					return err
				}
			}
			for _, name := range peerSubBuckets {
				if _, err := root.CreateBucketIfNotExists(name); err != nil {
					return err
				}
			}
			dropped = true
			meta = root.Bucket(peerMetaBucket)
		}

		return meta.Put(ownerIDKey, encodeInt64(userID))
	})
	if err != nil {
		return false, err
	}
	return dropped, nil
}

// encodePeerKey renders a peers.Key as prefix bytes followed by the ID in
// big-endian, which keeps same-prefix keys adjacent and lets the ID be
// recovered from the trailing 8 bytes regardless of the prefix.
func encodePeerKey(k peers.Key) []byte {
	b := make([]byte, 0, len(k.Prefix)+8)
	b = append(b, k.Prefix...)
	return binary.BigEndian.AppendUint64(b, uint64(k.ID))
}

func decodePeerKey(b []byte) (peers.Key, bool) {
	if len(b) < 8 {
		return peers.Key{}, false
	}
	split := len(b) - 8
	return peers.Key{
		Prefix: string(b[:split]),
		ID:     int64(binary.BigEndian.Uint64(b[split:])),
	}, true
}

func encodeInt64(v int64) []byte {
	return binary.BigEndian.AppendUint64(make([]byte, 0, 8), uint64(v))
}

func decodeInt64(b []byte) (int64, bool) {
	if len(b) != 8 {
		return 0, false
	}
	return int64(binary.BigEndian.Uint64(b)), true
}

// Save implements peers.Storage.
//
// An unchanged hash is the overwhelmingly common case — every dialog page,
// history page and update re-applies entities the cache already has — and it
// costs nothing here. Only a new or changed one is recorded, and even then
// the disk waits for the rest of the burst.
func (s *boltPeerStorage) Save(_ context.Context, key peers.Key, value peers.Value) error {
	encoded := string(encodePeerKey(key))

	s.mu.Lock()
	defer s.mu.Unlock()
	if have, ok := s.hashes[encoded]; ok && have == value.AccessHash {
		return nil
	}
	s.hashes[encoded] = value.AccessHash
	s.pending[encoded] = value.AccessHash
	s.scheduleLocked()
	return nil
}

// Find implements peers.Storage. From the map: gotd consults it on the
// update path, and a read that opened a transaction there would put the
// disk between an incoming message and the screen.
func (s *boltPeerStorage) Find(_ context.Context, key peers.Key) (peers.Value, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash, ok := s.hashes[string(encodePeerKey(key))]
	if !ok {
		return peers.Value{}, false, nil
	}
	return peers.Value{AccessHash: hash}, true, nil
}

// SavePhone implements peers.Storage.
func (s *boltPeerStorage) SavePhone(_ context.Context, phone string, key peers.Key) error {
	encoded := string(encodePeerKey(key))

	s.mu.Lock()
	defer s.mu.Unlock()
	if have, ok := s.phones[phone]; ok && have == encoded {
		return nil
	}
	s.phones[phone] = encoded
	s.pendingPhones[phone] = encoded
	s.scheduleLocked()
	return nil
}

// FindPhone implements peers.Storage.
//
// Mirrors peers.InmemoryStorage: the resolved key is reported even when the
// value lookup misses, so found refers to the VALUE rather than the phone.
func (s *boltPeerStorage) FindPhone(_ context.Context, phone string) (peers.Key, peers.Value, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	encoded, ok := s.phones[phone]
	if !ok {
		return peers.Key{}, peers.Value{}, false, nil
	}
	decoded, ok := decodePeerKey([]byte(encoded))
	if !ok {
		return peers.Key{}, peers.Value{}, false, nil
	}
	hash, ok := s.hashes[encoded]
	if !ok {
		return decoded, peers.Value{}, false, nil
	}
	return decoded, peers.Value{AccessHash: hash}, true, nil
}

// GetContactsHash implements peers.Storage. A missing hash is reported as
// 0, which makes contacts.getContacts return the full list.
func (s *boltPeerStorage) GetContactsHash(_ context.Context) (int64, error) {
	var hash int64
	err := s.db.View(func(tx *bolt.Tx) error {
		b := s.read(tx, peerMetaBucket)
		if b == nil {
			return nil
		}
		if v, ok := decodeInt64(b.Get(contactsHashKey)); ok {
			hash = v
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return hash, nil
}

// SaveContactsHash implements peers.Storage.
func (s *boltPeerStorage) SaveContactsHash(_ context.Context, hash int64) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := s.write(tx, peerMetaBucket)
		if err != nil {
			return err
		}
		return b.Put(contactsHashKey, encodeInt64(hash))
	})
}
