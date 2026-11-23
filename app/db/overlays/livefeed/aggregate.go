// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed

import (
	"context"
	"strconv"

	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/internal/domain/overlays/feed"
	"ItsBagelBot/pkg/bus"
)

// Consume is the CHANNEL_EVENTS handler; a payload that does not decode is quarantined rather than retried.
func (s *Service) Consume(msg *bus.Message) error {
	event, err := channelevents.Decode(msg.Payload)
	if err != nil {
		return bus.Poison(err)
	}
	return s.Handle(msg.Context(), event)
}

// Handle folds one channel event. It is safe to run again for the same event id: sums apply once per id and the rest is version guarded.
func (s *Service) Handle(ctx context.Context, e channelevents.Event) error {
	if e.Trial || e.Kind == channelevents.Chat {
		return nil
	}
	profile, err := s.profile(ctx, e.ChannelID)
	if err != nil || !profile.Owned {
		return err
	}
	c := channel(e.ChannelID)
	if e.Kind == channelevents.Session {
		return s.handleSession(ctx, c, e)
	}
	return s.handleEvent(ctx, c, e, profile.Subathon)
}

func (s *Service) handleSession(ctx context.Context, c channel, e channelevents.Event) error {
	if e.Change == nil || e.Change.State != channelevents.SessionStarted {
		return nil
	}
	rolled, err := s.store().roll(ctx, c, e)
	if err != nil || !rolled {
		return err
	}
	return s.publishStats(ctx, c, e.ID)
}

func (s *Service) handleEvent(ctx context.Context, c channel, e channelevents.Event, rules feed.SubathonRules) error {
	frame, framed := feed.FromChannel(e)
	if !framed {
		return nil
	}
	delta := feed.DeltaOf(e)
	session, err := s.store().session(ctx, c)
	if err != nil {
		return err
	}
	folded := folding{c: c, session: session, event: e, delta: delta}
	counted, err := s.store().fold(ctx, folded)
	if err != nil {
		return err
	}
	if err := s.store().remember(ctx, folded, frame); err != nil {
		return err
	}
	var clock *feed.Subathon
	if counted {
		if clock, err = s.afterCount(ctx, folded, rules); err != nil {
			return err
		}
	}
	if err := s.publishEvent(ctx, c, frame); err != nil {
		return err
	}
	if clock == nil {
		return nil
	}
	return s.publish(ctx, c, feed.FrameSubathon, e.ID+":subathon", clock)
}

// afterCount runs the effects that must not repeat for one event id and returns the subathon clock when the event moved it.
func (s *Service) afterCount(ctx context.Context, f folding, rules feed.SubathonRules) (*feed.Subathon, error) {
	userID, err := strconv.ParseUint(string(f.c), 10, 64)
	if err != nil {
		return nil, err
	}
	if err := s.Channels.AddFeedTotals(ctx, userID, f.delta); err != nil {
		return nil, err
	}
	return s.earnTime(ctx, f, rules)
}

func (s *Service) publishEvent(ctx context.Context, c channel, frame feed.Event) error {
	if err := s.publish(ctx, c, feed.FrameEvent, frame.ID, feed.Delivery{Event: frame}); err != nil {
		return err
	}
	return s.publishStats(ctx, c, frame.ID)
}
