package bus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const (
	defaultPublishBatchSize = 128
	defaultPublishBatchWait = time.Millisecond
	defaultPublishAckWait   = 2 * time.Second
	defaultPublishQueueSize = 16_384
	maxInflightCohorts      = 4

	messageIDHeader = "Bagelbot-Message-Id"
)

// StreamRouter is the strategy used to select a pooled connection. The default
// hashes the stream plus optional aggregate partition. Calls without a
// partition preserve stream-wide order; partitioned calls preserve order for
// that channel/tenant while allowing unrelated aggregates to publish in parallel.
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

// batchPublisher is one pooled connection with one active-object batcher per
// JetStream stream assigned by StreamRouter.
type batchPublisher struct {
	nc  *nats.Conn
	js  nats.JetStreamContext
	log *zap.Logger

	// wire selects the cohort protocol (per-message PubAcks or ADR-050 atomic
	// batches); sender is the connection's shared batch-ack inbox, present only
	// on the atomic wire.
	wire   wireMode
	sender *atomicSender

	mu       sync.RWMutex
	workerMu sync.Mutex
	closed   bool
	workers  map[string]*publishBatchWorker

	stateMu   sync.Mutex
	accepted  uint64
	completed uint64
	firstErr  error
	changed   chan struct{}
}

type publishRequest struct {
	msg       *nats.Msg
	confirmed chan error
}

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
	nc, err := nats.Connect(busPublishURL(url), busOptions(fmt.Sprintf("batch-publisher-%d", index))...)
	if err != nil {
		return nil, fmt.Errorf("bus: connect batch publisher: %w", err)
	}
	js, err := nc.JetStream(jsDomainOption()...)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bus: jetstream batch publisher: %w", err)
	}
	publisher := &batchPublisher{
		nc: nc, js: js, log: log, wire: wire,
		workers: make(map[string]*publishBatchWorker),
		changed: make(chan struct{}),
	}
	if wire == wireAtomic {
		sender, err := newAtomicSender(nc)
		if err != nil {
			nc.Close()
			return nil, err
		}
		publisher.sender = sender
	}
	return publisher, nil
}

func (p *publisherPool) PublishOwned(ctx context.Context, topic string, payload []byte) error {
	// NUID avoids UUIDv7's wall-clock/random-source coordination on the hot path.
	return p.publish(publishCommand{ctx: ctx, topic: topic, msgID: nuid.Next(), payload: payload})
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
	for _, member := range p.members {
		if err := member.Flush(ctx); err != nil {
			return err
		}
	}
	return nil
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
	wire := publishMessage(command)
	worker, err := p.acceptingWorker(command.stream)
	if err != nil {
		return err
	}
	request := newPublishRequest(command, wire)
	if err := p.admit(command.ctx, worker, request); err != nil {
		return err
	}
	return awaitPublishConfirmation(command.ctx, request)
}

func publishMessage(command publishCommand) *nats.Msg {
	wire := nats.NewMsg(command.topic)
	wire.Data = command.payload
	// Preserve the fleet abstraction's message identity for subscribers, but
	// deliberately omit Nats-Msg-Id. The custom header is transport metadata,
	// not a broker dedup key.
	wire.Header.Set(messageIDHeader, command.msgID)
	// Dual-write the former adapter's identity header for one rolling-release
	// window. Old consumers only understand this header, and an empty UUID is
	// unsafe for outgress's distributed lease owner. This is application
	// identity—not Nats-Msg-Id—and therefore does not enable broker deduplication.
	wire.Header.Set(legacyMessageIDHeader, command.msgID)
	if txn := newrelic.FromContext(command.ctx); txn != nil {
		headers := http.Header{}
		txn.InsertDistributedTraceHeaders(headers)
		for key := range headers {
			wire.Header.Set(key, headers.Get(key))
		}
	}
	return wire
}

func (p *batchPublisher) acceptingWorker(stream string) (*publishBatchWorker, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, errors.New("bus: publisher is closed")
	}
	worker, err := p.workerLocked(stream)
	if err != nil {
		p.mu.RUnlock()
		return nil, err
	}
	p.mu.RUnlock()
	return worker, nil
}

func newPublishRequest(command publishCommand, wire *nats.Msg) publishRequest {
	request := publishRequest{msg: wire}
	if command.confirmed {
		request.confirmed = make(chan error, 1)
	}
	return request
}

func (p *batchPublisher) admit(ctx context.Context, worker *publishBatchWorker, request publishRequest) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return errors.New("bus: publisher is closed")
	}
	select {
	case worker.requests <- request:
		p.markAccepted()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func awaitPublishConfirmation(ctx context.Context, request publishRequest) error {
	if request.confirmed == nil {
		return nil
	}
	select {
	case err := <-request.confirmed:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// workerLocked is called with the publisher read lock held. Worker creation is
// serialized separately because several first publishers may discover a stream
// at once.
func (p *batchPublisher) workerLocked(stream string) (*publishBatchWorker, error) {
	// Upgrade briefly to an independent mutex by using the connection's worker
	// map guard. The outer read lock prevents Close while this occurs.
	p.workerMu.Lock()
	defer p.workerMu.Unlock()
	if worker := p.workers[stream]; worker != nil {
		return worker, nil
	}
	worker := &publishBatchWorker{
		js:       p.js,
		requests: make(chan publishRequest, defaultPublishQueueSize),
		stop:     make(chan struct{}), done: make(chan struct{}), owner: p,
		slots: make(chan struct{}, maxInflightCohorts),
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

func (p *batchPublisher) markAccepted() {
	p.stateMu.Lock()
	p.accepted++
	p.notifyLocked()
	p.stateMu.Unlock()
}

func (p *batchPublisher) complete(count int, err error) {
	p.stateMu.Lock()
	p.completed += uint64(count)
	if err != nil && p.firstErr == nil {
		p.firstErr = err
	}
	p.notifyLocked()
	p.stateMu.Unlock()
	if err != nil {
		p.log.Error("asynchronous NATS publish failed", zap.Int("messages", count), zap.Error(err))
	}
}

func (p *batchPublisher) notifyLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

func (p *batchPublisher) Flush(ctx context.Context) error {
	p.stateMu.Lock()
	target := p.accepted
	for p.completed < target {
		changed := p.changed
		p.stateMu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
		p.stateMu.Lock()
	}
	err := p.firstErr
	p.stateMu.Unlock()
	return err
}

func (w *publishBatchWorker) run() {
	defer func() {
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
	batch := []publishRequest{first}
	timer := time.NewTimer(defaultPublishBatchWait)
	defer stopAndDrainTimer(timer)
	for len(batch) < defaultPublishBatchSize {
		select {
		case request := <-w.requests:
			batch = append(batch, request)
		case <-timer.C:
			return batch, true
		case <-w.stop:
			w.fail(batch, errors.New("bus: publisher closed"))
			return nil, false
		}
	}
	return batch, true
}

func stopAndDrainTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func (w *publishBatchWorker) publish(batch []publishRequest) {
	// A single-message cohort gains nothing from batch framing; ADR-050 also
	// shapes a lone commit as an ordinary PubAck, so the plain wire is simpler.
	if w.owner.wire == wireAtomic && len(batch) > 1 {
		w.publishAtomic(batch)
		return
	}
	w.slots <- struct{}{}
	futures, err := w.startAsync(batch)
	if err != nil {
		<-w.slots
		w.finish(batch, err)
		return
	}
	w.acks.Add(1)
	go func() {
		defer w.acks.Done()
		defer func() { <-w.slots }()
		w.finish(batch, w.awaitAsync(futures))
	}()
}

// startAsync sends cohorts serially from the active object, preserving wire
// order, while awaitAsync lets up to maxInflightCohorts overlap their PubAck
// waits. It uses only nats.go APIs and bounds the client's future set.
func (w *publishBatchWorker) startAsync(batch []publishRequest) ([]nats.PubAckFuture, error) {
	futures := make([]nats.PubAckFuture, 0, len(batch))
	for _, req := range batch {
		future, err := w.js.PublishMsgAsync(req.msg)
		if err != nil {
			return nil, fmt.Errorf("bus: async publish %s: %w", req.msg.Subject, err)
		}
		futures = append(futures, future)
	}
	return futures, nil
}

// awaitAsync is the strategy seam to replace only when nats.go exposes NATS
// 2.14 Fast-Ingest; no server wire protocol is reimplemented here.
func (w *publishBatchWorker) awaitAsync(futures []nats.PubAckFuture) error {
	timer := time.NewTimer(defaultPublishAckWait)
	defer timer.Stop()
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

func (w *publishBatchWorker) fail(batch []publishRequest, err error) {
	w.finish(batch, err)
}

func (w *publishBatchWorker) finish(batch []publishRequest, err error) {
	w.owner.complete(len(batch), err)
	for i := range batch {
		if batch[i].confirmed != nil {
			batch[i].confirmed <- err
		}
	}
}
