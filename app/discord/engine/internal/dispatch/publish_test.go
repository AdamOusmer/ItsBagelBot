// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const publishAttempts = 3

type publishCase struct {
	name       string
	from       string
	err        error
	succeedsOn int
	cancelled  bool
	attempts   int
	retried    bool
	why        string
}

func startErr(cause error) error {
	return fmt.Errorf("bus: async publish %s: %w", ddiscord.LaneDefault, cause)
}

func busPublishErrors() []publishCase {
	return []publishCase{{
		name:     "missing message id",
		from:     "publisherPool.PublishOwnedWithID",
		err:      errors.New("bus: confirmed publish requires a message ID"),
		attempts: 1,
		why:      "a caller bug; the same call fails identically every time",
	}, {
		name:     "publisher closed at admission",
		from:     "batchPublisher.admit",
		err:      errors.New("bus: publisher is closed"),
		attempts: 1,
		why:      "nothing was stored, but nothing will be: the pool is shut down",
	}, {
		name:     "publisher closed while the batch waited",
		from:     "publishBatchWorker.collectTimed",
		err:      errors.New("bus: publisher closed"),
		attempts: 1,
		why:      "same shutdown, one stage later",
	}, {
		name:     "caller context cancelled",
		from:     "admitLocked / awaitPublishConfirmation",
		err:      context.Canceled,
		attempts: 1,
		why:      "ambiguous once the wait is abandoned: the cohort may still land",
	}, {
		name: "cohort stored a prefix",
		from: "joinAsyncCohort",
		err: fmt.Errorf("bus: async cohort sent and stored %d/%d messages; the remainder never reached the wire: %w",
			5, 8, startErr(nats.ErrConnectionClosed)),
		attempts: 1,
		why:      "THE regression: errors.Is sees ErrConnectionClosed through a cohort that stored five messages",
	}, {
		name: "cohort sent but unresolved",
		from: "joinAsyncCohort",
		err: fmt.Errorf("bus: async cohort sent %d/%d messages and their acknowledgements did not all resolve: %w",
			5, 8, errors.Join(startErr(nats.ErrConnectionClosed), nats.ErrTimeout)),
		attempts: 1,
		why:      "same prefix, and the acks are unresolved on top of it",
	}, {
		name:     "cohort PubAck timeout",
		from:     "publishBatchWorker.awaitAsync",
		err:      errors.New("bus: asynchronous publish cohort PubAck timeout"),
		attempts: 1,
		why:      "JetStream may have stored the message and lost only the ack",
	}, {
		name:     "no stream listening",
		from:     "publishBatchWorker.awaitAsync (future.Err)",
		err:      nats.ErrNoResponders,
		attempts: publishAttempts,
		retried:  true,
		why:      "one worker's cohort is one stream, so a missing stream stored none of it",
	}}
}

func retryCases() []publishCase {
	return []publishCase{{
		name: "TestPublishRetriesBeforeGivingUp", err: nats.ErrNoResponders,
		attempts: publishAttempts, retried: true,
		why: "a pre-admission failure is retried, then reported once",
	}, {
		name: "TestPublishStopsRetryingOnceItSucceeds", err: nats.ErrNoResponders, succeedsOn: 2,
		attempts: 2,
		why:      "one failure, one success: the command was published",
	}, {
		name: "TestPublishGivesUpImmediatelyOnShutdown", err: nats.ErrNoResponders,
		cancelled: true, attempts: 1, retried: true,
		why: "a cancelled context ends the retry wait",
	}, {
		name: "TestPublishDoesNotRetryAnAmbiguousTimeout", err: nats.ErrTimeout,
		attempts: 1,
		why:      "a timeout may already have been stored",
	}, {
		name:       "TestPublishRetriesWrappedPreAdmissionErrors",
		err:        fmt.Errorf("publish %q: %w", "bagel.discord.cmd", nats.ErrNoResponders),
		succeedsOn: 2, attempts: 2,
		why: "wrapping must not hide the pre-admission verdict",
	}}
}

type flakyPublish struct {
	attempts   int
	succeedsOn int
	err        error
}

func (f *flakyPublish) publish(context.Context, ddiscord.Command) error {
	f.attempts++
	if f.attempts == f.succeedsOn {
		return nil
	}
	return f.err
}

func publishOnce(t *testing.T, tc publishCase) (*flakyPublish, *observer.ObservedLogs) {
	t.Helper()
	h := newHarness(t, ddiscord.Config{GuildID: testGuild, WelcomeChannelID: testWelcomeCh})
	pub := &flakyPublish{succeedsOn: tc.succeedsOn, err: tc.err}
	core, logs := observer.New(zapcore.DebugLevel)
	h.d.Publish, h.d.Log = pub.publish, zap.New(core)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if tc.cancelled {
		cancel()
	}

	h.sendCtx(ctx, testGuild, "GUILD_MEMBER_ADD", memberPayload(testGuild))
	return pub, logs
}

func TestConfirmedPublishErrorsAreClassifiedHonestly(t *testing.T) {
	for _, tc := range busPublishErrors() {
		t.Run(tc.name, func(t *testing.T) {
			_, logs := publishOnce(t, tc)

			requireLostLog(t, logs, tc)
		})
	}
}

func TestConfirmedPublishRetryLoopMatchesTheClassification(t *testing.T) {
	for _, tc := range append(retryCases(), busPublishErrors()...) {
		t.Run(tc.name, func(t *testing.T) {
			pub, logs := publishOnce(t, tc)

			require.Equal(t, tc.attempts, pub.attempts, tc.why)
			requireLostLog(t, logs, tc)
		})
	}
}

func requireLostLog(t *testing.T, logs *observer.ObservedLogs, tc publishCase) {
	t.Helper()
	lost := logs.FilterLevelExact(zapcore.ErrorLevel).All()
	if tc.succeedsOn != 0 {
		require.Empty(t, lost, "the command was published")
		return
	}
	require.Len(t, lost, 1, "exactly one error naming the lost command")
	fields := lost[0].ContextMap()
	require.Equal(t, ddiscord.TypePostEmbed, fields["type"])
	require.Equal(t, ddiscord.Lane(ddiscord.TypePostEmbed), fields["subject"])
	require.Equal(t, tc.retried, fields["retried"], tc.why)
}
