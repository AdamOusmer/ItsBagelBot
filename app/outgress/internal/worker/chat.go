package worker

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/outgress"
)

func (w *Worker) processChat(ctx context.Context, payload *outgress.Message) error {
	// The enabled/disabled decision belongs to the worker, not outgress: by the
	// time a chat send reaches here it is already authorized. Outgress only reads
	// the registry for the bot's mod status (which sets the chat rate capacity).
	registryStarted := time.Now()
	ch, found, err := w.registry.Get(ctx, payload.BroadcasterID)
	recordStageDuration(ctx, "outgress.registry_ms", registryStarted)
	if err != nil {
		return err
	}

	if err := w.takeChat(ctx, payload.BroadcasterID, w.modStatus(ctx, payload, ch, found)); err != nil {
		return err
	}

	// Helix Send Chat Message requires sender_id (the bot) in the body. Producers
	// only carry the target broadcaster_id + message; the bot identity is owned
	// here, so inject it. An explicit message sender_id wins; otherwise the
	// configured bot id.
	sender, ok := w.botIdentity("chat message", payload)
	if !ok {
		return nil
	}
	payload.Payload = withSenderID(payload.Payload, sender)

	return w.execute(ctx, payload)
}
