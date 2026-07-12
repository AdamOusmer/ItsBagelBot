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

	watermillUUIDHeader = "_watermill_message_uuid" // wire compatibility until subscribers migrate
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

	mu       sync.RWMutex
	workerMu sync.Mutex
	closed   bool
	workers  map[string]*publishBatchWorker

	stateMu    sync.Mutex
	accepted   uint64
	completed  uint64
	firstErr   error
	changed    chan struct{}
	duplicates atomic.Uint64
}

type publishRequest struct {
	msg       *nats.Msg
	msgID     string
	confirmed chan error
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

// DuplicateCount reports broker-folded publications for acceptance telemetry.
// It is intentionally outside Publisher's service-facing contract.
func (p *publisherPool) DuplicateCount() uint64 {
	var total uint64
	for _, member := range p.members {
		total += member.duplicates.Load()
	}
	return total
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
	for i := 0; i < poolSize; i++ {
		member, err := newBatchPublisherConnection(url, i, log)
		if err != nil {
			_ = pool.Close()
			return nil, err
		}
		pool.members = append(pool.members, member)
	}
	return pool, nil
}

func newBatchPublisherConnection(url string, index int, log *zap.Logger) (*batchPublisher, error) {
	nc, err := nats.Connect(busURL(url), busOptions(fmt.Sprintf("batch-publisher-%d", index))...)
	if err != nil {
		return nil, fmt.Errorf("bus: connect batch publisher: %w", err)
	}
	js, err := nc.JetStream(jsDomainOption()...)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bus: jetstream batch publisher: %w", err)
	}
	return &batchPublisher{
		nc: nc, js: js, log: log,
		workers: make(map[string]*publishBatchWorker),
		changed: make(chan struct{}),
	}, nil
}

func (p *publisherPool) PublishOwned(ctx context.Context, topic string, payload []byte) error {
	// NUID avoids UUIDv7's wall-clock/random-source coordination on the hot path.
	return p.publish(ctx, topic, nuid.Next(), payload, false)
}

func (p *publisherPool) PublishOwnedWithID(ctx context.Context, topic, msgID string, payload []byte) error {
	if msgID == "" {
		return errors.New("bus: idempotent publish requires an ID")
	}
	return p.publish(ctx, topic, msgID, payload, true)
}

func (p *publisherPool) publish(ctx context.Context, topic, msgID string, payload []byte, confirmed bool) error {
	stream := p.fixedStream
	if stream == "" {
		var err error
		stream, err = streamForTopic(topic)
		if err != nil {
			return err
		}
	}
	routeKey := stream
	if partition := publishPartition(ctx); partition != "" {
		routeKey += "\x00" + partition
	}
	member := p.members[p.router.Connection(routeKey, len(p.members))]
	return member.publish(ctx, stream, topic, msgID, payload, confirmed)
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

func (p *batchPublisher) publish(ctx context.Context, stream, topic, msgID string, payload []byte, confirmed bool) error {
	wire := nats.NewMsg(topic)
	wire.Data = payload
	wire.Header.Set(nats.MsgIdHdr, msgID)
	wire.Header.Set(watermillUUIDHeader, msgID)
	if txn := newrelic.FromContext(ctx); txn != nil {
		headers := http.Header{}
		txn.InsertDistributedTraceHeaders(headers)
		for key := range headers {
			wire.Header.Set(key, headers.Get(key))
		}
	}

	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return errors.New("bus: publisher is closed")
	}
	worker, err := p.workerLocked(stream)
	if err != nil {
		p.mu.RUnlock()
		return err
	}

	request := publishRequest{msg: wire, msgID: msgID}
	if confirmed {
		request.confirmed = make(chan error, 1)
	}
	select {
	case worker.requests <- request:
		p.markAccepted()
		p.mu.RUnlock()
	case <-ctx.Done():
		p.mu.RUnlock()
		return ctx.Err()
	}
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
		select {
		case <-w.stop:
			return
		case first := <-w.requests:
			batch := []publishRequest{first}
			timer := time.NewTimer(defaultPublishBatchWait)
		collect:
			for len(batch) < defaultPublishBatchSize {
				select {
				case req := <-w.requests:
					batch = append(batch, req)
				case <-timer.C:
					break collect
				case <-w.stop:
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					w.fail(batch, errors.New("bus: publisher closed"))
					return
				}
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			w.publish(batch)
		}
	}
}

func (w *publishBatchWorker) publish(batch []publishRequest) {
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
		future, err := w.js.PublishMsgAsync(req.msg, nats.MsgId(req.msgID))
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
		case ack := <-future.Ok():
			if ack != nil && ack.Duplicate {
				w.owner.duplicates.Add(1)
			}
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
