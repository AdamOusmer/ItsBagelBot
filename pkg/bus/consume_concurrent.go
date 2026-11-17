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

type ConcurrentConsumer struct{ done <-chan struct{} }

func ConsumeConcurrent(ctx context.Context, app *newrelic.Application, sub Subscriber, subject string, workers int, handle func(*Message) error, log *zap.Logger) (*ConcurrentConsumer, error) {
	if workers <= 0 {
		return nil, fmt.Errorf("consume %s: workers must be positive", subject)
	}
	messages, err := sub.Subscribe(ctx, subject)
	if err != nil {
		return nil, err
	}
	lane := newConsumeLane(app, subject, handle, log)
	return startConcurrentConsumer(messages, lane, workers), nil
}

func startConcurrentConsumer(messages <-chan *Message, lane consumeLane, workers int) *ConcurrentConsumer {
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
	return &ConcurrentConsumer{done: done}
}

type DemandConsumerConfig struct {
	App        *newrelic.Application
	Subscriber Subscriber
	Subject    string
	Handle     func(*Message) error
	Log        *zap.Logger
}

func ConsumeDemand(ctx context.Context, config DemandConsumerConfig) (*ConcurrentConsumer, error) {
	messages, err := config.Subscriber.Subscribe(ctx, config.Subject)
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go consumeDemand(messages, newConsumeLane(config.App, config.Subject, config.Handle, config.Log), done)
	return &ConcurrentConsumer{done: done}, nil
}

func consumeDemand(messages <-chan *Message, lane consumeLane, done chan<- struct{}) {
	var wg sync.WaitGroup
	for msg := range messages {
		wg.Add(1)
		go func() { defer wg.Done(); lane.process(msg) }()
	}
	wg.Wait()
	close(done)
}

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
