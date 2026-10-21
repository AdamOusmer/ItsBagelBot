// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"context"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

type Context struct {
	Event         ddiscord.Event
	GuildID       string
	BroadcasterID string
	Modules       ddiscord.GuildModules
	Settings      codec.RawMessage
	Log           *zap.Logger
}

func (c *Context) Decode(out any) error {
	if len(c.Settings) == 0 {
		return nil
	}
	return codec.Unmarshal(c.Settings, out)
}

func (c *Context) ModuleOn(name string) bool { return c.Modules.On(name) }

func (c *Context) Peer(name string, out any) error { return c.Modules.Decode(name, out) }

func (c *Context) Roles() ddiscord.RolesSettings { return c.Modules.Roles() }

type Emit func(cmd ddiscord.Command)

type Handler func(ctx context.Context, c *Context, emit Emit) error

type Module struct {
	Name    string
	Kind    ddiscord.ModuleKind
	Events  map[string]Handler
	Slash   map[string]Handler
	Buttons map[string]Handler
}
