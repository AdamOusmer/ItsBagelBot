// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"
	"errors"

	"github.com/google/uuid"

	"go.uber.org/zap"
)

const (
	loyaltyFlushInterval = 500 * time.Millisecond

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
	pub        bus.Publisher
	log        *zap.Logger
	done       chan struct{}
	finished   chan struct{}
	wake       chan struct{}
	wakeup     counterReporterWake
	earnWake   chan struct{}
	earnWakeup counterReporterWake

	now                    func() time.Time
	flushMu                sync.Mutex
	pending                counterPublications
	earnPending            counterPublications
	nextLog                time.Time
	mu                     sync.Mutex
	earn                   map[earnKey]*earnAgg
	bumps                  map[counterAgg]*bumpAgg
	earnNext, bumpNext     *idempotency.Handoff
	earnFlight, bumpFlight []*idempotency.Handoff
}

func NewLoyaltyReporter(pub bus.Publisher, log *zap.Logger) *LoyaltyReporter {
	r := &LoyaltyReporter{
		pub:      pub,
		log:      log,
		now:      time.Now,
		done:     make(chan struct{}),
		finished: make(chan struct{}),
		wake:     make(chan struct{}, 1),
		earnWake: make(chan struct{}, 1),
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
				r.wakeup.flush(r.flush)
			case <-r.earnWake:
				r.earnWakeup.flush(r.flush)
			case <-r.done:
				return
			}
		}
	}()
	return r
}

func (r *LoyaltyReporter) Earn(e PointEarning) {
	ctx := e.Context
	if ctx == nil {
		ctx = context.Background()
	}
	r.EarnContext(ctx, e)
}

func (r *LoyaltyReporter) EarnContext(ctx context.Context, e PointEarning) {
	if !e.validEarn() {
		return
	}
	key := earnKey{broadcasterID: e.BroadcasterID, viewerID: e.ViewerID}

	r.mu.Lock()
	joinGeneration(ctx, &r.earnNext)
	agg := r.earn[key]
	if agg == nil {
		agg = &earnAgg{}
		r.earn[key] = agg
	}
	agg.add(data.LoyaltyEarnEntry{ViewerLogin: e.Login, ViewerName: e.Name, Points: e.Points, WatchSeconds: e.WatchSeconds})

	overflow := len(r.earn) >= loyaltyMaxKeys
	r.mu.Unlock()

	if overflow {
		r.earnWakeup.nudge(r.earnWake)
	}
}

func (e PointEarning) validEarn() bool {
	return e.BroadcasterID != 0 && e.ViewerID != 0 && (e.Points != 0 || e.WatchSeconds != 0)
}

// Saturate before serialization so a producer window cannot wrap earned points.
func (agg *earnAgg) add(entry data.LoyaltyEarnEntry) {
	if entry.Points > 0 && agg.points > math.MaxInt64-entry.Points {
		agg.points = math.MaxInt64
	} else {
		agg.points += entry.Points
	}
	agg.watchSeconds += entry.WatchSeconds
	if entry.ViewerLogin != "" {
		agg.login = strings.Clone(entry.ViewerLogin)
	}
	if entry.ViewerName != "" {
		agg.name = strings.Clone(entry.ViewerName)
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
	r.BumpContext(context.Background(), target, delta)
}

func (r *LoyaltyReporter) BumpContext(ctx context.Context, target CounterBumpTarget, delta int64) {
	name, viewer := target.Name, target.Viewer
	if !target.acceptsDelta(counterDelta(delta)) {
		return
	}
	key := counterAgg{
		broadcasterID: target.BroadcasterID,
		name:          strings.Clone(name),
		scope:         strings.Clone(target.Scope),
		viewerID:      viewer.ID,
		command:       strings.Clone(target.Command),
	}

	r.mu.Lock()
	agg := r.bumps[key]
	if agg == nil {
		agg = &bumpAgg{}
		r.bumps[key] = agg
	}
	if !agg.acceptsDelta(counterDelta(delta)) {
		r.mu.Unlock()
		idempotency.Fail(ctx, errors.New("counter window exceeds signed integer range"))
		r.log.Error("counter window exceeds signed integer range")
		return
	}
	agg.delta += delta
	joinGeneration(ctx, &r.bumpNext)
	if viewer.Login != "" {
		agg.login = strings.Clone(viewer.Login)
	}
	if viewer.Name != "" {
		agg.name = strings.Clone(viewer.Name)
	}
	overflow := len(r.bumps) >= loyaltyMaxKeys
	r.mu.Unlock()

	if overflow {
		r.nudge()
	}
}

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
	r.wakeup.nudge(r.wake)
}

func (r *LoyaltyReporter) flush(ctx context.Context) {
	r.flushWith(ctx, counterPublicationAttempt{now: r.clock()})
}

func (r *LoyaltyReporter) flushWith(ctx context.Context, attempt counterPublicationAttempt) {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	defer r.reportBacklog()
	defer r.resolveGenerations(attempt)
	if !attempt.final {
		r.earnPending = r.earnPending.retry(ctx, r.pub, r.log, attempt)
		r.pending = r.pending.retry(ctx, r.pub, r.log, attempt)
		r.resolveGenerations(attempt)
	}
	earn, bumps := r.drain(attempt)
	r.publishEarned(earn)
	r.publishBumps(bumps)
	if attempt.final || len(earn) > 0 {
		r.earnPending = r.earnPending.retry(ctx, r.pub, r.log, attempt)
	}
	if attempt.final || len(bumps) > 0 {
		r.pending = r.pending.retry(ctx, r.pub, r.log, attempt)
	}
}

func (r *LoyaltyReporter) drain(attempt counterPublicationAttempt) (map[earnKey]*earnAgg, map[counterAgg]*bumpAgg) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var earn map[earnKey]*earnAgg
	if attempt.drains(r.earnPending) {
		earn = r.earn
	}
	var bumps map[counterAgg]*bumpAgg
	if attempt.drains(r.pending) {
		bumps = r.bumps
	}
	if len(earn) > 0 {
		r.earn = map[earnKey]*earnAgg{}
		transferGeneration(&r.earnFlight, &r.earnNext)
	} else {
		earn = nil
	}
	if len(bumps) > 0 {
		r.bumps = map[counterAgg]*bumpAgg{}
		transferGeneration(&r.bumpFlight, &r.bumpNext)
	} else {
		bumps = nil
	}
	return earn, bumps
}

func joinGeneration(ctx context.Context, generation **idempotency.Handoff) {
	if *generation == nil {
		*generation = &idempotency.Handoff{}
	}
	idempotency.Join(ctx, *generation)
	bus.HoldDelivery(ctx)
}

func transferGeneration(flights *[]*idempotency.Handoff, next **idempotency.Handoff) {
	if *next != nil {
		*flights = append(*flights, *next)
	}
	*next = nil
}

func settleGeneration(generations *[]*idempotency.Handoff, pending counterPublications, attempt counterPublicationAttempt) {
	if len(pending) > 0 && !failedAttempt(pending, attempt.now) {
		return
	}
	for _, generation := range *generations {
		reportGeneration(generation, len(pending))
	}
	if len(pending) == 0 {
		*generations = nil
	}
}

func failedAttempt(pending counterPublications, now time.Time) bool {
	for _, publication := range pending {
		if publication.lastAttempt.Equal(now) {
			return true
		}
	}
	return false
}

func reportGeneration(generation *idempotency.Handoff, pending int) {
	if pending > 0 {
		generation.Notify(errors.New("DATA handoff unconfirmed"))
		return
	}
	generation.Resolve(nil)
}

func (r *LoyaltyReporter) resolveGenerations(attempt counterPublicationAttempt) {
	settleGeneration(&r.earnFlight, r.earnPending, attempt)
	settleGeneration(&r.bumpFlight, r.pending, attempt)
}

func (r *LoyaltyReporter) backlog() counterReporterBacklog {
	r.mu.Lock()
	defer r.mu.Unlock()
	return counterReporterBacklog{bumps: len(r.bumps), earn: len(r.earn), pending: len(r.pending) + len(r.earnPending)}
}

func (r *LoyaltyReporter) reportBacklog() {
	backlog := r.backlog()
	r.wakeup.pending.Store(len(r.pending) > 0)
	r.earnWakeup.pending.Store(len(r.earnPending) > 0)
	backlog.log(r.log, &r.nextLog, r.clock())
}

func (r *LoyaltyReporter) clock() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}

func (r *LoyaltyReporter) publishEarned(earn map[earnKey]*earnAgg) {
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
	publishPerUser(r, perUser, data.SubjectLoyaltyEarned, func(userID uint64, chunk []data.LoyaltyEarnEntry) any {
		return data.LoyaltyEarnedDTO{BatchID: uuid.NewString(), UserID: userID, Entries: chunk}
	})
}

func (r *LoyaltyReporter) publishBumps(bumps map[counterAgg]*bumpAgg) {
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
	publishPerUser(r, perUser, data.SubjectLoyaltyCounters, func(userID uint64, chunk []data.CounterBumpEntry) any {
		return data.CounterBumpedDTO{BatchID: uuid.NewString(), UserID: userID, Bumps: chunk}
	})
}

func publishPerUser[E any](r *LoyaltyReporter, perUser map[uint64][]E, subject string, wrap func(uint64, []E) any) {
	for userID, entries := range perUser {
		for start := 0; start < len(entries); start += loyaltyChunk {
			chunk := entries[start:min(start+loyaltyChunk, len(entries))]
			payload := wrap(userID, chunk)
			if subject == data.SubjectLoyaltyCounters {
				r.pending = append(r.pending, counterPublication{
					id: payload.(data.CounterBumpedDTO).BatchID, subject: subject, payload: payload,
					entries: len(chunk), firstAttempt: r.clock(),
				})
				continue
			}
			r.earnPending = append(r.earnPending, counterPublication{
				id: payload.(data.LoyaltyEarnedDTO).BatchID, subject: subject, payload: payload,
				entries: len(chunk), firstAttempt: r.clock(),
			})
		}
	}
}

func (r *LoyaltyReporter) Close() {
	close(r.done)
	<-r.finished
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.flushWith(ctx, counterPublicationAttempt{now: r.clock(), final: true})
	r.backlog().shutdown(r.log)
}
