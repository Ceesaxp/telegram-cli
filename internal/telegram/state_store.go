package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path/filepath"
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
	return s.db.Close()
}

// boltPeerStorage is a peers.Storage backed by bbolt, so access hashes
// survive across runs. contrib has no peers.Storage implementation — its
// storage.PeerStorage is a different, resolver-oriented abstraction — so
// this small one lives here and shares the state database.
//
// All data lives under the ns bucket, which isolates one account from
// another (see peerNamespace).
type boltPeerStorage struct {
	db *bolt.DB
	ns []byte
}

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
	return &boltPeerStorage{db: db, ns: ns}, nil
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
func (s *boltPeerStorage) Save(_ context.Context, key peers.Key, value peers.Value) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := s.write(tx, peerBucket)
		if err != nil {
			return err
		}
		return b.Put(encodePeerKey(key), encodeInt64(value.AccessHash))
	})
}

// Find implements peers.Storage.
func (s *boltPeerStorage) Find(_ context.Context, key peers.Key) (value peers.Value, found bool, _ error) {
	err := s.db.View(func(tx *bolt.Tx) error {
		b := s.read(tx, peerBucket)
		if b == nil {
			return nil
		}
		raw := b.Get(encodePeerKey(key))
		if raw == nil {
			return nil
		}
		hash, ok := decodeInt64(raw)
		if !ok {
			return nil
		}
		value, found = peers.Value{AccessHash: hash}, true
		return nil
	})
	if err != nil {
		return peers.Value{}, false, err
	}
	return value, found, nil
}

// SavePhone implements peers.Storage.
func (s *boltPeerStorage) SavePhone(_ context.Context, phone string, key peers.Key) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := s.write(tx, peerPhoneBucket)
		if err != nil {
			return err
		}
		return b.Put([]byte(phone), encodePeerKey(key))
	})
}

// FindPhone implements peers.Storage.
func (s *boltPeerStorage) FindPhone(ctx context.Context, phone string) (key peers.Key, value peers.Value, found bool, err error) {
	err = s.db.View(func(tx *bolt.Tx) error {
		phones := s.read(tx, peerPhoneBucket)
		if phones == nil {
			return nil
		}
		raw := phones.Get([]byte(phone))
		if raw == nil {
			return nil
		}
		k, ok := decodePeerKey(raw)
		if !ok {
			return nil
		}
		// Mirror peers.InmemoryStorage: the resolved key is reported even
		// when the value lookup misses, so found refers to the value.
		key = k

		values := s.read(tx, peerBucket)
		if values == nil {
			return nil
		}
		hash, ok := decodeInt64(values.Get(raw))
		if !ok {
			return nil
		}

		value, found = peers.Value{AccessHash: hash}, true
		return nil
	})
	if err != nil {
		return peers.Key{}, peers.Value{}, false, err
	}
	return key, value, found, nil
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
