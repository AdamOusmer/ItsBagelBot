// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const (
	defaultPublishBatchSize = 128
	defaultPublishBatchWait = time.Millisecond
	defaultPublishAckWait   = 2 * time.Second
	minPublishAckWait       = time.Second
	maxPublishAckWait       = 30 * time.Second
	defaultPublishQueueSize = 16_384
	maxInflightCohorts      = 4
	maxInflightCohortSlots  = 64

	messageIDHeader = "Bagelbot-Message-Id"
)

func publishInflightCohorts() int {
	n := env.GetInt("NATS_PUBLISH_INFLIGHT_COHORTS", maxInflightCohorts)
	return min(max(n, 1), maxInflightCohortSlots)
}

type StreamRouter interface {
	Connection(stream string, poolSize int) int
}

type hashStreamRouter struct{}

func (hashStreamRouter) Connection(stream string, poolSize int) int {
	var hash uint32 = 2166136261
	for i := 0; i < len(stream); i++ {
		hash ^= uint32(stream[i])
		hash *= 16777619
	}
	return int(hash % uint32(poolSize))
}

type publisherPool struct {
	members     []*batchPublisher
	router      StreamRouter
	fixedStream string
}

type batchPublisher struct {
	nc  *nats.Conn
	js  nats.JetStreamContext
	log *zap.Logger

	wire   wireMode
	modern jsapi.JetStream

	mu      sync.RWMutex
	closed  bool
	workers map[string]*publishBatchWorker

	stateMu       sync.Mutex
	accepted      atomic.Uint64
	completed     uint64
	resolved      []publishResolution
	errors        []publishError
	activeFlushes map[uint64]flushState
	nextFlush     uint64

	signal *sync.Cond
}

type publishRequest struct {
	msg       *nats.Msg
	confirmed chan error
	sequence  uint64
}

type publishResolution struct {
	sequence uint64
	err      error
}

type publishError struct {
	first uint64
	last  uint64
	cause error
	count uint64
}

type flushState struct {
	target uint64
	err    error
}

const maxPendingPublishErrors = 128

type publishCommand struct {
	ctx       context.Context
	stream    string
	topic     string
	msgID     string
	payload   []byte
	confirmed bool
}

type publishBatchWorker struct {
	js       nats.JetStreamContext
	requests chan publishRequest
	stop     chan struct{}
	done     chan struct{}
	owner    *batchPublisher
	slots    chan struct{}
	acks     sync.WaitGroup

	timer *time.Timer

	ackTimers sync.Pool

	batchSize int
	batchWait time.Duration

	overlapCommit bool
	ackFirst      bool
	slotHeld      bool
	newAtomic     func() (atomicCohortPublisher, error)
}

func publishAckWait() time.Duration {
	return min(max(env.GetDuration("NATS_PUBLISH_ACK_WAIT", defaultPublishAckWait), minPublishAckWait), maxPublishAckWait)
}

func publishQueueSize() int {
	return min(max(env.GetInt("NATS_PUBLISH_QUEUE_SIZE", defaultPublishQueueSize), 64), 65_536)
}

func newPublisherPool(url string, log *zap.Logger) (Publisher, error) {
	return newPublisherPoolForStream(url, "", log)
}

func newPublisherPoolForStream(url, fixedStream string, log *zap.Logger) (Publisher, error) {
	if log == nil {
		log = zap.NewNop()
	}
	poolSize := env.GetInt("NATS_PUBLISH_CONNECTIONS", 4)
	if poolSize < 1 {
		poolSize = 1
	}
	if poolSize > 32 {
		poolSize = 32
	}
	pool := &publisherPool{members: make([]*batchPublisher, 0, poolSize), router: hashStreamRouter{}, fixedStream: fixedStream}
	wire := publishWireMode()
	for i := 0; i < poolSize; i++ {
		member, err := newBatchPublisherConnection(url, i, wire, log)
		if err != nil {
			_ = pool.Close()
			return nil, err
		}
		pool.members = append(pool.members, member)
	}
	return pool, nil
}

func newBatchPublisherConnection(url string, index int, wire wireMode, log *zap.Logger) (*batchPublisher, error) {
	nc, err := nats.Connect(busPublishURL(endpoint(url)), busOptions(clientName(fmt.Sprintf("batch-publisher-%d", index)))...)
	if err != nil {
		return nil, fmt.Errorf("bus: connect batch publisher: %w", err)
	}
	js, err := nc.JetStream(append(jsDomainOption(), nats.PublishAsyncTimeout(publishAckWait()))...)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bus: jetstream batch publisher: %w", err)
	}
	modern, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bus: modern jetstream batch publisher: %w", err)
	}
	publisher := &batchPublisher{
		nc: nc, js: js, modern: modern, log: log, wire: wire,
		workers: make(map[string]*publishBatchWorker),
	}
	publisher.signal = sync.NewCond(&publisher.stateMu)
	return publisher, nil
}

var nuidPool = sync.Pool{New: func() any { return nuid.New() }}

var wireMsgPool = sync.Pool{New: func() any { return nats.NewMsg("") }}

func nextNUID() string {
	generator := nuidPool.Get().(*nuid.NUID)
	id := generator.Next()
	nuidPool.Put(generator)
	return id
}

func (p *publisherPool) PublishOwned(ctx context.Context, topic string, payload []byte) error {
	return p.publish(publishCommand{ctx: ctx, topic: topic, msgID: nextNUID(), payload: payload})
}

func (p *publisherPool) PublishOwnedWithID(ctx context.Context, topic, msgID string, payload []byte) error {
	if msgID == "" {
		return errors.New("bus: confirmed publish requires a message ID")
	}
	return p.publish(publishCommand{ctx: ctx, topic: topic, msgID: msgID, payload: payload, confirmed: true})
}

func (p *publisherPool) publish(command publishCommand) error {
	stream, err := p.streamFor(command.topic)
	if err != nil {
		return err
	}
	command.stream = stream
	return p.connectionFor(command).publish(command)
}

func (p *publisherPool) streamFor(topic string) (string, error) {
	if p.fixedStream != "" {
		return p.fixedStream, nil
	}
	return streamForTopic(topic)
}

func (p *publisherPool) connectionFor(command publishCommand) *batchPublisher {
	routeKey := command.stream
	if partition := publishPartition(command.ctx); partition != "" {
		routeKey += "\x00" + partition
	}
	return p.members[p.router.Connection(routeKey, len(p.members))]
}

func (p *publisherPool) Flush(ctx context.Context) error {
	errors := make(chan error, len(p.members))
	targets := make([]uint64, len(p.members))
	flushIDs := make([]uint64, len(p.members))
	for i, member := range p.members {
		targets[i], flushIDs[i] = member.snapshotFlush()
	}
	for i, member := range p.members {
		go func(member *batchPublisher, target, flushID uint64) { errors <- member.waitFlush(ctx, target, flushID) }(member, targets[i], flushIDs[i])
	}
	var first error
	for range p.members {
		if err := <-errors; err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (p *publisherPool) Close() error {
	var errs []error
	for _, member := range p.members {
		if err := member.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("bus: close publisher pool: %w", errs[0])
	}
	return nil
}

func (p *batchPublisher) publish(command publishCommand) error {
	request := newPublishRequest(command, publishMessage(command))
	if err := p.admit(command.ctx, command.stream, request); err != nil {
		releaseUnadmittedRequest(request)
		return err
	}
	return awaitPublishConfirmation(command.ctx, request)
}

func releaseUnadmittedRequest(request publishRequest) {
	request.msg.Subject = ""
	request.msg.Data = nil
	wireMsgPool.Put(request.msg)
	if request.confirmed != nil {
		putConfirmChan(request.confirmed)
	}
}

func resetWireHeader(h nats.Header) nats.Header {
	if h == nil {
		h = make(nats.Header, 2)
	} else {
		for k := range h {
			if k == messageIDHeader {
				continue
			}
			delete(h, k)
		}
	}
	id, ok := h[messageIDHeader]
	if !ok || cap(id) != 1 {
		id = make([]string, 1)
	} else {
		id = id[:1]
		id[0] = ""
	}
	h[messageIDHeader] = id
	return h
}

func attachTraceHeaders(wire *nats.Msg, ctx context.Context) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		headers := http.Header{}
		txn.InsertDistributedTraceHeaders(headers)
		for key := range headers {
			wire.Header.Set(key, headers.Get(key))
		}
	}
}

func publishMessage(command publishCommand) *nats.Msg {
	wire := wireMsgPool.Get().(*nats.Msg)
	wire.Subject = command.topic
	wire.Reply = ""
	wire.Data = command.payload
	wire.Header = resetWireHeader(wire.Header)
	wire.Header[messageIDHeader][0] = command.msgID
	attachTraceHeaders(wire, command.ctx)
	return wire
}

func newPublishRequest(command publishCommand, wire *nats.Msg) publishRequest {
	request := publishRequest{msg: wire}
	if command.confirmed {
		request.confirmed = getConfirmChan()
	}
	return request
}

var confirmChanPool = sync.Pool{
	New: func() any { return make(chan error, 1) },
}

func getConfirmChan() chan error {
	confirmed := confirmChanPool.Get().(chan error)
	select {
	case <-confirmed:
	default:
	}
	return confirmed
}

func putConfirmChan(confirmed chan error) {
	confirmChanPool.Put(confirmed)
}

func (p *batchPublisher) admit(ctx context.Context, stream string, request publishRequest) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return errors.New("bus: publisher is closed")
	}
	if worker := p.workers[stream]; worker != nil {
		err := p.admitLocked(ctx, worker, request)
		p.mu.RUnlock()
		return err
	}
	p.mu.RUnlock()

	created, err := p.startWorker(stream)
	if err != nil {
		return err
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return errors.New("bus: publisher is closed")
	}
	return p.admitLocked(ctx, created, request)
}

func (p *batchPublisher) admitLocked(ctx context.Context, worker *publishBatchWorker, request publishRequest) error {
	request.sequence = p.markAccepted()
	select {
	case worker.requests <- request:
		return nil
	case <-ctx.Done():
		// A cancelled send must resolve its reservation, or ordered Flush waits forever.
		p.completeSequences([]uint64{request.sequence}, nil)
		return ctx.Err()
	}
}

func awaitPublishConfirmation(ctx context.Context, request publishRequest) error {
	if request.confirmed == nil {
		return nil
	}
	select {
	case err := <-request.confirmed:
		putConfirmChan(request.confirmed)
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *batchPublisher) startWorker(stream string) (*publishBatchWorker, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("bus: publisher is closed")
	}
	if worker := p.workers[stream]; worker != nil {
		return worker, nil
	}
	worker := &publishBatchWorker{
		js:       p.js,
		requests: make(chan publishRequest, publishQueueSize()),
		stop:     make(chan struct{}), done: make(chan struct{}), owner: p,
		slots:    make(chan struct{}, publishInflightCohorts()),
		ackFirst: env.GetBool("NATS_ATOMIC_ACK_FIRST", true),

		batchSize:     publishBatchSize(p.wire),
		batchWait:     publishBatchWait(p.wire),
		overlapCommit: atomicPublishOverlap(),
		ackTimers:     sync.Pool{New: func() any { return time.NewTimer(defaultPublishAckWait) }},
	}
	p.workers[stream] = worker
	go worker.run()
	return worker, nil
}

func (p *batchPublisher) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	flushErr := p.Flush(ctx)
	cancel()

	for _, worker := range p.workers {
		close(worker.stop)
	}
	for _, worker := range p.workers {
		<-worker.done
	}
	if err := p.nc.Drain(); err != nil {
		p.nc.Close()
		if flushErr != nil {
			return fmt.Errorf("bus: flush publisher: %v; drain: %w", flushErr, err)
		}
		return err
	}
	return flushErr
}

func (p *batchPublisher) markAccepted() uint64 {
	return p.accepted.Add(1)
}

func (p *batchPublisher) completeBatch(batch []publishRequest, err error) {
	p.stateMu.Lock()
	p.ensureResolutionCapacityLocked()
	for i := range batch {
		sequence := batch[i].sequence
		p.resolved[sequence%uint64(len(p.resolved))] = publishResolution{sequence: sequence, err: err}
	}
	p.advanceCompletedLocked()
	p.signal.Broadcast()
	p.stateMu.Unlock()
	if err != nil {
		p.log.Error("asynchronous NATS publish failed", zap.Int("messages", len(batch)), zap.Error(err))
	}
}

func (p *batchPublisher) completeSequences(sequences []uint64, err error) {
	p.stateMu.Lock()
	p.ensureResolutionCapacityLocked()
	for _, sequence := range sequences {
		p.resolved[sequence%uint64(len(p.resolved))] = publishResolution{sequence: sequence, err: err}
	}
	p.advanceCompletedLocked()
	p.signal.Broadcast()
	p.stateMu.Unlock()
	if err != nil {
		p.log.Error("asynchronous NATS publish failed", zap.Int("messages", len(sequences)), zap.Error(err))
	}
}

func (p *batchPublisher) advanceCompletedLocked() {
	for p.completed < p.accepted.Load() {
		next := p.completed + 1
		resolved := p.resolved[next%uint64(len(p.resolved))]
		if resolved.sequence != next {
			break
		}
		p.resolved[next%uint64(len(p.resolved))] = publishResolution{}
		p.completed = next
		if resolved.err != nil {
			p.recordErrorLocked(next, resolved.err)
		}
	}
}

func (p *batchPublisher) recordErrorLocked(sequence uint64, err error) {
	if len(p.errors) < maxPendingPublishErrors {
		p.errors = append(p.errors, publishError{first: sequence, last: sequence, cause: err, count: 1})
		return
	}
	tail := &p.errors[len(p.errors)-1]
	tail.last = sequence
	tail.count++
}

func (p *batchPublisher) ensureResolutionCapacityLocked() {
	needed := p.accepted.Load() - p.completed + 1
	if uint64(len(p.resolved)) >= needed {
		return
	}
	size := 1024
	for uint64(size) < needed {
		size *= 2
	}
	resized := make([]publishResolution, size)
	for _, resolved := range p.resolved {
		if resolved.sequence > p.completed {
			resized[resolved.sequence%uint64(size)] = resolved
		}
	}
	p.resolved = resized
}

func (p *batchPublisher) Flush(ctx context.Context) error {
	target, flushID := p.snapshotFlush()
	return p.waitFlush(ctx, target, flushID)
}

func (p *batchPublisher) snapshotFlush() (target, flushID uint64) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	target = p.accepted.Load()
	return target, p.registerFlushLocked(target)
}

func (p *batchPublisher) registerFlush(target uint64) uint64 {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.registerFlushLocked(target)
}

func (p *batchPublisher) registerFlushLocked(target uint64) uint64 {
	p.nextFlush++
	flushID := p.nextFlush
	if p.activeFlushes == nil {
		p.activeFlushes = make(map[uint64]flushState)
	}
	p.activeFlushes[flushID] = flushState{target: target}
	return flushID
}

func (p *batchPublisher) waitFlush(ctx context.Context, target, flushID uint64) error {
	p.stateMu.Lock()
	ctx, cancel := context.WithCancel(ctx)
	wake := make(chan struct{})
	go func() {
		<-ctx.Done()
		p.stateMu.Lock()
		p.signal.Broadcast()
		p.stateMu.Unlock()
		close(wake)
	}()
	var err error
	for p.completed < target {
		if ctx.Err() != nil {
			err = ctx.Err()
			break
		}
		p.signal.Wait()
	}
	resolved := err == nil
	if err == nil {
		p.captureFlushErrorsLocked()
		err = p.activeFlushes[flushID].err
	}
	p.finishFlushLocked(flushID, target, resolved)
	p.stateMu.Unlock()
	cancel()
	<-wake
	return err
}

func (p *batchPublisher) captureFlushErrorsLocked() {
	for id, state := range p.activeFlushes {
		captured, ok := captureFlushError(state, p.errors)
		if !ok {
			continue
		}
		state = captured
		p.activeFlushes[id] = state
	}
}

func captureFlushError(state flushState, failures []publishError) (flushState, bool) {
	if state.err != nil {
		return state, false
	}
	err := flushErrorForTarget(state.target, failures)
	if err == nil {
		return state, false
	}
	state.err = err
	return state, true
}

func flushErrorForTarget(target uint64, failures []publishError) error {
	for _, failure := range failures {
		if failure.first > target {
			break
		}
		if failure.count > 1 {
			return fmt.Errorf("bus: %d asynchronous publish messages failed (first: %w)", failure.count, failure.cause)
		}
		return failure.cause
	}
	return nil
}

func (p *batchPublisher) finishFlushLocked(flushID, target uint64, resolved bool) {
	delete(p.activeFlushes, flushID)
	if !resolved {
		return
	}
	p.dropReportedErrorsLocked(target)
}

func (p *batchPublisher) dropReportedErrorsLocked(target uint64) {
	for len(p.errors) > 0 {
		failure := &p.errors[0]
		if failure.first > target {
			return
		}
		if failure.last > target {
			failure.first = target + 1
			return
		}
		p.errors = p.errors[1:]
	}
}

func (w *publishBatchWorker) run() {
	defer func() {
		if w.timer != nil {
			w.timer.Stop()
		}
		w.acks.Wait()
		close(w.done)
	}()
	for {
		batch, ok := w.nextBatch()
		if !ok {
			return
		}
		w.publish(batch)
	}
}

func (w *publishBatchWorker) nextBatch() ([]publishRequest, bool) {
	select {
	case <-w.stop:
		return nil, false
	case first := <-w.requests:
		return w.collectBatch(first)
	}
}

func (w *publishBatchWorker) collectBatch(first publishRequest) ([]publishRequest, bool) {
	batch := make([]publishRequest, 1, w.batchSize)
	batch[0] = first
	if w.batchWait <= 0 {
		return w.drainReady(batch), true
	}
	w.armWindowTimer()
	return w.collectTimed(batch)
}

func (w *publishBatchWorker) drainReady(batch []publishRequest) []publishRequest {
	for len(batch) < w.batchSize {
		select {
		case request := <-w.requests:
			batch = append(batch, request)
		default:
			return batch
		}
	}
	return batch
}

func (w *publishBatchWorker) armWindowTimer() {
	if w.timer == nil {
		w.timer = time.NewTimer(w.batchWait)
	} else {
		w.timer.Reset(w.batchWait)
	}
}

func (w *publishBatchWorker) collectTimed(batch []publishRequest) ([]publishRequest, bool) {
	var slots chan<- struct{}
	for len(batch) < w.batchSize {
		select {
		case request := <-w.requests:
			batch = append(batch, request)
		case <-w.timer.C:
			if !w.slotGated() {
				return batch, true
			}
			slots = w.slots
		case slots <- struct{}{}:
			w.slotHeld = true
			return batch, true
		case <-w.stop:
			w.fail(batch, errors.New("bus: publisher closed"))
			return nil, false
		}
	}
	return batch, true
}

func (w *publishBatchWorker) slotGated() bool {
	if w.owner == nil || w.slots == nil {
		return false
	}
	switch w.owner.wire {

	case wireAtomic:
		return w.overlapCommit
	case wireFast:
		return false
	default:
		return true
	}
}

func (w *publishBatchWorker) takeSlot(held bool) {
	if !held {
		w.slots <- struct{}{}
	}
}

func (w *publishBatchWorker) dropSlot(held bool) {
	if held {
		<-w.slots
	}
}

var cohortStats struct{ cohorts, messages atomic.Uint64 }

func CohortStats() (cohorts, messages uint64) {
	return cohortStats.cohorts.Load(), cohortStats.messages.Load()
}

func (w *publishBatchWorker) publish(batch []publishRequest) {
	held := w.slotHeld
	w.slotHeld = false
	cohortStats.cohorts.Add(1)
	cohortStats.messages.Add(uint64(len(batch)))
	switch cohortWire(w.owner.wire, len(batch)) {
	case wireAtomic:
		w.publishAtomicCohort(batch, held)
	case wireFast:
		w.dropSlot(held)
		w.finishFast(batch, w.publishFast(batch))
	default:
		w.publishAsync(batch, held)
	}
}

func (w *publishBatchWorker) publishAsync(batch []publishRequest, held bool) {
	w.takeSlot(held)

	futures, startErr := w.startAsync(batch)
	w.acks.Add(1)
	go func() {
		defer w.acks.Done()
		defer func() { <-w.slots }()
		awaitErr := w.awaitAsync(futures)
		w.finish(batch, joinAsyncCohort(len(batch), futures, startErr, awaitErr))
	}()
}

// Callers must prove the broker stored none of the cohort, or a prefix is stored twice.
func (w *publishBatchWorker) publishCohortIndividually(batch []publishRequest) error {
	futures, startErr := w.startAsync(batch)
	awaitErr := w.awaitAsync(futures)
	return joinAsyncCohort(len(batch), futures, startErr, awaitErr)
}

func (w *publishBatchWorker) startAsync(batch []publishRequest) ([]nats.PubAckFuture, error) {
	futures := make([]nats.PubAckFuture, 0, len(batch))
	for _, req := range batch {
		future, err := w.js.PublishMsgAsync(req.msg)
		if err != nil {
			return futures, fmt.Errorf("bus: async publish %s: %w", req.msg.Subject, err)
		}
		futures = append(futures, future)
	}
	return futures, nil
}

func joinAsyncCohort(size int, futures []nats.PubAckFuture, startErr, awaitErr error) error {
	if startErr == nil {
		return awaitErr
	}
	if awaitErr != nil {
		return fmt.Errorf("bus: async cohort sent %d/%d messages and their acknowledgements did not all resolve: %w",
			len(futures), size, errors.Join(startErr, awaitErr))
	}
	return fmt.Errorf("bus: async cohort sent and stored %d/%d messages; the remainder never reached the wire: %w",
		len(futures), size, startErr)
}

func (w *publishBatchWorker) awaitAsync(futures []nats.PubAckFuture) error {
	timer := w.ackTimer()
	defer w.putAckTimer(timer)
	for _, future := range futures {
		select {
		case <-future.Ok():
		case err := <-future.Err():
			return err
		case <-timer.C:
			return errors.New("bus: asynchronous publish cohort PubAck timeout")
		}
	}
	return nil
}

func (w *publishBatchWorker) ackTimer() *time.Timer {
	if timer, ok := w.ackTimers.Get().(*time.Timer); ok {
		timer.Reset(publishAckWait())
		return timer
	}
	return time.NewTimer(publishAckWait())
}

func (w *publishBatchWorker) putAckTimer(timer *time.Timer) {
	timer.Stop()
	w.ackTimers.Put(timer)
}

func (w *publishBatchWorker) fail(batch []publishRequest, err error) {
	w.finish(batch, err)
}

func (w *publishBatchWorker) finish(batch []publishRequest, err error) {
	w.owner.completeBatch(batch, err)
	for i := range batch {
		if batch[i].confirmed != nil {
			batch[i].confirmed <- err
		}
		// After a failure nats.go still owns the envelope and may re-send it.
		if err != nil {
			continue
		}
		batch[i].msg.Subject = ""
		batch[i].msg.Data = nil
		wireMsgPool.Put(batch[i].msg)
	}

}
