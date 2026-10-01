// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tokenstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainrpc "ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const rpcTimeout = 2 * time.Second

type Store struct {
	nc     *nats.Conn
	prefix string
	userID string
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

type Loaded struct {
	AccessToken          string
	AccessTokenExpiresAt *time.Time
	RefreshToken         string
}

func (s *Store) Load(ctx context.Context) (Loaded, error) {
	r, err := bus.RequestJSONTimeout[reply](ctx, s.nc, s.prefix+".get", map[string]string{"user_id": s.userID}, rpcTimeout)
	if err != nil {
		return Loaded{}, fmt.Errorf("tokens get rpc: %w", err)
	}
	if r.Error != "" {
		return Loaded{}, bus.RPCReplyError{Subject: s.prefix + ".get", Message: r.Error}
	}
	return Loaded{
		AccessToken:          r.AccessToken,
		AccessTokenExpiresAt: r.AccessTokenExpiresAt,
		RefreshToken:         r.RefreshToken,
	}, nil
}

func Unavailable(err error) bool {
	if err == nil {
		return false
	}
	var refused bus.RPCReplyError
	if !errors.As(err, &refused) {
		return true
	}
	return !tokenNotFound(refused)
}

func tokenNotFound(refused bus.RPCReplyError) bool {
	if refused.Code == domainrpc.CodeNotFound {
		return true
	}
	return refused.Code == "" && legacyNotFoundMessage(refused.Message)
}

func legacyNotFoundMessage(message string) bool {
	return strings.Contains(message, "not found")
}

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
