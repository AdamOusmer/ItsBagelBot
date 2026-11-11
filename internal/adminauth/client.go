// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package adminauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"github.com/nats-io/nats.go"
)

type Role string

const (
	Moderator            Role = "moderator"
	Admin                Role = "admin"
	Owner                Role = "owner"
	DefaultAuthPrefix         = "bagel.rpc.admin.user.auth"
	DefaultAuditPrefix        = "bagel.rpc.admin.user.audit"
	ActiveStaffSubject        = "bagel.rpc.internal.admin.staff.active"
	AuditDeliverySubject      = "bagel.rpc.internal.admin.audit.append"
)

type Config struct{ AuthPrefix, AuditPrefix string }
type Client struct {
	nc  *nats.Conn
	cfg Config
}

func New(nc *nats.Conn, cfg Config) *Client {
	if cfg.AuthPrefix == "" {
		cfg.AuthPrefix = DefaultAuthPrefix
	}
	if cfg.AuditPrefix == "" {
		cfg.AuditPrefix = DefaultAuditPrefix
	}
	return &Client{nc: nc, cfg: cfg}
}

func (c *Client) request(ctx context.Context, subject string, req usersrpc.AuthRequest) (usersrpc.AuthReply, error) {
	if c == nil {
		return usersrpc.AuthReply{}, errors.New("admin authorization unavailable")
	}
	if c.nc == nil {
		return usersrpc.AuthReply{}, errors.New("admin authorization unavailable")
	}
	reply, err := bus.RequestJSONTimeout[usersrpc.AuthReply](ctx, c.nc, subject, req, 5*time.Second)
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	return reply, nil
}

func (c *Client) Authorize(ctx context.Context, actor string, min Role) (usersrpc.AuthReply, error) {
	if _, err := bus.UserID(actor); err != nil {
		return usersrpc.AuthReply{}, err
	}
	if c == nil {
		return usersrpc.AuthReply{}, errors.New("admin authorization unavailable")
	}
	reply, err := c.request(ctx, strings.TrimSuffix(c.cfg.AuthPrefix, ".")+".check", usersrpc.AuthRequest{UserID: actor})
	if err != nil {
		return reply, err
	}
	if !reply.Admin {
		return reply, errors.New("forbidden: actor is not active staff")
	}
	if rank(Role(reply.Role)) < rank(min) {
		return reply, fmt.Errorf("forbidden: %s role or higher required", min)
	}
	if rank(Role(reply.Role)) == 0 {
		return reply, errors.New("forbidden: unknown staff role")
	}
	return reply, nil
}

func rank(role Role) int {
	switch role {
	case Owner:
		return 3
	case Admin:
		return 2
	case Moderator:
		return 1
	}
	return 0
}

func (c *Client) ActiveIDs(ctx context.Context) (map[uint64]struct{}, error) {
	reply, err := c.request(ctx, ActiveStaffSubject, usersrpc.AuthRequest{})
	if err != nil {
		return nil, err
	}
	ids := make(map[uint64]struct{}, len(reply.Admins))
	for _, actor := range reply.Admins {
		if actor.Active {
			ids[actor.ID] = struct{}{}
		}
	}
	return ids, nil
}

func (c *Client) AppendAudit(ctx context.Context, req usersrpc.AuthRequest) error {
	if c == nil {
		return errors.New("admin authorization unavailable")
	}
	_, err := c.request(ctx, strings.TrimSuffix(c.cfg.AuditPrefix, ".")+".append", req)
	return err
}

func (c *Client) AppendAuditDelivery(ctx context.Context, req usersrpc.AuthRequest) error {
	_, err := c.request(ctx, AuditDeliverySubject, req)
	return err
}
