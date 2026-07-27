package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

const (
	consumerStatusHeader        = "Status"
	consumerLastSequenceHeader  = "Nats-Last-Consumer"
	streamLastSequenceHeader    = "Nats-Last-Stream"
	consumerStalledHeader       = "Nats-Consumer-Stalled"
	shadowConsumerDeliverPrefix = "_INBOX.BAGEL.R3."
	shadowConsumerHeartbeat     = time.Second
)

type consumerResult struct {
	Name                 string  `json:"name"`
	Server               string  `json:"server"`
	TLSVersion           string  `json:"tls_version"`
	TLSCipher            string  `json:"tls_cipher"`
	Expected             int64   `json:"expected"`
	Received             int64   `json:"received"`
	Bytes                int64   `json:"bytes"`
	StartedUnixMS        int64   `json:"started_unix_ms"`
	FinishedUnixMS       int64   `json:"finished_unix_ms"`
	MessagesPerSec       float64 `json:"messages_per_second"`
	FlowResponses        int64   `json:"flow_responses"`
	LastConsumerSequence uint64  `json:"last_consumer_sequence"`
	LastStreamSequence   uint64  `json:"last_stream_sequence"`
	SequenceGaps         int64   `json:"sequence_gaps"`
	Duplicates           int64   `json:"duplicates"`
	Reconnects           int64   `json:"reconnects"`
	Disconnects          int64   `json:"disconnects"`
	AsyncErrors          int64   `json:"async_errors"`
	Passed               bool    `json:"passed"`
	Failure              string  `json:"failure,omitempty"`
}

type consumerStateResult struct {
	Name              string `json:"name"`
	DeliveredConsumer uint64 `json:"delivered_consumer"`
	DeliveredStream   uint64 `json:"delivered_stream"`
	AckFloorConsumer  uint64 `json:"ack_floor_consumer"`
	AckFloorStream    uint64 `json:"ack_floor_stream"`
	Pending           uint64 `json:"pending"`
	AckPending        int    `json:"ack_pending"`
	Redelivered       int    `json:"redelivered"`
	Expected          int64  `json:"expected"`
	Passed            bool   `json:"passed"`
	Failure           string `json:"failure,omitempty"`
}

type consumerCursor struct {
	mu       sync.Mutex
	consumer uint64
	stream   uint64
}

type consumerCounters struct {
	expected      int64
	received      atomic.Int64
	bytes         atomic.Int64
	flowResponses atomic.Int64
	gaps          atomic.Int64
	duplicates    atomic.Int64
	started       atomic.Int64
	finished      atomic.Int64
	complete      chan struct{}
	flowComplete  chan struct{}
	failures      chan error
	completeOnce  sync.Once
	flowOnce      sync.Once
	firstErr      atomic.Pointer[string]
}

func safeConsumerName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, char := range name {
		switch {
		case char == '_', char == '-':
		case char >= 'A' && char <= 'Z':
		case char >= 'a' && char <= 'z':
		case char >= '0' && char <= '9':
		default:
			return false
		}
	}
	return true
}

func shadowConsumerDeliverSubject(name string) string {
	return shadowConsumerDeliverPrefix + name
}

func shadowConsumerConfig(cfg config) jsapi.ConsumerConfig {
	return jsapi.ConsumerConfig{
		Name:           cfg.consumerName,
		Durable:        cfg.consumerName,
		Description:    "ItsBagelBot R3 shadow AckFlowControl ceiling consumer",
		DeliverPolicy:  jsapi.DeliverAllPolicy,
		AckPolicy:      jsapi.AckFlowControlPolicy,
		MaxDeliver:     -1,
		FilterSubject:  cfg.subject,
		ReplayPolicy:   jsapi.ReplayInstantPolicy,
		MaxAckPending:  cfg.consumerMaxPending,
		DeliverSubject: shadowConsumerDeliverSubject(cfg.consumerName),
		DeliverGroup:   cfg.consumerGroup,
		FlowControl:    true,
		IdleHeartbeat:  shadowConsumerHeartbeat,
		Replicas:       0,
		MemoryStorage:  true,
	}
}

func (r *acceptanceRun) executeConsumer() error {
	result, runErr := consumeShadow(r.cfg, r.setup, r.tlsConfig != nil)
	result.Passed = runErr == nil
	if runErr != nil {
		result.Failure = runErr.Error()
	}
	out, err := json.MarshalIndent(map[string]any{
		"stream": r.cfg.stream, "subject": r.cfg.subject, "consumer": result,
	}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return runErr
}

func consumeShadow(cfg config, c client, secure bool) (consumerResult, error) {
	result := consumerResult{Name: cfg.consumerName, Expected: int64(cfg.messages)}
	if err := describeConsumerConnection(&result, c, secure); err != nil {
		return result, err
	}

	c.stats.failures = make(chan error, 1)
	counters := &consumerCounters{
		expected: int64(cfg.messages), complete: make(chan struct{}), flowComplete: make(chan struct{}),
		failures: make(chan error, 1),
	}
	cursor := &consumerCursor{}
	sub, err := startShadowConsumer(cfg, c, counters, cursor)
	if err != nil {
		return result, err
	}
	defer sub.Unsubscribe()

	ctx, cancel := benchmarkContext(cfg)
	defer cancel()
	if err := waitForConsumerStart(ctx, cfg.startAtUnixMS); err != nil {
		return result, err
	}
	if err := awaitConsumer(ctx, counters, c.stats); err != nil {
		applyConsumerCounters(&result, counters, cursor, c.stats)
		return result, err
	}
	if err := c.nc.FlushTimeout(cfg.ackTimeout); err != nil {
		applyConsumerCounters(&result, counters, cursor, c.stats)
		return result, fmt.Errorf("flush final flow response: %w", err)
	}
	applyConsumerCounters(&result, counters, cursor, c.stats)
	return result, validateConsumerResult(result, counters.firstErr.Load())
}

// startShadowConsumer subscribes to the delivery subject before the consumer
// exists, because a delivery into a subject with no interest is lost outright
// and never comes back. AckFlowControl forces AckWait to zero (the server
// rejects any other value), so the pending timer cancels itself on its first
// tick and there is no AckWait redelivery, no MaxDeliver exhaustion and no
// advisory — a pre-interest window would simply show up as a short receive count
// with nothing to explain it.
//
// Production's only redelivery on this policy is the client-side scheduled retry
// hop, which this harness does not exercise: the isolated stream carries a single
// subject, and nats-server requires a schedule's target to be a different subject
// of the same stream.
func startShadowConsumer(
	cfg config,
	c client,
	counters *consumerCounters,
	cursor *consumerCursor,
) (*nats.Subscription, error) {
	sub, err := c.nc.QueueSubscribe(
		shadowConsumerDeliverSubject(cfg.consumerName),
		cfg.consumerGroup,
		shadowConsumerHandler(c.nc, counters, cursor),
	)
	if err != nil {
		return nil, fmt.Errorf("subscribe shadow consumer: %w", err)
	}
	if err := c.nc.FlushTimeout(cfg.ackTimeout); err != nil {
		sub.Unsubscribe()
		return nil, fmt.Errorf("activate shadow consumer: %w", err)
	}
	if err := ensureShadowConsumer(cfg, c.modern); err != nil {
		sub.Unsubscribe()
		return nil, err
	}
	return sub, nil
}

func ensureShadowConsumer(cfg config, js jsapi.JetStream) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ackTimeout)
	defer cancel()
	if _, err := js.CreateOrUpdatePushConsumer(ctx, cfg.stream, shadowConsumerConfig(cfg)); err != nil {
		return fmt.Errorf("create shadow flow consumer %s: %w", cfg.consumerName, err)
	}
	return nil
}

func describeConsumerConnection(result *consumerResult, c client, secure bool) error {
	result.Server = c.nc.ConnectedServerName()
	if !secure {
		result.TLSVersion = "plaintext-local"
		return nil
	}
	state, err := c.nc.TLSConnectionState()
	if err != nil {
		return fmt.Errorf("consumer connection is not using verified TLS: %w", err)
	}
	result.TLSVersion = tlsVersion(state.Version)
	result.TLSCipher = tls.CipherSuiteName(state.CipherSuite)
	return nil
}

func shadowConsumerHandler(
	nc *nats.Conn,
	counters *consumerCounters,
	cursor *consumerCursor,
) nats.MsgHandler {
	return func(msg *nats.Msg) {
		if isConsumerStatus(msg) {
			respondShadowFlowControl(nc, msg, counters, cursor)
			return
		}
		metadata, err := msg.Metadata()
		if err != nil {
			counters.fail(fmt.Errorf("decode consumer sequence: %w", err))
			return
		}
		cursor.record(metadata.Sequence.Consumer, metadata.Sequence.Stream, counters)
		now := time.Now().UnixMilli()
		counters.started.CompareAndSwap(0, now)
		counters.bytes.Add(int64(len(msg.Data)))
		received := counters.received.Add(1)
		if received == counters.expected {
			counters.finished.Store(now)
			counters.completeOnce.Do(func() { close(counters.complete) })
		} else if received > counters.expected {
			counters.fail(fmt.Errorf("received more than expected: %d/%d", received, counters.expected))
		}
	}
}

func isConsumerStatus(msg *nats.Msg) bool {
	return len(msg.Data) == 0 && msg.Header.Get(consumerStatusHeader) != ""
}

func (c *consumerCursor) record(consumer, stream uint64, counters *consumerCounters) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case consumer <= c.consumer:
		counters.duplicates.Add(1)
	case c.consumer > 0 && consumer != c.consumer+1:
		counters.gaps.Add(1)
	}
	if consumer > c.consumer {
		c.consumer = consumer
		c.stream = stream
	}
}

func (c *consumerCursor) snapshot() (uint64, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.consumer, c.stream
}

func respondShadowFlowControl(
	nc *nats.Conn,
	control *nats.Msg,
	counters *consumerCounters,
	cursor *consumerCursor,
) {
	response, consumerSequence := shadowFlowControlResponse(control, cursor)
	if response == nil {
		return
	}
	if err := nc.PublishMsg(response); err != nil {
		counters.fail(fmt.Errorf("publish flow-control response: %w", err))
		return
	}
	counters.flowResponses.Add(1)
	if consumerSequence >= uint64(counters.expected) && counters.received.Load() >= counters.expected {
		counters.flowOnce.Do(func() { close(counters.flowComplete) })
	}
}

// shadowFlowControlResponse mirrors the production hot lane exactly: the ack
// floor claimed is always this process's own receipt cursor, never the pair the
// server put on the heartbeat. The server reports what it sent, and answering
// with that would advance the replicated ack floor past deliveries that never
// arrived. A response is still sent with an empty cursor — the delivery window
// reopens on the response arriving, and the server reads an ack floor only when
// the headers are there.
func shadowFlowControlResponse(control *nats.Msg, cursor *consumerCursor) (*nats.Msg, uint64) {
	reply := control.Reply
	if stalled := control.Header.Get(consumerStalledHeader); stalled != "" {
		reply = stalled
	}
	if reply == "" {
		return nil, 0
	}
	response := nats.NewMsg(reply)
	consumerSequence, streamSequence := cursor.snapshot()
	if consumerSequence == 0 || streamSequence == 0 {
		return response, 0
	}
	response.Header.Set(consumerLastSequenceHeader, strconv.FormatUint(consumerSequence, 10))
	response.Header.Set(streamLastSequenceHeader, strconv.FormatUint(streamSequence, 10))
	return response, consumerSequence
}

func waitForConsumerStart(ctx context.Context, unixMS int64) error {
	if unixMS <= 0 {
		return nil
	}
	wait := time.Until(time.UnixMilli(unixMS))
	if wait < -time.Second {
		return fmt.Errorf("consumer missed shared start barrier by %s", -wait)
	}
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func awaitConsumer(ctx context.Context, counters *consumerCounters, stats *connectionStats) error {
	select {
	case <-counters.complete:
	case err := <-counters.failures:
		return err
	case err := <-stats.failures:
		return err
	case <-ctx.Done():
		return fmt.Errorf("consumer did not receive %d messages: %w", counters.expected, ctx.Err())
	}
	select {
	case <-counters.flowComplete:
		return nil
	case err := <-counters.failures:
		return err
	case err := <-stats.failures:
		return err
	case <-ctx.Done():
		return fmt.Errorf("consumer received all messages but final flow window did not settle: %w", ctx.Err())
	}
}

func (c *consumerCounters) fail(err error) {
	if err == nil {
		return
	}
	message := err.Error()
	if c.firstErr.CompareAndSwap(nil, &message) {
		select {
		case c.failures <- err:
		default:
		}
	}
}

func applyConsumerCounters(
	result *consumerResult,
	counters *consumerCounters,
	cursor *consumerCursor,
	stats *connectionStats,
) {
	result.Received = counters.received.Load()
	result.Bytes = counters.bytes.Load()
	result.FlowResponses = counters.flowResponses.Load()
	result.SequenceGaps = counters.gaps.Load()
	result.Duplicates = counters.duplicates.Load()
	result.StartedUnixMS = counters.started.Load()
	result.FinishedUnixMS = counters.finished.Load()
	result.LastConsumerSequence, result.LastStreamSequence = cursor.snapshot()
	result.Reconnects = stats.reconnects.Load()
	result.Disconnects = stats.disconnects.Load()
	result.AsyncErrors = stats.asyncErrors.Load()
	if duration := result.FinishedUnixMS - result.StartedUnixMS; duration > 0 {
		result.MessagesPerSec = float64(result.Received) / (float64(duration) / 1000)
	}
}

func validateConsumerResult(result consumerResult, firstErr *string) error {
	switch {
	case firstErr != nil:
		return errors.New(*firstErr)
	case result.Received != result.Expected:
		return fmt.Errorf("consumer received %d/%d messages", result.Received, result.Expected)
	case result.LastConsumerSequence != uint64(result.Expected):
		return fmt.Errorf("consumer sequence ended at %d/%d", result.LastConsumerSequence, result.Expected)
	// Gaps are the delivered-vs-received reconciliation: the consumer sequence is
	// dense, so a skip means the server delivered something this subscription
	// never took off the wire and never will. Duplicates are reported but never
	// gated — this ack policy has no AckWait, no MaxDeliver and no redelivery, so
	// there is no mechanism that could produce one.
	case result.SequenceGaps != 0:
		return fmt.Errorf("consumer sequence gaps=%d across %d received deliveries",
			result.SequenceGaps, result.Received)
	case result.FlowResponses == 0:
		return errors.New("consumer sent no receipt-level flow responses")
	case result.Reconnects+result.Disconnects+result.AsyncErrors > 0:
		return fmt.Errorf(
			"consumer connection instability: reconnects=%d disconnects=%d async_errors=%d",
			result.Reconnects, result.Disconnects, result.AsyncErrors,
		)
	default:
		return nil
	}
}

func (r *acceptanceRun) executeConsumerCheck() error {
	state, err := waitForConsumerState(r.cfg, r.setup.modern)
	state.Passed = err == nil
	if err != nil {
		state.Failure = err.Error()
	}
	out, marshalErr := json.MarshalIndent(map[string]any{
		"stream": r.cfg.stream, "subject": r.cfg.subject, "consumer_state": state,
	}, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	fmt.Println(string(out))
	return err
}

func waitForConsumerState(cfg config, js jsapi.JetStream) (consumerStateResult, error) {
	deadline := time.Now().Add(cfg.consumerSettleTimeout)
	var last consumerStateResult
	for {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ackTimeout)
		consumer, err := js.Consumer(ctx, cfg.stream, cfg.consumerName)
		if err == nil {
			var info *jsapi.ConsumerInfo
			info, err = consumer.Info(ctx)
			if err == nil {
				last = consumerStateFromInfo(cfg, info)
				if consumerStateReady(last) {
					cancel()
					return last, nil
				}
			}
		}
		cancel()
		if time.Now().After(deadline) {
			if err != nil {
				return last, fmt.Errorf("inspect shadow consumer: %w", err)
			}
			return last, fmt.Errorf(
				"shadow consumer did not settle: pending=%d ack_pending=%d ack_floor=%d/%d",
				last.Pending, last.AckPending, last.AckFloorConsumer, cfg.messages,
			)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func consumerStateFromInfo(cfg config, info *jsapi.ConsumerInfo) consumerStateResult {
	return consumerStateResult{
		Name:              cfg.consumerName,
		DeliveredConsumer: info.Delivered.Consumer,
		DeliveredStream:   info.Delivered.Stream,
		AckFloorConsumer:  info.AckFloor.Consumer,
		AckFloorStream:    info.AckFloor.Stream,
		Pending:           info.NumPending,
		AckPending:        info.NumAckPending,
		Redelivered:       info.NumRedelivered,
		Expected:          int64(cfg.messages),
	}
}

// consumerStateReady is the settle gate. NumRedelivered is reported but not
// required to be zero: nothing under AckFlowControl can redeliver, so gating on
// it would assert a property of the server rather than of the run.
func consumerStateReady(state consumerStateResult) bool {
	return state.DeliveredConsumer == uint64(state.Expected) &&
		state.AckFloorConsumer == uint64(state.Expected) &&
		state.Pending == 0 &&
		state.AckPending == 0
}
