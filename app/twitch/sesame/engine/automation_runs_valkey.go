// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	runLogTTL          = 7 * 24 * time.Hour
	runRateTTL         = 120 * time.Second
	runLogKeyPrefix    = "automation:runs:"
	runRatePrefix      = "automation:rate:"
	runLogBuffer       = 1024
	runLogBatch        = 256
	runLogFlushTimeout = 2 * time.Second
	runLogCloseTimeout = 5 * time.Second
	runLeaseSize       = 4
	runLeaseIdle       = 5 * time.Second
	runLeaseTimeout    = 2 * time.Second
)

var reserveRuns = pkg_valkey.NewScript(`
local count = tonumber(redis.call('GET', KEYS[1]) or '0')
local grant = math.max(math.min(tonumber(ARGV[1]), tonumber(ARGV[2]) - count), 0)
if grant > 0 then
  count = redis.call('INCRBY', KEYS[1], grant)
end
redis.call('EXPIRE', KEYS[1], ARGV[3])
return {grant, count}
`)

func runLogKey(broadcasterID uint64) string {
	return runLogKeyPrefix + "{" + strconv.FormatUint(broadcasterID, 10) + "}"
}

func runRateKey(broadcasterID uint64, at time.Time) string {
	return runRatePrefix + "{" + strconv.FormatUint(broadcasterID, 10) + "}:" + strconv.FormatInt(at.Unix()/60, 10)
}

type queuedRun struct {
	broadcasterID uint64
	rec           automationrpc.RunRecord
}

type ValkeyRunLog struct {
	client valkey.Client
	log    *zap.Logger

	mu     sync.Mutex
	ring   []queuedRun
	head   int
	size   int
	closed bool

	dropped  atomic.Uint64
	wake     chan struct{}
	drainReq chan chan struct{}
	done     chan struct{}
	finished chan struct{}
	stop     sync.Once

	flushMu  sync.Mutex
	batch    []queuedRun
	reported uint64
}

func NewValkeyRunLog(client valkey.Client, log *zap.Logger) *ValkeyRunLog {
	l := newValkeyRunLog(client, log, runLogBuffer)
	go l.run()
	return l
}

func newValkeyRunLog(client valkey.Client, log *zap.Logger, capacity int) *ValkeyRunLog {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyRunLog{
		client:   pkg_valkey.PrimaryThroughput(client),
		log:      log,
		ring:     make([]queuedRun, capacity),
		wake:     make(chan struct{}, 1),
		drainReq: make(chan chan struct{}),
		done:     make(chan struct{}),
		finished: make(chan struct{}),
		batch:    make([]queuedRun, runLogBatch),
	}
}

func (l *ValkeyRunLog) Record(_ context.Context, broadcasterID uint64, rec automationrpc.RunRecord) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		l.dropped.Add(1)
		return
	}
	if l.size == len(l.ring) {
		l.head = (l.head + 1) % len(l.ring)
		l.size--
		l.dropped.Add(1)
	}
	l.ring[(l.head+l.size)%len(l.ring)] = queuedRun{broadcasterID: broadcasterID, rec: rec}
	l.size++
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

func (l *ValkeyRunLog) run() {
	defer close(l.finished)
	for {
		select {
		case <-l.wake:
			l.drainAll(context.Background())
		case ack := <-l.drainReq:
			l.drainAll(context.Background())
			close(ack)
		case <-l.done:
			return
		}
	}
}

func (l *ValkeyRunLog) Close() {
	l.stop.Do(func() {
		l.mu.Lock()
		l.closed = true
		l.mu.Unlock()
		close(l.done)
		<-l.finished
		ctx, cancel := context.WithTimeout(context.Background(), runLogCloseTimeout)
		defer cancel()
		l.drainAll(ctx)
	})
}

func (l *ValkeyRunLog) drainAll(ctx context.Context) {
	l.flushMu.Lock()
	defer l.flushMu.Unlock()
	for ctx.Err() == nil && l.flushBatch(ctx) {
	}
	if total := l.dropped.Load(); total != l.reported {
		l.log.Warn("automations: run records dropped", zap.Uint64("dropped", total-l.reported))
		l.reported = total
	}
}

func (l *ValkeyRunLog) pop() []queuedRun {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := min(l.size, len(l.batch))
	for i := range n {
		slot := &l.ring[l.head]
		l.batch[i] = *slot
		*slot = queuedRun{}
		l.head = (l.head + 1) % len(l.ring)
	}
	l.size -= n
	return l.batch[:n]
}

func (l *ValkeyRunLog) flushBatch(parent context.Context) bool {
	batch := l.pop()
	if len(batch) == 0 {
		return false
	}
	cmds := l.encode(batch)
	clear(batch)
	l.write(parent, cmds)
	return true
}

func (l *ValkeyRunLog) encode(batch []queuedRun) valkey.Commands {
	cmds := make(valkey.Commands, 0, len(batch)+8)
	var keys []uint64
	for _, q := range batch {
		push, ok := l.pushCommand(q)
		if !ok {
			continue
		}
		cmds = append(cmds, push)
		if !slices.Contains(keys, q.broadcasterID) {
			keys = append(keys, q.broadcasterID)
		}
	}
	for _, id := range keys {
		key := runLogKey(id)
		cmds = append(cmds,
			l.client.B().Ltrim().Key(key).Start(0).Stop(automation.RunsKept-1).Build(),
			l.client.B().Expire().Key(key).Seconds(int64(runLogTTL.Seconds())).Build(),
		)
	}
	return cmds
}

func (l *ValkeyRunLog) pushCommand(q queuedRun) (valkey.Completed, bool) {
	body, err := codec.Marshal(q.rec)
	if err != nil {
		l.log.Warn("automations: run record encode failed", zap.Error(err))
		return valkey.Completed{}, false
	}
	return l.client.B().Lpush().Key(runLogKey(q.broadcasterID)).Element(string(body)).Build(), true
}

func (l *ValkeyRunLog) write(parent context.Context, cmds valkey.Commands) {
	if len(cmds) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(parent, runLogFlushTimeout)
	defer cancel()
	for _, res := range l.client.DoMulti(ctx, cmds...) {
		if err := res.Error(); err != nil {
			l.log.Warn("automations: run record write failed", zap.Error(err))
			return
		}
	}
}

func (l *ValkeyRunLog) drain(ctx context.Context) error {
	ack := make(chan struct{})
	select {
	case l.drainReq <- ack:
	case <-l.finished:
		l.drainAll(ctx)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *ValkeyRunLog) Recent(ctx context.Context, broadcasterID uint64, automationID string, limit int) ([]automationrpc.RunRecord, error) {
	if limit <= 0 || limit > automation.RunsKept {
		limit = automation.RunsKept
	}
	if err := l.drain(ctx); err != nil {
		return nil, err
	}
	// Primary view on purpose: a replica read can miss what drain just flushed.
	raw, err := l.client.Do(ctx, l.client.B().Lrange().Key(runLogKey(broadcasterID)).Start(0).Stop(-1).Build()).AsStrSlice()
	if err != nil {
		return nil, err
	}
	return runFilter{automationID: automationID, limit: limit}.apply(raw), nil
}

type runFilter struct {
	automationID string
	limit        int
}

func (f runFilter) apply(raw []string) []automationrpc.RunRecord {
	out := make([]automationrpc.RunRecord, 0, f.limit)
	for _, item := range raw {
		rec, ok := f.decode(item)
		if !ok {
			continue
		}
		out = append(out, rec)
		if len(out) == f.limit {
			break
		}
	}
	return out
}

func (f runFilter) decode(item string) (automationrpc.RunRecord, bool) {
	var rec automationrpc.RunRecord
	if codec.Unmarshal([]byte(item), &rec) != nil {
		return rec, false
	}
	return rec, f.automationID == "" || rec.AutomationID == f.automationID
}

type runLease struct {
	left      int
	closed    bool
	refused   bool
	touchedAt time.Time
	recheckAt time.Time
}

type ValkeyRunLimiter struct {
	client valkey.Client
	now    func() time.Time

	mu     sync.Mutex
	window int64
	leases map[uint64]runLease

	done     chan struct{}
	finished chan struct{}
	stop     sync.Once
}

func NewValkeyRunLimiter(client valkey.Client) *ValkeyRunLimiter {
	l := newValkeyRunLimiter(client)
	go l.run()
	return l
}

func newValkeyRunLimiter(client valkey.Client) *ValkeyRunLimiter {
	return &ValkeyRunLimiter{
		client:   pkg_valkey.Primary(client),
		now:      time.Now,
		leases:   map[uint64]runLease{},
		done:     make(chan struct{}),
		finished: make(chan struct{}),
	}
}

func (l *ValkeyRunLimiter) run() {
	defer close(l.finished)
	ticker := time.NewTicker(runLeaseIdle)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.sweep(context.Background(), time.Now(), runLeaseIdle)
		case <-l.done:
			return
		}
	}
}

func (l *ValkeyRunLimiter) Close() {
	l.stop.Do(func() {
		close(l.done)
		<-l.finished
		l.sweep(context.Background(), l.now(), 0)
	})
}

type returnedLease struct {
	key string
	n   int64
}

func (l *ValkeyRunLimiter) sweep(ctx context.Context, at time.Time, idle time.Duration) {
	returns := l.takeIdle(at, idle)
	if len(returns) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, runLeaseTimeout)
	defer cancel()
	cmds := make(valkey.Commands, len(returns))
	for i, r := range returns {
		cmds[i] = l.client.B().Decrby().Key(r.key).Decrement(r.n).Build()
	}
	l.client.DoMulti(ctx, cmds...)
}

func (l *ValkeyRunLimiter) takeIdle(at time.Time, idle time.Duration) []returnedLease {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.window != (runSlot{at: at}).window() {
		return nil
	}
	var returns []returnedLease
	for id, lease := range l.leases {
		if lease.left == 0 || at.Sub(lease.touchedAt) < idle {
			continue
		}
		returns = append(returns, returnedLease{key: runRateKey(id, at), n: int64(lease.left)})
		l.leases[id] = runLease{touchedAt: at}
	}
	return returns
}

type runSlot struct {
	broadcasterID uint64
	at            time.Time
}

func (s runSlot) window() int64 { return s.at.Unix() / 60 }

func (l *ValkeyRunLimiter) AllowRun(ctx context.Context, broadcasterID uint64) (bool, error) {
	slot := runSlot{broadcasterID: broadcasterID, at: l.now()}
	allowed, decided := l.spend(slot)
	if decided {
		return allowed, nil
	}
	return l.reserve(ctx, slot)
}

// Matches gossip's per-channel Govee budget so a flooding recipe stops here, before an RPC per step.
const (
	maxLightsPerMinute = 8
	lightsWindow       = time.Minute
)

func (l *ValkeyRunLimiter) AllowLights(ctx context.Context, broadcasterID uint64) (bool, error) {
	window := strconv.FormatInt(l.now().Unix()/int64(lightsWindow.Seconds()), 10)
	key := "automation:lights:{" + strconv.FormatUint(broadcasterID, 10) + "}:" + window
	used, err := pkg_valkey.Incr(ctx, l.client, key, lightsWindow)
	return used <= maxLightsPerMinute, err
}

func (l *ValkeyRunLimiter) FirstRefusal(broadcasterID uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	lease := l.leases[broadcasterID]
	if lease.refused || !lease.closed {
		return false
	}
	if l.window != (runSlot{at: l.now()}).window() {
		return false
	}
	lease.refused = true
	l.leases[broadcasterID] = lease
	return true
}

func (l *ValkeyRunLimiter) spend(slot runSlot) (allowed, decided bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if window := slot.window(); l.window != window {
		clear(l.leases)
		l.window = window
	}
	lease := l.leases[slot.broadcasterID]
	switch {
	case lease.left > 0:
		lease.left--
		lease.touchedAt = slot.at
		l.leases[slot.broadcasterID] = lease
		return true, true
	case lease.closed && slot.at.Before(lease.recheckAt):
		return false, true
	}
	lease.touchedAt = slot.at
	lease.recheckAt = slot.at.Add(runLeaseIdle)
	l.leases[slot.broadcasterID] = lease
	return false, false
}

func (l *ValkeyRunLimiter) reserve(ctx context.Context, slot runSlot) (bool, error) {
	call := pkg_valkey.ScriptCall{
		Script: reserveRuns,
		Key:    runRateKey(slot.broadcasterID, slot.at),
		Args: []string{
			strconv.Itoa(runLeaseSize),
			strconv.Itoa(automation.MaxRunsPerMinute),
			strconv.Itoa(int(runRateTTL.Seconds())),
		},
	}
	reply, err := pkg_valkey.RunScripts(ctx, l.client, []pkg_valkey.ScriptCall{call})[0].AsIntSlice()
	if err != nil {
		return true, err
	}
	if len(reply) != 2 {
		return true, errReserveReply
	}
	l.store(slot, reservation{granted: reply[0], total: reply[1]})
	return reply[0] > 0, nil
}

var errReserveReply = errors.New("automations: malformed run reservation reply")

type reservation struct {
	granted int64
	total   int64
}

func (l *ValkeyRunLimiter) store(slot runSlot, res reservation) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.window != slot.window() {
		return
	}
	lease := l.leases[slot.broadcasterID]
	lease.left += int(max(res.granted-1, 0))
	lease.closed = res.total >= automation.MaxRunsPerMinute
	lease.touchedAt = slot.at
	lease.recheckAt = slot.at.Add(runLeaseIdle)
	l.leases[slot.broadcasterID] = lease
}
