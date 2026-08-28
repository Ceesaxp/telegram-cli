package telegram

import (
	"fmt"

	"github.com/gotd/td/tg"
)

// GetUser returns a user by ID.
func (c *Client) GetUser(userID int64) (*User, error) {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.peers.ResolveUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user %d: %w", userID, err)
	}
	return userFromTG(peer.Raw()), nil
}

// GetContacts returns the contact list.
func (c *Client) GetContacts() ([]*User, error) {
	ctx, cancel := opCtx()
	defer cancel()
	res, err := c.api.ContactsGetContacts(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("get contacts: %w", err)
	}

	contacts, ok := res.(*tg.ContactsContacts)
	if !ok {
		return nil, fmt.Errorf("unexpected contacts type %T", res)
	}

	users := make([]*User, 0, len(contacts.Users))
	raw := make([]tg.UserClass, 0, len(contacts.Users))
	for _, uc := range contacts.Users {
		if u, ok := uc.(*tg.User); ok {
			raw = append(raw, u)
			users = append(users, userFromTG(u))
		}
	}

	// Seed the peers manager so these users are resolvable later.
	if err := c.peers.Apply(ctx, raw, nil); err != nil {
		return nil, fmt.Errorf("apply peers: %w", err)
	}
	return users, nil
}
