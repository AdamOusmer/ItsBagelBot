// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"maps"
	"slices"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/valkey"
)

type Refusal struct {
	Round  Round
	Viewer ViewerID
	Reason string
	Notice *RefusedViewer
}

type RefusedViewer struct {
	Login, Reason string
	Need          int64
}

func (s *Store) Refuse(ctx context.Context, req Refusal) (bool, int64, error) {
	r := req.Round
	wire := valkey.Refusal{Viewer: string(req.Viewer), Reason: req.Reason, TTL: roundTTL,
		Channel: uint64(r.Channel), RoundID: string(r.ID), Wake: wakeChannel, BatchAfter: Pins.RefusalInterval}
	notice, err := req.encodedNotice()
	if err != nil {
		return false, 0, err
	}
	wire.Notice = notice
	return keysFor(r.Channel, r.ID).Refuse(ctx, s.client, wire)
}

func (req Refusal) encodedNotice() (string, error) {
	if req.Notice == nil {
		return "", nil
	}
	return codec.MarshalToString(req.Notice)
}

func (data snapshot) refusalAt() time.Time {
	return fromMillis(parseInt(data.stored.Round["refusal_at"]))
}

func (s *Store) flushRefusals(ctx context.Context, data snapshot, now time.Time) error {
	at := data.refusalAt()
	if at.IsZero() || data.refusalsStillBatching(at, now) {
		return nil
	}
	r := data.round
	keys := keysFor(r.Channel, r.ID)
	raw, err := keys.ReadRefusals(ctx, s.client)
	if err != nil {
		return err
	}
	viewers, err := decodeRefusedViewers(raw)
	if err != nil {
		return err
	}
	if err := s.announceRefusals(ctx, r, viewers); err != nil {
		return err
	}
	return keys.AckRefusals(ctx, s.client, slices.Collect(maps.Keys(raw)))
}

func (data snapshot) refusalsStillBatching(at, now time.Time) bool {
	return data.round.Phase == PhaseOpen && at.After(now)
}

func decodeRefusedViewers(raw map[string]string) ([]RefusedViewer, error) {
	viewers := make([]RefusedViewer, 0, len(raw))
	for _, entry := range raw {
		var viewer RefusedViewer
		if err := codec.Unmarshal([]byte(entry), &viewer); err != nil {
			return nil, err
		}
		viewers = append(viewers, viewer)
	}
	return viewers, nil
}

func (s *Store) announceRefusals(ctx context.Context, r Round, viewers []RefusedViewer) error {
	if len(viewers) == 0 {
		return nil
	}
	return s.cfg.Announcer.Announce(ctx, Notice{Event: NoticeRefusals, Round: r, Refused: viewers})
}
