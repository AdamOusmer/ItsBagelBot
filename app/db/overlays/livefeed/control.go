// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed

import (
	"context"
	"crypto/rand"
	"time"

	"ItsBagelBot/internal/domain/overlays/feed"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"
)

var testBucket = ratelimit.NewSpec(5, 1)

// Control runs one dashboard action on the channel's overlay feed.
func (s *Service) Control(ctx context.Context, req overlaysrpc.FeedControlRequest) (overlaysrpc.FeedControlReply, error) {
	if req.Actor != req.UserID {
		return overlaysrpc.FeedControlReply{}, overlaysrpc.ErrFeedNotOwner
	}
	if err := s.requireAlpha(ctx, req.UserID); err != nil {
		return overlaysrpc.FeedControlReply{}, err
	}
	c := channel(req.UserID)
	if err := s.act(ctx, c, req); err != nil {
		return overlaysrpc.FeedControlReply{}, err
	}
	control, err := s.readControl(ctx, c)
	if err != nil {
		return overlaysrpc.FeedControlReply{}, err
	}
	clock, err := s.readClock(ctx, c)
	return overlaysrpc.FeedControlReply{Control: control, Subathon: clock}, err
}

func (s *Service) act(ctx context.Context, c channel, req overlaysrpc.FeedControlRequest) error {
	switch req.Action {
	case overlaysrpc.FeedPause, overlaysrpc.FeedResume, overlaysrpc.FeedMute, overlaysrpc.FeedUnmute, overlaysrpc.FeedSkip:
		return s.setControl(ctx, c, controlChanges[req.Action])
	case overlaysrpc.FeedClear:
		return s.clear(ctx, c)
	case overlaysrpc.FeedReplay:
		return s.replay(ctx, c, req.Args.EventID)
	case overlaysrpc.FeedTest:
		return s.test(ctx, c, req.Args)
	case overlaysrpc.FeedSubathonAdd, overlaysrpc.FeedSubathonPause, overlaysrpc.FeedSubathonResume, overlaysrpc.FeedSubathonReset:
		return s.setClock(ctx, c, req)
	}
	return overlaysrpc.ErrFeedUnknownAction
}

var controlChanges = map[overlaysrpc.FeedAction]func(*feed.Control){
	overlaysrpc.FeedPause:  func(c *feed.Control) { c.Paused = true },
	overlaysrpc.FeedResume: func(c *feed.Control) { c.Paused = false },
	overlaysrpc.FeedMute:   func(c *feed.Control) { c.Muted = true },
	overlaysrpc.FeedUnmute: func(c *feed.Control) { c.Muted = false },
	overlaysrpc.FeedSkip:   func(c *feed.Control) { c.Skip++ },
}

func (s *Service) setControl(ctx context.Context, c channel, change func(*feed.Control)) error {
	var next feed.Control
	err := s.controlDocument(c).Update(ctx, s.Client, func(raw string) (string, error) {
		var current feed.Control
		if raw != "" {
			if err := codec.Unmarshal([]byte(raw), &current); err != nil {
				return "", err
			}
		}
		change(&current)
		next = current
		encoded, err := codec.Marshal(current)
		return string(encoded), err
	})
	if err != nil {
		return err
	}
	return s.publishState(ctx, c, feed.FrameControl, next)
}

// clear forgets the session's logged events and tells the overlay to drop what it is showing.
func (s *Service) clear(ctx context.Context, c channel) error {
	session, err := s.store().session(ctx, c)
	if err != nil {
		return err
	}
	if err := s.store().clearRecent(ctx, c, session); err != nil {
		return err
	}
	return s.setControl(ctx, c, controlChanges[overlaysrpc.FeedSkip])
}

func (s *Service) replay(ctx context.Context, c channel, eventID string) error {
	session, err := s.store().session(ctx, c)
	if err != nil {
		return err
	}
	events, err := s.store().recent(ctx, c, session, 0)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.ID == eventID {
			return s.publishAlert(ctx, c, nonce("replay"), feed.Delivery{Event: event, Replay: true})
		}
	}
	return overlaysrpc.ErrFeedUnknownEvent
}

func (s *Service) test(ctx context.Context, c channel, args overlaysrpc.FeedControlArgs) error {
	var fields feed.Fields
	if args.Fields != nil {
		fields = *args.Fields
	}
	event := feed.Emulated(args.Kind, fields)
	if args.Kind == feed.Chat || event.Validate() != nil {
		return overlaysrpc.ErrFeedInvalidArgs
	}
	allowed, err := s.Allow(ctx, testBucket.ForDynamicKey("ratelimit:overlays:feed-test:", "overlays:feed-test", string(c)))
	if err != nil {
		return err
	}
	if !allowed {
		return overlaysrpc.ErrFeedThrottled
	}
	return s.publishAlert(ctx, c, event.ID, feed.Delivery{Event: event, Test: true})
}

func (s *Service) setClock(ctx context.Context, c channel, req overlaysrpc.FeedControlRequest) error {
	if req.Action == overlaysrpc.FeedSubathonAdd && req.Args.Seconds == 0 {
		return overlaysrpc.ErrFeedInvalidArgs
	}
	profile, err := s.profile(ctx, req.UserID)
	if err != nil {
		return err
	}
	now := time.Now()
	clock, err := s.updateClock(ctx, c, func(current feed.Subathon) feed.Subathon {
		return clockChange(req, profile.Subathon, current, now)
	})
	if err != nil {
		return err
	}
	return s.publishState(ctx, c, feed.FrameSubathon, clock)
}

func clockChange(req overlaysrpc.FeedControlRequest, rules feed.SubathonRules, current feed.Subathon, now time.Time) feed.Subathon {
	switch req.Action {
	case overlaysrpc.FeedSubathonAdd:
		return current.Add(rules, req.Args.Seconds, now)
	case overlaysrpc.FeedSubathonPause:
		return current.SetRunning(rules, false, now)
	case overlaysrpc.FeedSubathonResume:
		return current.SetRunning(rules, true, now)
	}
	return current.Reset(rules, now)
}

func nonce(kind string) string { return kind + ":" + rand.Text() }
