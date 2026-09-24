// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch

import (
	"context"
	"errors"
	"fmt"
	"testing"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap/zapcore"
)

type busError struct {
	name  string
	from  string
	err   error
	retry bool
	why   string
}

func startErr(cause error) error {
	return fmt.Errorf("bus: async publish %s: %w", ddiscord.LaneDefault, cause)
}

func busPublishErrors() []busError {
	return []busError{{
		name:  "missing message id",
		from:  "publisherPool.PublishOwnedWithID",
		err:   errors.New("bus: confirmed publish requires a message ID"),
		retry: false,
		why:   "a caller bug; the same call fails identically every time",
	}, {
		name:  "publisher closed at admission",
		from:  "batchPublisher.admit",
		err:   errors.New("bus: publisher is closed"),
		retry: false,
		why:   "nothing was stored, but nothing will be: the pool is shut down",
	}, {
		name:  "publisher closed while the batch waited",
		from:  "publishBatchWorker.collectTimed",
		err:   errors.New("bus: publisher closed"),
		retry: false,
		why:   "same shutdown, one stage later",
	}, {
		name:  "caller context cancelled",
		from:  "admitLocked / awaitPublishConfirmation",
		err:   context.Canceled,
		retry: false,
		why:   "ambiguous once the wait is abandoned: the cohort may still land",
	}, {
		name: "cohort stored a prefix",
		from: "joinAsyncCohort",
		err: fmt.Errorf("bus: async cohort sent and stored %d/%d messages; the remainder never reached the wire: %w",
			5, 8, startErr(nats.ErrConnectionClosed)),
		retry: false,
		why:   "THE regression: errors.Is sees ErrConnectionClosed through a cohort that stored five messages",
	}, {
		name: "cohort sent but unresolved",
		from: "joinAsyncCohort",
		err: fmt.Errorf("bus: async cohort sent %d/%d messages and their acknowledgements did not all resolve: %w",
			5, 8, errors.Join(startErr(nats.ErrConnectionClosed), nats.ErrTimeout)),
		retry: false,
		why:   "same prefix, and the acks are unresolved on top of it",
	}, {
		name:  "cohort PubAck timeout",
		from:  "publishBatchWorker.awaitAsync",
		err:   errors.New("bus: asynchronous publish cohort PubAck timeout"),
		retry: false,
		why:   "JetStream may have stored the message and lost only the ack",
	}, {
		name:  "no stream listening",
		from:  "publishBatchWorker.awaitAsync (future.Err)",
		err:   nats.ErrNoResponders,
		retry: true,
		why:   "one worker's cohort is one stream, so a missing stream stored none of it",
	}}
}

func TestConfirmedPublishErrorsAreClassifiedHonestly(t *testing.T) {
	for _, tc := range busPublishErrors() {
		t.Run(tc.name, func(t *testing.T) {
			if got := preAdmission(tc.err); got != tc.retry {
				t.Fatalf("preAdmission(%v) = %t, want %t\n  shape from pkg/bus %s\n  %s",
					tc.err, got, tc.retry, tc.from, tc.why)
			}
		})
	}
}

func TestConfirmedPublishRetryLoopMatchesTheClassification(t *testing.T) {
	for _, tc := range busPublishErrors() {
		t.Run(tc.name, func(t *testing.T) {
			pub := &flakyPublish{failFor: 99, failWith: tc.err}
			d, logs := observedDispatcher(pub.publish)

			d.publishAll(context.Background(), []ddiscord.Command{{Type: "post"}})

			want := 1
			if tc.retry {
				want = publishAttempts
			}
			if pub.attempts != want {
				t.Fatalf("attempts = %d, want %d (%s)", pub.attempts, want, tc.why)
			}
			errs := logs.FilterLevelExact(zapcore.ErrorLevel).All()
			if len(errs) != 1 {
				t.Fatalf("error logs = %d, want exactly one naming the lost command", len(errs))
			}
			if got := errs[0].ContextMap()["retried"]; got != tc.retry {
				t.Fatalf("logged retried = %v, want %t", got, tc.retry)
			}
		})
	}
}
