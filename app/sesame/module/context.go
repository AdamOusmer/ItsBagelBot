// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/codec"
	"strings"

	"go.uber.org/zap"
)

// Context is the per-message state the engine builds and hands to each command
// Run and event handler. It is confined to a single consumer goroutine, so its
// lazily-computed fields need no synchronization. The engine pools it: it gets
// one, fills it, runs the interested modules, then Resets and returns it.
// Modules must not retain it past the call.
type Context struct {
	Env           lane.Envelope
	Regress       Regress
	BroadcasterID uint64
	Log           *zap.Logger

	// Locale is the broadcaster's console UI language ("en", "fr", …). The engine
	// fills it (from the user projection) before running a command, so system
	// replies can be localized. Empty means the default language.
	Locale string

	// Config is this module's raw configuration blob from its ModuleView; the
	// engine sets it before calling a named module. Empty for core modules.
	Config codec.RawMessage

	// Num is the inline numeric suffix a NumericSuffix command absorbed from its
	// trigger ("30" for "!clip30"), or empty when none was typed or the command
	// does not opt into NumericSuffix. Set by the engine before running a baked
	// command; a command reads it to interpret the number (e.g. clip duration).
	Num string

	role    Role
	roleSet bool

	// emoteCodes is the lazily-built set of emote codes spelled natively in this
	// message (see EmoteCodes); emotesBuilt distinguishes "not built yet" from
	// "built, none found", so a message without spans never re-scans and the
	// accessor can return nil - not an empty map - as its steady-state answer.
	emoteCodes  map[string]struct{}
	emotesBuilt bool
}

// Chatter returns the chatter's resolved role, parsed once from the event badges
// and the broadcaster id. Non-chat events resolve to RoleEveryone.
func (c *Context) Chatter() Role {
	if !c.roleSet {
		c.role = ParseRole(c.Env)
		c.roleSet = true
	}
	return c.role
}

// Decode unmarshals the module's Config into out. A missing config is not an
// error: out is left at its zero value.
func (c *Context) Decode(out any) error {
	if len(c.Config) == 0 {
		return nil
	}
	return codec.Unmarshal(c.Config, out)
}

// EmoteCodes returns the emote codes spelled natively in THIS message, built
// once per pooled context from Env.Emotes: each span's Begin..End window over
// the raw Env.Text runes, lowercased. The automod treats the result as
// authoritative - the spans are what the chat client actually rendered, so
// they carry native Twitch emotes and cheermotes that no third-party fetch can
// ever contain, and they stay trustworthy even when that fetch is down.
//
// Lowercasing is deliberate (2026-08-23): cheermote text varies in case
// ("cheer100" and "Cheer100" both appear in real chat) while the fetched
// third-party sets stay exact-case; a false rescue here is bounded by the span
// proving the client rendered an emote at exactly that offset, so case
// strictness would only cost rescues, not prevent abuse. Malformed or
// out-of-range spans (a forward-incompatible producer, a squashed-line offset
// drift) are skipped rather than fatal - one bad span must not blank the rest.
//
// Steady state is zero-alloc: a message without Emotes returns nil from a
// single boolean check, never touching []rune conversion.
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

// Reset zeroes the per-message fields so the Context can be reused from a pool.
// The struct itself stays usable; only the values are cleared. The Log pointer
// is left in place since the engine re-sets it per message anyway, but the lazy
// role cache is cleared so it never leaks across messages.
func (c *Context) Reset() {
	c.Env = lane.Envelope{}
	c.Regress = RegressStandard
	c.BroadcasterID = 0
	c.Locale = ""
	c.Config = nil
	c.Num = ""
	c.role = RoleEveryone
	c.roleSet = false
	c.emoteCodes = nil
	c.emotesBuilt = false
}
