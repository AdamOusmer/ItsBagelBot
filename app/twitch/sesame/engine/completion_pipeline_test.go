// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessRequiredFailureResumesCompletedModules(t *testing.T) {
	for _, mode := range []string{"handler", "command", "async", "unnamed"} {
		t.Run(mode, func(t *testing.T) { testRequiredFailure(t, mode) })
	}
}

func testRequiredFailure(t *testing.T, mode string) {
	var healthy, attempts atomic.Int64
	fail := atomic.Bool{}
	fail.Store(true)
	first := module.NewModule("healthy", module.KindCore)
	first.On(chatType, func(context.Context, *module.Context, module.Emit) error { healthy.Add(1); return nil })
	run := func(context.Context) error {
		attempts.Add(1)
		if fail.Load() {
			return errors.New("required mutation failed")
		}
		return nil
	}
	broken := failingCompletionModule(mode, run)
	healthyModule := first.Build()
	if mode == "unnamed" {
		healthyModule.Name, broken.Name = "", ""
	}
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Seq: NewSequencer(), Dedup: NewEventDedup(newRecordingStore(), "", time.Hour, nil)}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), healthyModule, broken), Config{})
	t.Cleanup(p.Close)
	msg := commandMsg(t, "required", "!broken")
	err := p.Process(msg)
	require.Equal(t, mode != "async", err != nil)
	awaitCompletion(t, msg, false)
	fail.Store(false)
	retry := commandMsg(t, "required", "!broken")
	require.NoError(t, p.Process(retry))
	awaitCompletion(t, retry, true)
	require.Equal(t, []int64{2, 2}, []int64{healthy.Load(), attempts.Load()})
}

func failingCompletionModule(mode string, run func(context.Context) error) module.Module {
	m := module.NewModule("broken", module.KindCore)
	if mode == "command" {
		m.Command("broken").Run(func(ctx context.Context, _ *module.Context, _ string, _ module.Emit) error { return run(ctx) })
		return m.Build()
	}
	m.On(chatType, func(ctx context.Context, _ *module.Context, _ module.Emit) error {
		if mode == "async" {
			go idempotency.Async(ctx, run)()
			return nil
		}
		return run(ctx)
	})
	return m.Build()
}

func TestProcessAnalyticsFailureCannotBeCoveredByLaterFlush(t *testing.T) {
	client := valkeytest.Spawn(t)
	rec := analytics.New(client, zap.NewNop(), nil, analytics.WithoutTicker())
	t.Cleanup(rec.Close)
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Analytics: rec, Dedup: NewEventDedup(idempotency.NewValkeyStore(client, "analytics-test:", nil), "analytics-test:", time.Hour, nil)}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), silentCore()), Config{})
	t.Cleanup(p.Close)
	key := catalog.CounterKey(time.Now())
	require.NoError(t, client.Do(t.Context(), client.B().Set().Key(key).Value("wrong type").Build()).Error())
	first := commandMsg(t, "analytics", "hello")
	require.NoError(t, p.Process(first))
	require.Error(t, rec.Flush(t.Context()))
	assertPendingCompletion(t, first)
	retry := commandMsg(t, "analytics", "hello")
	require.NoError(t, p.Process(retry))
	assertPendingCompletion(t, retry)
	require.NoError(t, client.Do(t.Context(), client.B().Del().Key(key).Build()).Error())
	require.NoError(t, rec.Flush(t.Context()))
	awaitCompletion(t, retry, true)
	awaitCompletion(t, first, true)
	count, err := client.Do(t.Context(), client.B().Hget().Key(key).Field("chat.messages").Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}

func TestProcessFinishFlushesOrNaksBeforeDeadline(t *testing.T) {
	for _, mode := range []string{"flush", "failed", "expired"} {
		t.Run(mode, func(t *testing.T) {
			b := newCompletionBroker(t)
			p, reporter := b.pipeline(t, valkeytest.Spawn(t))
			t.Cleanup(reporter.Close)
			t.Cleanup(p.Close)
			b.pub.fail.Store(mode == "failed")
			msg := commandMsg(t, "shutdown", "!hello")
			require.NoError(t, p.Process(msg))
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			if mode == "expired" {
				cancel()
			}
			p.Finish(ctx)
			if mode != "flush" {
				assertPendingCompletion(t, msg)
				b.pub.fail.Store(false)
				p.Finish(t.Context())
			}
			awaitCompletion(t, msg, true)
		})
	}
}

func TestProcessAsyncTransferWaitsForReporterHandoff(t *testing.T) {
	b := newCompletionBroker(t)
	r := &LoyaltyReporter{pub: b.pub, log: zap.NewNop(), earn: map[earnKey]*earnAgg{}, bumps: map[counterAgg]*bumpAgg{}}
	release, queued := make(chan struct{}), make(chan struct{})
	m := module.NewModule("deferred", module.KindCore)
	m.On(chatType, func(ctx context.Context, c *module.Context, _ module.Emit) error {
		id := c.BroadcasterID
		go idempotency.Async(ctx, func(ctx context.Context) error {
			<-release
			r.EarnContext(ctx, PointEarning{BroadcasterID: id, ViewerID: 999, Points: 3})
			close(queued)
			return nil
		})()
		return nil
	})
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: b.pub, Log: zap.NewNop(), Seq: NewSequencer()}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), m.Build()), Config{})
	t.Cleanup(p.Close)
	msg := commandMsg(t, "async-transfer", "hello")
	require.NoError(t, p.Process(msg))
	close(release)
	<-queued
	select {
	case <-msg.Acked():
		t.Fatal("async task completion bypassed its reporter")
	case <-msg.Nacked():
		t.Fatal("async task lost its handler context")
	default:
	}
	r.flush(t.Context())
	awaitCompletion(t, msg, true)
}

type completionFlushPublisher struct {
	fakePublisher
	fail bool
}

func (p *completionFlushPublisher) Flush(context.Context) error {
	if p.fail {
		return errors.New("outgress flush failed")
	}
	return nil
}

func TestProcessLegacyOutputClaimWaitsForFlush(t *testing.T) {
	pub := &completionFlushPublisher{fail: true}
	var calls int
	m := module.NewModule("legacy", module.KindCore)
	m.On(chatType, func(_ context.Context, _ *module.Context, emit module.Emit) error {
		calls++
		emit(&module.Output{Type: "chat", BroadcasterID: "123", Text: "hello"})
		return nil
	})
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: pub, Log: zap.NewNop(), Seq: NewSequencer(), Dedup: NewEventDedup(newRecordingStore(), "", time.Hour, nil)}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), m.Build()), Config{})
	t.Cleanup(p.Close)
	first := commandMsg(t, "legacy", "hello")
	require.Error(t, p.Process(first))
	awaitCompletion(t, first, false)
	pub.fail = false
	retry := commandMsg(t, "legacy", "hello")
	require.NoError(t, p.Process(retry))
	awaitCompletion(t, retry, true)
	require.Equal(t, 2, calls)
}

type completionBlockedPublisher struct {
	bus.Publisher
	started chan struct{}
	release chan struct{}
}

func (p *completionBlockedPublisher) PublishOwnedWithID(ctx context.Context, subject, id string, body []byte) error {
	if subject != data.SubjectCommandUsed {
		return p.Publisher.PublishOwnedWithID(ctx, subject, id, body)
	}
	select {
	case p.started <- struct{}{}:
	default:
	}
	select {
	case <-p.release:
		return p.Publisher.PublishOwnedWithID(ctx, subject, id, body)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestProcessShutdownDeadlineIncludesAnInflightFlush(t *testing.T) {
	b := newCompletionBroker(t)
	blocked := &completionBlockedPublisher{Publisher: b.pub.Publisher, started: make(chan struct{}, 1), release: make(chan struct{})}
	b.pub.Publisher = blocked
	p, reporter := b.pipeline(t, valkeytest.Spawn(t))
	t.Cleanup(reporter.Close)
	t.Cleanup(p.Close)
	defer close(blocked.release)
	msg := commandMsg(t, "blocked-shutdown", "!hello")
	require.NoError(t, p.Process(msg))
	select {
	case <-blocked.started:
	case <-time.After(2 * time.Second):
		t.Fatal("reporter never started")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	p.Finish(ctx)
	require.Less(t, time.Since(started), time.Second)
	assertPendingCompletion(t, msg)
}

func assertPendingCompletion(t *testing.T, msg *bus.Message) {
	t.Helper()
	select {
	case <-msg.Acked():
		t.Fatal("unconfirmed generation acknowledged")
	case <-msg.Nacked():
		t.Fatal("publishable generation nacked")
	default:
	}
}
