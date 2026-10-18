// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/internal/resolve"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/idempotency"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

type Dispatcher struct {
	Registry *registry.Registry
	Resolver resolve.Resolver
	Store    discordstore.Store
	Publish  modules.Publish
	Log      *zap.Logger
}

const dedupTTL = 2 * time.Minute

func Dedup(store idempotency.Store, log *zap.Logger, metrics idempotency.Metrics) func(idempotency.Handler) idempotency.Handler {
	return idempotency.Guard(idempotency.Config{
		Store: store, Key: idempotency.MessageUUIDKey, TTL: dedupTTL, Log: log, Metrics: metrics,
	})
}

func (d *Dispatcher) Handle(msg *bus.Message) error {
	var ev ddiscord.Event
	if err := codec.Unmarshal(msg.Payload, &ev); err != nil {
		ctx := msg.Context()
		d.Log.Warn("dropping malformed discord ingress event", zap.String("message_id", msg.UUID), zap.Error(err))
		if txn := newrelic.FromContext(ctx); txn != nil {
			txn.NoticeError(err)
		}
		return nil
	}
	ctx := msg.Context()
	log := d.Log.With(zap.String("guild_id", ev.GuildID), zap.String("event_type", ev.Type), zap.String("message_id", msg.UUID))
	cfg, broadcasterID, ok := d.Resolver.ByGuild(ctx, ev.GuildID)
	if !ok {
		return nil
	}

	var emitted []ddiscord.Command
	emit := func(c ddiscord.Command) { emitted = append(emitted, c) }

	c := &module.Context{Event: ev, Config: cfg, BroadcasterID: broadcasterID, Log: log}
	modules.EnsureDesk(ctx, d.Store, cfg, emit)
	d.runHandlers(ctx, c, emit)

	d.publishAll(ctx, emitted)
	return nil
}

func (d *Dispatcher) runHandlers(ctx context.Context, c *module.Context, emit module.Emit) {
	for _, h := range d.handlersFor(c.Event) {
		if err := runHandler(ctx, c, emit, h); err != nil {
			c.Log.Warn("discord module handler failed", zap.Error(err))
		}
	}
}

func runHandler(ctx context.Context, c *module.Context, emit module.Emit, h module.Handler) (err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		c.Log.Error("discord module handler panicked", zap.Any("panic", r))
		if txn := newrelic.FromContext(ctx); txn != nil {
			txn.NoticeError(fmt.Errorf("discord module handler panic: %v", r))
		}
	}()
	return h(ctx, c, emit)
}

func (d *Dispatcher) handlersFor(ev ddiscord.Event) []module.Handler {
	if ev.Type != "INTERACTION_CREATE" {
		return d.Registry.Events(ev.Type)
	}
	in, err := decode.Decode[decode.InteractionEvent](ev.Raw)
	if err != nil {
		d.Log.Warn("dropping undecodable discord interaction",
			zap.String("event_type", ev.Type), zap.String("guild_id", ev.GuildID), zap.Error(err))
		return nil
	}
	if in.Data.CustomID != "" {
		if h, ok := d.Registry.Button(in.Data.CustomID); ok {
			return []module.Handler{h}
		}
		return nil
	}
	if h, ok := d.Registry.Slash(in.Data.Name); ok {
		return []module.Handler{h}
	}
	return nil
}

// Total retry time must stay far below the ingress consumer's AckWait, or redelivery double-posts.
const (
	publishAttempts   = 3
	publishRetryDelay = 100 * time.Millisecond
)

func (d *Dispatcher) publishAll(ctx context.Context, cmds []ddiscord.Command) {
	for _, c := range cmds {
		if err := d.publishRetry(ctx, c); err != nil {
			d.Log.Error("discord command publish failed",
				zap.String("subject", ddiscord.Lane(c.Type)),
				zap.String("type", c.Type),
				zap.Bool("retried", preAdmission(err)),
				zap.Error(err))
		}
	}
}

// Only no-responders proves the broker stored nothing; retrying any other error can post twice.
func preAdmission(err error) bool {
	return errors.Is(err, nats.ErrNoResponders)
}

func (d *Dispatcher) publishRetry(ctx context.Context, c ddiscord.Command) error {
	var err error
	for attempt := range publishAttempts {
		if err = d.Publish(ctx, c); err == nil {
			return nil
		}
		if !preAdmission(err) || attempt == publishAttempts-1 {
			break
		}
		if waitErr := sleep(ctx, publishRetryDelay); waitErr != nil {
			return err
		}
	}
	return err
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
