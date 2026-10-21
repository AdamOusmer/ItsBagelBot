// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/internal/resolve"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/discordstore"
	catalog "ItsBagelBot/internal/domain/analytics"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/idempotency"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

type Dispatcher struct {
	Registry  *registry.Registry
	Resolver  resolve.Resolver
	Store     discordstore.Store
	Publish   modules.Publish
	Log       *zap.Logger
	Analytics *analytics.Recorder
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
	mods, broadcasterID, ok := d.Resolver.ByGuild(ctx, ev.GuildID)
	if !ok {
		return nil
	}
	if ev.Type == guildDelete {
		d.recordGuildDelete(ev)
		return nil
	}

	var emitted []ddiscord.Command
	emit := func(c ddiscord.Command) { emitted = append(emitted, c) }

	c := &module.Context{Event: ev, GuildID: mods.GuildID, BroadcasterID: broadcasterID, Modules: mods, Log: log}
	modules.EnsureDesk(ctx, d.Store, mods, emit)
	d.runHandlers(ctx, c, emit)

	d.publishAll(ctx, emitted)
	return nil
}

const guildDelete = "GUILD_DELETE"

// An outage also sends GUILD_DELETE, flagged unavailable; only a real removal counts.
func (d *Dispatcher) recordGuildDelete(ev ddiscord.Event) {
	gone, err := decode.Decode[struct {
		Unavailable bool `json:"unavailable"`
	}](ev.Raw)
	if err == nil && !gone.Unavailable {
		d.Analytics.Count(catalog.Lifecycle, "guild_removed")
	}
}

func (d *Dispatcher) runHandlers(ctx context.Context, c *module.Context, emit module.Emit) {
	route := d.route(c.Event)
	if len(route.Handlers) > 0 {
		d.Analytics.Count(route.Metric, route.Label)
	}
	defer func() { c.Settings = nil }()
	for _, b := range route.Handlers {
		if !c.ModuleOn(b.Module) {
			route.declineOff(c, emit)
			continue
		}
		c.Settings = c.Modules.Rows[b.Module].Settings
		if err := runHandler(ctx, c, emit, b.Handler); err != nil {
			c.Log.Warn("discord module handler failed", zap.String("module", b.Module), zap.Error(err))
		}
	}
}

type tally struct {
	Metric catalog.Metric
	Label  string
}

type route struct {
	tally
	Handlers []registry.Bound
	Token    string
}

func (r route) declineOff(c *module.Context, emit module.Emit) {
	if r.Token == "" {
		return
	}
	emit(cmd.Followup(cmd.GuildTarget(c.GuildID), cmd.Token(r.Token), "That feature is turned off.", true))
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

func (d *Dispatcher) route(ev ddiscord.Event) route {
	if ev.Type != "INTERACTION_CREATE" {
		return route{tally: tally{catalog.DiscordEvents, registry.EventLabel(ev.Type)}, Handlers: d.Registry.Events(ev.Type)}
	}
	in, err := decode.Decode[decode.InteractionEvent](ev.Raw)
	if err != nil {
		d.Log.Warn("dropping undecodable discord interaction",
			zap.String("event_type", ev.Type), zap.String("guild_id", ev.GuildID), zap.Error(err))
		return route{}
	}
	if in.Data.CustomID != "" {
		return routeOf(in.Token, in.Data.CustomID)(d.Registry.Button(in.Data.CustomID))
	}
	return routeOf(in.Token, in.Data.Name)(d.Registry.Slash(in.Data.Name))
}

func routeOf(token, label string) func(registry.Bound, bool) route {
	return func(b registry.Bound, ok bool) route {
		if !ok {
			return route{}
		}
		return route{tally: tally{catalog.DiscordInteractions, label}, Handlers: []registry.Bound{b}, Token: token}
	}
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
