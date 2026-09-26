// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nuid"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	ConsumerGroup    = "loyalty-watchtime"
	QuarantineStream = "watchtime:quarantine"
	awardTimeout     = 10 * time.Second
	claimIdle        = 30 * time.Second
)

// Consumer projects accepted awards to the durable repository. Each replica
// handles one delivery at a time; the repository supplies transactional replay
// protection. A failed commit leaves the delivery pending for later reclamation.
type Consumer struct {
	client      valkey.Client
	handle      func(context.Context, data.WatchAwardDTO) error
	log         *zap.Logger
	name        string
	claimCursor string
	maintain    func(context.Context, int64) error
}

type ConsumerOption func(*Consumer)

// WithHistoryMaintenance keeps SQL ownership with the injected repository;
// the consumer supplies the safe source cutoff using its existing client.
func WithHistoryMaintenance(maintain func(context.Context, int64) error) ConsumerOption {
	return func(c *Consumer) { c.maintain = maintain }
}

func NewConsumer(client valkey.Client, handle func(context.Context, data.WatchAwardDTO) error, log *zap.Logger, options ...ConsumerOption) *Consumer {
	if log == nil {
		log = zap.NewNop()
	}
	c := &Consumer{client: pkgvalkey.Primary(client), handle: handle, log: log, name: nuid.Next(), claimCursor: "0-0"}
	for _, option := range options {
		option(c)
	}
	return c
}

// Ensure starts at the oldest retained award, including records accepted before
// the first loyalty consumer came online. BUSYGROUP means another replica won.
func (c *Consumer) Ensure(ctx context.Context) error {
	err := c.client.Do(ctx, c.client.B().XgroupCreate().Key(Stream).Group(ConsumerGroup).Id("0").Mkstream().Build()).Error()
	if err != nil && strings.HasPrefix(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

// Run stops on cancellation and retries transient read/commit failures. Blocking
// reads are bounded so shutdown never waits on an indefinite Stream read.
func (c *Consumer) Run(ctx context.Context) {
	if c.handle == nil {
		c.log.Error("watchtime: no award repository configured")
		return
	}
	state := consumerSchedule{}
	for ctx.Err() == nil {
		if !c.consumeCycle(ctx, &state) {
			return
		}
	}
}

type consumerSchedule struct {
	nextClaim       time.Time
	nextMaintenance time.Time
	ready           bool
}

func (c *Consumer) consumeCycle(ctx context.Context, state *consumerSchedule) bool {
	if !state.ready {
		state.ready = c.prepareOutbox(ctx)
		if !state.ready {
			return consumerPause(ctx, time.Second)
		}
	}
	c.recoverDue(ctx, state)
	c.maintainDue(ctx, state)
	return c.readFresh(ctx, state)
}

func (c *Consumer) prepareOutbox(ctx context.Context) bool {
	if err := c.Ensure(ctx); err != nil {
		c.log.Error("watchtime: outbox unavailable", zap.Error(err))
		return false
	}
	return true
}

func (c *Consumer) recoverDue(ctx context.Context, state *consumerSchedule) {
	if !time.Now().After(state.nextClaim) {
		return
	}
	state.nextClaim = time.Now().Add(time.Second)
	c.recordStreamFailure(ctx, state, "watchtime: pending recovery failed", c.recoverBatch(ctx, claimIdle))
}

func (c *Consumer) maintainDue(ctx context.Context, state *consumerSchedule) {
	if c.maintain == nil {
		return
	}
	if !time.Now().After(state.nextMaintenance) {
		return
	}
	state.nextMaintenance = time.Now().Add(10 * time.Second)
	_ = c.pruneHistory(ctx) // Logs the result with cutoff/stage context.
}

func (c *Consumer) readFresh(ctx context.Context, state *consumerSchedule) bool {
	err := c.read(ctx)
	if err == nil {
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	c.recordStreamFailure(ctx, state, "watchtime: outbox read failed", err)
	return consumerPause(ctx, time.Second)
}

func (c *Consumer) recordStreamFailure(ctx context.Context, state *consumerSchedule, message string, err error) {
	if err == nil {
		return
	}
	if ctx.Err() != nil {
		return
	}
	c.log.Error(message, zap.Error(err))
	if strings.HasPrefix(err.Error(), "NOGROUP") {
		state.ready = false
	}
}

func (c *Consumer) pruneHistory(parent context.Context) (err error) {
	started := time.Now()
	var cutoff int64
	stage, result := "outbox_barrier", "blocked"
	defer func() {
		if err != nil {
			result = "failed"
		}
		fields := []zap.Field{zap.String("stage", stage), zap.String("result", result), zap.Int64("retirement_cutoff_ms", cutoff), zap.Duration("maintenance_duration", time.Since(started))}
		if err != nil {
			if parent.Err() == nil {
				c.log.Warn("watchtime: history maintenance failed", append(fields, zap.Error(err))...)
			}
			return
		}
		c.log.Debug("watchtime: history maintenance result", fields...)
	}()
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cutoff, err = NewStore(c.client).SafePruneBefore(ctx, time.Now().Add(-HistoryRetention).UnixMilli())
	if err != nil || cutoff == 0 {
		return err
	}
	stage = "sql_history"
	err = c.maintain(ctx, cutoff)
	if err == nil {
		result = "completed"
	}
	return err
}

// Check exposes a stalled outbox independently of the NATS/SQL health checks.
// An accepted award waiting longer than a complete watch interval needs attention.
func (c *Consumer) Check(ctx context.Context) error {
	entries, err := c.client.Do(ctx, c.client.B().Xrange().Key(Stream).Start("-").End("+").Count(1).Build()).AsXRange()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	ms, err := strconv.ParseInt(strings.SplitN(entries[0].ID, "-", 2)[0], 10, 64)
	if err != nil {
		return errors.New("invalid watch outbox entry ID")
	}
	if time.Since(time.UnixMilli(ms)) > 5*time.Minute {
		return errors.New("watch awards have been pending for more than five minutes")
	}
	return nil
}

func consumerPause(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *Consumer) read(ctx context.Context) error {
	streams, err := c.client.Do(ctx, c.client.B().Xreadgroup().Group(ConsumerGroup, c.name).Count(1).Block(1000).Streams().Key(Stream).Id(">").Build()).AsXRead()
	if valkey.IsValkeyNil(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range streams[Stream] {
		c.process(ctx, entry)
	}
	return nil
}

func (c *Consumer) reclaim(ctx context.Context, idle time.Duration) error {
	_, err := c.reclaimOne(ctx, idle)
	return err
}

// Recovery gets a bounded share alongside fresh work. Claim one at a time so
// no long local buffer sits idle long enough to be reclaimed by another replica.
func (c *Consumer) recoverBatch(ctx context.Context, idle time.Duration) error {
	deadline := time.Now().Add(2 * time.Second)
	for i := 0; i < 16 && time.Now().Before(deadline) && ctx.Err() == nil; i++ {
		n, err := c.reclaimOne(ctx, idle)
		if err != nil {
			return err
		}
		if n == 0 && c.claimCursor == "0-0" {
			return nil
		}
	}
	return ctx.Err()
}

func (c *Consumer) reclaimOne(ctx context.Context, idle time.Duration) (int, error) {
	parts, err := c.client.Do(ctx, c.client.B().Xautoclaim().Key(Stream).Group(ConsumerGroup).Consumer(c.name).MinIdleTime(strconv.FormatInt(idle.Milliseconds(), 10)).Start(c.claimCursor).Count(1).Build()).ToArray()
	if err != nil {
		return 0, err
	}
	if len(parts) < 2 {
		return 0, errors.New("invalid watch pending recovery reply")
	}
	c.claimCursor, err = parts[0].ToString()
	if err != nil {
		return 0, err
	}
	entries, err := parts[1].AsXRange()
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		c.process(ctx, entry)
	}
	return len(entries), nil
}

func (c *Consumer) ack(ctx context.Context, id string, award data.WatchAwardDTO) error {
	// SQL already remembers this operation. Clear the short-lived acceptance
	// digest with the delivery so Valkey memory grows with unpaid work, while
	// an uncertain acknowledgement can safely retry the SQL operation.
	return c.client.Do(ctx, c.client.B().Eval().Script(`redis.call('XACK',KEYS[1],ARGV[1],ARGV[2]); redis.call('XDEL',KEYS[1],ARGV[2]); redis.call('HDEL',KEYS[2],ARGV[3]); redis.call('ZREM',KEYS[3],ARGV[2]); return 1`).Numkeys(3).Key(Stream, "watchtime:operations:"+strconv.FormatUint(award.UserID, 10), OutboxWindowIndex).Arg(ConsumerGroup, id, OperationID(award)).Build()).Error()
}

func (c *Consumer) quarantine(ctx context.Context, entry valkey.XRangeEntry, cause error) error {
	// Quarantine and ACK are one primary operation; a malformed item cannot
	// monopolize the recovery queue or disappear without an inspectable record.
	return c.client.Do(ctx, c.client.B().Eval().Script(`
redis.call('XADD',KEYS[2],'*','source_id',ARGV[2],'reason',ARGV[3],'payload',ARGV[4])
redis.call('XACK',KEYS[1],ARGV[1],ARGV[2]); redis.call('XDEL',KEYS[1],ARGV[2]); redis.call('ZREM',KEYS[3],ARGV[2]); return 1`).Numkeys(3).Key(Stream, QuarantineStream, OutboxWindowIndex).Arg(ConsumerGroup, entry.ID, fmt.Sprint(cause), entry.FieldValues["payload"]).Build()).Error()
}
