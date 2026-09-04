// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package commands is outgress's Command consumer: it binds both
// DISCORD_OUTGRESS lanes and drains LaneMod to empty before it ever touches
// LaneDefault (see internal/domain/discord's Lane doc for why moderation
// preempts). Handler dispatch itself lives in handlers.go.
package commands

import (
	"context"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// nakDelay/maxRedeliveries match Twitch outgress's own chat lanes: a failed
// command is retried a few times at a short, paced interval before it ages
// out at the stream's 60s MaxAge.
const (
	nakDelay        = time.Second
	maxRedeliveries = 3
)

// Consumer drains DISCORD_OUTGRESS's two lanes with strict mod-first
// priority.
type Consumer struct {
	NATSURL string
	Log     *zap.Logger
	Handle  func(context.Context, ddiscord.Command) error
}

// Run binds both lanes and pumps them until ctx is cancelled. It returns
// once both subscriptions are established; the pump itself runs on its own
// goroutine.
func (c *Consumer) Run(ctx context.Context) (func(), error) {
	mod, err := bus.NewLaneSubscriber(bus.LaneConfig{
		URL: c.NATSURL, Stream: bus.DiscordOutgressStream.Name, Subject: ddiscord.LaneMod,
		Group: "discord-outgress-mod", NakDelay: nakDelay, MaxRedeliveries: maxRedeliveries,
	}, c.Log)
	if err != nil {
		return nil, err
	}
	def, err := bus.NewLaneSubscriber(bus.LaneConfig{
		URL: c.NATSURL, Stream: bus.DiscordOutgressStream.Name, Subject: ddiscord.LaneDefault,
		Group: "discord-outgress-default", NakDelay: nakDelay, MaxRedeliveries: maxRedeliveries,
	}, c.Log)
	if err != nil {
		_ = mod.Close()
		return nil, err
	}

	modCh, err := mod.Subscribe(ctx, ddiscord.LaneMod)
	if err != nil {
		_ = mod.Close()
		_ = def.Close()
		return nil, err
	}
	defCh, err := def.Subscribe(ctx, ddiscord.LaneDefault)
	if err != nil {
		_ = mod.Close()
		_ = def.Close()
		return nil, err
	}

	go c.pump(ctx, modCh, defCh)

	return func() { _ = mod.Close(); _ = def.Close() }, nil
}

// pump is the mod-first priority loop: every iteration checks the mod
// channel alone (non-blocking) before it is willing to take a default
// message, so a sustained flood on mod is served exclusively and default
// only makes progress in the gaps.
//
// This is not linearizable priority -- if both channels have a message
// ready at the exact instant the blocking select below is entered, Go
// picks between them at random, so one default message can occasionally
// slip in ahead of a mod message that arrived a moment later. That is
// bounded and self-correcting: the very next iteration re-checks mod alone
// again, so the worst case is one out-of-order default send during a race
// window measured in microseconds, never a mod backlog building up behind
// a default flood.
func (c *Consumer) pump(ctx context.Context, modCh, defCh <-chan *bus.Message) {
	for {
		if handled, closed := c.pollMod(modCh); closed {
			return
		} else if handled {
			continue
		}
		if !c.waitTurn(ctx, modCh, defCh) {
			return
		}
	}
}

// waitTurn is pump's blocking half, extracted on its own (CodeScene: Complex
// Method -- pump inlining this select pushed it back over the cyclomatic
// limit even after pollMod/take/process were already split out). Runs once
// pollMod has confirmed nothing is already waiting on mod, and blocks for
// ctx cancellation or the next message on either lane. Reports whether the
// pump should keep running: false on cancellation or a closed channel.
func (c *Consumer) waitTurn(ctx context.Context, modCh, defCh <-chan *bus.Message) bool {
	select {
	case <-ctx.Done():
		return false
	case msg, ok := <-modCh:
		return c.take(msg, ok)
	case msg, ok := <-defCh:
		return c.take(msg, ok)
	}
}

// pollMod is the non-blocking mod-first check pump's own doc describes:
// handled is true once it has consumed a mod message (the loop should
// restart immediately rather than fall through to the blocking select below,
// so a sustained mod backlog never waits its turn behind default), closed is
// true once the mod channel itself is gone (the pump should stop).
func (c *Consumer) pollMod(modCh <-chan *bus.Message) (handled, closed bool) {
	select {
	case msg, ok := <-modCh:
		return c.take(msg, ok), !ok
	default:
		return false, false
	}
}

// take processes one channel receive and reports whether the pump should
// keep running: false means the channel closed and msg is the zero value.
func (c *Consumer) take(msg *bus.Message, ok bool) bool {
	if !ok {
		return false
	}
	c.process(msg)
	return true
}

func (c *Consumer) process(msg *bus.Message) {
	var cmd ddiscord.Command
	if err := codec.Unmarshal(msg.Payload, &cmd); err != nil {
		c.Log.Warn("dropping malformed discord command", zap.Error(err))
		msg.Ack()
		return
	}
	if err := c.Handle(msg.Context(), cmd); err != nil {
		c.Log.Warn("discord command handler failed", zap.String("type", cmd.Type), zap.Error(err))
		msg.Nack()
		return
	}
	msg.Ack()
}
