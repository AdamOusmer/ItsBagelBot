// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"go.uber.org/zap"
)

// The payload is immutable: a failed acknowledgement may still mean the broker
// stored it. Retrying the same batch ID lets the database consumer deduplicate it.
type counterPublication struct {
	id           string
	subject      string
	payload      any
	entries      int
	firstAttempt time.Time
}

// The loyalty service's receipt retention must exceed this plus the BAGEL_DATA MaxAge.
const counterPublicationGiveUp = time.Hour

func abandonStaleCounterPublications(log *zap.Logger, pending []counterPublication, now time.Time) []counterPublication {
	return slices.DeleteFunc(pending, func(publication counterPublication) bool {
		age := now.Sub(publication.firstAttempt)
		if publication.firstAttempt.IsZero() || age < counterPublicationGiveUp {
			return false
		}
		log.Error("counter batch abandoned after retry horizon",
			zap.String("batch_id", publication.id),
			zap.String("subject", publication.subject),
			zap.Duration("age", age),
			zap.Int("entries", publication.entries),
		)
		return true
	})
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
