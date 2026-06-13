// Package tokenstore reads and writes the bot account's OAuth token through
// the users service token RPC, so the admin panel and every consumer agree
// on a single stored token instead of each carrying its own copy.
package tokenstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
)

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
	req, _ := json.Marshal(map[string]string{"user_id": s.userID})

	msg, err := s.nc.RequestWithContext(ctx, s.prefix+".get", req)
	if err != nil {
		return "", fmt.Errorf("tokens get rpc: %w", err)
	}

	var r reply
	if err := json.Unmarshal(msg.Data, &r); err != nil {
		return "", fmt.Errorf("tokens get unmarshal: %w", err)
	}
	if r.Error != "" {
		return "", fmt.Errorf("tokens get: %s", r.Error)
	}
	return r.RefreshToken, nil
}

// Save persists the freshly rotated token pair.
func (s *Store) Save(ctx context.Context, accessToken, refreshToken string) error {
	req, _ := json.Marshal(map[string]string{
		"user_id":       s.userID,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})

	msg, err := s.nc.RequestWithContext(ctx, s.prefix+".save", req)
	if err != nil {
		return fmt.Errorf("tokens save rpc: %w", err)
	}

	var r reply
	if err := json.Unmarshal(msg.Data, &r); err != nil {
		return fmt.Errorf("tokens save unmarshal: %w", err)
	}
	if r.Error != "" {
		return fmt.Errorf("tokens save: %s", r.Error)
	}
	return nil
}
