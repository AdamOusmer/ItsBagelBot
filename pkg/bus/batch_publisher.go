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
	jsapi "github.com/nats-io/nats.go/jetstream"
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

	// wire selects the cohort protocol (per-message PubAcks, ADR-050 atomic
	// batches or Fast-Ingest sessions). modern is the jetstream.JetStream handle
	// Orbit's batch publishers require; js stays for the per-message wire.
	wire   wireMode
	modern jsapi.JetStream

	mu       sync.RWMutex
	workerMu sync.Mutex
	closed   bool
	workers  map[string]*publishBatchWorker

	stateMu   sync.Mutex
	accepted  uint64
	completed uint64
	// firstErr is the first cohort failure no Flush has reported yet, and
	// firstErrAt the completion position its cohort started at. Flush is a
	// per-call result, so it takes the error out on the way past instead of
	// latching it; a latch would fail every later Flush on this connection for
	// the life of the process.
	firstErr   error
	firstErrAt uint64
	changed    chan struct{}
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

	// Cohort shape is fixed per wire at worker creation: the Fast-Ingest wire
	// amortizes a session over a much longer collection window than the
	// per-message and atomic wires can use.
	batchSize int
	batchWait time.Duration

	// overlapCommit lets an atomic cohort's commit ack be awaited off this
	// goroutine. It trades strict cross-cohort stream order for the commit RTT;
	// publishAtomicOverlapped documents exactly what that costs.
	overlapCommit bool
	// newAtomic is the test seam for Orbit's batch publisher. Production leaves
	// it nil and atomicPublisher opens the real ADR-050 session.
	newAtomic func() (atomicCohortPublisher, error)
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
	modern, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bus: modern jetstream batch publisher: %w", err)
	}
	return &batchPublisher{
		nc: nc, js: js, modern: modern, log: log, wire: wire,
		workers: make(map[string]*publishBatchWorker),
		changed: make(chan struct{}),
	}, nil
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
		slots:         make(chan struct{}, maxInflightCohorts),
		batchSize:     publishBatchSize(p.wire),
		batchWait:     publishBatchWait(p.wire),
		overlapCommit: atomicPublishOverlap(),
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
	if err != nil && p.firstErr == nil {
		p.firstErr = err
		// Record the failure at the cohort's first message so a Flush can tell a
		// cohort overlapping its window from one made only of messages admitted
		// after the call returned its own result.
		p.firstErrAt = p.completed
	}
	p.completed += uint64(count)
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
	err := p.takeWindowErrLocked(target)
	p.stateMu.Unlock()
	return err
}

// takeWindowErrLocked returns the pending cohort failure that overlaps this
// flush window and clears it, so the next Flush answers for its own window. A
// failure recorded once the window had already fully resolved belongs to a later
// call and is left in place for it.
func (p *batchPublisher) takeWindowErrLocked(target uint64) error {
	if p.firstErr == nil || p.firstErrAt >= target {
		return nil
	}
	err := p.firstErr
	p.firstErr = nil
	p.firstErrAt = 0
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
	timer := time.NewTimer(w.batchWait)
	defer stopAndDrainTimer(timer)
	for len(batch) < w.batchSize {
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

// publish drives one cohort. Cohorts are staged from this goroutine in wire
// order; what may move off it is the wait for the broker's verdict — plus, on
// the overlapping atomic path, the commit that carries the cohort's last
// message — and only as far as each wire's ordering contract allows.
func (w *publishBatchWorker) publish(batch []publishRequest) {
	switch cohortWire(w.owner.wire, len(batch)) {
	case wireAtomic:
		w.publishAtomicCohort(batch)
	case wireFast:
		w.finishFast(batch, w.publishFast(batch))
	default:
		w.publishAsync(batch)
	}
}

// publishAsync sends the cohort message by message and lets up to
// maxInflightCohorts cohorts overlap their PubAck waits. A send that fails
// part-way still leaves everything already handed to nats.go on the wire, so
// the cohort is resolved from the same goroutine that would have awaited a
// complete one.
func (w *publishBatchWorker) publishAsync(batch []publishRequest) {
	w.slots <- struct{}{}
	futures, startErr := w.startAsync(batch)
	w.acks.Add(1)
	go func() {
		defer w.acks.Done()
		defer func() { <-w.slots }()
		awaitErr := w.awaitAsync(futures)
		w.finish(batch, joinAsyncCohort(len(batch), futures, startErr, awaitErr))
	}()
}

// publishCohortIndividually re-publishes a whole cohort message by message and
// waits for every PubAck. Callers must have proven the broker stored nothing of
// the cohort; otherwise this path stores a prefix a second time.
func (w *publishBatchWorker) publishCohortIndividually(batch []publishRequest) error {
	futures, startErr := w.startAsync(batch)
	awaitErr := w.awaitAsync(futures)
	return joinAsyncCohort(len(batch), futures, startErr, awaitErr)
}

// startAsync sends cohorts serially from the active object, preserving wire
// order, while awaitAsync overlaps their PubAck waits. It uses only nats.go
// APIs and bounds the client's future set. Both fallbacks re-publish a
// definitely rejected cohort through it. A refused send returns the futures
// created so far rather than abandoning them: those messages are on the wire.
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

// joinAsyncCohort reports a partially sent cohort honestly. nats.go refuses a
// publish once its future set is full and once the connection is closed, but
// everything it already accepted is on the wire and is normally stored, so those
// PubAcks are resolved before the cohort is reported instead of being abandoned
// under the send error. A caller that retries on this error stores the sent
// prefix twice, which is why the message counts are in the text.
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

// awaitAsync drains one cohort's PubAcks in publish order. A timeout here is
// ambiguous for the messages still unresolved, so the cohort fails without
// replay.
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
