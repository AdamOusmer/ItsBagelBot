// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"time"

	commandsrpc "ItsBagelBot/internal/domain/rpc/commands"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const commandsRPCTimeout = 2 * time.Second

type CommandsRPC struct {
	nc     *nats.Conn
	prefix string
}

func NewCommandsRPC(nc *nats.Conn, prefix string) *CommandsRPC {
	return &CommandsRPC{nc: nc, prefix: prefix}
}

func (c *CommandsRPC) Upsert(ctx context.Context, userID string, name, response string) error {
	reply, err := bus.RequestJSONTimeout[commandsrpc.DashboardReply](ctx, c.nc, c.prefix+".upsert", commandsrpc.DashboardRequest{
		UserID:   userID,
		Name:     name,
		Response: response,
		IsActive: true,
		Perm:     "everyone",
	}, commandsRPCTimeout)
	if err != nil {
		return err
	}
	if reply.Error != "" {
		return errors.New(reply.Error)
	}
	return nil
}

func (c *CommandsRPC) Delete(ctx context.Context, userID string, name string) error {
	reply, err := bus.RequestJSONTimeout[commandsrpc.DashboardReply](ctx, c.nc, c.prefix+".delete", commandsrpc.DashboardRequest{
		UserID: userID,
		Name:   name,
	}, commandsRPCTimeout)
	if err != nil {
		return err
	}
	if reply.Error != "" {
		return errors.New(reply.Error)
	}
	return nil
}
