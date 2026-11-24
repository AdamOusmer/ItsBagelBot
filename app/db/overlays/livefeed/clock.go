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

func decodeClock(raw string) (feed.Subathon, error) {
	var clock feed.Subathon
	if raw == "" {
		return clock, nil
	}
	return clock, codec.Unmarshal([]byte(raw), &clock)
}

func encodeClock(clock feed.Subathon) (string, error) {
	encoded, err := codec.Marshal(clock)
	return string(encoded), err
}

func (s *Service) updateClock(ctx context.Context, c channel, change func(feed.Subathon) feed.Subathon) (feed.Subathon, error) {
	var next feed.Subathon
	err := s.clockDocument(c).Update(ctx, s.Client, func(raw string) (string, error) {
		current, err := decodeClock(raw)
		if err != nil {
			return "", err
		}
		next = change(current)
		return encodeClock(next)
	})
	return next, err
}

func (s *Service) readClock(ctx context.Context, c channel) (feed.Subathon, error) {
	raw, err := s.clockDocument(c).Read(ctx, s.Client)
	if err != nil {
		return feed.Subathon{}, err
	}
	return decodeClock(raw)
}

func (s *Service) earnTime(ctx context.Context, f folding, rules feed.SubathonRules) (*feed.Subathon, error) {
	earned := rules.Earned(f.event)
	if earned == 0 {
		return nil, nil
	}
	var next feed.Subathon
	receipt := valkey.Key{Name: f.c.earned(f.event.ID), TTL: receiptTTL}
	applied, err := s.clockDocument(f.c).UpdateOnce(ctx, s.Client, receipt, func(raw string) (string, error) {
		current, err := decodeClock(raw)
		if err != nil {
			return "", err
		}
		next = current.Add(rules, earned, time.Now())
		return encodeClock(next)
	})
	if err != nil || applied {
		return &next, err
	}
	next, err = s.readClock(ctx, f.c)
	return &next, err
}
