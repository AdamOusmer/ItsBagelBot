// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"
	"sync"

	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// ConcurrentConsumer runs a fixed number of delivery handlers. A handler may
// wait for a shared batch without preventing the next delivery from joining it.
// The subscription's own bounded channel supplies backpressure.
type ConcurrentConsumer struct{ done <-chan struct{} }

// ConsumeConcurrent preserves Consume's telemetry and acknowledgement contract:
// successful handlers ACK only after they return; failures request redelivery.
// Close the subscriber or cancel ctx before calling Drain.
func ConsumeConcurrent(ctx context.Context, app *newrelic.Application, sub Subscriber, subject string, workers int, handle func(*Message) error, log *zap.Logger) (*ConcurrentConsumer, error) {
	if workers < 1 {
		return nil, fmt.Errorf("consume %s: workers must be positive", subject)
	}
	messages, err := sub.Subscribe(ctx, subject)
	if err != nil {
		return nil, err
	}
	lane := newConsumeLane(app, subject, handle, log)
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for msg := range messages {
				lane.process(msg)
			}
		}()
	}
	go func() { wg.Wait(); close(done) }()
	return &ConcurrentConsumer{done: done}, nil
}

// Drain waits for every admitted handler to finish; cancellation bounds shutdown.
func (c *ConcurrentConsumer) Drain(ctx context.Context) error {
	if c == nil {
		return nil
	}
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
