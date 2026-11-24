// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/db/overlays/repository"
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

func (s *Service) Handle(ctx context.Context, e channelevents.Event) error {
	if e.Trial || e.Kind == channelevents.Chat {
		return nil
	}
	profile, ok, err := s.eligible(ctx, e.ChannelID)
	if err != nil || !ok {
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
	userID, err := strconv.ParseUint(string(c), 10, 64)
	if err != nil {
		return err
	}
	if err := s.Channels.PurgeFeedReceipts(ctx, userID, time.Now().Add(-repository.FeedReceiptTTL)); err != nil {
		return err
	}
	if err := s.publishState(ctx, c, feed.FrameCreditsReplace, map[feed.Kind][]feed.Leader{}); err != nil {
		return err
	}
	return s.publishStats(ctx, c)
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
	clock, err := s.countDurable(ctx, folded, rules)
	if err != nil {
		return err
	}
	if _, err := s.store().fold(ctx, folded); err != nil {
		return err
	}
	if err := s.store().remember(ctx, folded, frame); err != nil {
		return err
	}
	if err := s.publishEvent(ctx, folded, frame); err != nil {
		return err
	}
	if clock == nil {
		return nil
	}
	return s.publishState(ctx, c, feed.FrameSubathon, clock)
}

func (s *Service) countDurable(ctx context.Context, f folding, rules feed.SubathonRules) (*feed.Subathon, error) {
	userID, err := strconv.ParseUint(string(f.c), 10, 64)
	if err != nil {
		return nil, err
	}
	if err := s.Channels.AddFeedTotals(ctx, userID, f.event.ID, f.delta); err != nil {
		return nil, err
	}
	return s.earnTime(ctx, f, rules)
}

func (s *Service) publishEvent(ctx context.Context, f folding, frame feed.Event) error {
	if err := s.publishAlert(ctx, f.c, frame.ID, feed.Delivery{Event: frame}); err != nil {
		return err
	}
	if err := s.publishStats(ctx, f.c); err != nil {
		return err
	}
	return s.publishCredits(ctx, f)
}

func (s *Service) publishCredits(ctx context.Context, f folding) error {
	if len(f.delta.Credits) == 0 {
		return nil
	}
	touched, err := s.store().touchedCredits(ctx, f)
	if err != nil {
		return err
	}
	return s.publishState(ctx, f.c, feed.FrameCredits, touched)
}
