// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"

	"github.com/google/uuid"

	"go.uber.org/zap"
)

const (
	loyaltyFlushInterval = 5 * time.Second

	loyaltyMaxKeys = 8192

	loyaltyChunk = 1000
)

type earnKey struct {
	broadcasterID uint64
	viewerID      uint64
}

type earnAgg struct {
	points       int64
	watchSeconds uint64
	login        string
	name         string
}

type counterAgg struct {
	broadcasterID uint64
	name          string
	scope         string
	viewerID      uint64
	command       string
}

type bumpAgg struct {
	delta int64
	login string
	name  string
}

type LoyaltyReporter struct {
	pub      bus.Publisher
	log      *zap.Logger
	done     chan struct{}
	finished chan struct{}
	wake     chan struct{}

	flushMu sync.Mutex
	pending []counterPublication
	mu      sync.Mutex
	earn    map[earnKey]*earnAgg
	bumps   map[counterAgg]*bumpAgg
}

func NewLoyaltyReporter(pub bus.Publisher, log *zap.Logger) *LoyaltyReporter {
	r := &LoyaltyReporter{
		pub:      pub,
		log:      log,
		done:     make(chan struct{}),
		finished: make(chan struct{}),
		wake:     make(chan struct{}, 1),
		earn:     map[earnKey]*earnAgg{},
		bumps:    map[counterAgg]*bumpAgg{},
	}
	go func() {
		defer close(r.finished)
		ticker := time.NewTicker(loyaltyFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.flush(context.Background())
			case <-r.wake:
				r.flush(context.Background())
			case <-r.done:
				return
			}
		}
	}()
	return r
}

func (r *LoyaltyReporter) Earn(broadcasterID, viewerID uint64, login, name string, points int64, watchSeconds uint64) {
	if broadcasterID == 0 || viewerID == 0 || (points == 0 && watchSeconds == 0) {
		return
	}
	key := earnKey{broadcasterID: broadcasterID, viewerID: viewerID}

	r.mu.Lock()
	agg := r.earn[key]
	if agg == nil {
		agg = &earnAgg{}
		r.earn[key] = agg
	}
	agg.points += points
	agg.watchSeconds += watchSeconds
	if login != "" {
		agg.login = login
	}
	if name != "" {
		agg.name = name
	}
	overflow := len(r.earn) >= loyaltyMaxKeys
	r.mu.Unlock()

	if overflow {
		r.nudge()
	}
}

type CounterBumpTarget struct {
	BroadcasterID uint64
	Name          string
	Scope         string
	Viewer        Viewer
	Command       string
}

func ChannelBump(broadcasterID uint64, name string) CounterBumpTarget {
	return CounterBumpTarget{BroadcasterID: broadcasterID, Name: name, Scope: data.CounterScopeChannel}
}

func BotBump(name string) CounterBumpTarget {
	return CounterBumpTarget{Name: name, Scope: data.CounterScopeBot}
}

func (r *LoyaltyReporter) BumpBot(name string, delta int64) {
	r.Bump(BotBump(name), delta)
}

func (r *LoyaltyReporter) BumpChannel(broadcasterID uint64, name string, delta int64) {
	r.Bump(ChannelBump(broadcasterID, name), delta)
}

func (r *LoyaltyReporter) Bump(target CounterBumpTarget, delta int64) {
	name, viewer := target.Name, target.Viewer
	if !target.acceptsDelta(counterDelta(delta)) {
		return
	}
	key := counterAgg{
		broadcasterID: target.BroadcasterID,
		name:          name,
		scope:         target.Scope,
		viewerID:      viewer.ID,
		command:       target.Command,
	}

	r.mu.Lock()
	agg := r.bumps[key]
	if agg == nil {
		agg = &bumpAgg{}
		r.bumps[key] = agg
	}
	if !agg.acceptsDelta(counterDelta(delta)) {
		r.mu.Unlock()
		r.log.Error("counter window exceeds signed integer range")
		return
	}
	agg.delta += delta
	if viewer.Login != "" {
		agg.login = viewer.Login
	}
	if viewer.Name != "" {
		agg.name = viewer.Name
	}
	overflow := len(r.bumps) >= loyaltyMaxKeys
	r.mu.Unlock()

	if overflow {
		r.nudge()
	}
}

// counterDelta is a signed change, including decrements for custom counters.
type counterDelta int64

func (target CounterBumpTarget) acceptsDelta(delta counterDelta) bool {
	if target.Name == "" {
		return false
	}
	if delta == 0 || delta < counterDelta(-data.MaxCounter) {
		return false
	}
	if data.SystemCounter(target.Name) && delta < 0 {
		return false
	}
	return (target.BroadcasterID == 0) == (target.Scope == data.CounterScopeBot)
}

func (agg *bumpAgg) acceptsDelta(delta counterDelta) bool {
	if delta > 0 {
		return agg.delta <= data.MaxCounter-int64(delta)
	}
	return agg.delta >= -data.MaxCounter-int64(delta)
}

func (r *LoyaltyReporter) nudge() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *LoyaltyReporter) flush(ctx context.Context) {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	r.pending = retryCounterPublications(ctx, r.pub, r.log, r.pending)
	r.mu.Lock()
	earn := r.earn
	var bumps map[counterAgg]*bumpAgg
	if len(r.pending) == 0 {
		bumps = r.bumps
	}
	if len(earn) > 0 {
		r.earn = map[earnKey]*earnAgg{}
	}
	if len(bumps) > 0 {
		r.bumps = map[counterAgg]*bumpAgg{}
	}
	r.mu.Unlock()

	r.publishEarned(ctx, earn)
	r.publishBumps(ctx, bumps)
	r.pending = retryCounterPublications(ctx, r.pub, r.log, r.pending)
}

func (r *LoyaltyReporter) publishEarned(ctx context.Context, earn map[earnKey]*earnAgg) {
	perUser := map[uint64][]data.LoyaltyEarnEntry{}
	for key, agg := range earn {
		perUser[key.broadcasterID] = append(perUser[key.broadcasterID], data.LoyaltyEarnEntry{
			ViewerID:     key.viewerID,
			ViewerLogin:  agg.login,
			ViewerName:   agg.name,
			Points:       agg.points,
			WatchSeconds: agg.watchSeconds,
		})
	}
	publishPerUser(ctx, r, perUser, data.SubjectLoyaltyEarned, func(userID uint64, chunk []data.LoyaltyEarnEntry) any {
		return data.LoyaltyEarnedDTO{UserID: userID, Entries: chunk}
	})
}

func (r *LoyaltyReporter) publishBumps(ctx context.Context, bumps map[counterAgg]*bumpAgg) {
	perUser := map[uint64][]data.CounterBumpEntry{}
	for key, agg := range bumps {
		perUser[key.broadcasterID] = append(perUser[key.broadcasterID], data.CounterBumpEntry{
			Name:        key.name,
			Scope:       key.scope,
			ViewerID:    key.viewerID,
			ViewerLogin: agg.login,
			ViewerName:  agg.name,
			Command:     key.command,
			Delta:       agg.delta,
		})
	}
	publishPerUser(ctx, r, perUser, data.SubjectLoyaltyCounters, func(userID uint64, chunk []data.CounterBumpEntry) any {
		return data.CounterBumpedDTO{BatchID: uuid.NewString(), UserID: userID, Bumps: chunk}
	})
}

func publishPerUser[E any](ctx context.Context, r *LoyaltyReporter, perUser map[uint64][]E, subject string, wrap func(uint64, []E) any) {
	for userID, entries := range perUser {
		for start := 0; start < len(entries); start += loyaltyChunk {
			chunk := entries[start:min(start+loyaltyChunk, len(entries))]
			payload := wrap(userID, chunk)
			if subject == data.SubjectLoyaltyCounters {
				r.pending = append(r.pending, counterPublication{id: payload.(data.CounterBumpedDTO).BatchID, subject: subject, payload: payload})
				continue
			}
			if err := bus.PublishJSON(ctx, r.pub, subject, payload); err != nil {
				r.log.Debug("failed to publish loyalty window",
					zap.String("subject", subject),
					module.BIDField(userID),
					zap.Int("entries", len(chunk)),
					zap.Error(err),
				)
			}
		}
	}
}

func (r *LoyaltyReporter) Close() {
	close(r.done)
	<-r.finished
	r.flush(context.Background())
	if len(r.pending) > 0 {
		r.log.Error("counter batches remain unconfirmed at shutdown", zap.Int("batches", len(r.pending)))
	}
}
