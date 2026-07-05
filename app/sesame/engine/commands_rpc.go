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

// CommandsRPC implements CommandManager by forwarding mutations to the commands
// service's dashboard RPC over NATS request/reply.
type CommandsRPC struct {
	nc     *nats.Conn
	prefix string // e.g. "bagel.rpc.commands"
}

// NewCommandsRPC returns a CommandManager backed by the commands dashboard RPC.
// prefix is the NATS subject prefix the commands service subscribes to
// (default "bagel.rpc.commands"); the client appends ".upsert" / ".delete".
func NewCommandsRPC(nc *nats.Conn, prefix string) *CommandsRPC {
	return &CommandsRPC{nc: nc, prefix: prefix}
}

// Upsert creates or updates a custom command for the given broadcaster.
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

// Delete removes a custom command for the given broadcaster.
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
