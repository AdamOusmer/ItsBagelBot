package modules

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const (
	followageModuleName  = "followage"
	followageCooldown    = 15 * time.Second
	accountAgeModuleName = "accountage"
	accountAgeCooldown   = 15 * time.Second
)

// Followage owns the built-in viewer-lookup commands that read Twitch through
// outgress: !followage [user] and !accountage [user]. Each does target
// normalization, a cached lookup through the matching Sesame service, result
// formatting, and the chat reply; outgress only performs the authenticated
// Twitch read behind that request/reply boundary. Both commands are toggleable
// per broadcaster under their own module key, checked lazily on use.
//
// Their replies are wired here as closures over a shared lookupCall so the
// nil-service, error and formatting handling lives in exactly one place.
func Followage(d engine.Deps) module.Module {
	log := moduleLog(d)

	followage := func(ctx context.Context, c *module.Context, target lookupTarget) string {
		bid := c.Env.BroadcasterUserID
		return lookupCall[engine.FollowageResult]{
			log: log, logKey: followageModuleName, unavailable: "Followage is unavailable right now.",
			read:   readIf(d.Followage != nil, func() (engine.FollowageResult, error) { return d.Followage.Lookup(ctx, bid, target.id, target.login) }),
			format: func(res engine.FollowageResult) string { return formatFollowageResult(target.name, bid, res) },
		}.run()
	}

	accountAge := func(ctx context.Context, _ *module.Context, target lookupTarget) string {
		return lookupCall[engine.AccountAgeResult]{
			log: log, logKey: accountAgeModuleName, unavailable: "Account age is unavailable right now.",
			read:   readIf(d.AccountAge != nil, func() (engine.AccountAgeResult, error) { return d.AccountAge.Lookup(ctx, target.id, target.login) }),
			format: func(res engine.AccountAgeResult) string { return formatAccountAgeResult(target.name, res) },
		}.run()
	}

	m := module.NewModule("", module.KindCore)
	m.Command("followage").Everyone().Cooldown(followageCooldown).Run(lookupRun(d, followageModuleName, followage))
	m.Command("accountage").Everyone().Cooldown(accountAgeCooldown).Run(lookupRun(d, accountAgeModuleName, accountAge))
	return m.Build()
}

// replyFunc renders one built-in lookup command's chat reply for a resolved
// target.
type replyFunc func(ctx context.Context, c *module.Context, target lookupTarget) string

// lookupRun is the shared body of the built-in lookup commands: honor the
// per-broadcaster toggle, resolve the target, then emit the command's reply.
func lookupRun(d engine.Deps, moduleName string, reply replyFunc) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if !moduleEnabled(ctx, d, c.BroadcasterID, moduleName) {
			return nil
		}
		emitLookup(c, reply(ctx, c, parseLookupTarget(args, c)), emit)
		return nil
	}
}

// lookupCall is one built-in command's cached read plus how to present it. run()
// is the single place the shared nil-service, error and format handling lives,
// so !followage and !accountage don't each repeat it.
type lookupCall[T any] struct {
	log         *zap.Logger
	logKey      string
	unavailable string
	read        func() (T, error) // nil when the backing service is not wired
	format      func(T) string
}

func (lc lookupCall[T]) run() string {
	if lc.read == nil {
		return lc.unavailable
	}
	result, err := lc.read()
	if err != nil {
		lc.log.Warn(lc.logKey+": lookup failed", zap.Error(err))
		return lc.unavailable
	}
	return lc.format(result)
}

// readIf returns read when ok, else nil, letting a caller drop a read whose
// backing service is absent without an inline conditional at the call site.
func readIf[T any](ok bool, read func() (T, error)) func() (T, error) {
	if !ok {
		return nil
	}
	return read
}

// lookupTarget is the normalized subject of a viewer-lookup command: an
// explicit "@user" argument (login/name, no id yet) or, absent one, the chatter
// themselves (login, display name and id straight off the envelope).
type lookupTarget struct {
	login string
	name  string
	id    string
}

// parseLookupTarget reads the optional first "@user" argument, falling back to
// the chatter. Shared by !followage and !accountage.
func parseLookupTarget(args string, c *module.Context) lookupTarget {
	fields := strings.Fields(args)
	if len(fields) > 0 {
		login := strings.TrimPrefix(fields[0], "@")
		if login != "" {
			return lookupTarget{login: login, name: login}
		}
	}
	return lookupTarget{login: c.Env.ChatterUserLogin, name: c.Env.ChatterName(), id: c.Env.ChatterUserID}
}

func emitLookup(c *module.Context, text string, emit module.Emit) {
	emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
}

func formatFollowageResult(targetName, broadcasterID string, result engine.FollowageResult) string {
	if !result.UserFound {
		return fmt.Sprintf("@%s is not a Twitch user.", targetName)
	}
	if result.TargetID == broadcasterID {
		return fmt.Sprintf("@%s is the broadcaster.", targetName)
	}
	if !result.Following {
		return fmt.Sprintf("@%s is not following this channel.", targetName)
	}
	return fmt.Sprintf("@%s has followed for %s.", targetName, humanizeDuration(time.Since(result.FollowedAt)))
}

func formatAccountAgeResult(targetName string, result engine.AccountAgeResult) string {
	if !result.UserFound {
		return fmt.Sprintf("@%s is not a Twitch user.", targetName)
	}
	return fmt.Sprintf("@%s's account is %s old.", targetName, humanizeDuration(time.Since(result.CreatedAt)))
}

// humanizeDuration renders a span as the two largest non-zero units (e.g.
// "2 years, 3 months"), used by both !followage and !accountage.
func humanizeDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	minutes := int64(d / time.Minute)
	if minutes < 1 {
		return "less than a minute"
	}
	units := []struct {
		minutes int64
		name    string
	}{{365 * 24 * 60, "year"}, {30 * 24 * 60, "month"}, {24 * 60, "day"}, {60, "hour"}, {1, "minute"}}
	parts := make([]string, 0, 2)
	for _, unit := range units {
		if n := minutes / unit.minutes; n > 0 {
			name := unit.name
			if n != 1 {
				name += "s"
			}
			parts = append(parts, fmt.Sprintf("%d %s", n, name))
			minutes %= unit.minutes
			if len(parts) == 2 {
				break
			}
		}
	}
	return strings.Join(parts, ", ")
}

// moduleLog returns the module logger, or a no-op when Deps carries none.
func moduleLog(d engine.Deps) *zap.Logger {
	if d.Log == nil {
		return zap.NewNop()
	}
	return d.Log
}

// moduleEnabled reports whether a built-in command's per-broadcaster toggle is
// on. A missing row (or a projection read error, or no projection at all) fails
// open: a transient blip must not silently swallow the command.
func moduleEnabled(ctx context.Context, d engine.Deps, broadcasterID uint64, moduleName string) bool {
	if d.Proj == nil {
		return true
	}
	views, err := d.Proj.Modules(ctx, broadcasterID)
	if err != nil {
		moduleLog(d).Warn(moduleName+": module state read failed, allowing", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return true
	}
	for _, v := range views {
		if v.Name == moduleName {
			return v.IsEnabled
		}
	}
	return true
}
