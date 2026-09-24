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
	followageModuleName  = engine.FollowageModuleName
	accountAgeModuleName = engine.AccountAgeModuleName

	followageCooldown  = 15 * time.Second
	accountAgeCooldown = 15 * time.Second
)

func Followage(d engine.Deps) module.Module {
	log := moduleLog(d)

	followage := func(ctx context.Context, c *module.Context, target lookupTarget) string {
		bid := c.Env.BroadcasterUserID
		return lookupCall[engine.FollowageResult]{
			log: log, logKey: followageModuleName, unavailable: i18n.T(c.Locale, "followage.unavailable"),
			read: readIf(d.Followage != nil, func() (engine.FollowageResult, error) { return d.Followage.Lookup(ctx, bid, target.id, target.login) }),
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

type replyFunc func(ctx context.Context, c *module.Context, target lookupTarget) string

func lookupRun(d engine.Deps, moduleName string, reply replyFunc) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if !moduleEnabled(ctx, d, c.BroadcasterID, moduleName) {
			return nil
		}
		emitLookup(c, reply(ctx, c, parseLookupTarget(args, c)), emit)
		return nil
	}
}

type lookupCall[T any] struct {
	log         *zap.Logger
	logKey      string
	unavailable string
	read        func() (T, error)
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

func readIf[T any](ok bool, read func() (T, error)) func() (T, error) {
	if !ok {
		return nil
	}
	return read
}

type lookupTarget struct {
	login string
	name  string
	id    string
}

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

type lookupReply struct {
	locale     string
	targetName string
}

func (r lookupReply) broadcaster() string {
	return fmt.Sprintf(i18n.T(r.locale, "followage.broadcaster"), r.targetName)
}

func (r lookupReply) followage(result engine.FollowageResult) string {
	if !result.UserFound {
		return fmt.Sprintf(i18n.T(r.locale, "lookup.not_user"), r.targetName)
	}
	if !result.Following {
		return fmt.Sprintf(i18n.T(r.locale, "followage.not_following"), r.targetName)
	}
	return fmt.Sprintf(i18n.T(r.locale, "followage.followed"), r.targetName, i18n.HumanizeDuration(r.locale, time.Since(result.FollowedAt)))
}

func (r lookupReply) accountAge(result engine.AccountAgeResult) string {
	if !result.UserFound {
		return fmt.Sprintf(i18n.T(r.locale, "lookup.not_user"), r.targetName)
	}
	return fmt.Sprintf(i18n.T(r.locale, "accountage.age"), r.targetName, i18n.HumanizeDuration(r.locale, time.Since(result.CreatedAt)))
}

func moduleLog(d engine.Deps) *zap.Logger {
	if d.Log == nil {
		return zap.NewNop()
	}
	return d.Log
}

func moduleEnabled(ctx context.Context, d engine.Deps, broadcasterID uint64, moduleName string) bool {
	return engine.ModuleGate{
		Proj:          d.Proj,
		Log:           moduleLog(d),
		BroadcasterID: broadcasterID,
		Name:          moduleName,
	}.BuiltinEnabled(ctx)
}
