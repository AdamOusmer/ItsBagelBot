package bus

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/pkg/monitor"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

// Consume subscribes to subject and feeds every message to handle, one at a
// time. A handler error nacks the message so JetStream redelivers it; handlers
// must therefore be idempotent (ADR 0003). The loop ends when ctx is cancelled.
//
// Every message is processed inside its own New Relic transaction, joined to
// the publisher's trace when the metadata carries trace headers. The
// transaction is exposed through the message context, so handlers and the
// instrumented database driver report into it automatically. A nil app makes
// all of that a no-op.
//
// Consume is fully independent of ConsumeWeighted: it owns its own single
// subject and serial loop, so the two can evolve separately.
func Consume(ctx context.Context, app *newrelic.Application, sub Subscriber, subject string, handle func(*Message) error, log *zap.Logger) error {

	messages, err := sub.Subscribe(ctx, subject)
	if err != nil {
		return err
	}

	go func() {
		for msg := range messages {
			process(app, subject, msg, handle, log)
		}
	}()

	return nil
}

// process runs one message inside its own New Relic transaction and applies the
// ack/nack discipline shared by Consume and ConsumeWeighted: ack only after
// handle returns nil, nack on any error so JetStream redelivers. A nil app
// makes the New Relic calls no-ops.
func process(app *newrelic.Application, subject string, msg *Message, handle func(*Message) error, log *zap.Logger) {

	txn := app.StartTransaction("consume " + normalizedDestination(subject))
	acceptTraceHeaders(txn, metadataHeaders(msg.Metadata))
	addMessagingTransactionAttributes(txn, messagingAttributes{operation: "process", destination: subject})
	txn.AddAttribute("messaging.queue_ms", float64(msg.deliveryWait(time.Now()).Microseconds())/1000)
	log = monitor.TraceLogger(txn, log)

	msg.SetContext(newrelic.NewContext(msg.Context(), txn))

	processSegment := txn.StartSegment("message.process")
	err := handle(msg)
	processSegment.AddAttribute(resultAttribute, processResult(err))
	processSegment.End()
	if err != nil {
		// Expected backpressure (rate limits and a deliberate system pause) still
		// nacks, but must not turn an overload into one New Relic error and warning
		// log per delivery attempt. Packages opt in through this tiny structural
		// interface, avoiding a dependency from bus onto any worker package.
		quiet := isExpectedNack(err)
		if !quiet {
			txn.NoticeError(err)
		}
		txn.AddAttribute(resultAttribute, processResult(err))
		txn.End()

		if quiet {
			log.Debug("event deferred by expected backpressure",
				zap.String("subject", subject),
				zap.String("message_id", msg.UUID),
				zap.Error(err))
		} else {
			log.Warn("event handling failed, nacking",
				zap.String("subject", subject),
				zap.String("message_id", msg.UUID),
				zap.Error(err))
		}
		msg.Nack()
		return
	}

	txn.AddAttribute(resultAttribute, "ok")
	txn.End()
	msg.Ack()
}

func metadataHeaders(metadata Metadata) nats.Header {
	headers := make(nats.Header, len(metadata))
	for key, value := range metadata {
		headers.Set(key, value)
	}
	return headers
}

func processResult(err error) string {
	if err == nil {
		return "ok"
	}
	if isExpectedNack(err) {
		return "deferred"
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
