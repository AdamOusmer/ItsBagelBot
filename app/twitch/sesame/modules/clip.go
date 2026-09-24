// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const (
	clipMinDuration = 5
	clipMaxDuration = 60
)

const clipCooldown = 15 * time.Second

const clipModuleName = "clip"

func Clip(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule("", module.KindCore)
	m.Command("clip").Everyone().LiveOnly().NumericSuffix().Cooldown(clipCooldown).Run(clipRun(d, log))
	return m.Build()
}

func clipRun(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		enabled, reply := clipSettings(ctx, d, c.BroadcasterID, log)
		if !enabled {
			return nil
		}
		emit(&module.Output{
			Type:          outgress.TypeClip,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          strings.TrimSpace(args),
			To:            c.Env.ChatterName(),
			Duration:      clipDuration(c.Num),
			Template:      reply,
		})
		return nil
	}
}

func clipDuration(num string) float64 {
	if num == "" {
		return 0
	}
	n, err := strconv.Atoi(num)
	if err != nil || n > clipMaxDuration {
		return clipMaxDuration
	}
	if n < clipMinDuration {
		return clipMinDuration
	}
	return float64(n)
}

type clipConfig struct {
	Reply string `json:"reply"`
}

func clipSettings(ctx context.Context, d engine.Deps, broadcasterID uint64, log *zap.Logger) (enabled bool, reply string) {
	view, state, err := engine.ModuleLookup{Proj: d.Proj, BroadcasterID: broadcasterID, Name: clipModuleName, Absent: engine.ModuleOn}.Resolve(ctx)
	if err != nil {
		log.Warn("clip: module state read failed, allowing",
			module.BIDField(broadcasterID), zap.Error(err))
	}
	var cfg clipConfig
	if len(view.Configs) > 0 {
		_ = codec.Unmarshal(view.Configs, &cfg)
	}
	return state != engine.ModuleOff, strings.TrimSpace(cfg.Reply)
}
