// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

type WeightedLane struct {
	Sub     Subscriber
	Subject string
	Handle  func(*Message) error
	Reserve int
}

type ScalePolicy struct {
	MinRoutines    int
	MaxRoutines    int
	MinConsumers   int
	MaxConsumers   int
	ScaleUpAfter   time.Duration
	ScaleDownAfter time.Duration
}

// Handlers must be safe for concurrent use.
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

	for len(s.units) < policy.MinConsumers {
		unit, err := s.startUnit()
		if err != nil {
			for _, started := range s.units {
				go started.stop()
			}
			return nil, err
		}
		s.units = append(s.units, unit)
	}

	go s.run()

	return &Weighted{dispatched: s.dispatched}, nil
}

type Weighted struct {
	dispatched *sync.WaitGroup
}

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

type supervisor struct {
	ctx      context.Context
	app      *newrelic.Application
	lanes    []WeightedLane
	reserves []int
	policy   ScalePolicy
	log      *zap.Logger

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
					saturatedSince = now
				}
			case len(s.units) > s.policy.MinConsumers && allCalm:
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
	go u.stop()
	s.log.Info("weighted consumer retired", zap.Int("consumers", len(s.units)))
}

func (s *supervisor) startUnit() (*consumerUnit, error) {
	uctx, cancel := context.WithCancel(s.ctx)

	channels, err := subscribeLanes(uctx, s.lanes)
	if err != nil {
		cancel()
		return nil, err
	}

	pool := newRoutinePool(s.reserves, s.policy.MinRoutines)
	go func() {
		<-uctx.Done()
		pool.close()
	}()

	workers := newWorkerPool(newConsumeLanes(s.app, s.lanes, s.log), pool, s.dispatched, s.policy.MaxRoutines)
	workers.resize(s.policy.MinRoutines)

	readers := startReaders(channels, pool, workers.work, s.dispatched)

	u := &consumerUnit{pool: pool, workers: workers, cancel: cancel, done: make(chan struct{})}
	go u.awaitDrain(readers)
	go u.autoscaleRoutines(uctx, s.policy, s.log)

	return u, nil
}

func newConsumeLanes(app *newrelic.Application, lanes []WeightedLane, log *zap.Logger) []consumeLane {
	procs := make([]consumeLane, len(lanes))
	for i, lane := range lanes {
		procs[i] = newConsumeLane(app, lane.Subject, lane.Handle, log)
	}
	return procs
}

type consumerUnit struct {
	pool    *routinePool
	workers *workerPool
	cancel  context.CancelFunc
	done    chan struct{}
}

// Order is required: readers, then close work, then drain the pool.
func (u *consumerUnit) awaitDrain(readers *sync.WaitGroup) {
	defer close(u.done)
	readers.Wait()
	u.workers.stop()
}

func (u *consumerUnit) stop() {
	u.cancel()
	<-u.done
}

// Spawn before widening the gate and narrow it before retiring, so workers never drop below capacity.
func (u *consumerUnit) setRoutines(n int) {
	_, capacity := u.pool.stats()
	if n > capacity {
		u.workers.resize(n)
		u.pool.setCapacity(n)
		return
	}
	u.pool.setCapacity(n)
	u.workers.resize(n)
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
			case inflight >= capacity && capacity < policy.MaxRoutines:
				calmSince = time.Time{}
				if saturatedSince.IsZero() {
					saturatedSince = now
				}
				if now.Sub(saturatedSince) >= policy.ScaleUpAfter {
					u.setRoutines(capacity + 1)
					saturatedSince = now
					log.Debug("consumer routines scaled up",
						zap.Int("routines", capacity+1), zap.Int("workers", u.workers.liveWorkers()))
				}
			case inflight*2 <= capacity && capacity > policy.MinRoutines:
				saturatedSince = time.Time{}
				if calmSince.IsZero() {
					calmSince = now
				}
				if now.Sub(calmSince) >= policy.ScaleDownAfter {
					u.setRoutines(capacity - 1)
					calmSince = now
					log.Debug("consumer routines scaled down",
						zap.Int("routines", capacity-1), zap.Int("workers", u.workers.liveWorkers()))
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
	if p.MinConsumers < 1 {
		p.MinConsumers = 1
	}
	if p.MaxConsumers < p.MinConsumers {
		p.MaxConsumers = p.MinConsumers
	}
	if p.ScaleUpAfter <= 0 {
		p.ScaleUpAfter = 5 * time.Second
	}
	if p.ScaleDownAfter <= 0 {
		p.ScaleDownAfter = 30 * time.Second
	}
	return p
}

func subscribeLanes(ctx context.Context, lanes []WeightedLane) ([]<-chan *Message, error) {
	channels := make([]<-chan *Message, len(lanes))
	for i, lane := range lanes {
		messages, err := lane.Sub.Subscribe(ctx, lane.Subject)
		if err != nil {
			return nil, err
		}
		channels[i] = messages
	}
	return channels, nil
}

type dispatch struct {
	msg  *Message
	lane int
}

func startReaders(channels []<-chan *Message, pool *routinePool, work chan<- dispatch, dispatched *sync.WaitGroup) *sync.WaitGroup {

	var wg sync.WaitGroup

	for i, messages := range channels {
		wg.Add(1)
		dispatched.Add(1)
		go func(lane int, msgs <-chan *Message) {
			defer wg.Done()
			defer dispatched.Done()
			for msg := range msgs {
				if !pool.acquire(lane) {
					msg.Nack()
					return
				}
				dispatched.Add(1)
				work <- dispatch{msg: msg, lane: lane}
			}
		}(i, messages)
	}

	return &wg
}

type workerPool struct {
	work   chan dispatch
	retire chan struct{}

	procs      []consumeLane
	gate       *routinePool
	dispatched *sync.WaitGroup

	alive sync.WaitGroup
	live  atomic.Int64

	mu      sync.Mutex
	desired int
}

func newWorkerPool(procs []consumeLane, gate *routinePool, dispatched *sync.WaitGroup, max int) *workerPool {
	return &workerPool{
		work:       make(chan dispatch, max),
		retire:     make(chan struct{}, max),
		procs:      procs,
		gate:       gate,
		dispatched: dispatched,
	}
}

func (w *workerPool) resize(n int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for w.desired < n {
		w.desired++
		w.growLocked()
	}
	for w.desired > n {
		w.desired--
		w.shrinkLocked()
	}
}

func (w *workerPool) growLocked() {
	select {
	case <-w.retire:
	default:
		w.live.Add(1)
		w.alive.Add(1)
		go w.run()
	}
}

func (w *workerPool) shrinkLocked() {
	select {
	case w.retire <- struct{}{}:
	default:
	}
}

func (w *workerPool) run() {
	defer w.alive.Done()
	defer w.live.Add(-1)

	for {
		select {
		case <-w.retire:
			return
		case d, ok := <-w.work:
			if !ok {
				return
			}
			w.handle(d)
		}
	}
}

func (w *workerPool) handle(d dispatch) {
	defer w.dispatched.Done()
	defer w.gate.release(d.lane)
	w.procs[d.lane].process(d.msg)
}

// Call only after every reader exited.
func (w *workerPool) stop() {
	close(w.work)
	w.alive.Wait()
}

func (w *workerPool) liveWorkers() int {
	return int(w.live.Load())
}

type routinePool struct {
	mu sync.Mutex

	conds   []*sync.Cond
	waiters []int

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
		waiters:      make([]int, len(laneReserve)),
	}
	for range laneReserve {
		p.conds = append(p.conds, sync.NewCond(&p.mu))
	}
	return p
}

func (p *routinePool) laneLimit(lane int) int {
	otherReserve := p.totalReserve - p.laneReserve[lane]
	limit := p.capacity - ceilDiv(p.capacity*otherReserve, 100)
	if limit < 1 {
		limit = 1
	}
	return limit
}

func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}

func (p *routinePool) acquire(lane int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.waiters[lane]++
	for !p.closed && (p.inflight >= p.capacity || p.laneInflight[lane] >= p.laneLimit(lane)) {
		p.conds[lane].Wait()
	}
	p.waiters[lane]--
	if p.closed {
		return false
	}
	p.inflight++
	p.laneInflight[lane]++
	return true
}

func (p *routinePool) release(lane int) {
	p.mu.Lock()
	saturated := p.inflight >= p.capacity
	p.inflight--
	p.laneInflight[lane]--
	wake := p.waiters[lane] > 0
	p.mu.Unlock()
	if saturated {
		for _, c := range p.conds {
			c.Broadcast()
		}
	} else if wake {
		p.conds[lane].Signal()
	}
}

// Scaling callers must go through consumerUnit.setRoutines, which orders this against the workers.
func (p *routinePool) setCapacity(n int) {
	p.mu.Lock()
	p.capacity = n
	for _, c := range p.conds {
		c.Broadcast()
	}
	p.mu.Unlock()
}

func (p *routinePool) stats() (inflight, capacity int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inflight, p.capacity
}

func (p *routinePool) close() {
	p.mu.Lock()
	p.closed = true
	for _, c := range p.conds {
		c.Broadcast()
	}
	p.mu.Unlock()
}
