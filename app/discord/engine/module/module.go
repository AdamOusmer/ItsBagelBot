// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"context"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"go.uber.org/zap"
)

type Context struct {
	Event         ddiscord.Event
	Config        ddiscord.Config
	BroadcasterID string
	Log           *zap.Logger
}

type Emit func(cmd ddiscord.Command)

type Handler func(ctx context.Context, c *Context, emit Emit) error

type Module struct {
	Name    string
	Events  map[string]Handler
	Slash   map[string]Handler
	Buttons map[string]Handler
}
