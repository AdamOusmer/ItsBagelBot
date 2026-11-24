// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed

import (
	"context"
	"maps"
	"time"

	"ItsBagelBot/internal/domain/overlays/feed"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/valkey"
)

func (s *Service) clockDocument(c channel) valkey.CASDocument {
	return valkey.CASDocument{Key: c.subathon(), TTL: stateTTL}
}

// clockDoc is the stored timer; Applied holds the event ids that already earned time, by unix second, until their receipt window passes.
type clockDoc struct {
	feed.Subathon
	Applied map[string]int64 `json:"applied,omitempty"`
}

func (d *clockDoc) earn(eventID string, rules feed.SubathonRules, seconds int64, now time.Time) {
	cutoff := now.Add(-receiptTTL).Unix()
	maps.DeleteFunc(d.Applied, func(_ string, at int64) bool { return at < cutoff })
	if _, done := d.Applied[eventID]; done {
		return
	}
	if d.Applied == nil {
		d.Applied = map[string]int64{}
	}
	d.Applied[eventID] = now.Unix()
	d.Subathon = d.Subathon.Add(rules, seconds, now)
}

// editClock applies edit to the channel's stored timer; concurrent pods retry until their change lands on the latest value.
func (s *Service) editClock(ctx context.Context, c channel, edit func(*clockDoc)) (feed.Subathon, error) {
	var next clockDoc
	err := s.clockDocument(c).Update(ctx, s.Client, func(raw string) (string, error) {
		var current clockDoc
		if raw != "" {
			if err := codec.Unmarshal([]byte(raw), &current); err != nil {
				return "", err
			}
		}
		edit(&current)
		next = current
		encoded, err := codec.Marshal(current)
		return string(encoded), err
	})
	return next.Subathon, err
}

func (s *Service) updateClock(ctx context.Context, c channel, change func(feed.Subathon) feed.Subathon) (feed.Subathon, error) {
	return s.editClock(ctx, c, func(d *clockDoc) { d.Subathon = change(d.Subathon) })
}

func (s *Service) readClock(ctx context.Context, c channel) (feed.Subathon, error) {
	raw, err := s.clockDocument(c).Read(ctx, s.Client)
	var clock clockDoc
	if err != nil || raw == "" {
		return clock.Subathon, err
	}
	return clock.Subathon, codec.Unmarshal([]byte(raw), &clock)
}

func (s *Service) earnTime(ctx context.Context, f folding, rules feed.SubathonRules) (*feed.Subathon, error) {
	earned := rules.Earned(f.event)
	if earned == 0 {
		return nil, nil
	}
	clock, err := s.editClock(ctx, f.c, func(d *clockDoc) { d.earn(f.event.ID, rules, earned, time.Now()) })
	return &clock, err
}
