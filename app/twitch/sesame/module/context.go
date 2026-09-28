// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"ItsBagelBot/internal/domain/event/lane"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/pkg/codec"
	"context"
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
	// LocaleLoaded records even default-language and failed lookups.
	LocaleLoaded bool
	// LocaleLookup is supplied once by the engine. Chat handlers resolve it
	// only when a matched reply requires the broadcaster's language.
	LocaleLookup func(context.Context, uint64) (string, error)

	Config codec.RawMessage

	Num string

	Command string

	role    Role
	roleSet bool

	emoteCodes  map[string]struct{}
	emotesBuilt bool

	eventVersion int64
}

// EnsureLocale resolves the broadcaster's language at most once per message.
// Trial provenance already establishes the default language without an account
// lookup. A missing lookup (standalone module tests) preserves an explicit Locale.
func (c *Context) EnsureLocale(ctx context.Context) {
	if c.LocaleLoaded {
		return
	}
	c.LocaleLoaded = true
	if c.Env.Origin == "trial" {
		c.Locale = ""
		return
	}
	if c.Locale != "" || c.LocaleLookup == nil {
		return
	}
	if locale, err := c.LocaleLookup(ctx, c.BroadcasterID); err == nil {
		c.Locale = locale
	}
}

func (c *Context) Chatter() Role {
	if !c.roleSet {
		c.role = ParseRole(c.Env)
		c.roleSet = true
	}
	return c.role
}

// EventVersion falls back to one clock reading per event, shared by every
// handler, so their versioned live and watch-time writes agree.
func (c *Context) EventVersion() int64 {
	if c.eventVersion == 0 {
		c.eventVersion = c.Env.EventVersion()
	}
	if c.eventVersion == 0 {
		c.eventVersion = livekey.VersionNow()
	}
	return c.eventVersion
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
	c.LocaleLoaded = false
	c.LocaleLookup = nil
	c.Config = nil
	c.Num = ""
	c.Command = ""
	c.role = RoleEveryone
	c.roleSet = false
	c.emoteCodes = nil
	c.emotesBuilt = false
	c.eventVersion = 0
}

func (c *Context) BID() zap.Field { return BIDField(c.BroadcasterID) }

func (c *Context) Palette(namespace Namespace, kv ...string) Palette {
	return KV(kv...).WithLocale(Locale(c.Locale)).WithNamespace(namespace)
}

func BIDField(id uint64) zap.Field { return zap.Uint64("broadcaster_id", id) }
