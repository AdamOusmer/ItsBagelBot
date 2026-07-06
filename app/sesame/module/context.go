package module

import (
	"encoding/json"

	"ItsBagelBot/internal/domain/event/lane"

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
	Config json.RawMessage

	// Num is the inline numeric suffix a NumericSuffix command absorbed from its
	// trigger ("30" for "!clip30"), or empty when none was typed or the command
	// does not opt into NumericSuffix. Set by the engine before running a baked
	// command; a command reads it to interpret the number (e.g. clip duration).
	Num string

	role    Role
	roleSet bool
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
	return json.Unmarshal(c.Config, out)
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
}
