// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
)

const (
	boardSeedLimit = 100
	boardSeedRetry = time.Minute
)

var (
	boardCounters   = []projection.CounterName{data.CounterMessagesProcessed, data.CounterEventsProcessed}
	channelCounters = []projection.CounterName{
		data.CounterMessagesProcessed, data.CounterEventsProcessed,
		data.CounterCommandsAnswered, data.CounterModActionsTaken,
	}
)

var errLiveSeedRead = errors.New("live counter seed: loyalty read failed")

type liveCounterStore interface {
	ApplyLiveCounters(ctx context.Context, b projection.LiveBatch) (projection.LiveOutcome, error)
	SeedLiveCounters(ctx context.Context, userID uint64, values []projection.CounterValue, seededAt time.Time) error
	GetLiveCounters(ctx context.Context, userID uint64, names []projection.CounterName) (map[projection.CounterName]int64, bool, error)
	BoardSeeded(ctx context.Context, name projection.CounterName) (bool, error)
	SeedBoard(ctx context.Context, name projection.CounterName, entries []projection.BoardEntry) error
	DeleteLiveCounters(ctx context.Context, userID uint64, boards []projection.CounterName) error
}

type liveScope struct {
	scope  string
	names  []projection.CounterName
	boards bool
}

func liveScopeFor(userID uint64) liveScope {
	if userID == 0 {
		return liveScope{scope: data.CounterScopeBot, names: boardCounters}
	}
	return liveScope{scope: data.CounterScopeChannel, names: channelCounters, boards: true}
}

func (s liveScope) tracks(b data.CounterBumpEntry) bool {
	return b.Scope == s.scope && b.ViewerID == 0 && b.Command == ""
}

func (s liveScope) values(value func(name projection.CounterName) int64) []projection.CounterValue {
	out := make([]projection.CounterValue, 0, len(s.names))
	for _, name := range s.names {
		out = append(out, projection.CounterValue{
			Name: name, Value: value(name), Board: s.boards && slices.Contains(boardCounters, name),
		})
	}
	return out
}

func (s liveScope) deltas(bumps []data.CounterBumpEntry) []projection.CounterValue {
	sums := map[projection.CounterName]int64{}
	for _, b := range bumps {
		if s.tracks(b) {
			sums[projection.CounterName(b.Name)] += b.Delta
		}
	}
	return slices.DeleteFunc(s.values(func(name projection.CounterName) int64 { return sums[name] }),
		func(v projection.CounterValue) bool { return v.Value == 0 })
}

func (p *Projector) HandleCounterBumps(msg *bus.Message) error {
	var dto data.CounterBumpedDTO
	if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
		p.drop(msg, data.SubjectLoyaltyCounters, err)
		return nil
	}
	storedAt := msg.StoredAt()
	if storedAt.IsZero() {
		storedAt = time.Now()
	}
	return p.applyCounterBumps(msg.Context(), msg.UUID, storedAt, dto)
}

func (p *Projector) applyCounterBumps(ctx context.Context, msgID string, storedAt time.Time, dto data.CounterBumpedDTO) error {
	deltas := liveScopeFor(dto.UserID).deltas(dto.Bumps)
	if p.live == nil || len(deltas) == 0 {
		return nil
	}
	out, err := p.live.ApplyLiveCounters(ctx, projection.LiveBatch{
		UserID: dto.UserID, MsgID: msgID, StoredAt: storedAt, Deltas: deltas,
	})
	if err != nil || out != projection.LiveNeedsSeed {
		return err
	}
	return p.seedLiveCounters(ctx, dto.UserID)
}

func (p *Projector) seedLiveCounters(ctx context.Context, userID uint64) error {
	if p.loyalty == nil {
		return errLiveSeedRead
	}
	uid := strconv.FormatUint(userID, 10)
	scope := liveScopeFor(userID)
	read := make(map[projection.CounterName]int64, len(scope.names))
	for _, name := range scope.names {
		v, ok := p.loyalty.get(ctx, uid, string(name))
		if !ok {
			return errLiveSeedRead
		}
		read[name] = v
	}
	// Stamped after the reads: a batch the reads may already include is skipped, never counted twice.
	seededAt := time.Now()
	return p.live.SeedLiveCounters(ctx, userID, scope.values(func(name projection.CounterName) int64 { return read[name] }), seededAt)
}

func (p *Projector) liveTotals(ctx context.Context, userID uint64, names []projection.CounterName) (map[projection.CounterName]int64, bool) {
	if p.live == nil {
		return nil, false
	}
	values, seeded, err := p.live.GetLiveCounters(ctx, userID, names)
	if err != nil {
		return nil, false
	}
	if seeded {
		return values, true
	}
	if p.seedLiveCounters(ctx, userID) != nil {
		return nil, false
	}
	values, seeded, err = p.live.GetLiveCounters(ctx, userID, names)
	return values, seeded && err == nil
}

func (p *Projector) SeedBoards(ctx context.Context) {
	if p.live == nil || p.loyalty == nil {
		return
	}
	for _, name := range boardCounters {
		for !p.seedBoard(ctx, name) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(boardSeedRetry):
			}
		}
	}
}

func (p *Projector) seedBoard(ctx context.Context, name projection.CounterName) bool {
	done, err := p.live.BoardSeeded(ctx, name)
	if err != nil {
		return false
	}
	if done {
		return true
	}
	rows, ok := p.loyalty.board(ctx, string(name), boardSeedLimit)
	if !ok {
		return false
	}
	entries := make([]projection.BoardEntry, 0, len(rows))
	for _, row := range rows {
		if id, err := strconv.ParseUint(row.UserID, 10, 64); err == nil {
			entries = append(entries, projection.BoardEntry{UserID: id, Value: row.Value})
		}
	}
	return p.live.SeedBoard(ctx, name, entries) == nil
}
