package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
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

	batchIDHeader       = "Nats-Batch-Id"
	batchSequenceHeader = "Nats-Batch-Sequence"
	batchCommitHeader   = "Nats-Batch-Commit"
	requiredAPIHeader   = "Nats-Required-Api-Level"
	watermillUUIDHeader = "_watermill_message_uuid" // wire compatibility until subscribers migrate
)

// StreamRouter is the strategy used to select a pooled connection. The default
// hashes the stream name, preserving order for one stream while allowing
// BAGEL_DATA, TWITCH_OUTGRESS and control streams to write in parallel.
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
	members []*batchPublisher
	router  StreamRouter
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

	stateMu   sync.Mutex
	accepted  uint64
	completed uint64
	firstErr  error
	changed   chan struct{}
}

type publishRequest struct {
	msg   *nats.Msg
	msgID string
}

type publishBatchWorker struct {
	stream   string
	nc       *nats.Conn
	js       nats.JetStreamContext
	inbox    string
	replies  *nats.Subscription
	requests chan publishRequest
	stop     chan struct{}
	done     chan struct{}
	log      *zap.Logger
	owner    *batchPublisher
}

type jetStreamPubAck struct {
	Stream    string `json:"stream"`
	Sequence  uint64 `json:"seq"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Batch     string `json:"batch,omitempty"`
	Count     int    `json:"count,omitempty"`
	Error     *struct {
		Code        int    `json:"code"`
		ErrorCode   int    `json:"err_code"`
		Description string `json:"description"`
	} `json:"error,omitempty"`
}

func newPublisherPool(url string, log *zap.Logger) (Publisher, error) {
	if log == nil {
		log = zap.NewNop()
	}
	poolSize := env.GetInt("NATS_PUBLISH_CONNECTIONS", min(runtime.GOMAXPROCS(0), 4))
	if poolSize < 1 {
		poolSize = 1
	}
	if poolSize > 32 {
		poolSize = 32
	}
	pool := &publisherPool{members: make([]*batchPublisher, 0, poolSize), router: hashStreamRouter{}}
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
	stream, err := streamForTopic(topic)
	if err != nil {
		return err
	}
	member := p.members[p.router.Connection(stream, len(p.members))]
	return member.publish(ctx, stream, topic, payload)
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

func (p *batchPublisher) publish(ctx context.Context, stream, topic string, payload []byte) error {
	// JetStream deduplication requires a unique token, not UUID formatting.
	// NUID avoids UUIDv7's wall-clock/random-source coordination on this hot path.
	msgID := nuid.Next()
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
	defer p.mu.RUnlock()
	if p.closed {
		return errors.New("bus: publisher is closed")
	}
	worker, err := p.workerLocked(stream)
	if err != nil {
		return err
	}

	request := publishRequest{msg: wire, msgID: msgID}
	select {
	case worker.requests <- request:
		p.markAccepted()
		return nil
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
	inbox := nats.NewInbox()
	replies, err := p.nc.SubscribeSync(inbox + ".>")
	if err != nil {
		return nil, fmt.Errorf("bus: subscribe batch replies for %s: %w", stream, err)
	}
	worker := &publishBatchWorker{
		stream: stream, nc: p.nc, js: p.js, inbox: inbox, replies: replies,
		requests: make(chan publishRequest, defaultPublishQueueSize),
		stop:     make(chan struct{}), done: make(chan struct{}), log: p.log, owner: p,
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
	defer close(w.done)
	defer w.replies.Unsubscribe() //nolint:errcheck -- connection shutdown is authoritative
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
	if len(batch) == 1 {
		w.owner.complete(1, w.publishOne(batch[0]))
		return
	}
	if err := w.publishAtomic(batch); err == nil {
		w.owner.complete(len(batch), nil)
		return
	} else {
		// The final PubAck can be lost after a successful commit, and a batch can
		// be rejected because just one MsgId is already present. Reconcile every
		// member individually under the same MsgId. A stored member returns a
		// duplicate-success ack; a missing member is stored now.
		w.log.Debug("atomic publish batch fell back to individual reconciliation",
			zap.String("stream", w.stream), zap.Int("messages", len(batch)), zap.Error(err))
	}
	var firstErr error
	for _, req := range batch {
		if err := w.publishOne(req); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	w.owner.complete(len(batch), firstErr)
}

func (w *publishBatchWorker) publishAtomic(batch []publishRequest) error {
	batchID := nuid.Next()
	reply := w.inbox + "." + batchID
	for i := range batch {
		msg := batch[i].msg
		msg.Header.Set(batchIDHeader, batchID)
		msg.Header.Set(batchSequenceHeader, fmt.Sprint(i+1))
		msg.Header.Set(requiredAPIHeader, "2")
		if i == len(batch)-1 {
			msg.Header.Set(batchCommitHeader, "1")
			msg.Reply = reply
		}
		if err := w.nc.PublishMsg(msg); err != nil {
			return fmt.Errorf("send atomic batch member %d: %w", i+1, err)
		}
	}

	ackMsg, err := w.replies.NextMsg(defaultPublishAckWait)
	if err != nil {
		return fmt.Errorf("wait atomic batch ack: %w", err)
	}
	var ack jetStreamPubAck
	if err := json.Unmarshal(ackMsg.Data, &ack); err != nil {
		return fmt.Errorf("decode atomic batch ack: %w", err)
	}
	if ack.Error != nil {
		return fmt.Errorf("atomic batch rejected: code=%d err_code=%d %s",
			ack.Error.Code, ack.Error.ErrorCode, ack.Error.Description)
	}
	if ack.Sequence == 0 || ack.Batch != batchID || ack.Count != len(batch) {
		return fmt.Errorf("invalid atomic batch ack: seq=%d batch=%q count=%d",
			ack.Sequence, ack.Batch, ack.Count)
	}
	return nil
}

func (w *publishBatchWorker) publishOne(req publishRequest) error {
	msg := cloneWithoutBatchHeaders(req.msg)
	_, err := w.js.PublishMsg(msg, nats.MsgId(req.msgID))
	if err != nil {
		return fmt.Errorf("bus: publish %s: %w", msg.Subject, err)
	}
	return nil
}

func cloneWithoutBatchHeaders(src *nats.Msg) *nats.Msg {
	msg := &nats.Msg{Subject: src.Subject, Data: src.Data, Header: make(nats.Header)}
	for key, values := range src.Header {
		switch key {
		case batchIDHeader, batchSequenceHeader, batchCommitHeader, requiredAPIHeader:
			continue
		}
		msg.Header[key] = append([]string(nil), values...)
	}
	return msg
}

func (w *publishBatchWorker) fail(batch []publishRequest, err error) {
	w.owner.complete(len(batch), err)
}
