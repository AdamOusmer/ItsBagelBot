// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type testExpectedNack struct{}

func (testExpectedNack) Error() string      { return "expected" }
func (testExpectedNack) ExpectedNack() bool { return true }

type testRetryAfter struct{ delay time.Duration }

func (testRetryAfter) Error() string               { return "lease held" }
func (e testRetryAfter) RetryAfter() time.Duration { return e.delay }

type feedSubscriber struct{ messages chan *Message }

func (s *feedSubscriber) Subscribe(context.Context, string) (<-chan *Message, error) {
	return s.messages, nil
}

func (s *feedSubscriber) Close() error { return nil }

func testLane(app *newrelic.Application, rate uint64, log *zap.Logger, handle func(*Message) error) consumeLane {
	return consumeLane{
		app:     app,
		txnName: "consume bagel.rpc.commands",
		subject: "bagel.rpc.commands.run",
		handle:  handle,
		log:     log,
		stats:   &laneStats{destination: "bagel.rpc.commands", sampleRate: rate},
	}
}

func TestIsExpectedNack(t *testing.T) {
	assert.True(t, isExpectedNack(fmt.Errorf("wrapped: %w", testExpectedNack{})), "a wrapped expected nack is recognized")
	assert.False(t, isExpectedNack(errors.New("failure")), "an ordinary error is not expected")
}

func TestConsumeLaneCarriesTheRequestedRetryDelayToTheNack(t *testing.T) {
	lane := testLane(newLocalApplication(t), 1000, zap.NewNop(), func(msg *Message) error {
		if msg.UUID == "leased" {
			return fmt.Errorf("wrapped: %w", testRetryAfter{45 * time.Second})
		}
		return errors.New("boom")
	})

	for id, delay := range map[string]time.Duration{"leased": 45 * time.Second, "failed": 0} {
		msg := NewMessage(id, nil)
		var got time.Duration
		nacked := false
		msg.setResolveHandler(func(acked bool) { nacked, got = !acked, msg.requestedRetryDelay() })

		lane.process(msg)

		assert.True(t, nacked, "%s must be nacked", id)
		assert.Equal(t, delay, got, "%s retry delay", id)
	}
}

type outcomeCase struct {
	name          string
	handler       func(*Message) error
	wantAcked     bool
	wantLog       string
	failureLogged bool
}

func (c outcomeCase) wantLines() int {
	if c.wantLog == "" {
		return 0
	}
	return 1
}

func TestConsumeResolvesEveryDeliveryByTheHandlerOutcome(t *testing.T) {
	for _, tc := range []outcomeCase{
		{"a handler success acks", func(*Message) error { return nil }, true, "", false},
		{"a handler error nacks and is logged as a failure", func(*Message) error { return errors.New("handler exploded") }, false, "event handling failed, nacking", true},
		{"expected backpressure nacks without a failure log", func(*Message) error { return fmt.Errorf("wrapped: %w", testExpectedNack{}) }, false, "event deferred by expected backpressure", false},
		{"a handler panic nacks instead of crashing the lane", func(*Message) error { panic("boom") }, false, "consume handler panic recovered", true},
	} {
		for _, app := range []struct {
			name string
			app  *newrelic.Application
		}{{"without an application", nil}, {"with an application", newLocalApplication(t)}} {
			t.Run(tc.name+" "+app.name, func(t *testing.T) {
				core, logs := observer.New(zap.DebugLevel)
				feed := &feedSubscriber{messages: make(chan *Message, 2)}
				require.NoError(t, Consume(context.Background(), app.app, feed, "bagel.rpc.commands.run", tc.handler, zap.New(core)))
				msg := NewMessage("delivery", nil)

				feed.messages <- msg
				resolved := func() bool { return signalClosed(msg.Acked()) || signalClosed(msg.Nacked()) }

				waitFor(t, resolved, "the delivery was never resolved")
				assert.Equal(t, tc.wantAcked, signalClosed(msg.Acked()))
				assert.Equal(t, tc.wantLines(), logs.FilterMessage(tc.wantLog).Len(), "log lines for %q", tc.wantLog)
				assert.Equal(t, tc.failureLogged, logs.FilterMessage("event handling failed, nacking").Len() > 0, "failure log")
				close(feed.messages)
			})
		}
	}
}
