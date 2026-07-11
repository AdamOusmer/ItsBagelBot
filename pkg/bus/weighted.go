package bus

import (
	"context"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

// ConsumeWeighted is the central, autoscaling consumer that sits between NATS
// and the pipeline:
//
//	NATS lanes -> ConsumeWeighted -> consumers, each with its own routine pool
//
// It runs one or more consumer units. Each unit owns its own subscriptions to
// every lane and its own bounded pool of pipeline routines, and dispatches each
// message to its lane's handler on its own goroutine. The pool grows or shrinks
// with load along two independent tiers:
//
//	routines per consumer: each unit scales its own pool from MinRoutines up to
//	MaxRoutines on its own saturation, a step at a time after ScaleUpAfter, and
//	back down after ScaleDownAfter.
//
//	consumers: once every unit is pinned at MaxRoutines and saturated, a new
//	unit (its own subscriptions + pool) spins up, up to MaxConsumers; when every
//	unit is calm, the newest unit is retired, down to one.
//
// A lane may reserve a percentage of each unit's pool for itself
// (WeightedLane.Reserve) so a flood on one lane can never consume the slots
// another lane is entitled to: the premium lane reserving 25% means the other
// lanes together never hold more than 75% of any unit's pool, leaving premium a
// guaranteed quarter on every unit.
//
// Ack discipline matches Consume: a message's goroutine runs the handler to
// completion and only then acks (nil) or nacks (error), so the redelivery
// retry budget is preserved. Handlers must be safe for concurrent use.
//
// ConsumeWeighted returns once the first unit is running; the units and the
// supervisor stop when ctx is cancelled. The returned *Weighted lets a graceful
// shutdown wait for handlers already dispatched to finish (Drain) before the
// publishers they emit onto are closed.
type WeightedLane struct {
	Sub     message.Subscriber
	Subject string
	Handle  func(*message.Message) error
	// Reserve is the percentage (0..100) of the pool kept exclusively for this
	// lane. Reserves across lanes must sum to at most 100.
	Reserve int
}

// ScalePolicy bounds and paces the autoscaler. Zero values are replaced with
// safe defaults.
type ScalePolicy struct {
	MinRoutines    int           // floor for routines per consumer (>= 1)
	MaxRoutines    int           // ceiling for routines per consumer
	MaxConsumers   int           // ceiling on the number of consumers (>= 1)
	ScaleUpAfter   time.Duration // sustained saturation before growing a step
	ScaleDownAfter time.Duration // sustained calm before shrinking a step
}

func ConsumeWeighted(ctx context.Context, app *newrelic.Application, lanes []WeightedLane, policy ScalePolicy, log *zap.Logger) (*Weighted, error) {

	policy = policy.normalized()

	reserves := make([]int, len(lanes))
	for i, lane := range lanes {
		reserves[i] = lane.Reserve
	}

	s := &supervisor{
		ctx:        ctx,
		app:        app,
		lanes:      lanes,
		reserves:   reserves,
		policy:     policy,
		log:        log,
		dispatched: &sync.WaitGroup{},
	}

	// The first unit starts synchronously so a Subscribe error surfaces here.
	first, err := s.startUnit()
	if err != nil {
		return nil, err
	}
	s.units = []*consumerUnit{first}

	go s.run()

	return &Weighted{dispatched: s.dispatched}, nil
}

// Weighted is the handle ConsumeWeighted returns; Drain is its only method.
type Weighted struct {
	// dispatched counts every reader loop and every dispatched handler goroutine
	// across all units. It starts positive (the first unit's readers are added
	// before ConsumeWeighted returns, so before any Drain) and stays positive
	// while any reader is alive, so a handler's Add never races Drain's Wait: the
	// counter only reaches zero once every reader has exited and every handler has
	// returned.
	dispatched *sync.WaitGroup
}

// Drain blocks until every reader loop has exited and every handler goroutine
// already dispatched has returned, or until ctx is done, whichever comes first.
//
// Call it only after the context passed to ConsumeWeighted has been cancelled,
// so the readers have stopped pulling new messages; Drain then converges as the
// last in-flight handlers run to completion and ack. It returns nil when
// everything drained, or ctx.Err() when the deadline hit first (in which case
// some handlers may still be running and their events will be redelivered).
func (w *Weighted) Drain(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		w.dispatched.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// supervisor owns the set of consumer units and scales their count between 1
// and MaxConsumers. units is mutated only from run, so it needs no lock.
type supervisor struct {
	ctx      context.Context
	app      *newrelic.Application
	lanes    []WeightedLane
	reserves []int
	policy   ScalePolicy
	log      *zap.Logger

	// dispatched counts every reader loop and every dispatched handler across all
	// units (including retired ones), so Drain can wait for in-flight work on
	// shutdown. See Weighted for why the counting is race-free.
	dispatched *sync.WaitGroup

	units []*consumerUnit
}

func (s *supervisor) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var saturatedSince, calmSince time.Time

	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-ticker.C:
			// A new unit is justified only when every existing unit is already
			// pinned at MaxRoutines and saturated; retiring one is safe only
			// when every unit is calm.
			allMaxSaturated, allCalm := true, true
			for _, u := range s.units {
				inflight, capacity := u.pool.stats()
				if !(capacity >= s.policy.MaxRoutines && inflight >= capacity) {
					allMaxSaturated = false
				}
				if inflight*2 > capacity {
					allCalm = false
				}
			}

			switch {
			case len(s.units) < s.policy.MaxConsumers && allMaxSaturated:
				calmSince = time.Time{}
				if saturatedSince.IsZero() {
					saturatedSince = now
				}
				if now.Sub(saturatedSince) >= s.policy.ScaleUpAfter {
					s.addUnit()
					saturatedSince = now // wait a full window before the next
				}
			case len(s.units) > 1 && allCalm:
				saturatedSince = time.Time{}
				if calmSince.IsZero() {
					calmSince = now
				}
				if now.Sub(calmSince) >= s.policy.ScaleDownAfter {
					s.retireUnit()
					calmSince = now
				}
			default:
				saturatedSince, calmSince = time.Time{}, time.Time{}
			}
		}
	}
}

func (s *supervisor) addUnit() {
	u, err := s.startUnit()
	if err != nil {
		s.log.Error("failed to start consumer unit", zap.Error(err))
		return
	}
	s.units = append(s.units, u)
	s.log.Info("weighted consumer added", zap.Int("consumers", len(s.units)))
}

func (s *supervisor) retireUnit() {
	last := len(s.units) - 1
	u := s.units[last]
	s.units = s.units[:last]
	go u.stop() // cancel + drain off the supervisor's path
	s.log.Info("weighted consumer retired", zap.Int("consumers", len(s.units)))
}

// startUnit brings up one consumer unit: its own subscriptions to every lane,
// its own routine pool sized at the floor, and its own routine-tier autoscaler.
func (s *supervisor) startUnit() (*consumerUnit, error) {
	uctx, cancel := context.WithCancel(s.ctx)

	pool := newRoutinePool(s.reserves, s.policy.MinRoutines)
	go func() {
		<-uctx.Done()
		pool.close()
	}()

	wg, err := startReaders(uctx, s.app, s.lanes, pool, s.dispatched, s.log)
	if err != nil {
		cancel()
		return nil, err
	}

	u := &consumerUnit{pool: pool, cancel: cancel, wg: wg}
	go u.autoscaleRoutines(uctx, s.policy, s.log)

	return u, nil
}

// consumerUnit is one independent consumer: its own pool, subscriptions, and
// routine-tier autoscaler. The pool's capacity is the unit's current routine
// count, scaling between MinRoutines and MaxRoutines on the unit's own load.
type consumerUnit struct {
	pool   *routinePool
	cancel context.CancelFunc
	wg     *sync.WaitGroup
}

func (u *consumerUnit) stop() {
	u.cancel()
	u.wg.Wait()
}

func (u *consumerUnit) autoscaleRoutines(ctx context.Context, policy ScalePolicy, log *zap.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var saturatedSince, calmSince time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			inflight, capacity := u.pool.stats()

			switch {
			case inflight >= capacity && capacity < policy.MaxRoutines: // saturated, room to grow
				calmSince = time.Time{}
				if saturatedSince.IsZero() {
					saturatedSince = now
				}
				if now.Sub(saturatedSince) >= policy.ScaleUpAfter {
					u.pool.setCapacity(capacity + 1)
					saturatedSince = now
					log.Debug("consumer routines scaled up", zap.Int("routines", capacity+1))
				}
			case inflight*2 <= capacity && capacity > policy.MinRoutines: // calm, room to shrink
				saturatedSince = time.Time{}
				if calmSince.IsZero() {
					calmSince = now
				}
				if now.Sub(calmSince) >= policy.ScaleDownAfter {
					u.pool.setCapacity(capacity - 1)
					calmSince = now
					log.Debug("consumer routines scaled down", zap.Int("routines", capacity-1))
				}
			default:
				saturatedSince, calmSince = time.Time{}, time.Time{}
			}
		}
	}
}

func (p ScalePolicy) normalized() ScalePolicy {
	if p.MinRoutines < 1 {
		p.MinRoutines = 1
	}
	if p.MaxRoutines < p.MinRoutines {
		p.MaxRoutines = p.MinRoutines
	}
	if p.MaxConsumers < 1 {
		p.MaxConsumers = 1
	}
	if p.ScaleUpAfter <= 0 {
		p.ScaleUpAfter = 5 * time.Second
	}
	if p.ScaleDownAfter <= 0 {
		p.ScaleDownAfter = 30 * time.Second
	}
	return p
}

// startReaders subscribes to every lane under ctx and runs one reader goroutine
// per lane that pulls messages, claims a pool slot (blocking under
// backpressure), and dispatches the handler on its own goroutine. The returned
// WaitGroup is done when every reader loop has exited (ctx cancelled, channels
// closed); messages already dispatched keep running and release their slot on
// completion. A Subscribe failure aborts: ctx is left to the caller to cancel.
//
// dispatched counts both the reader loops and the dispatched handlers so a
// graceful shutdown can wait for in-flight work (see Weighted.Drain). Counting
// the reader in the same group keeps the count positive while the reader can
// still add handlers, so a handler's Add never races the Wait.
func startReaders(ctx context.Context, app *newrelic.Application, lanes []WeightedLane, pool *routinePool, dispatched *sync.WaitGroup, log *zap.Logger) (*sync.WaitGroup, error) {

	var wg sync.WaitGroup

	for i, lane := range lanes {
		messages, err := lane.Sub.Subscribe(ctx, lane.Subject)
		if err != nil {
			return nil, err
		}

		wg.Add(1)
		dispatched.Add(1)
		go func(lane int, subject string, handle func(*message.Message) error, msgs <-chan *message.Message) {
			defer wg.Done()
			defer dispatched.Done()
			for msg := range msgs {
				if !pool.acquire(lane) {
					// Pool is shutting down: hand the message back unprocessed.
					msg.Nack()
					return
				}
				dispatched.Add(1)
				go func(m *message.Message) {
					defer dispatched.Done()
					defer pool.release(lane)
					process(app, subject, m, handle, log)
				}(msg)
			}
		}(i, lane.Subject, lane.Handle, messages)
	}

	return &wg, nil
}

// routinePool is the shared, resizable admission gate. inflight is the number
// of running handler goroutines; laneInflight tracks them per lane so a lane's
// reserve can be honoured. A lane may hold at most capacity minus the slots
// reserved for the other lanes, so each reserved lane always keeps its share.
type routinePool struct {
	mu           sync.Mutex
	cond         *sync.Cond
	capacity     int
	totalReserve int
	inflight     int
	laneInflight []int
	laneReserve  []int
	closed       bool
}

func newRoutinePool(laneReserve []int, capacity int) *routinePool {
	total := 0
	for _, r := range laneReserve {
		total += r
	}
	p := &routinePool{
		capacity:     capacity,
		totalReserve: total,
		laneInflight: make([]int, len(laneReserve)),
		laneReserve:  laneReserve,
	}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// laneLimit is the most slots lane may hold: the whole pool minus the slots
// reserved for every other lane. The slots reserved for the other lanes are
// rounded UP so a reserving lane keeps its share even in a small pool: a 25%
// reserve must still hold one slot in a 2-slot pool, where truncating the
// product down would round the reservation to zero and let a flood on the
// unreserved lane take the whole pool. Every lane keeps at least one slot, so a
// pool that has shrunk to a single routine never starves a lane completely
// (at capacity 1 no reservation can be honoured anyway).
func (p *routinePool) laneLimit(lane int) int {
	otherReserve := p.totalReserve - p.laneReserve[lane]
	limit := p.capacity - ceilDiv(p.capacity*otherReserve, 100)
	if limit < 1 {
		limit = 1
	}
	return limit
}

// ceilDiv returns ceil(a/b) for a >= 0 and b > 0 without floating point, so a
// fractional reserved slot rounds up to a whole reserved slot.
func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}

// acquire blocks until a slot is free for lane, returning false only when the
// pool is closing (ctx cancelled) so the caller stops pulling.
func (p *routinePool) acquire(lane int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for !p.closed && (p.inflight >= p.capacity || p.laneInflight[lane] >= p.laneLimit(lane)) {
		p.cond.Wait()
	}
	if p.closed {
		return false
	}
	p.inflight++
	p.laneInflight[lane]++
	return true
}

func (p *routinePool) release(lane int) {
	p.mu.Lock()
	p.inflight--
	p.laneInflight[lane]--
	p.mu.Unlock()
	p.cond.Broadcast()
}

func (p *routinePool) setCapacity(n int) {
	p.mu.Lock()
	p.capacity = n
	p.mu.Unlock()
	p.cond.Broadcast()
}

func (p *routinePool) stats() (inflight, capacity int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inflight, p.capacity
}

func (p *routinePool) close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.cond.Broadcast()
}
