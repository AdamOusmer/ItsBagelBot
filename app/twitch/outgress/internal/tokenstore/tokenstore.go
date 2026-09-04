// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package tokenstore reads and writes the bot account's OAuth token through
// the users service token RPC, so the admin panel and every consumer agree
// on a single stored token instead of each carrying its own copy.
package tokenstore

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const rpcTimeout = 2 * time.Second

type Store struct {
	nc     *nats.Conn
	prefix string // e.g. "bagel.rpc.internal.tokens"
	userID string // the bot account's Twitch user id
}

func New(nc *nats.Conn, prefix, userID string) *Store {
	return &Store{nc: nc, prefix: prefix, userID: userID}
}

type reply struct {
	AccessToken          string     `json:"access_token"`
	RefreshToken         string     `json:"refresh_token"`
	AccessTokenExpiresAt *time.Time `json:"access_token_expires_at,omitempty"`
	Error                string     `json:"error"`
}

// Loaded is what Load hands back: the stored refresh token plus, when the
// users service has one, the stored access token and its absolute expiry.
//
// AccessTokenExpiresAt is nil whenever the users service doesn't know the
// expiry -- either the row predates that column, it was written by a path
// that doesn't track it (admin token-set, dashboard OAuth callback), or this
// reply came from a users-service build that predates the field entirely
// (an old binary's JSON just omits it, which decodes to nil here with no
// version check needed). Callers MUST treat nil as "not usable", never as
// "valid forever" -- see twitch.NewStoredUserTokenSource, the one reader of
// this type.
type Loaded struct {
	AccessToken          string
	AccessTokenExpiresAt *time.Time
	RefreshToken         string
}

// Load returns the bot account's stored refresh token, and its stored access
// token plus expiry when the users service has one (see Loaded's doc). A
// missing token row surfaces as an error; callers decide whether that is
// fatal.
func (s *Store) Load(ctx context.Context) (Loaded, error) {
	r, err := bus.RequestJSONTimeout[reply](ctx, s.nc, s.prefix+".get", map[string]string{"user_id": s.userID}, rpcTimeout)
	if err != nil {
		return Loaded{}, fmt.Errorf("tokens get rpc: %w", err)
	}
	if r.Error != "" {
		return Loaded{}, fmt.Errorf("tokens get: %s", r.Error)
	}
	return Loaded{
		AccessToken:          r.AccessToken,
		AccessTokenExpiresAt: r.AccessTokenExpiresAt,
		RefreshToken:         r.RefreshToken,
	}, nil
}

// Save persists the freshly rotated token pair, plus the access token's
// absolute expiry when the caller knows it (see NewStoredUserTokenSource's
// refresh closure, which does: Twitch's expires_in becomes this value).
// expiresAt may be nil; the users service then stores no expiry, and any
// future Load must fall back to minting rather than adopt a token whose
// lifetime nobody tracked.
func (s *Store) Save(ctx context.Context, accessToken, refreshToken string, expiresAt *time.Time) error {
	req := map[string]any{
		"user_id":       s.userID,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	}
	if expiresAt != nil {
		req["access_token_expires_at"] = *expiresAt
	}

	r, err := bus.RequestJSONTimeout[reply](ctx, s.nc, s.prefix+".save", req, rpcTimeout)
	if err != nil {
		return fmt.Errorf("tokens save rpc: %w", err)
	}
	if r.Error != "" {
		return fmt.Errorf("tokens save: %s", r.Error)
	}
	return nil
}
