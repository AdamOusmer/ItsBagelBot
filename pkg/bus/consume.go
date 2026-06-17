package bus

import (
	"context"
	"net/http"

	"github.com/ThreeDotsLabs/watermill/message"

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
func Consume(ctx context.Context, app *newrelic.Application, sub message.Subscriber, subject string, handle func(*message.Message) error, log *zap.Logger) error {

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
func process(app *newrelic.Application, subject string, msg *message.Message, handle func(*message.Message) error, log *zap.Logger) {

	txn := app.StartTransaction("consume " + subject)

	headers := http.Header{}
	for key, value := range msg.Metadata {
		headers.Set(key, value)
	}
	txn.AcceptDistributedTraceHeaders(newrelic.TransportQueue, headers)

	msg.SetContext(newrelic.NewContext(msg.Context(), txn))

	if err := handle(msg); err != nil {
		txn.NoticeError(err)
		txn.End()

		log.Warn("event handling failed, nacking",
			zap.String("subject", subject),
			zap.String("message_id", msg.UUID),
			zap.Error(err),
		)
		msg.Nack()
		return
	}

	txn.End()
	msg.Ack()
}
