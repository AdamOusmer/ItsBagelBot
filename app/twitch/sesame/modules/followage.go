// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const (
	// The module rows live on the engine side: the {followage} and
	// {accountage} response tokens are gated by the very same rows, and two
	// spellings of one row is one rename away from a channel where the
	// command runs and the token stays literal.
	followageModuleName  = engine.FollowageModuleName
	accountAgeModuleName = engine.AccountAgeModuleName

	followageCooldown  = 15 * time.Second
	accountAgeCooldown = 15 * time.Second
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
			log: log, logKey: followageModuleName, unavailable: i18n.T(c.Locale, "followage.unavailable"),
			read: readIf(d.Followage != nil, func() (engine.FollowageResult, error) { return d.Followage.Lookup(ctx, bid, target.id, target.login) }),
			// The broadcaster case is resolved here, where bid is in scope.
			format: func(res engine.FollowageResult) string {
				r := lookupReply{locale: c.Locale, targetName: target.name}
				if res.UserFound && res.TargetID == bid {
					return r.broadcaster()
				}
				return r.followage(res)
			},
		}.run()
	}

	accountAge := func(ctx context.Context, c *module.Context, target lookupTarget) string {
		return lookupCall[engine.AccountAgeResult]{
			log: log, logKey: accountAgeModuleName, unavailable: i18n.T(c.Locale, "accountage.unavailable"),
			read: readIf(d.AccountAge != nil, func() (engine.AccountAgeResult, error) { return d.AccountAge.Lookup(ctx, target.id, target.login) }),
			format: func(res engine.AccountAgeResult) string {
				return lookupReply{locale: c.Locale, targetName: target.name}.accountAge(res)
			},
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

// lookupReply renders a viewer-lookup command's chat text in the broadcaster
// locale. Bundling the locale and resolved target name onto the receiver keeps
// the per-result formatters off a string-heavy argument list.
type lookupReply struct {
	locale     string
	targetName string
}

// broadcaster is the reply when the looked-up user is the broadcaster.
func (r lookupReply) broadcaster() string {
	return fmt.Sprintf(i18n.T(r.locale, "followage.broadcaster"), r.targetName)
}

// followage renders the !followage result (broadcaster case handled upstream).
func (r lookupReply) followage(result engine.FollowageResult) string {
	if !result.UserFound {
		return fmt.Sprintf(i18n.T(r.locale, "lookup.not_user"), r.targetName)
	}
	if !result.Following {
		return fmt.Sprintf(i18n.T(r.locale, "followage.not_following"), r.targetName)
	}
	return fmt.Sprintf(i18n.T(r.locale, "followage.followed"), r.targetName, i18n.HumanizeDuration(r.locale, time.Since(result.FollowedAt)))
}

// accountAge renders the !accountage result.
func (r lookupReply) accountAge(result engine.AccountAgeResult) string {
	if !result.UserFound {
		return fmt.Sprintf(i18n.T(r.locale, "lookup.not_user"), r.targetName)
	}
	return fmt.Sprintf(i18n.T(r.locale, "accountage.age"), r.targetName, i18n.HumanizeDuration(r.locale, time.Since(result.CreatedAt)))
}

// moduleLog returns the module logger, or a no-op when Deps carries none.
func moduleLog(d engine.Deps) *zap.Logger {
	if d.Log == nil {
		return zap.NewNop()
	}
	return d.Log
}

// moduleEnabled reports whether a built-in command's per-broadcaster toggle is
// on, through the one gate the response tokens also read.
func moduleEnabled(ctx context.Context, d engine.Deps, broadcasterID uint64, moduleName string) bool {
	return engine.ModuleGate{
		Proj:          d.Proj,
		Log:           moduleLog(d),
		BroadcasterID: broadcasterID,
		Name:          moduleName,
	}.BuiltinEnabled(ctx)
}
