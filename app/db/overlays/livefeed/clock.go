// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/overlays/feed"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/valkey"
)

func (s *Service) clockDocument(c channel) valkey.CASDocument {
	return valkey.CASDocument{Key: c.subathon(), TTL: stateTTL}
}

// updateClock applies change to the channel's subathon timer; concurrent pods retry until their change lands on the latest value.
func (s *Service) updateClock(ctx context.Context, c channel, change func(feed.Subathon) feed.Subathon) (feed.Subathon, error) {
	var next feed.Subathon
	err := s.clockDocument(c).Update(ctx, s.Client, func(raw string) (string, error) {
		var current feed.Subathon
		if raw != "" {
			if err := codec.Unmarshal([]byte(raw), &current); err != nil {
				return "", err
			}
		}
		next = change(current)
		encoded, err := codec.Marshal(next)
		return string(encoded), err
	})
	return next, err
}

func (s *Service) readClock(ctx context.Context, c channel) (feed.Subathon, error) {
	raw, err := s.clockDocument(c).Read(ctx, s.Client)
	var clock feed.Subathon
	if err != nil || raw == "" {
		return clock, err
	}
	return clock, codec.Unmarshal([]byte(raw), &clock)
}

func (s *Service) earnTime(ctx context.Context, f folding, rules feed.SubathonRules) (*feed.Subathon, error) {
	earned := rules.Earned(f.event)
	if earned == 0 {
		return nil, nil
	}
	now := time.Now()
	clock, err := s.updateClock(ctx, f.c, func(current feed.Subathon) feed.Subathon { return current.Add(rules, earned, now) })
	return &clock, err
}
