// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/pkg/monitor"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

// Consume subscribes to subject and feeds every message to handle, one at a
// time. A handler error nacks the message so JetStream redelivers it; handlers
// must therefore be idempotent (ADR 0003). The loop ends when ctx is cancelled.
//
// A sampled share of messages is processed inside its own New Relic
// transaction, joined to the publisher's trace when the metadata carries trace
// headers. That transaction is exposed through the message context, so handlers
// and the instrumented database driver report into it automatically; unsampled
// messages carry no transaction and newrelic.FromContext returns nil for them,
// which every handler already tolerates because it is what a nil app has always
// produced. Failures are instrumented regardless of sampling, and every message
// is counted on the side. See consumeLane.process for the whole contract.
//
// Consume is fully independent of ConsumeWeighted: it owns its own single
// subject and serial loop, so the two can evolve separately.
func Consume(ctx context.Context, app *newrelic.Application, sub Subscriber, subject string, handle func(*Message) error, log *zap.Logger) error {

	messages, err := sub.Subscribe(ctx, subject)
	if err != nil {
		return err
	}

	lane := newConsumeLane(app, subject, handle, log)

	go func() {
		for msg := range messages {
			lane.process(msg)
		}
	}()

	return nil
}

// consumeLane bundles the invariants of one subscription — everything process
// needs that does not change between deliveries. It exists so the per-message
// call carries only the message: the New Relic transaction name in particular
// is derived from the subject, and deriving it here means the destination
// normalization and the concatenation it feeds run once per subscription
// rather than on every delivery of every lane.
type consumeLane struct {
	app     *newrelic.Application
	txnName string
	subject string
	handle  func(*Message) error
	log     *zap.Logger
	// stats is shared, by pointer, with every other consumeLane built for the
	// same destination — the weighted consumer builds one per consumer unit, and
	// the sampling cursor and counters have to be the lane's, not the unit's.
	// Never nil for a lane built through newConsumeLane.
	stats *laneStats
}

func newConsumeLane(app *newrelic.Application, subject string, handle func(*Message) error, log *zap.Logger) consumeLane {
	return consumeLane{
		app:     app,
		txnName: "consume " + normalizedDestination(subject),
		subject: subject,
		handle:  handle,
		log:     log,
		stats:   consumeTelemetry.register(app, subject, consumeNRSampleRate),
	}
}

// process runs one message under the lane's New Relic sampling contract and
// applies the ack/nack discipline shared by Consume and ConsumeWeighted: ack
// only after handle returns nil, nack on any error so JetStream redelivers.
//
// One message in every consumeNRSampleRate pays for a full transaction: trace
// join, messaging attributes, queue wait, a message.process segment, and —
// because the transaction is in the handler's context — the instrumented
// database driver's datastore spans. The rest run with no transaction at all.
// That is the point: at 100k msg/s the whole consumer path has roughly 10µs per
// message, and one go-agent transaction can spend that alone.
//
// Two things are never sampled away. Handler failures that are not expected
// backpressure are instrumented retroactively (see processUnsampled), and every
// message, sampled or not, is counted in the lane's atomic counters and reported
// by the side channel in telemetry.go.
//
// A nil app makes every New Relic call a no-op, as before. The subject stays out
// of APM names (see normalizedDestination) but belongs in the logs.
func (lane consumeLane) process(msg *Message) {
	// One clock read per message, shared by the queue_ms attribute and the
	// counters, so the unsampled path adds a vDSO call and nothing else.
	wait := msg.deliveryWait(time.Now())

	if lane.stats.sample() {
		lane.processSampled(msg, wait)
		return
	}
	lane.processUnsampled(msg, wait)
}

// processSampled is the pre-sampling path, unchanged: this is exactly what every
// message did before, and exactly what every message still does at the default
// sample rate of 1.
func (lane consumeLane) processSampled(msg *Message, wait time.Duration) {
	txn := lane.startTransaction(msg, wait)
	log := monitor.TraceLogger(txn, lane.log)

	msg.SetContext(newrelic.NewContext(msg.Context(), txn))

	processSegment := txn.StartSegment("message.process")
	out := classify(lane.handle(msg), wait)
	processSegment.AddAttribute(resultAttribute, out.result)
	processSegment.End()

	// Expected backpressure (rate limits and a deliberate system pause) still
	// nacks, but must not turn an overload into one New Relic error and warning
	// log per delivery attempt. Packages opt in through this tiny structural
	// interface, avoiding a dependency from bus onto any worker package.
	if out.loud() {
		txn.NoticeError(out.err)
	}
	txn.AddAttribute(resultAttribute, out.result)
	txn.End()

	lane.finish(msg, out, log)
}

// processUnsampled runs the handler with no transaction, then buys one back only
// if the handler actually failed.
//
// The retroactive transaction is genuinely skewed: its clock starts after the
// handler returned, so its duration is the reporting, not the work. That is
// worth an error reaching New Relic with a real stack and trace id, and it is
// labelled sampled="error" precisely so nobody reads those durations as latency.
// Expected backpressure creates nothing at all — an overload must not turn into
// a transaction per delivery attempt, which is the whole reason the quiet path
// exists.
func (lane consumeLane) processUnsampled(msg *Message, wait time.Duration) {
	// Deliberate, not redundant: the handler contract is that msg.Context() is
	// usable, so the delivery context is handed through explicitly rather than
	// left to whatever the subscriber happened to set. It carries no transaction,
	// so newrelic.FromContext returns nil — the same thing it returns under a nil
	// app, which is the case every handler in the fleet already guards for.
	msg.SetContext(msg.Context())

	out := classify(lane.handle(msg), wait)

	log := lane.log
	if out.loud() {
		log = lane.noticeUnsampledError(msg, out)
	}

	lane.finish(msg, out, log)
}

// noticeUnsampledError reports a failure that sampling would otherwise have
// swallowed, and returns the trace-linked logger so the warning line still
// correlates with the transaction exactly as it does on the sampled path.
func (lane consumeLane) noticeUnsampledError(msg *Message, out outcome) *zap.Logger {
	txn := lane.startTransaction(msg, out.wait)
	txn.AddAttribute(sampledAttribute, "error")
	txn.NoticeError(out.err)
	txn.AddAttribute(resultAttribute, out.result)

	// Before End, not after: an ended transaction reports empty trace metadata,
	// so a logger derived from it afterwards would silently drop trace.id and the
	// warning would not correlate with the error it describes.
	log := monitor.TraceLogger(txn, lane.log)
	txn.End()
	return log
}

// startTransaction opens the lane transaction and applies the attributes the
// sampled and the retroactive-error paths agree on. Nil-safe throughout: every
// go-agent call here tolerates a nil application and a nil transaction.
func (lane consumeLane) startTransaction(msg *Message, wait time.Duration) *newrelic.Transaction {
	txn := lane.app.StartTransaction(lane.txnName)
	acceptMetadataTraceHeaders(txn, msg.Metadata)
	addMessagingTransactionAttributes(txn, messagingAttributes{operation: "process", destination: lane.subject})
	txn.AddAttribute(queueMillisAttribute, float64(wait.Microseconds())/1000)
	return txn
}

// finish is the sampling-independent tail: count the delivery, log it, and
// resolve it. Both paths route through here so the ack/nack discipline and the
// log lines can never drift apart between them.
func (lane consumeLane) finish(msg *Message, out outcome, log *zap.Logger) {
	lane.stats.record(out.result, out.wait)

	switch {
	case out.err == nil:
		msg.Ack()
		return
	case out.result == resultDeferred:
		log.Debug("event deferred by expected backpressure",
			zap.String("subject", lane.subject),
			zap.String("message_id", msg.UUID),
			zap.Error(out.err))
	default:
		log.Warn("event handling failed, nacking",
			zap.String("subject", lane.subject),
			zap.String("message_id", msg.UUID),
			zap.Error(out.err))
	}
	msg.Nack()
}

// outcome is one delivery classified exactly once. The classification walks the
// error chain (isExpectedNack), and the pre-sampling code derived it three times
// per message — for the segment attribute, for the transaction attribute, and
// again to decide whether the failure was loud. Carrying it costs nothing: the
// struct never escapes, so it stays in registers.
type outcome struct {
	err    error
	result string
	wait   time.Duration
}

func classify(err error, wait time.Duration) outcome {
	return outcome{err: err, result: processResult(err), wait: wait}
}

// loud reports a failure that must reach New Relic. Expected backpressure is not
// loud: it is a nack the fleet asked for, not an incident.
func (o outcome) loud() bool {
	return o.err != nil && o.result != resultDeferred
}

func processResult(err error) string {
	if err == nil {
		return resultOK
	}
	if isExpectedNack(err) {
		return resultDeferred
	}
	return messagingResult(err)
}

func isExpectedNack(err error) bool {
	type marker interface{ ExpectedNack() bool }
	if expected, ok := err.(marker); ok {
		return expected.ExpectedNack()
	}
	var expected marker
	return errors.As(err, &expected) && expected.ExpectedNack()
}
