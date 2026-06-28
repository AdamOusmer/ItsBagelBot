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
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
}

// Load returns the bot account's stored refresh token. A missing token row
// surfaces as an error; callers decide whether that is fatal.
func (s *Store) Load(ctx context.Context) (string, error) {
	r, err := bus.RequestJSONTimeout[reply](ctx, s.nc, s.prefix+".get", map[string]string{"user_id": s.userID}, rpcTimeout)
	if err != nil {
		return "", fmt.Errorf("tokens get rpc: %w", err)
	}
	if r.Error != "" {
		return "", fmt.Errorf("tokens get: %s", r.Error)
	}
	return r.RefreshToken, nil
}

// Save persists the freshly rotated token pair.
func (s *Store) Save(ctx context.Context, accessToken, refreshToken string) error {
	req := map[string]string{
		"user_id":       s.userID,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
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
