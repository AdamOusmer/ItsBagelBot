package main

import (
	"context"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/bus"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

// pacerMinSleep is the shortest gap the pacer will actually sleep for. Below it
// the send proceeds early and the absolute schedule pulls the rate back on the
// next message: one timer per message at six figures a second costs more
// scheduler time than it paces, and the jitter it adds lands in the latency
// numbers the run exists to report.
const pacerMinSleep = time.Millisecond

// pendingDupLimit bounds the injected-duplicate backlog per lane. A lane that
// has stalled must not turn its duplicate queue into unbounded memory; dropping
// the overflow and counting it keeps the guard's sample honest about its own
// size.
const pendingDupLimit = 1 << 16

// samplePoolSize bounds the confirmed publishes in flight for measurement. A
// confirmed publish waits out its cohort's commit, so taking that wait on the
// lane goroutine makes the sampler the run's constraint the moment the round
// trip is long: at 152 ms it capped a fleet-wide 8192-lane run at 54k msg/s.
// Sixty-four outstanding keeps a 1-in-512 sample populated at six figures a
// second, and is small enough that draining the pool at the end of a step is a
// bounded wait rather than a second measurement.
const samplePoolSize = 64

type publisherConfig struct {
	URL          string
	Subject      string
	Lanes        int
	Replicas     int
	PublisherID  string
	PayloadBytes int
	DupFraction  float64
	DupDelay     time.Duration
	SampleEvery  int
	SampleCap    int
	Plan         rampPlan
	Detector     ceilingDetector
	StableFrac   float64
	SoakFor      time.Duration
	TickEvery    time.Duration
	LatencyTol   float64
}

// pacer converts a target rate into send times against an absolute schedule, so
// a slow patch is caught up rather than silently lowering the offered rate. That
// is what makes the load open-loop: the offered count is decided by the plan, and
// falling behind shows up as achieved < offered instead of as a smaller demand.
type pacer struct {
	start  time.Time
	period time.Duration
	n      int64
	timer  *time.Timer
}

func newPacer(start time.Time, rate float64) *pacer {
	period := time.Duration(float64(time.Second) / rate)
	if period < 1 {
		period = 1
	}
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	return &pacer{start: start, period: period, timer: timer}
}

func (p *pacer) stop() { p.timer.Stop() }

func (p *pacer) wait(done <-chan struct{}) bool {
	p.n++
	delay := time.Until(p.start.Add(time.Duration(p.n) * p.period))
	if delay < pacerMinSleep {
		select {
		case <-done:
			return false
		default:
			return true
		}
	}
	p.timer.Reset(delay)
	select {
	case <-p.timer.C:
		return true
	case <-done:
		p.timer.Stop()
		return false
	}
}

// pendingDup is one injected duplicate waiting for its slot. It carries the
// event id, never the bytes: the second copy is re-encoded with its own wire
// sequence, because the whole point of the injection is two wire positions under
// one logical identity.
type pendingDup struct {
	id  string
	due time.Time
}

// laneState is one ordered wire stream. Everything here is single-goroutine, so
// the sequence counter and the RNG need no synchronisation — which matters, since
// this is the loop that has to reach six figures a second.
type laneState struct {
	index     int
	key       string
	seq       uint64
	rng       *rand.Rand
	immediate []string
	delayed   []pendingDup
	dropped   int64
}

func newLanes(publisher string, count int) []*laneState {
	lanes := make([]*laneState, 0, max(count, 1))
	for index := 0; index < max(count, 1); index++ {
		lanes = append(lanes, newLaneState(publisher, index))
	}
	return lanes
}

func newLaneState(publisher string, index int) *laneState {
	return &laneState{
		index: index,
		key:   laneKey(publisher, index),
		rng:   rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
}

// nextDup pops a duplicate that is due. Immediate ones (the back-to-back
// variant) go first; the delayed queue is FIFO because the delay is constant, so
// its head is always the earliest due.
func (l *laneState) nextDup(now time.Time) (string, bool) {
	if len(l.immediate) > 0 {
		id := l.immediate[0]
		l.immediate = l.immediate[1:]
		return id, true
	}
	if len(l.delayed) > 0 && !now.Before(l.delayed[0].due) {
		head := l.delayed[0]
		l.delayed = l.delayed[1:]
		return head.id, true
	}
	return "", false
}

// classify assigns an event's guard class and, for a treatment event, schedules
// its second copy. The control set is deliberately the same size as the
// treatment set: without an equally sampled never-duplicated population there is
// nothing to measure false positives against, and "zero false positives" over a
// population of zero is not a result.
func (l *laneState) classify(fraction float64, delay time.Duration, now time.Time, id string) eventClass {
	roll := l.rng.Float64()
	if roll < fraction {
		l.scheduleDup(id, delay, now)
		return classTreatment
	}
	if roll < 2*fraction {
		return classControl
	}
	return classPlain
}

// scheduleDup splits the injected duplicates evenly between the two variants
// that fail differently: a back-to-back copy tests the claim race between two
// handler routines holding the same key at once, a delayed copy tests the claim
// surviving in Valkey after the local LRU entry is no longer the thing answering.
func (l *laneState) scheduleDup(id string, delay time.Duration, now time.Time) {
	if len(l.immediate)+len(l.delayed) >= pendingDupLimit {
		l.dropped++
		return
	}
	if l.rng.IntN(2) == 0 {
		l.immediate = append(l.immediate, id)
		return
	}
	l.delayed = append(l.delayed, pendingDup{id: id, due: now.Add(delay)})
}

// samplePool runs the timed publishes off the lane goroutines. Slots are taken
// without waiting, and that is the whole point: a full pool means the
// measurement is already the slowest thing in the run, and the right answer
// there is to skip the sample, not to hold the offered load until a slot frees.
type samplePool struct {
	slots chan struct{}
	wg    sync.WaitGroup
}

func newSamplePool(size int) *samplePool {
	return &samplePool{slots: make(chan struct{}, max(size, 1))}
}

// offer runs fn on a pooled goroutine, and reports false when the pool was full
// and fn was not run at all.
func (p *samplePool) offer(fn func()) bool {
	select {
	case p.slots <- struct{}{}:
	default:
		return false
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer func() { <-p.slots }()
		fn()
	}()
	return true
}

// wait drains the outstanding samples. A step is scored only after this: a
// confirmed publish still in flight belongs to the step that offered it, and
// charging its verdict to the next step attributes a failure to the wrong rate.
func (p *samplePool) wait() { p.wg.Wait() }

// publishState is the whole publisher role: one production publisher, the lane
// states, and the counters a step is read off.
type publishState struct {
	cfg     publisherConfig
	pub     bus.Publisher
	log     *zap.Logger
	pad     string
	latency *sampler
	samples *samplePool
	// lanes outlive the steps. A fresh laneState per step would restart every
	// sequence at 1, and the consumer would score the whole next step as one
	// enormous regression instead of measuring what it was for.
	lanes    []*laneState
	achieved atomic.Int64
	failures atomic.Int64
	total    atomic.Int64
	sampleN  atomic.Int64
}

func runPublisher(ctx context.Context, cfg publisherConfig, log *zap.Logger) error {
	// The fixed-stream constructor is mandatory here: the shadow subject is not
	// in the fleet catalog, so the ordinary NewPublisher would fail to resolve a
	// stream for it before a single byte reached the wire.
	pub, err := bus.NewPublisherForStream(cfg.URL, stressStreamName, log)
	if err != nil {
		return err
	}
	defer pub.Close()

	state := &publishState{
		cfg: cfg, pub: pub, log: log,
		pad:     padFor(cfg.PayloadBytes),
		latency: newSampler(cfg.SampleCap),
		samples: newSamplePool(samplePoolSize),
		lanes:   newLanes(cfg.PublisherID, cfg.Lanes),
	}
	go state.tick(ctx)

	summary := state.run(ctx)
	emit(summary)
	// A cancelled or expired run is not a failure: the summary above already
	// reports which steps completed and whether any was truncated. Exiting
	// non-zero here would make an operator's Ctrl-C look like a broken rig.
	if err := ctx.Err(); err != nil {
		log.Warn("publisher run ended early", zap.Error(err))
	}
	return nil
}

// run drives the ramp, then the soak. The soak runs even when the ramp never
// breached: an "at least this much" ceiling still deserves a stability check at
// 85% of it, and skipping it would make an under-provisioned run silently
// report less than a saturated one.
func (s *publishState) run(ctx context.Context) publisherSummary {
	started := time.Now()
	steps := s.ramp(ctx)
	reached := s.cfg.Detector.resolve(steps)
	stable := stableRate(reached, s.cfg.StableFrac)

	soak := s.soak(ctx, stable)
	return publisherSummary{
		envelopeHeader: envelopeHeader{Rig: rigName, Role: rolePublish, Kind: kindSummary},
		PublisherID:    s.cfg.PublisherID,
		Replicas:       s.cfg.Replicas,
		Lanes:          s.cfg.Lanes,
		Wire:           publishWire(),
		Connections:    publishConnections(),
		PayloadBytes:   s.cfg.PayloadBytes,
		DupFraction:    s.cfg.DupFraction,
		Subject:        s.cfg.Subject,
		Steps:          steps,
		Ceiling:        reached,
		StableRate:     stable,
		Soak:           soak,
		SoakStable:     soakStable(soak, steps, s.cfg.LatencyTol),
		DupsDropped:    s.dupsDropped(),
		StartedAt:      started,
		EndedAt:        time.Now(),
	}
}

// dupsDropped is the injected-duplicate backlog the lanes had to discard. It is
// reported rather than swallowed because it shrinks the guard's test vector: a
// run with a large number here validated the guard against fewer duplicates than
// its flags claim.
func (s *publishState) dupsDropped() int64 {
	var total int64
	for _, lane := range s.lanes {
		total += lane.dropped
	}
	return total
}

func soakStable(soak *stepResult, steps []stepResult, tolerance float64) bool {
	if soak == nil || len(steps) == 0 {
		return true
	}
	return soak.Latency.stable(steps[0].Latency, tolerance)
}

func (s *publishState) ramp(ctx context.Context) []stepResult {
	var steps []stepResult
	for index := 0; ; index++ {
		rate, ok := s.cfg.Plan.rateAt(index)
		if !ok || ctx.Err() != nil {
			return steps
		}
		result := s.runStep(ctx, index, rate, s.cfg.Plan.Hold, false)
		steps = append(steps, result)
		emit(stepLine{
			envelopeHeader: envelopeHeader{Rig: rigName, Role: rolePublish, Kind: kindStep},
			PublisherID:    s.cfg.PublisherID, Step: result,
		})
		if breached, reason := s.cfg.Detector.breach(result); breached {
			s.log.Warn("ceiling reached", zap.Int("step", index), zap.String("reason", reason))
			return steps
		}
	}
}

func (s *publishState) soak(ctx context.Context, rate float64) *stepResult {
	if rate <= 0 || s.cfg.SoakFor <= 0 || ctx.Err() != nil {
		return nil
	}
	s.log.Info("soaking at the stable rate",
		zap.Float64("msgs_per_sec", rate), zap.Duration("for", s.cfg.SoakFor))
	result := s.runStep(ctx, -1, rate, s.cfg.SoakFor, true)
	emit(stepLine{
		envelopeHeader: envelopeHeader{Rig: rigName, Role: rolePublish, Kind: kindStep},
		PublisherID:    s.cfg.PublisherID, Step: result,
	})
	return &result
}

// stepLine is the per-step record on stdout, so a run that is killed mid-ramp
// still leaves every completed step behind instead of only the summary it never
// reached.
type stepLine struct {
	envelopeHeader
	PublisherID string     `json:"publisher_id"`
	Step        stepResult `json:"step"`
}

// runStep offers one rate for one hold window. The offered count is computed
// from the plan up front and never adjusted, which is what keeps the comparison
// meaningful: achieved is measured, offered is demanded.
func (s *publishState) runStep(ctx context.Context, index int, fleetRate float64, hold time.Duration, isSoak bool) stepResult {
	lanes := len(s.lanes)
	share := fleetRate / float64(max(s.cfg.Replicas, 1))
	laneRate := share / float64(lanes)
	perLane := int64(math.Round(laneRate * hold.Seconds()))

	s.achieved.Store(0)
	s.failures.Store(0)
	// Discarded on purpose: this empties the reservoir so the step reports its
	// own latency instead of the ramp's running distribution.
	s.latency.drain()

	start := time.Now()
	deadline := start.Add(time.Duration(float64(hold) * s.cfg.Plan.Overrun))
	truncated := s.runLanes(ctx, laneRate, perLane, start, deadline)
	// The deferred samples belong to the step that offered them, so they resolve
	// before the flush that closes it and before any counter is read.
	s.samples.wait()
	flushErrors := s.flush(ctx)
	elapsed := time.Since(start)

	return stepResult{
		Index: index, OfferedRate: share, Offered: perLane * int64(lanes),
		Achieved: s.achieved.Load(), Failures: s.failures.Load(), FlushErrors: flushErrors,
		ElapsedSec: elapsed.Seconds(), Truncated: truncated,
		Latency: s.latency.drain(), Soak: isSoak,
	}
}

func (s *publishState) runLanes(ctx context.Context, laneRate float64, perLane int64, start, deadline time.Time) bool {
	var wg sync.WaitGroup
	var truncated atomic.Bool
	for _, lane := range s.lanes {
		wg.Add(1)
		go func(state *laneState) {
			defer wg.Done()
			if s.runLane(ctx, state, laneRate, perLane, start, deadline) {
				truncated.Store(true)
			}
		}(lane)
	}
	wg.Wait()
	return truncated.Load()
}

// runLane sends one lane's share. It reports whether it was cut short by the
// overrun deadline: a lane that could not drain its offered count inside twice
// its hold window has already answered the ceiling question, and letting it run
// on would stretch one step over the wall clock of several and hide the collapse
// inside a single sample.
func (s *publishState) runLane(ctx context.Context, lane *laneState, rate float64, count int64, start, deadline time.Time) bool {
	if rate <= 0 || count <= 0 {
		return false
	}
	pace := newPacer(start, rate)
	defer pace.stop()
	done := ctx.Done()

	for sent := int64(0); sent < count; sent++ {
		if !pace.wait(done) {
			return true
		}
		now := time.Now()
		if now.After(deadline) {
			return true
		}
		s.sendOne(ctx, lane, now)
	}
	return false
}

// sendOne emits either a due duplicate or a fresh event. A duplicate consumes a
// scheduled slot rather than riding on top of one, so the offered rate stays the
// rate the plan asked for instead of drifting up with the injection fraction.
func (s *publishState) sendOne(ctx context.Context, lane *laneState, now time.Time) {
	lane.seq++
	event := envelope{Pub: s.cfg.PublisherID, Lane: lane.index, Seq: lane.seq, SentAt: now.UnixNano(), Pad: s.pad}
	if id, ok := lane.nextDup(now); ok {
		event.EventID, event.Class = id, classTreatment
	} else {
		event.EventID = eventID(s.cfg.PublisherID, lane.index, lane.seq)
		event.Class = lane.classify(s.cfg.DupFraction, s.cfg.DupDelay, now, event.EventID)
	}
	s.publish(ctx, lane.key, &event)
}

// publish sends one event. A sampled event is handed to the pool and its verdict
// arrives later; everything else resolves inline, which is the path that has to
// stay free of anything the measurement does.
func (s *publishState) publish(ctx context.Context, partition string, event *envelope) {
	ctx = bus.WithPublishPartition(ctx, partition)
	s.total.Add(1)
	if s.sampleDue() {
		if s.samples.offer(func() { s.settle(s.timedPublish(ctx, event)) }) {
			return
		}
		// The pool is full, so this event is sent untimed rather than held. The
		// skip is counted next to the distribution it thinned.
		s.latency.skip()
	}
	s.settle(bus.PublishJSON(ctx, s.pub, s.cfg.Subject, event))
}

// sampleDue advances the sample counter and reports whether this send owes one.
// It advances even when the pool then refuses the sample, so the interval stays
// the one the flag asked for instead of drifting with pool pressure.
func (s *publishState) sampleDue() bool {
	return s.cfg.SampleEvery >= 1 && s.sampleN.Add(1)%int64(s.cfg.SampleEvery) == 0
}

// settle scores one publish. It runs on the lane goroutine for an ordinary send
// and on a pooled goroutine for a sampled one, which is why the step waits for
// the pool before it reads these counters.
func (s *publishState) settle(err error) {
	if err != nil {
		s.failures.Add(1)
		return
	}
	s.achieved.Add(1)
}

// timedPublish is bus.PublishConfirmed, the only way to observe publish-to-PubAck
// from outside pkg/bus: it rides the same cohort as everything around it and
// waits for that cohort's acknowledgement, so the number it yields is the real
// one rather than a probe's.
//
// Running it off the lane costs one thing and buys another. The sampled event can
// now reach the wire behind its own successor, so the consumer's lane ledger
// scores one gap and one regression per sample — at one in 512 that is what those
// two counters will read. The alternative was letting the measurement decide the
// offered rate, which is the one thing an open-loop rig must not do.
func (s *publishState) timedPublish(ctx context.Context, event *envelope) error {
	body, err := sonic.ConfigFastest.Marshal(event)
	if err != nil {
		return err
	}
	start := time.Now()
	err = bus.PublishConfirmed(ctx, s.pub, bus.Publication{
		Subject: s.cfg.Subject, ID: event.EventID, Payload: body,
	})
	if err == nil {
		s.latency.record(time.Since(start))
	}
	return err
}

// flush resolves everything admitted during the step before the step is scored.
// Without it a cohort that failed after the last send would be charged to the
// next step, and the ceiling would be attributed to the wrong rate.
func (s *publishState) flush(ctx context.Context) int64 {
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.pub.Flush(flushCtx); err != nil {
		s.log.Error("publisher flush reported a cohort failure", zap.Error(err))
		return 1
	}
	return 0
}

// tick emits the live counters so a run can be watched without waiting for a
// step to close, and so a run killed by an operator still left a trace of where
// it had got to.
func (s *publishState) tick(ctx context.Context) {
	if s.cfg.TickEvery <= 0 {
		return
	}
	ticker := time.NewTicker(s.cfg.TickEvery)
	defer ticker.Stop()
	last := s.total.Load()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := s.total.Load()
			emit(publisherTick{
				envelopeHeader: envelopeHeader{Rig: rigName, Role: rolePublish, Kind: kindTick},
				PublisherID:    s.cfg.PublisherID,
				Attempted:      now,
				RateMsgPerSec:  float64(now-last) / s.cfg.TickEvery.Seconds(),
				Failures:       s.failures.Load(),
			})
			last = now
		}
	}
}

type publisherTick struct {
	envelopeHeader
	PublisherID   string  `json:"publisher_id"`
	Attempted     int64   `json:"attempted"`
	RateMsgPerSec float64 `json:"msgs_per_sec"`
	Failures      int64   `json:"failures"`
}
