// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/outgress"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	runLogTTL          = outgress.RewardResultTTL
	runLogKeyPrefix    = "automation:runs:"
	runLogBuffer       = 1024
	runLogBatch        = 256
	runLogFlushTimeout = 2 * time.Second
	runLogCloseTimeout = 5 * time.Second
)

func runLogKey(broadcasterID uint64) string {
	return runLogKeyPrefix + "{" + strconv.FormatUint(broadcasterID, 10) + "}"
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
	records := runFilter{automationID: automationID, limit: limit}.apply(raw)
	l.overlayRewardResults(ctx, broadcasterID, records)
	return records, nil
}

func (l *ValkeyRunLog) overlayRewardResults(ctx context.Context, broadcasterID uint64, records []automationrpc.RunRecord) {
	waiting := make([]*automationrpc.RunRecord, 0, len(records))
	cmds := make(valkey.Commands, 0, len(records))
	id := strconv.FormatUint(broadcasterID, 10)
	for i := range records {
		if awaitsRewardResult(records[i]) {
			waiting = append(waiting, &records[i])
			cmds = append(cmds, l.client.B().Hgetall().Key(outgress.RewardResultKey(id, records[i].RunID)).Build())
		}
	}
	for i, results := range l.readRewardResults(ctx, cmds) {
		applyRewardResults(waiting[i], results)
	}
}

func (l *ValkeyRunLog) readRewardResults(ctx context.Context, cmds valkey.Commands) []map[string]string {
	if len(cmds) == 0 {
		return nil
	}
	replies := l.client.DoMulti(ctx, cmds...)
	out := make([]map[string]string, len(replies))
	for i, res := range replies {
		results, err := res.AsStrMap()
		if err != nil {
			l.log.Warn("automations: reward result read failed", zap.Error(err))
		}
		out[i] = results
	}
	return out
}

func awaitsRewardResult(rec automationrpc.RunRecord) bool {
	return slices.ContainsFunc(rec.Actions, func(a automationrpc.ActionRecord) bool { return a.Slot != "" })
}

func applyRewardResults(rec *automationrpc.RunRecord, results map[string]string) {
	rewardFailed := false
	for i := range rec.Actions {
		action := &rec.Actions[i]
		result, known := results[action.Slot]
		action.Slot = ""
		if !known {
			continue
		}
		action.Status, action.Detail = rewardStep(result)
		rewardFailed = rewardFailed || action.Status == automationrpc.StatusFailed
	}
	if rewardFailed && rec.Outcome == automationrpc.OutcomeOK {
		rec.Outcome = automationrpc.OutcomePartial
	}
}

func rewardStep(result string) (status, detail string) {
	if result == outgress.RewardResultOK {
		return automationrpc.StatusOK, ""
	}
	return automationrpc.StatusFailed, result
}

type runFilter struct {
	automationID string
	limit        int
}

func (f runFilter) apply(raw []string) []automationrpc.RunRecord {
	out := make([]automationrpc.RunRecord, 0, f.limit)
	rows := make(map[string]int, f.limit)
	for _, item := range raw {
		rec, ok := f.decode(item)
		if !ok {
			continue
		}
		if row, seen := rows[rec.RunID]; seen {
			out[row] = mergeRunParts(out[row], rec)
			continue
		}
		if len(out) < f.limit {
			rows[rec.RunID] = len(out)
			out = append(out, rec)
		}
	}
	return out
}

// Parallel paths that paused each finish on their own, so one run can hold several entries; history shows one outcome.
func mergeRunParts(newer, older automationrpc.RunRecord) automationrpc.RunRecord {
	merged := newer
	merged.At = older.At
	merged.DurationMS = max(newer.DurationMS, older.DurationMS)
	if outcomeSeverity(older.Outcome) > outcomeSeverity(newer.Outcome) {
		merged.Outcome, merged.Reason = older.Outcome, older.Reason
	}
	shared := sharedActionPrefix(newer.Actions, older.Actions)
	merged.Actions = append(slices.Clone(newer.Actions), older.Actions[shared:]...)
	return merged
}

func outcomeSeverity(outcome string) int {
	switch outcome {
	case automationrpc.OutcomeFailed:
		return 3
	case automationrpc.OutcomePartial:
		return 2
	case automationrpc.OutcomeLimited:
		return 1
	}
	return 0
}

func sharedActionPrefix(a, b []automationrpc.ActionRecord) int {
	shared := 0
	for shared < min(len(a), len(b)) && a[shared] == b[shared] {
		shared++
	}
	return shared
}

func (f runFilter) decode(item string) (automationrpc.RunRecord, bool) {
	var rec automationrpc.RunRecord
	if codec.Unmarshal([]byte(item), &rec) != nil {
		return rec, false
	}
	return rec, f.automationID == "" || rec.AutomationID == f.automationID
}
