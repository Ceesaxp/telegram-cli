package telegram

import (
	"context"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/updates"
)

// peerUsersPrefix is the key prefix gotd's peers.Manager files user access
// hashes under. It is not exported by gotd, so it is repeated here and
// pinned by TestUserHasherSeesWhatThePeerManagerStored: if gotd ever
// renames it, that test fails rather than every private message quietly
// costing a getDifference.
const peerUsersPrefix = "users_"

// peerUserHasher is the updates.UserAccessHasher the update manager
// consults before accepting a private short update, answered out of the
// persisted peer cache instead of a map that starts empty every session.
//
// gotd gates updateShortMessage on "is this sender's access hash known":
// unknown means a full updates.getDifference, and the message that asked
// only comes back if the difference returns it. With the default in-memory
// hasher every sender is unknown until a full user entity has passed
// through the manager THIS session — dialogs, history and the peer manager
// do not count — so a quiet account paid a difference fetch per contact
// per run, and a "difference too long" reply in the middle of one dropped
// the message for good. The peer manager already keeps exactly these
// hashes, on disk; this hands them over.
//
// The user ID gotd passes first is the account asking. The peer namespace
// is already per account (see peerNamespace), so it is not part of the key.
type peerUserHasher struct {
	storage peers.Storage
}

var _ updates.UserAccessHasher = peerUserHasher{}

func userPeerKey(id int64) peers.Key {
	return peers.Key{Prefix: peerUsersPrefix, ID: id}
}

func (h peerUserHasher) GetUserAccessHash(ctx context.Context, _, targetUserID int64) (int64, bool, error) {
	v, found, err := h.storage.Find(ctx, userPeerKey(targetUserID))
	if err != nil || !found {
		return 0, false, err
	}
	return v.AccessHash, true, nil
}

func (h peerUserHasher) SetUserAccessHash(ctx context.Context, _, targetUserID, accessHash int64) error {
	return h.storage.Save(ctx, userPeerKey(targetUserID), peers.Value{AccessHash: accessHash})
}

// userHasher returns the user access hasher backed by the peer cache, or
// nil for in-memory. A true nil interface, for the same reason as
// peerStorage: a typed nil would make gotd call into nothing.
func (s *stateStores) userHasher() updates.UserAccessHasher {
	if s == nil || s.peers == nil {
		return nil
	}
	return peerUserHasher{storage: s.peers}
}
