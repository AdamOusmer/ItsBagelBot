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
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
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

type Loaded struct {
	AccessToken          string
	AccessTokenExpiresAt *time.Time
	RefreshToken         string
}

func (s *Store) Load(ctx context.Context) (Loaded, error) {
	r, err := bus.RequestJSONTimeout[usersrpc.TokensReply](ctx, s.nc, s.prefix+".get", usersrpc.TokensRequest{UserID: s.userID}, rpcTimeout)
	if err != nil {
		return Loaded{}, fmt.Errorf("tokens get rpc: %w", err)
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
	req := usersrpc.TokensRequest{
		UserID:               s.userID,
		AccessToken:          accessToken,
		RefreshToken:         refreshToken,
		AccessTokenExpiresAt: expiresAt,
	}
	if _, err := bus.RequestJSONTimeout[usersrpc.TokensReply](ctx, s.nc, s.prefix+".save", req, rpcTimeout); err != nil {
		return fmt.Errorf("tokens save rpc: %w", err)
	}
	return nil
}
