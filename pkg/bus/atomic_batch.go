package bus

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"go.uber.org/zap"

	"ItsBagelBot/pkg/env"
)

// ADR-050 atomic batch publishing (NATS 2.14): a cohort is written as one
// batch and the broker answers with a single commit PubAck instead of one
// PubAck per message. That removes the dominant per-event cost of the async
// publish path — the reply-subject routing and ack message for every event.
// Fleet publishing deliberately omits Nats-Msg-Id so atomic and single wires
// both avoid the broker's per-message dedup index.
//
// The mode is selected by NATS_PUBLISH_WIRE=atomic and defaults to the
// per-message PubAck wire, so it ships dark and is enabled per service only
// after the live R3 acceptance matrix passes.
//
// Failure handling is explicitly at-most-once. A negative server reply proves
// the batch was rejected and permits a per-message fallback. Transport errors,
// timeouts and malformed/mismatched commit acknowledgements are ambiguous, so
// the cohort fails without replay rather than risking a double-store.

const (
	batchIDHdr     = "Nats-Batch-Id"
	batchSeqHdr    = "Nats-Batch-Sequence"
	batchCommitHdr = "Nats-Batch-Commit"
)

// wireMode selects how a publish cohort reaches the broker.
type wireMode int

const (
	// wireSingle publishes every message with its own PubAck (nats.go async).
	wireSingle wireMode = iota
	// wireAtomic publishes a cohort as one ADR-050 atomic batch.
	wireAtomic
)

func publishWireMode() wireMode {
	if env.Get("NATS_PUBLISH_WIRE", "single") == "atomic" {
		return wireAtomic
	}
	return wireSingle
}

// atomicBatchMax is the server's per-batch message ceiling (ADR-050). The
// cohort collector caps batches at defaultPublishBatchSize, far below it; this
// guard only protects against a future batch-size bump past the protocol.
const atomicBatchMax = 1000

var errAtomicAckTimeout = errors.New("bus: atomic batch commit ack timeout")

// atomicSender owns one connection's reply inbox for batch acknowledgements.
// Sends happen serially from each stream's active-object worker; several
// workers on the same connection share the sender, so registration is locked.
type atomicSender struct {
	nc     *nats.Conn
	prefix string
	sub    *nats.Subscription

	mu    sync.Mutex
	waits map[string]chan *nats.Msg
}

func newAtomicSender(nc *nats.Conn) (*atomicSender, error) {
	s := &atomicSender{
		nc:     nc,
		prefix: nats.NewInbox() + ".",
		waits:  make(map[string]chan *nats.Msg),
	}
	sub, err := nc.Subscribe(s.prefix+">", s.route)
	if err != nil {
		return nil, fmt.Errorf("bus: subscribe atomic batch inbox: %w", err)
	}
	s.sub = sub
	return s, nil
}

// route delivers broker replies to the cohort awaiting them. Reply subjects
// are <prefix>s.<id> for the batch-opening message (zero-byte on success,
// error JSON otherwise) and <prefix>c.<id> for the commit PubAck.
func (s *atomicSender) route(msg *nats.Msg) {
	token, ok := strings.CutPrefix(msg.Subject, s.prefix)
	if !ok {
		return
	}
	kind, id, ok := strings.Cut(token, ".")
	if !ok {
		return
	}
	if kind == "s" && len(msg.Data) == 0 {
		return // successful batch open; only the commit ack resolves the cohort
	}
	s.mu.Lock()
	ch := s.waits[id]
	s.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

type atomicCohort struct {
	id string
	ch chan *nats.Msg
	n  int
}

func (s *atomicSender) begin(id string) chan *nats.Msg {
	// Two slots: a batch-open error and the commit reply can both arrive.
	ch := make(chan *nats.Msg, 2)
	s.mu.Lock()
	s.waits[id] = ch
	s.mu.Unlock()
	return ch
}

func (s *atomicSender) end(id string) {
	s.mu.Lock()
	delete(s.waits, id)
	s.mu.Unlock()
}

// send writes one cohort as an atomic batch, preserving the caller's wire
// order. The first message carries a reply so a rejected batch open surfaces
// immediately; intermediate messages travel unacknowledged; the final message
// commits the batch and receives the single PubAck.
func (s *atomicSender) send(batch []publishRequest) (atomicCohort, error) {
	if len(batch) > atomicBatchMax {
		return atomicCohort{}, fmt.Errorf("bus: cohort of %d exceeds the %d-message atomic batch limit", len(batch), atomicBatchMax)
	}
	id := nuid.Next()
	cohort := atomicCohort{id: id, ch: s.begin(id), n: len(batch)}
	frameBatch(s.prefix, batch, id)
	for _, req := range batch {
		if err := s.nc.PublishMsg(req.msg); err != nil {
			s.end(id)
			return atomicCohort{}, fmt.Errorf("bus: atomic batch publish %s: %w", req.msg.Subject, err)
		}
	}
	return cohort, nil
}

// frameBatch stamps ADR-050 batch framing onto a cohort: the opening message
// carries a reply for immediate rejection errors, intermediates travel
// unacknowledged, and the final message commits the batch.
func frameBatch(prefix string, batch []publishRequest, id string) {
	for i, req := range batch {
		msg := req.msg
		msg.Header.Set(batchIDHdr, id)
		msg.Header.Set(batchSeqHdr, strconv.Itoa(i+1))
		switch i {
		case 0:
			msg.Reply = prefix + "s." + id
		case len(batch) - 1:
			msg.Header.Set(batchCommitHdr, "1")
			msg.Reply = prefix + "c." + id
		default:
			msg.Reply = ""
		}
	}
}

// await blocks until the cohort's commit PubAck, a broker error reply, or the
// timeout. The commit ack must account for every message in the batch.
func (s *atomicSender) await(cohort atomicCohort, timeout time.Duration) error {
	defer s.end(cohort.id)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case msg := <-cohort.ch:
		return checkBatchAck(msg.Data, cohort)
	case <-timer.C:
		return errAtomicAckTimeout
	}
}

// batchPubAck is the ADR-050 commit acknowledgement. nats.go v1.52 does not
// yet expose the batch fields on its PubAck, so they are decoded here.
type batchPubAck struct {
	Stream    string          `json:"stream"`
	Sequence  uint64          `json:"seq"`
	Duplicate bool            `json:"duplicate"`
	BatchID   string          `json:"batch"`
	Count     int             `json:"count"`
	Error     *batchAckAPIErr `json:"error"`
}

type batchAckAPIErr struct {
	Code        int    `json:"code"`
	ErrCode     int    `json:"err_code"`
	Description string `json:"description"`
}

func (e *batchAckAPIErr) Error() string {
	return fmt.Sprintf("bus: atomic batch rejected (%d/%d): %s", e.Code, e.ErrCode, e.Description)
}

func checkBatchAck(data []byte, cohort atomicCohort) error {
	var ack batchPubAck
	if err := sonic.ConfigFastest.Unmarshal(data, &ack); err != nil {
		return fmt.Errorf("bus: undecodable atomic batch ack: %w", err)
	}
	if ack.Error != nil {
		return ack.Error
	}
	if ack.Sequence == 0 {
		return errors.New("bus: atomic batch ack carries no stream sequence")
	}
	if ack.BatchID != cohort.id || ack.Count != cohort.n {
		return fmt.Errorf("bus: atomic batch ack mismatch: batch %q count %d, want %q count %d",
			ack.BatchID, ack.Count, cohort.id, cohort.n)
	}
	return nil
}

// stripBatchHeaders reverts a request staged for an atomic batch so it can be
// re-published individually: leftover batch headers would make the broker
// treat it as part of an unknown batch, and the stale reply subject must not
// leak into nats.go's own async reply management.
func stripBatchHeaders(batch []publishRequest) {
	for _, req := range batch {
		req.msg.Header.Del(batchIDHdr)
		req.msg.Header.Del(batchSeqHdr)
		req.msg.Header.Del(batchCommitHdr)
		req.msg.Reply = ""
	}
}

// publishAtomic drives one cohort over the atomic wire: batch send from the
// worker's serial loop and commit await in the bounded overlap goroutine. Only
// a typed broker rejection may fall back to per-message publishing.
func (w *publishBatchWorker) publishAtomic(batch []publishRequest) {
	w.slots <- struct{}{}
	cohort, err := w.owner.sender.send(batch)
	if err != nil {
		<-w.slots
		w.finish(batch, err)
		return
	}
	w.acks.Add(1)
	go func() {
		defer w.acks.Done()
		defer func() { <-w.slots }()
		if err := w.owner.sender.await(cohort, defaultPublishAckWait); err != nil {
			if atomicFallbackSafe(err) {
				w.fallbackIndividually(batch, err)
				return
			}
			w.finish(batch, err)
			return
		}
		w.finish(batch, nil)
	}()
}

func atomicFallbackSafe(err error) bool {
	var rejected *batchAckAPIErr
	return errors.As(err, &rejected)
}

// fallbackIndividually re-drives a definitely rejected atomic cohort message
// by message. Callers must prove the broker did not commit before entering.
func (w *publishBatchWorker) fallbackIndividually(batch []publishRequest, cause error) {
	w.owner.log.Warn("atomic batch rejected; re-publishing cohort individually",
		zap.Int("messages", len(batch)), zap.Error(cause))
	stripBatchHeaders(batch)
	futures, err := w.startAsync(batch)
	if err != nil {
		w.finish(batch, err)
		return
	}
	w.finish(batch, w.awaitAsync(futures))
}
