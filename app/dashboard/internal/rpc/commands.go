package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
)

// CommandView is the canonical command record shared between the RPC layer and
// the templ pages (agent C imports this type from package rpc).
type CommandView struct {
	Name     string `json:"name"`
	Response string `json:"response"`
	IsActive bool   `json:"is_active"`
}

// Commands is a NATS request-reply client for the commands service.
type Commands struct {
	nc     *nats.Conn
	prefix string // e.g. "bagel.rpc.commands"
}

// NewCommands creates a Commands RPC client.
func NewCommands(nc *nats.Conn, prefix string) *Commands {
	return &Commands{nc: nc, prefix: prefix}
}

// commandsReply is the shared reply envelope for all commands subjects.
type commandsReply struct {
	Commands []CommandView `json:"commands"`
	Error    string        `json:"error"`
}

func (c *Commands) do(ctx context.Context, verb string, body []byte) ([]CommandView, error) {
	msg, err := c.nc.RequestWithContext(ctx, c.prefix+"."+verb, body)
	if err != nil {
		return nil, fmt.Errorf("commands %s rpc: %w", verb, err)
	}
	var reply commandsReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return nil, fmt.Errorf("commands %s unmarshal: %w", verb, err)
	}
	if reply.Error != "" {
		return nil, fmt.Errorf("commands %s: %s", verb, reply.Error)
	}
	return reply.Commands, nil
}

// List returns all commands for the given user.
func (c *Commands) List(ctx context.Context, userID string) ([]CommandView, error) {
	body, _ := json.Marshal(map[string]string{"user_id": userID})
	return c.do(ctx, "list", body)
}

// Upsert creates or updates a command and returns the full updated list.
func (c *Commands) Upsert(ctx context.Context, userID, name, response string, isActive bool) ([]CommandView, error) {
	body, _ := json.Marshal(map[string]any{
		"user_id":   userID,
		"name":      name,
		"response":  response,
		"is_active": isActive,
	})
	return c.do(ctx, "upsert", body)
}

// Delete removes a command by name and returns the updated list.
func (c *Commands) Delete(ctx context.Context, userID, name string) ([]CommandView, error) {
	body, _ := json.Marshal(map[string]string{"user_id": userID, "name": name})
	return c.do(ctx, "delete", body)
}
