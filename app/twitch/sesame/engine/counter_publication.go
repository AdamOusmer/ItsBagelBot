// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"go.uber.org/zap"
)

// The payload is immutable: a failed acknowledgement may still mean the broker
// stored it. Retrying the same batch ID lets the database consumer deduplicate it.
type counterPublication struct {
	id      string
	subject string
	payload any
}

func retryCounterPublications(ctx context.Context, pub bus.Publisher, log *zap.Logger, pending []counterPublication) []counterPublication {
	for len(pending) > 0 {
		publication := pending[0]
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		body, err := codec.FastMarshal(publication.payload)
		if err == nil {
			err = bus.PublishConfirmed(attempt, pub, bus.Publication{Subject: publication.subject, ID: publication.id, Payload: body})
		}
		cancel()
		if err != nil {
			log.Error("counter batch retained for retry", zap.String("subject", publication.subject), zap.Error(err))
			return pending
		}
		pending[0] = counterPublication{}
		pending = pending[1:]
	}
	return nil
}
