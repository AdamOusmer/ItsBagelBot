// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

func retryPersonalityFeed(ctx context.Context, store engine.PersonalityStore, c *module.Context) (engine.FeedCounts, error) {
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	eventID := feedEventID(c)
	name := c.Env.BroadcasterName()
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return engine.FeedCounts{}, err
		}
		counts, err := store.Feed(ctx, c.BroadcasterID, name, eventID)
		if !feedRetryAllowed(eventID, attempt, err) {
			return counts, err
		}
		if waitErr := waitPersonalityFeed(ctx, attempt); waitErr != nil {
			return engine.FeedCounts{}, errors.Join(err, waitErr)
		}
	}
}

func feedRetryAllowed(eventID string, attempt int, err error) bool {
	if eventID == "" || attempt >= 2 {
		return false
	}
	var refusal bus.RPCReplyError
	if errors.As(err, &refusal) {
		return refusal.Code == rpc.CodeUnavailable
	}
	if errors.Is(err, nats.ErrNoResponders) || errors.Is(err, nats.ErrTimeout) {
		return true
	}
	var timeout net.Error
	return errors.As(err, &timeout) && timeout.Timeout()
}

func waitPersonalityFeed(ctx context.Context, attempt int) error {
	backoff := 10 * time.Millisecond << attempt
	timer := time.NewTimer(backoff + rand.N(backoff))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
