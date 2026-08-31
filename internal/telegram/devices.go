package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/td/tg"
)

// authorizationLister is the one method [DeviceCount] needs.
//
// A named interface rather than the concrete *tg.Client, for the reason
// qr.go has one: c.api is a generated struct with no seam in it, so a
// function that reaches for it directly can only be exercised against a live
// Telegram connection. Declaring the method this way costs a line and makes
// the counting testable — and it doubles as the compile-time check that the
// response shape this reads is the one gotd returns, so a dependency bump
// that reshapes it fails here rather than silently reporting no devices.
type authorizationLister interface {
	AccountGetAuthorizations(ctx context.Context) (*tg.AccountAuthorizations, error)
}

// DeviceCount is how many sessions are currently authorised on this account,
// this one included.
//
// It is the number Telegram's own clients show under "Devices", and it is
// worth a cell in the top bar for the reason Telegram gives it a screen: a
// count higher than the user expects is how an unauthorised login is
// noticed. A constant would not be worth the space; this varies, and when it
// varies it means something.
//
// One RPC, no paging: account.getAuthorizations returns the whole list. The
// caller fetches it when the connection becomes ready and holds the answer —
// sessions are created and revoked by hand, on the scale of days, so polling
// for it would spend requests to watch a number that does not move.
//
// The count is 0 on every error, which is not merely the zero value: 0 is
// also what "not asked yet" looks like to the only consumer, and both mean
// the top bar draws no device cell. A caller that wants to tell them apart
// has the error; a caller that does not can ignore it safely.
func (c *Client) DeviceCount() (int, error) {
	ctx, cancel := opCtx()
	defer cancel()
	return deviceCount(ctx, c.api)
}

func deviceCount(ctx context.Context, api authorizationLister) (int, error) {
	res, err := api.AccountGetAuthorizations(ctx)
	if err != nil {
		return 0, fmt.Errorf("get authorizations: %w", err)
	}
	return len(res.Authorizations), nil
}
