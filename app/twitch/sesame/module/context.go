// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/codec"
	"strings"

	"go.uber.org/zap"
)

// Pooled by the engine: modules must not retain it past the call.
type Context struct {
	Env           lane.Envelope
	Regress       Regress
	BroadcasterID uint64
	Log           *zap.Logger

	Locale string

	Config codec.RawMessage

	Num string

	Command string

	role    Role
	roleSet bool

	emoteCodes  map[string]struct{}
	emotesBuilt bool
}

func (c *Context) Chatter() Role {
	if !c.roleSet {
		c.role = ParseRole(c.Env)
		c.roleSet = true
	}
	return c.role
}

func (c *Context) Decode(out any) error {
	if len(c.Config) == 0 {
		return nil
	}
	return codec.Unmarshal(c.Config, out)
}

func (c *Context) EmoteCodes() map[string]struct{} {
	if c.emotesBuilt {
		return c.emoteCodes
	}
	c.emotesBuilt = true
	if len(c.Env.Emotes) > 0 && c.Env.Text != "" {
		runes := []rune(c.Env.Text)
		codes := make(map[string]struct{}, len(c.Env.Emotes))
		for _, s := range c.Env.Emotes {
			if s.Begin < 0 || s.End > len(runes) || s.Begin >= s.End {
				continue
			}
			codes[strings.ToLower(string(runes[s.Begin:s.End]))] = struct{}{}
		}
		if len(codes) > 0 {
			c.emoteCodes = codes
		}
	}
	return c.emoteCodes
}

func (c *Context) Reset() {
	c.Env = lane.Envelope{}
	c.Regress = RegressStandard
	c.BroadcasterID = 0
	c.Locale = ""
	c.Config = nil
	c.Num = ""
	c.Command = ""
	c.role = RoleEveryone
	c.roleSet = false
	c.emoteCodes = nil
	c.emotesBuilt = false
}

func (c *Context) BID() zap.Field { return BIDField(c.BroadcasterID) }

func BIDField(id uint64) zap.Field { return zap.Uint64("broadcaster_id", id) }
