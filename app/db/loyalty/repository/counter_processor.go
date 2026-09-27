// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/valkey"
	"github.com/google/uuid"
	valkey_go "github.com/valkey-io/valkey-go"
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
	request, err := p.enqueueCounter(ctx, dto)
	if err != nil {
		return err
	}
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *CounterProcessor) beginCounterAdmission(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrCounterProcessorClosed
	}
	p.admissions.Add(1)
	return nil
}

func (p *CounterProcessor) enqueueCounter(ctx context.Context, dto data.CounterBumpedDTO) (*counterRequest, error) {
	if err := p.beginCounterAdmission(ctx); err != nil {
		return nil, err
	}
	defer p.admissions.Done()
	// Reserve bounded payload space before copying. The worker retains an
	// immutable payload if its caller leaves while persistence is in flight.
	select {
	case p.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	request := newCounterRequest(ctx, dto)
	select {
	case p.queue <- request:
		return request, nil
	case <-ctx.Done():
		<-p.slots
		return nil, ctx.Err()
	}
}

func newCounterRequest(ctx context.Context, dto data.CounterBumpedDTO) *counterRequest {
	dto.Bumps = append([]data.CounterBumpEntry(nil), dto.Bumps...)
	for i := range dto.Bumps {
		dto.Bumps[i].Scope, _ = ValidScope(dto.Bumps[i].Scope)
	}
	return &counterRequest{ctx: ctx, dto: dto, result: make(chan error, 1)}
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
	collector := counterQueueCollector{processor: p}
	for {
		first, ok := collector.next()
		if !ok {
			return
		}
		p.processBatch(collector.collect(first))
	}
}

type counterQueueCollector struct {
	processor *CounterProcessor
	carry     *counterRequest
	stopping  bool
}

func (c *counterQueueCollector) next() (*counterRequest, bool) {
	if c.carry != nil {
		request := c.carry
		c.carry = nil
		return request, true
	}
	if c.stopping {
		return c.drainNext()
	}
	select {
	case request := <-c.processor.queue:
		return request, true
	case <-c.processor.stop:
		c.stopping = true
		return c.drainNext()
	}
}

func (c *counterQueueCollector) drainNext() (*counterRequest, bool) {
	select {
	case request := <-c.processor.queue:
		return request, true
	default:
		return nil, false
	}
}

func (c *counterQueueCollector) collect(first *counterRequest) []*counterRequest {
	batch := []*counterRequest{first}
	bumps := len(first.dto.Bumps)
	timer := time.NewTimer(counterBatchWindow)
	defer timer.Stop()
	for len(batch) < counterBatchMessages && bumps < counterBatchBumps {
		select {
		case request := <-c.processor.queue:
			if bumps+len(request.dto.Bumps) > counterBatchBumps {
				c.carry = request
				return batch
			}
			batch = append(batch, request)
			bumps += len(request.dto.Bumps)
		case <-timer.C:
			return batch
		case <-c.processor.stop:
			c.stopping = true
			return batch
		}
	}
	return batch
}

func validateCounterBatch(dto data.CounterBumpedDTO) error {
	if err := validateCounterBatchSize(dto); err != nil {
		return err
	}
	sums := make(map[bumpKey]int64, len(dto.Bumps))
	for _, bump := range dto.Bumps {
		key, err := validatedCounterTarget(dto.UserID, bump)
		if err != nil {
			return err
		}
		delta, err := addCounterDelta(sums[key], bump.Delta)
		if err != nil {
			return err
		}
		sums[key] = delta
	}
	return nil
}

func validateCounterBatchSize(dto data.CounterBumpedDTO) error {
	if len(dto.BatchID) > 256 {
		return fmt.Errorf("%w: counter batch size or id", ErrInvalidInput)
	}
	if len(dto.Bumps) == 0 {
		return fmt.Errorf("%w: counter batch size or id", ErrInvalidInput)
	}
	if len(dto.Bumps) > counterMaxBumps {
		return fmt.Errorf("%w: counter batch size or id", ErrInvalidInput)
	}
	return nil
}

func validatedCounterTarget(userID uint64, bump data.CounterBumpEntry) (bumpKey, error) {
	name := normalizeName(bump.Name)
	if !usableCounterName(name) {
		return bumpKey{}, fmt.Errorf("%w: counter name", ErrInvalidInput)
	}
	scope, err := ValidScope(bump.Scope)
	if err != nil {
		return bumpKey{}, err
	}
	bump.Scope = scope
	if err := validateCounterNamespace(userID, bump); err != nil {
		return bumpKey{}, err
	}
	if data.SystemCounter(name) && bump.Delta < 0 {
		return bumpKey{}, fmt.Errorf("%w: negative system counter delta", ErrInvalidInput)
	}
	key, _ := scopeKey(userID, name, bump)
	return key, nil
}

func validateCounterNamespace(userID uint64, bump data.CounterBumpEntry) error {
	if (userID == 0) != (bump.Scope == data.CounterScopeBot) {
		return fmt.Errorf("%w: counter namespace or viewer", ErrInvalidInput)
	}
	if viewerScoped(bump.Scope) && bump.ViewerID == 0 {
		return fmt.Errorf("%w: counter namespace or viewer", ErrInvalidInput)
	}
	return nil
}

func addCounterDelta(current, delta int64) (int64, error) {
	if err := validateBatchDelta(current, delta); err != nil {
		return 0, fmt.Errorf("%w: counter delta exceeds signed range", ErrInvalidInput)
	}
	return current + delta, nil
}

func (p *CounterProcessor) processBatch(batch []*counterRequest) {
	deliveries := p.gatherCounterDeliveries(batch)
	if len(deliveries) == 0 {
		return
	}
	// An individual caller's cancellation must not poison its neighbours.
	ctx, cancel := context.WithTimeout(context.Background(), counterSQLTimeout)
	defer cancel()
	if err := p.repo.lockCounterPersistence(ctx); err != nil {
		setCounterDeliveryErrors(deliveries, err)
		p.finish(deliveries)
		return
	}
	defer p.repo.persistMu.Unlock()
	owner := "pending:" + uuid.NewString()
	tracked := p.claimCounterDeliveries(ctx, deliveries, owner)
	p.persistDeliveries(ctx, activeCounterDeliveries(deliveries))
	p.finalizeCounterClaims(tracked, owner)
	p.finish(deliveries)
}

func (p *CounterProcessor) gatherCounterDeliveries(batch []*counterRequest) []*counterDelivery {
	deliveries := make([]*counterDelivery, 0, len(batch))
	seen := make(map[string]*counterDelivery, len(batch))
	for _, request := range batch {
		if err := request.ctx.Err(); err != nil {
			p.finishCounterRequest(request, err)
			continue
		}
		key := counterReceiptKey(request.dto)
		if previous := seen[key]; previous != nil {
			previous.aliases = append(previous.aliases, request)
			continue
		}
		delivery := &counterDelivery{request: request, key: key}
		if key != "" {
			seen[key] = delivery
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries
}

func setCounterDeliveryErrors(deliveries []*counterDelivery, err error) {
	for _, delivery := range deliveries {
		delivery.err = err
	}
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
	setCounterDeliveryErrors(deliveries, err)
}

func (p *CounterProcessor) finish(deliveries []*counterDelivery) {
	for _, delivery := range deliveries {
		p.finishCounterRequest(delivery.request, delivery.err)
		for _, alias := range delivery.aliases {
			p.finishCounterRequest(alias, delivery.err)
		}
	}
}

func (p *CounterProcessor) finishCounterRequest(request *counterRequest, err error) {
	request.result <- err
	<-p.slots
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
