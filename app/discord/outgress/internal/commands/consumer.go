// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const (
	nakDelay        = time.Second
	maxRedeliveries = 3
)

type Consumer struct {
	NATSURL string
	Log     *zap.Logger
	Handle  func(context.Context, ddiscord.Command) error
}

type Lanes struct {
	Mod     bus.Subscriber
	Default bus.Subscriber
	Close   func()
}

func (c *Consumer) Run(ctx context.Context) (Lanes, error) {
	mod, err := bus.NewLaneSubscriber(bus.LaneConfig{
		URL: c.NATSURL, Stream: bus.DiscordOutgressStream.Name, Subject: ddiscord.LaneMod,
		Group: "discord-outgress-mod", NakDelay: nakDelay, MaxRedeliveries: maxRedeliveries,
	}, c.Log)
	if err != nil {
		return Lanes{}, err
	}
	def, err := bus.NewLaneSubscriber(bus.LaneConfig{
		URL: c.NATSURL, Stream: bus.DiscordOutgressStream.Name, Subject: ddiscord.LaneDefault,
		Group: "discord-outgress-default", NakDelay: nakDelay, MaxRedeliveries: maxRedeliveries,
	}, c.Log)
	if err != nil {
		_ = mod.Close()
		return Lanes{}, err
	}

	modCh, err := mod.Subscribe(ctx, ddiscord.LaneMod)
	if err != nil {
		_ = mod.Close()
		_ = def.Close()
		return Lanes{}, err
	}
	defCh, err := def.Subscribe(ctx, ddiscord.LaneDefault)
	if err != nil {
		_ = mod.Close()
		_ = def.Close()
		return Lanes{}, err
	}

	go c.pump(ctx, modCh, defCh)

	return Lanes{Mod: mod, Default: def, Close: func() { _ = mod.Close(); _ = def.Close() }}, nil
}

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

func (c *Consumer) pollMod(modCh <-chan *bus.Message) (handled, closed bool) {
	select {
	case msg, ok := <-modCh:
		return c.take(msg, ok), !ok
	default:
		return false, false
	}
}

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
