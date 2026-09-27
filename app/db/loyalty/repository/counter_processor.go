// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/valkey"
	"github.com/google/uuid"
	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	counterBatchWindow   = 20 * time.Millisecond
	counterBatchMessages = 128
	counterQueueMessages = 256
	counterMaxBumps      = 4096
	counterBatchBumps    = 8192
	counterCompletedTTL  = 5 * time.Minute
	// SQL plus gate admission is bounded to 30 seconds after claims. The
	// remaining margin lets completion/release finish before the owner expires.
	counterPendingTTL   = 45 * time.Second
	counterSQLTimeout   = 30 * time.Second
	counterCacheTimeout = 5 * time.Second
)

var (
	ErrCounterPending         = errors.New("counter batch is already being processed")
	ErrCounterProcessorClosed = errors.New("counter processor is closed")
)

// A claim and its expiry are atomic. A pending delivery is retryable, never
// acknowledged as completed. Owners prevent late cleanup from deleting a
// replacement claim after a timeout.
const counterClaimScript = `local value = redis.call('GET', KEYS[1])
if value == 'done' then return 2 end
if value then return 0 end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return 1`
const counterCompleteScript = `if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], 'done', 'PX', ARGV[2])
return 1`
const counterReleaseScript = `if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
return redis.call('DEL', KEYS[1])`

type counterRequest struct {
	ctx    context.Context
	dto    data.CounterBumpedDTO
	result chan error
}

// CounterProcessor coalesces acknowledged counter writes into bounded SQL
// transactions and explicit Valkey pipelines. Completion receipts live for
// five minutes. The SQL-to-Valkey crash gap and replays after that expiry can
// count again; these counters intentionally tolerate that tradeoff. Watch
// awards and balances retain their separate durable posting semantics.
type CounterProcessor struct {
	repo         *Loyalty
	client       valkey_go.Client
	queue        chan *counterRequest
	slots        chan struct{}
	stop         chan struct{}
	done         chan struct{}
	mu           sync.Mutex
	closed       bool
	cacheWarning atomic.Int64
	// admissions fences senders against shutdown without closing queue under a
	// concurrent sender. The worker drains every admitted request before exit.
	admissions sync.WaitGroup
}

func NewCounterProcessor(repo *Loyalty, client valkey_go.Client) *CounterProcessor {
	p := &CounterProcessor{repo: repo, client: valkey.Primary(client), queue: make(chan *counterRequest, counterQueueMessages), slots: make(chan struct{}, counterQueueMessages+counterBatchMessages+1), stop: make(chan struct{}), done: make(chan struct{})}
	go p.run()
	return p
}

// Process returns success only once SQL has committed, or a completed receipt
// proves this batch previously committed. Cancellation before admission makes
// no write. Cancellation after admission can leave a committed write; a retry
// keeps its BatchID and consults the same receipt.
func (p *CounterProcessor) Process(ctx context.Context, dto data.CounterBumpedDTO) error {
	if err := validateCounterBatch(dto); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrCounterProcessorClosed
	}
	p.admissions.Add(1)
	p.mu.Unlock()
	// Reserve bounded payload space before copying. Callers can safely return
	// on cancellation while the independently bounded worker finishes a write.
	select {
	case p.slots <- struct{}{}:
	case <-ctx.Done():
		p.admissions.Done()
		return ctx.Err()
	}
	dto.Bumps = append([]data.CounterBumpEntry(nil), dto.Bumps...)
	for i := range dto.Bumps {
		dto.Bumps[i].Scope, _ = ValidScope(dto.Bumps[i].Scope)
	}
	request := &counterRequest{ctx: ctx, dto: dto, result: make(chan error, 1)}
	select {
	case p.queue <- request:
	case <-ctx.Done():
		<-p.slots
		p.admissions.Done()
		return ctx.Err()
	}
	p.admissions.Done()
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close rejects new calls and waits for all admitted batches. A caller's
// shutdown deadline bounds the wait; the bounded worker continues draining
// if that deadline expires. Close never closes the shared Valkey client.
func (p *CounterProcessor) Close(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		go func() { p.admissions.Wait(); close(p.stop) }()
	}
	p.mu.Unlock()
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *CounterProcessor) run() {
	defer close(p.done)
	stopping := false
	var carry *counterRequest
	for {
		var first *counterRequest
		if carry != nil {
			first, carry = carry, nil
		} else if stopping {
			select {
			case first = <-p.queue:
			default:
				return
			}
		} else {
			select {
			case first = <-p.queue:
			case <-p.stop:
				stopping = true
				continue
			}
		}
		batch := []*counterRequest{first}
		bumps := len(first.dto.Bumps)
		timer := time.NewTimer(counterBatchWindow)
	collect:
		for len(batch) < counterBatchMessages && bumps < counterBatchBumps {
			select {
			case request := <-p.queue:
				if bumps+len(request.dto.Bumps) > counterBatchBumps {
					carry = request
					break collect
				}
				batch = append(batch, request)
				bumps += len(request.dto.Bumps)
			case <-timer.C:
				break collect
			case <-p.stop:
				stopping = true
				break collect
			}
		}
		timer.Stop()
		p.processBatch(batch)
	}
}

func validateCounterBatch(dto data.CounterBumpedDTO) error {
	if len(dto.BatchID) > 256 || len(dto.Bumps) == 0 || len(dto.Bumps) > counterMaxBumps {
		return fmt.Errorf("%w: counter batch size or id", ErrInvalidInput)
	}
	sums := make(map[bumpKey]int64, len(dto.Bumps))
	for _, bump := range dto.Bumps {
		name := normalizeName(bump.Name)
		if !usableCounterName(name) {
			return fmt.Errorf("%w: counter name", ErrInvalidInput)
		}
		scope, err := ValidScope(bump.Scope)
		if err != nil {
			return err
		}
		bump.Scope = scope
		if (dto.UserID == 0) != (scope == data.CounterScopeBot) || (viewerScoped(scope) && bump.ViewerID == 0) {
			return fmt.Errorf("%w: counter namespace or viewer", ErrInvalidInput)
		}
		if data.SystemCounter(name) && bump.Delta < 0 {
			return fmt.Errorf("%w: negative system counter delta", ErrInvalidInput)
		}
		key, _ := scopeKey(dto.UserID, name, bump)
		delta, err := addCounterDelta(sums[key], bump.Delta)
		if err != nil {
			return err
		}
		sums[key] = delta
	}
	return nil
}

func addCounterDelta(current, delta int64) (int64, error) {
	max := data.MaxCounter
	if delta > max || delta < -max || (delta > 0 && current > max-delta) || (delta < 0 && current < -max-delta) {
		return 0, fmt.Errorf("%w: counter delta exceeds signed range", ErrInvalidInput)
	}
	return current + delta, nil
}

func counterReceiptKey(dto data.CounterBumpedDTO) string {
	if dto.BatchID == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(dto.BatchID))
	return "loyalty:counter:receipt:" + strconv.FormatUint(dto.UserID, 10) + ":" + hex.EncodeToString(hash[:])
}

type counterDelivery struct {
	request     *counterRequest
	key         string
	aliases     []*counterRequest
	err         error
	claimed     bool
	cacheFailed bool
}

func (p *CounterProcessor) processBatch(batch []*counterRequest) {
	deliveries := make([]*counterDelivery, 0, len(batch))
	seen := make(map[string]*counterDelivery, len(batch))
	for _, request := range batch {
		if err := request.ctx.Err(); err != nil {
			request.result <- err
			<-p.slots
			continue
		}
		key := counterReceiptKey(request.dto)
		if previous := seen[key]; key != "" && previous != nil {
			previous.aliases = append(previous.aliases, request)
			continue
		}
		delivery := &counterDelivery{request: request, key: key}
		if key != "" {
			seen[key] = delivery
		}
		deliveries = append(deliveries, delivery)
	}
	if len(deliveries) == 0 {
		return
	}
	// Do not let one canceled caller poison its healthy coalesced neighbours.
	// Individual callers may leave early; admitted SQL is independently bounded.
	ctx, cancel := context.WithTimeout(context.Background(), counterSQLTimeout)
	defer cancel()
	if err := p.repo.lockCounterPersistence(ctx); err != nil {
		for _, d := range deliveries {
			d.err = err
		}
		p.finish(deliveries)
		return
	}
	defer p.repo.persistMu.Unlock()
	owner := "pending:" + uuid.NewString()
	claims := make([]valkey_go.Completed, 0, len(deliveries))
	tracked := make([]*counterDelivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.key == "" {
			continue
		}
		claims = append(claims, p.client.B().Eval().Script(counterClaimScript).Numkeys(1).Key(delivery.key).Arg(owner, strconv.FormatInt(counterPendingTTL.Milliseconds(), 10)).Build())
		tracked = append(tracked, delivery)
	}
	if len(claims) > 0 {
		claimCtx, claimCancel := context.WithTimeout(ctx, counterCacheTimeout)
		results := p.client.DoMulti(claimCtx, claims...)
		claimCancel()
		for i, delivery := range tracked {
			if i >= len(results) {
				delivery.cacheFailed = true
				p.warnCounterCache(errors.New("counter claim response missing"))
				continue
			}
			status, err := results[i].ToInt64()
			switch {
			case err != nil:
				delivery.cacheFailed = true
				p.warnCounterCache(fmt.Errorf("counter claim: %w", err))
			case status == 1:
				delivery.claimed = true
			case status == 2: // Already completed: exclude it from SQL.
			case status == 0:
				delivery.err = ErrCounterPending
			default:
				delivery.err = fmt.Errorf("unexpected counter claim status %d", status)
			}
		}
	}
	active := make([]*counterDelivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.err == nil && (delivery.key == "" || delivery.claimed || delivery.cacheFailed) {
			active = append(active, delivery)
		}
	}
	p.persistDeliveries(ctx, active)
	// Completion/release has its own short bound even when SQL exhausted its
	// deadline. Owner checks make cleanup safe if another claimant took over.
	cacheCtx, cacheCancel := context.WithTimeout(context.Background(), counterCacheTimeout)
	defer cacheCancel()
	commands := make([]valkey_go.Completed, 0, len(tracked))
	finalized := make([]*counterDelivery, 0, len(tracked))
	for _, delivery := range tracked {
		if (delivery.claimed || delivery.cacheFailed) && delivery.err == nil {
			commands = append(commands, p.client.B().Eval().Script(counterCompleteScript).Numkeys(1).Key(delivery.key).Arg(owner, strconv.FormatInt(counterCompletedTTL.Milliseconds(), 10)).Build())
			finalized = append(finalized, delivery)
		} else if delivery.err != nil && !errors.Is(delivery.err, ErrCounterPending) {
			// Include unknown claims: a response can fail after SET reached Valkey.
			commands = append(commands, p.client.B().Eval().Script(counterReleaseScript).Numkeys(1).Key(delivery.key).Arg(owner).Build())
			finalized = append(finalized, delivery)
		}
	}
	if len(commands) > 0 {
		results := p.client.DoMulti(cacheCtx, commands...)
		for i, delivery := range finalized {
			if delivery.err != nil {
				continue
			} // Preserve genuine SQL/claim failures.
			if i >= len(results) {
				p.warnCounterCache(errors.New("counter completion response missing"))
				continue
			}
			status, err := results[i].ToInt64()
			if err != nil {
				p.warnCounterCache(fmt.Errorf("counter completion: %w", err))
			} else if status != 1 {
				p.warnCounterCache(errors.New("counter completion lease lost"))
			}
		}
	}
	p.finish(deliveries)
}

// Only proven permanent range failures are isolated. Each unsuccessful SQL
// attempt rolls back in full before any smaller transaction is attempted.
// Transient or uncertain commit failures remain retryable for every delivery.
func (p *CounterProcessor) persistDeliveries(ctx context.Context, deliveries []*counterDelivery) {
	if len(deliveries) == 0 {
		return
	}
	dtos := make([]data.CounterBumpedDTO, len(deliveries))
	for i, delivery := range deliveries {
		dtos[i] = delivery.request.dto
	}
	err := p.repo.persistCounterBatch(ctx, dtos)
	if errors.Is(err, ErrInvalidInput) && len(deliveries) > 1 {
		middle := len(deliveries) / 2
		p.persistDeliveries(ctx, deliveries[:middle])
		p.persistDeliveries(ctx, deliveries[middle:])
		return
	}
	for _, delivery := range deliveries {
		delivery.err = err
	}
}

func (p *CounterProcessor) finish(deliveries []*counterDelivery) {
	for _, delivery := range deliveries {
		delivery.request.result <- delivery.err
		<-p.slots
		for _, alias := range delivery.aliases {
			alias.result <- delivery.err
			<-p.slots
		}
	}
}

func (r *Loyalty) lockCounterPersistence(ctx context.Context) error {
	if r.persistMu.TryLock() {
		return nil
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if r.persistMu.TryLock() {
				return nil
			}
		}
	}
}

// Dedup is best effort for these loss-tolerant counters. A Valkey outage must
// not discard a counter window whose SQL write can succeed; warnings are
// throttled so an outage cannot flood logs. Explicit live-owner collisions
// remain retryable because another delivery has not yet proved persistence.
func (p *CounterProcessor) warnCounterCache(err error) {
	now := time.Now().Unix()
	previous := p.cacheWarning.Load()
	if now-previous < 60 || !p.cacheWarning.CompareAndSwap(previous, now) {
		return
	}
	if p.repo.log != nil {
		p.repo.log.Warn("loyalty: counter dedup unavailable; recount is possible", zap.Error(err))
	}
}
