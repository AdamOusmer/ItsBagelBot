package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

// ConsumeWeighted is the central, autoscaling consumer that sits between NATS
// and the pipeline:
//
//	NATS lanes -> ConsumeWeighted -> consumers, each with its own routine pool
//
// It runs one or more consumer units. Each unit owns its own subscriptions to
// every lane, its own bounded admission gate, and a fleet of persistent worker
// routines that run the lane handlers. A reader never spawns anything: it claims
// a gate slot for its lane and hands the message to the fleet over a channel.
// The gate and the fleet grow or shrink together with load, along two
// independent tiers:
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
// Ack discipline matches Consume: a message's worker runs the handler to
// completion and only then acks (nil) or nacks (error), so the redelivery
// retry budget is preserved. Handlers must be safe for concurrent use.
//
// ConsumeWeighted returns once the first unit is running; the units and the
// supervisor stop when ctx is cancelled. The returned *Weighted lets a graceful
// shutdown wait for handlers already dispatched to finish (Drain) before the
// publishers they emit onto are closed.
type WeightedLane struct {
	Sub     Subscriber
	Subject string
	Handle  func(*Message) error
	// Reserve is the percentage (0..100) of the pool kept exclusively for this
	// lane. Reserves across lanes must sum to at most 100.
	Reserve int
}

// ScalePolicy bounds and paces the autoscaler. Zero values are replaced with
// safe defaults.
type ScalePolicy struct {
	MinRoutines    int           // floor for routines per consumer (>= 1)
	MaxRoutines    int           // ceiling for routines per consumer
	MinConsumers   int           // connection/subscription floor (>= 1)
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

	// Start the subscription floor synchronously so short bursts have their
	// intended connection parallelism before this scaler or KEDA polls.
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

// Weighted is the handle ConsumeWeighted returns; Drain is its only method.
type Weighted struct {
	// dispatched counts every reader loop and every dispatch handed to a worker,
	// across all units. It starts positive (the first unit's readers are added
	// before ConsumeWeighted returns, so before any Drain) and stays positive
	// while any reader is alive, so a dispatch's Add never races Drain's Wait: only
	// a reader adds, only a live reader can add, and every reader is itself counted
	// until it exits. The counter therefore reaches zero only once every reader has
	// exited and every dispatch that ever crossed the channel has been run to
	// completion by a worker.
	//
	// Persistent workers do not weaken that: a worker's Done is deferred inside
	// workerPool.handle, and the work channel is closed only after every reader has
	// exited, so no counted dispatch can be stranded in the channel by a shutdown.
	dispatched *sync.WaitGroup
}

// Drain blocks until every reader loop has exited and every dispatch already
// handed to a worker has been run to completion, or until ctx is done, whichever
// comes first.
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

	// dispatched counts every reader loop and every dispatch across all units
	// (including retired ones), so Drain can wait for in-flight work on shutdown.
	// See Weighted for why the counting is race-free.
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
	go u.stop() // cancel + drain off the supervisor's path
	s.log.Info("weighted consumer retired", zap.Int("consumers", len(s.units)))
}

// startUnit brings up one consumer unit: its own subscriptions to every lane,
// its admission gate and worker fleet sized at the floor, its readers, and its
// own routine-tier autoscaler.
//
// Nothing is started until every subscription is up, so a Subscribe failure
// leaves no reader and no worker behind: cancelling uctx releases the
// subscriptions that did come up and there is nothing else to unwind.
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

// newConsumeLanes bundles each lane's invariants (subject, transaction name,
// handler) once per unit. They never change between deliveries, so a dispatch
// carries only the message and its lane index and the worker looks the rest up.
func newConsumeLanes(app *newrelic.Application, lanes []WeightedLane, log *zap.Logger) []consumeLane {
	procs := make([]consumeLane, len(lanes))
	for i, lane := range lanes {
		procs[i] = newConsumeLane(app, lane.Subject, lane.Handle, log)
	}
	return procs
}

// consumerUnit is one independent consumer: its own admission gate, worker
// fleet, subscriptions, and routine-tier autoscaler. The gate's capacity is the
// unit's current routine count, scaling between MinRoutines and MaxRoutines on
// the unit's own load, and the fleet follows it.
type consumerUnit struct {
	pool    *routinePool
	workers *workerPool
	cancel  context.CancelFunc
	// done is closed once every reader has exited and the fleet has drained the
	// work channel. It is driven by awaitDrain rather than by stop, because units
	// that outlive the supervisor are torn down by context cancellation alone and
	// stop is never called on them.
	done chan struct{}
}

// awaitDrain retires the unit's machinery in the only order that cannot strand a
// counted dispatch: readers first (after which nothing can send), then the work
// channel closed, then the fleet drained to the last buffered message.
func (u *consumerUnit) awaitDrain(readers *sync.WaitGroup) {
	defer close(u.done)
	readers.Wait()
	u.workers.stop()
}

func (u *consumerUnit) stop() {
	u.cancel()
	<-u.done
}

// setRoutines moves the unit to n routines: n admission slots and n workers.
//
// The order is the contract. Growing spawns before the gate widens; shrinking
// narrows the gate before it retires. Either way live workers >= capacity holds
// at every instant, which is what makes an admitted message's slot worth a
// worker rather than a place in a queue (see workerPool).
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
			case inflight >= capacity && capacity < policy.MaxRoutines: // saturated, room to grow
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
			case inflight*2 <= capacity && capacity > policy.MinRoutines: // calm, room to shrink
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

// subscribeLanes subscribes to every lane under ctx before any reader exists, so
// a failure on a later lane leaves nothing running: cancelling ctx releases the
// subscriptions that did come up.
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

// dispatch is the unit of work a reader hands to the fleet: the message and the
// index of the lane it arrived on. It is deliberately two words wide and holds
// no closure, so a dispatch is a plain copy into a pre-sized channel buffer —
// nothing on the reader's serial path escapes to the heap.
type dispatch struct {
	msg  *Message
	lane int
}

// startReaders runs one reader goroutine per lane. A reader pulls messages,
// claims a gate slot for its lane (blocking under backpressure, honouring the
// lane reserves), and hands the message to the persistent worker fleet.
//
// That hand-off is the whole point of the fleet. The reader loop is serial, so
// whatever it costs per message is the lane's dispatch ceiling, and a `go`
// statement here was the expensive part of it: ~250-360ns of spawn and scheduler
// churn plus 40 bytes of closure per message, against ~160-200ns and no
// allocation at all for the send (BenchmarkDispatchGoroutinePerMessage against
// BenchmarkDispatchHandoff, M1 Pro, GOMAXPROCS 2 through 10). At 100k msg/s per
// pod that is a couple of percent of a core and ~4MB/s of garbage the reader no
// longer makes before any handler has run. What is left of the loop is the
// admission check, one WaitGroup increment and one channel send.
//
// The returned WaitGroup is done when every reader loop has exited (ctx
// cancelled, channels closed, or the gate closed); dispatches already handed
// over keep running and release their slot on completion.
//
// dispatched counts both the reader loops and every dispatch so a graceful
// shutdown can wait for in-flight work (see Weighted.Drain). Counting the reader
// in the same group keeps the count positive while the reader can still add, so
// a dispatch's Add never races the Wait; the matching Done is the worker's, run
// after the handler returns.
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
					// Gate is shutting down: hand the message back unprocessed.
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

// workerPool is the unit's persistent handler fleet: long-lived goroutines that
// receive dispatches and run the lane handler on them. Its size follows the
// admission gate's capacity, so it is resizable without being respawned.
//
// Fleet size >= gate capacity is the load-bearing relation, and it is what keeps
// a lane's reserve meaningful under the shared channel. At any instant every
// admitted message is either running in a worker, sitting in the work buffer, or
// on its way from a reader, so
//
//	buffered + sending = admitted - running <= capacity - running <= workers - running
//
// and the right-hand side is exactly the number of workers not running anything.
// Every queued dispatch therefore has an idle worker waiting for it: a premium
// message that claims its reserved slot is picked up within a scheduler hop, not
// behind a standard handler's full runtime. consumerUnit.setRoutines maintains
// the relation across resizes by spawning before the gate widens and retiring
// after it narrows.
//
// The fleet is a throughput knob, never a correctness bound: concurrency is
// capped by the gate whatever the worker count happens to be mid-resize.
type workerPool struct {
	work   chan dispatch
	retire chan struct{}

	procs      []consumeLane
	gate       *routinePool
	dispatched *sync.WaitGroup

	// alive is done when every spawned worker has returned; live is the same
	// count readable without blocking, for the scale logs and the resize tests.
	alive sync.WaitGroup
	live  atomic.Int64

	mu      sync.Mutex
	desired int
}

// newWorkerPool builds an idle fleet over procs. max is the largest capacity the
// gate can be given (ScalePolicy.MaxRoutines); it sizes both channels:
//
//	work: the gate admits at most capacity <= max messages at once and each one
//	holds its slot until a worker is done with it, so at most max dispatches can
//	be outstanding and the reader's send never blocks. The admission check stays
//	the single point of backpressure. A capacity somehow raised past max would
//	only make the send block — backpressure, never loss.
//
//	retire: one token per worker the fleet is shrinking by. Tokens outstanding
//	are live minus desired, which is bounded by max for the same reason.
func newWorkerPool(procs []consumeLane, gate *routinePool, dispatched *sync.WaitGroup, max int) *workerPool {
	return &workerPool{
		work:       make(chan dispatch, max),
		retire:     make(chan struct{}, max),
		procs:      procs,
		gate:       gate,
		dispatched: dispatched,
	}
}

// resize moves the fleet towards n workers, one step per unit of difference.
// Growth cancels a pending retirement in preference to spawning, so the ±1 the
// autoscaler applies each second cannot churn goroutines when it oscillates
// around a step: a shrink immediately followed by a grow costs nothing at all.
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

// growLocked adds one worker. A token still in the retire buffer belongs to a
// worker that has not acted on it yet — taking it back keeps that worker instead
// of spawning a replacement for it.
func (w *workerPool) growLocked() {
	select {
	case <-w.retire:
	default:
		w.live.Add(1)
		w.alive.Add(1)
		go w.run()
	}
}

// shrinkLocked retires one worker by leaving it a token. The default arm is
// unreachable while capacity stays within max (see newWorkerPool); if it ever
// fires the surplus worker simply stays, which costs an idle goroutine and
// breaks nothing, because the gate — not the worker count — bounds concurrency.
func (w *workerPool) shrinkLocked() {
	select {
	case w.retire <- struct{}{}:
	default:
	}
}

// run is one persistent worker. It leaves the loop only between messages, either
// to retire on a token from resize or at shutdown when the work channel closes,
// so a shrink can never kill a handler mid-message.
//
// Shutdown cannot strand a counted dispatch. The channel is closed only after
// every reader has exited (consumerUnit.awaitDrain), and the select's retire arm
// cannot empty the fleet ahead of the buffer: tokens outstanding are live minus
// desired, so at least desired (>= MinRoutines >= 1) workers have no token to
// take and stay until the receive reports the channel closed and drained.
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

// handle runs one dispatch: the lane's handler inside its own New Relic
// transaction under the shared ack discipline (consumeLane.process), then the
// slot release and the Drain bookkeeping the reader counted up before the send.
func (w *workerPool) handle(d dispatch) {
	defer w.dispatched.Done()
	defer w.gate.release(d.lane)
	w.procs[d.lane].process(d.msg)
}

// stop closes the work channel and waits for the fleet to drain it. It is safe
// only after every reader has exited, so that nothing can send on a closed
// channel; awaitDrain is the one caller that guarantees it.
func (w *workerPool) stop() {
	close(w.work)
	w.alive.Wait()
}

// liveWorkers is the number of spawned workers that have not returned yet.
func (w *workerPool) liveWorkers() int {
	return int(w.live.Load())
}

// routinePool is the shared, resizable admission gate. inflight is the number of
// admitted messages — running in a worker or queued for one — and laneInflight
// tracks them per lane so a lane's reserve can be honoured. A lane may hold at
// most capacity minus the slots reserved for the other lanes, so each reserved
// lane always keeps its share.
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

// setCapacity resizes the gate. Callers on the scaling path must go through
// consumerUnit.setRoutines instead, which orders this against the worker fleet
// so live workers >= capacity holds throughout the move.
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
