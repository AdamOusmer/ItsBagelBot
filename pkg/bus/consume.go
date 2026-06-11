package bus

import (
	"context"
	"net/http"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

// Consume subscribes to subject and feeds every message to handle. A handler
// error nacks the message so JetStream redelivers it; handlers must therefore
// be idempotent (ADR 0003). The loop ends when ctx is cancelled.
//
// Every message is processed inside its own New Relic transaction, joined to
// the publisher's trace when the metadata carries trace headers. The
// transaction is exposed through the message context, so handlers and the
// instrumented database driver report into it automatically. A nil app makes
// all of that a no-op.
func Consume(ctx context.Context, app *newrelic.Application, sub message.Subscriber, subject string, handle func(*message.Message) error, log *zap.Logger) error {

	messages, err := sub.Subscribe(ctx, subject)
	if err != nil {
		return err
	}

	go func() {
		for msg := range messages {

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
				continue
			}

			txn.End()
			msg.Ack()
		}
	}()

	return nil
}
