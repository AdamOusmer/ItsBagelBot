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
	followageModuleName = "followage"
	followageCooldown   = 15 * time.Second
)

// Followage owns the complete built-in !followage [user] command: target
// normalization, cached lookup through Sesame's Followage service, result
// formatting, and the chat reply. Outgress only performs the authenticated
// Twitch read behind that request/reply boundary.
func Followage(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	m := module.NewModule("", module.KindCore)
	m.Command("followage").Everyone().Cooldown(followageCooldown).Run(func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if !followageEnabled(ctx, d, c.BroadcasterID, log) {
			return nil
		}
		targetLogin := ""
		if fields := strings.Fields(args); len(fields) > 0 {
			targetLogin = strings.TrimPrefix(fields[0], "@")
		}
		targetName, targetID := targetLogin, ""
		if targetLogin == "" {
			targetLogin = c.Env.ChatterUserLogin
			targetName = c.Env.ChatterName()
			targetID = c.Env.ChatterUserID
		}
		if targetName == "" {
			targetName = targetLogin
		}
		if d.Followage == nil {
			emitFollowage(c, "Followage is unavailable right now.", emit)
			return nil
		}
		result, err := d.Followage.Lookup(ctx, c.Env.BroadcasterUserID, targetID, targetLogin)
		if err != nil {
			log.Warn("followage: lookup failed", zap.Error(err))
			emitFollowage(c, "Followage is unavailable right now.", emit)
			return nil
		}
		var reply string
		switch {
		case !result.UserFound:
			reply = fmt.Sprintf("@%s is not a Twitch user.", targetName)
		case result.TargetID == c.Env.BroadcasterUserID:
			reply = fmt.Sprintf("@%s is the broadcaster.", targetName)
		case !result.Following:
			reply = fmt.Sprintf("@%s is not following this channel.", targetName)
		default:
			reply = fmt.Sprintf("@%s has followed for %s.", targetName, humanizeFollowage(time.Since(result.FollowedAt)))
		}
		emitFollowage(c, reply, emit)
		return nil
	})
	return m.Build()
}

func emitFollowage(c *module.Context, text string, emit module.Emit) {
	emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
}

func humanizeFollowage(d time.Duration) string {
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

func followageEnabled(ctx context.Context, d engine.Deps, broadcasterID uint64, log *zap.Logger) bool {
	if d.Proj == nil {
		return true
	}
	views, err := d.Proj.Modules(ctx, broadcasterID)
	if err != nil {
		log.Warn("followage: module state read failed, allowing", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return true
	}
	for _, v := range views {
		if v.Name == followageModuleName {
			return v.IsEnabled
		}
	}
	return true
}
