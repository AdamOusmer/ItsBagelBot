// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/pkg/bus"
)

const publishTimeout = 3 * time.Second

var messageBudget = 10 * time.Second

func messageContext(msg *bus.Message) (context.Context, context.CancelFunc) {
	return context.WithTimeout(msg.Context(), messageBudget)
}

// Detached so a reply already decided still publishes after the message budget runs out.
func publishContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
}

func publishWithin(ctx context.Context, pub bus.Publisher, out bus.Publication) error {
	ctx, cancel := publishContext(ctx)
	defer cancel()
	if out.ID == "" {
		return bus.PublishRaw(ctx, pub, out.Subject, out.Payload)
	}
	return bus.PublishConfirmed(ctx, pub, out)
}

func flushWithin(ctx context.Context, pub bus.Publisher) error {
	ctx, cancel := publishContext(ctx)
	defer cancel()
	return pub.Flush(ctx)
}
