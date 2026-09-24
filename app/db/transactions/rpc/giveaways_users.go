// Copyright (c) 2026 Adam Ousmer. All rights reserved.

package rpc

import (
	"context"
	"errors"
	"strconv"
	"time"

	giveaways "ItsBagelBot/internal/domain/rpc/giveaways"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"github.com/nats-io/nats.go"
)

const defaultUsersAuthSubject = "bagel.rpc.admin.user.auth.check"

type UsersGiveawayClient struct {
	nc                                    *nats.Conn
	pool, coverage, prepare, commit, auth string
}

func NewUsersGiveawayClient(nc *nats.Conn) *UsersGiveawayClient {
	return &UsersGiveawayClient{nc: nc, pool: giveaways.UsersPoolSubject, coverage: giveaways.UsersCoverageSubject,
		prepare: giveaways.UsersPrepareSubject, commit: giveaways.UsersCommitSubject, auth: defaultUsersAuthSubject}
}

func (c *UsersGiveawayClient) Pool(ctx context.Context, before *time.Time) (usersrpc.GiveawayPoolReply, error) {
	r, err := bus.RequestJSONTimeout[usersrpc.GiveawayPoolReply](ctx, c.nc, c.pool, usersrpc.GiveawayPoolRequest{CreatedBefore: before}, 5*time.Second)
	if err != nil {
		return r, err
	}
	if r.Error != "" {
		return r, errors.New(r.Error)
	}
	return r, nil
}

func (c *UsersGiveawayClient) Coverage(ctx context.Context, id uint64) (usersrpc.PremiumCoverage, error) {
	r, err := bus.RequestJSONTimeout[usersrpc.GiveawayCoverageReply](ctx, c.nc, c.coverage, usersrpc.GiveawayCoverageRequest{UserID: strconv.FormatUint(id, 10), Now: time.Now().UTC()}, 5*time.Second)
	if err != nil {
		return usersrpc.PremiumCoverage{}, err
	}
	if r.Error != "" {
		return usersrpc.PremiumCoverage{}, errors.New(r.Error)
	}
	if r.Coverage == nil {
		return usersrpc.PremiumCoverage{}, errors.New("users returned empty coverage")
	}
	return *r.Coverage, nil
}

func (c *UsersGiveawayClient) Prepare(ctx context.Context, req usersrpc.PreparePremiumGrantRequest) (usersrpc.PremiumGrant, error) {
	return c.grant(ctx, c.prepare, req)
}

func (c *UsersGiveawayClient) Commit(ctx context.Context, req usersrpc.CommitPremiumGrantRequest) (usersrpc.PremiumGrant, error) {
	return c.grant(ctx, giveaways.UsersCommitSubject, req)
}

func (c *UsersGiveawayClient) grant(ctx context.Context, subject string, payload any) (usersrpc.PremiumGrant, error) {
	r, err := bus.RequestJSONTimeout[struct {
		Grant *usersrpc.PremiumGrant `json:"grant,omitempty"`
		Error string                 `json:"error,omitempty"`
	}](ctx, c.nc, subject, payload, 5*time.Second)
	if err != nil {
		return usersrpc.PremiumGrant{}, err
	}
	if r.Error != "" {
		return usersrpc.PremiumGrant{}, errors.New(r.Error)
	}
	if r.Grant == nil {
		return usersrpc.PremiumGrant{}, errors.New("users returned empty grant")
	}
	return *r.Grant, nil
}

func (c *UsersGiveawayClient) Email(ctx context.Context, id uint64) (string, error) {
	subject := "bagel.rpc.internal.users.email.get"
	r, err := bus.RequestJSONTimeout[usersrpc.EmailGetReply](ctx, c.nc, subject, usersrpc.EmailGetRequest{UserID: strconv.FormatUint(id, 10)}, 5*time.Second)
	if err != nil {
		return "", err
	}
	if r.Error != "" {
		return "", errors.New(r.Error)
	}
	return r.Email, nil
}

func (c *UsersGiveawayClient) Authorize(ctx context.Context, actor string) (uint64, error) {
	id, err := bus.UserID(actor)
	if err != nil {
		return 0, err
	}
	r, err := bus.RequestJSONTimeout[usersrpc.AuthReply](ctx, c.nc, c.auth, usersrpc.AuthRequest{UserID: actor}, 5*time.Second)
	if err != nil {
		return 0, err
	}
	if r.Error != "" {
		return 0, errors.New(r.Error)
	}
	if !adminRole(r) {
		return 0, errors.New("giveaway admin role required")
	}
	return id, nil
}

func adminRole(reply usersrpc.AuthReply) bool {
	if !reply.Admin {
		return false
	}
	return reply.Role == "admin" || reply.Role == "owner"
}
