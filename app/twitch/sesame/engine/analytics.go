// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	catalog "ItsBagelBot/internal/domain/analytics"
)

func (p *Pipeline) recordActive(broadcasterID uint64, accountCreatedAtMicros int64) {
	p.analytics.Distinct(catalog.ChannelsActive, broadcasterID)
	if accountCreatedAtMicros > 0 {
		p.analytics.Cohort(broadcasterID, time.UnixMicro(accountCreatedAtMicros))
	}
}

func (p *Pipeline) recordBaked(broadcasterID uint64, bc BoundCommand) {
	p.analytics.Count(catalog.CommandUses, bc.Cmd.Name)
	p.analytics.Distinct(catalog.ModuleActive, broadcasterID, moduleLabel(bc.Owner))
}

func (p *Pipeline) recordHandled(mctx *module.Context, m module.Module, emittedBefore int, took time.Duration) {
	label := moduleLabel(m)
	p.analytics.Count(catalog.ModuleLatency, label, latencyBucket(took))
	if mctx.Emitted > emittedBefore {
		p.analytics.Count(catalog.ModuleEvents, label)
		p.analytics.Distinct(catalog.ModuleActive, mctx.BroadcasterID, label)
	}
}

func latencyBucket(d time.Duration) string {
	switch {
	case d < 10*time.Millisecond:
		return "lt10ms"
	case d < 50*time.Millisecond:
		return "lt50ms"
	case d < 250*time.Millisecond:
		return "lt250ms"
	case d < time.Second:
		return "lt1s"
	default:
		return "ge1s"
	}
}

func errorClass(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	return "internal"
}
