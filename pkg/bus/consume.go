// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/pkg/monitor"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

// A handler error nacks for redelivery, so handle must be idempotent.
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

type consumeLane struct {
	app     *newrelic.Application
	txnName string
	subject string
	handle  func(*Message) error
	log     *zap.Logger
	stats   *laneStats
}

func newConsumeLane(app *newrelic.Application, subject string, handle func(*Message) error, log *zap.Logger) consumeLane {
	if handle != nil && log != nil {
		base := handle
		lg := log
		handle = func(msg *Message) (err error) {
			defer func() {
				if r := recover(); r != nil {
					lg.Error("consume handler panic recovered",
						zap.String("subject", subject), zap.Any("panic", r))
					err = fmt.Errorf("handler panic: %v", r)
				}
			}()
			return base(msg)
		}
	}
	return consumeLane{
		app:     app,
		txnName: "consume " + normalizedDestination(subject),
		subject: subject,
		handle:  handle,
		log:     log,
		stats:   consumeTelemetry.register(app, subject, consumeNRSampleRate),
	}
}

func (lane consumeLane) process(msg *Message) {
	wait := msg.deliveryWait(time.Now())

	if lane.stats.sample() {
		lane.processSampled(msg, wait)
		return
	}
	lane.processUnsampled(msg, wait)
}

func (lane consumeLane) processSampled(msg *Message, wait time.Duration) {
	txn := lane.startTransaction(msg, wait)
	log := monitor.TraceLogger(txn, lane.log)

	msg.SetContext(newrelic.NewContext(msg.Context(), txn))

	processSegment := txn.StartSegment("message.process")
	out := classify(lane.handle(msg), wait)
	processSegment.AddAttribute(resultAttribute, out.result)
	processSegment.End()

	if out.loud() {
		txn.NoticeError(out.err)
	}
	txn.AddAttribute(resultAttribute, out.result)
	txn.End()

	lane.finish(msg, out, log)
}

func (lane consumeLane) processUnsampled(msg *Message, wait time.Duration) {
	msg.SetContext(msg.Context())

	out := classify(lane.handle(msg), wait)

	log := lane.log
	if out.loud() {
		log = lane.noticeUnsampledError(msg, out)
	}

	lane.finish(msg, out, log)
}

func (lane consumeLane) noticeUnsampledError(msg *Message, out outcome) *zap.Logger {
	txn := lane.startTransaction(msg, out.wait)
	txn.AddAttribute(sampledAttribute, "error")
	txn.NoticeError(out.err)
	txn.AddAttribute(resultAttribute, out.result)

	log := monitor.TraceLogger(txn, lane.log)
	txn.End()
	return log
}

func (lane consumeLane) startTransaction(msg *Message, wait time.Duration) *newrelic.Transaction {
	txn := lane.app.StartTransaction(lane.txnName)
	acceptMetadataTraceHeaders(txn, msg.Metadata)
	addMessagingTransactionAttributes(txn, messagingAttributes{operation: "process", destination: lane.subject})
	txn.AddAttribute(queueMillisAttribute, float64(wait.Microseconds())/1000)
	return txn
}

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

type outcome struct {
	err    error
	result string
	wait   time.Duration
}

func classify(err error, wait time.Duration) outcome {
	return outcome{err: err, result: processResult(err), wait: wait}
}

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
