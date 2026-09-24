// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/outgress"
)

func (w *Worker) processChat(ctx context.Context, payload *outgress.Message) error {
	registryStarted := time.Now()
	ch, found := w.chatChannel(ctx, payload.BroadcasterID)
	recordStageDuration(ctx, "outgress.registry_ms", registryStarted)

	if err := w.takeChat(ctx, payload.BroadcasterID, w.modStatus(ctx, payload, ch, found)); err != nil {
		return err
	}

	sender, ok := w.botIdentity("chat message", payload)
	if !ok {
		return nil
	}
	payload.Payload = withSenderID(payload.Payload, sender)

	return w.execute(ctx, payload)
}
