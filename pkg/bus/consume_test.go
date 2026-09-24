// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type testExpectedNack struct{}

func (testExpectedNack) Error() string      { return "expected" }
func (testExpectedNack) ExpectedNack() bool { return true }

func TestIsExpectedNack(t *testing.T) {
	if !isExpectedNack(fmt.Errorf("wrapped: %w", testExpectedNack{})) {
		t.Fatal("wrapped expected nack was not recognized")
	}
	if isExpectedNack(errors.New("failure")) {
		t.Fatal("ordinary error was classified as expected")
	}
}

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

func handlerSawTransaction(seen *[]bool, err error) func(*Message) error {
	return func(msg *Message) error {
		*seen = append(*seen, newrelic.FromContext(msg.Context()) != nil)
		return err
	}
}

func TestConsumeLaneSamplesOneMessageInN(t *testing.T) {
	app := newLocalApplication(t, nil)

	var seen []bool
	lane := testLane(app, 3, zap.NewNop(), handlerSawTransaction(&seen, nil))

	for i := 0; i < 6; i++ {
		lane.process(NewMessage("id", nil))
	}

	want := []bool{true, false, false, true, false, false}
	for i, expected := range want {
		if seen[i] != expected {
			t.Fatalf("delivery %d transaction = %v, want %v (all=%v)", i+1, seen[i], expected, seen)
		}
	}
	if got := lane.stats.ok.Load(); got != 6 {
		t.Fatalf("counted %d ok deliveries, want 6 — unsampled messages must still be measured", got)
	}
}

func TestConsumeLaneAtRateOneInstrumentsEveryMessage(t *testing.T) {
	app := newLocalApplication(t, nil)

	var seen []bool
	lane := testLane(app, 1, zap.NewNop(), handlerSawTransaction(&seen, nil))

	messages := make([]*Message, 4)
	for i := range messages {
		messages[i] = NewMessage("id", nil)
		lane.process(messages[i])
	}

	if consumeNRSampleRate != 100 {
		t.Fatalf("shipped sample rate = %d, want 100", consumeNRSampleRate)
	}
	for i, sampled := range seen {
		if !sampled {
			t.Fatalf("delivery %d had no transaction at sample rate 1 (all=%v)", i+1, seen)
		}
	}
	for i, msg := range messages {
		select {
		case <-msg.Acked():
		default:
			t.Fatalf("delivery %d was not acked", i+1)
		}
	}
}

func TestConsumeLaneInstrumentsUnsampledFailures(t *testing.T) {
	app := newLocalApplication(t, nil)
	core, logs := observer.New(zap.DebugLevel)

	lane := testLane(app, 1000, zap.New(core), func(msg *Message) error {
		if msg.UUID == "boom" {
			return errors.New("handler exploded")
		}
		return nil
	})

	lane.process(NewMessage("warmup", nil))

	failed := NewMessage("boom", nil)
	lane.process(failed)

	warnings := logs.FilterMessage("event handling failed, nacking").All()
	if len(warnings) != 1 {
		t.Fatalf("got %d warn lines, want exactly 1", len(warnings))
	}
	if _, ok := warnings[0].ContextMap()["trace.id"]; !ok {
		t.Fatalf("unsampled failure logged without a transaction: %v", warnings[0].ContextMap())
	}
	select {
	case <-failed.Nacked():
	default:
		t.Fatal("failed delivery was not nacked")
	}
	if got := lane.stats.failed.Load(); got != 1 {
		t.Fatalf("counted %d failures, want 1", got)
	}
}

func TestConsumeLaneUnsampledBackpressureCreatesNoTransaction(t *testing.T) {
	app := newLocalApplication(t, nil)
	core, logs := observer.New(zap.DebugLevel)

	lane := testLane(app, 1000, zap.New(core), func(msg *Message) error {
		if msg.UUID == "paused" {
			return fmt.Errorf("wrapped: %w", testExpectedNack{})
		}
		return nil
	})

	lane.process(NewMessage("warmup", nil))

	deferred := NewMessage("paused", nil)
	lane.process(deferred)

	debugs := logs.FilterMessage("event deferred by expected backpressure").All()
	if len(debugs) != 1 {
		t.Fatalf("got %d debug lines, want exactly 1", len(debugs))
	}
	if _, ok := debugs[0].ContextMap()["trace.id"]; ok {
		t.Fatalf("quiet unsampled nack created a transaction: %v", debugs[0].ContextMap())
	}
	if logs.FilterMessage("event handling failed, nacking").Len() != 0 {
		t.Fatal("expected backpressure was logged as a failure")
	}
	select {
	case <-deferred.Nacked():
	default:
		t.Fatal("deferred delivery was not nacked")
	}
	if got := lane.stats.deferred.Load(); got != 1 {
		t.Fatalf("counted %d deferrals, want 1", got)
	}
}

func laneOutcomeByID(msg *Message) error {
	switch msg.UUID {
	case "deferred":
		return testExpectedNack{}
	case "failed":
		return errors.New("boom")
	default:
		return nil
	}
}

func processQueued(lane *consumeLane, queued time.Duration, ids ...string) {
	for _, id := range ids {
		msg := NewMessage(id, nil)
		msg.receivedAt = time.Now().Add(-queued)
		lane.process(msg)
	}
}

type laneCounts struct {
	ok, deferred, failed uint64
}

func requireLaneCounters(t *testing.T, stats *laneStats, want laneCounts) {
	t.Helper()
	got := laneCounts{ok: stats.ok.Load(), deferred: stats.deferred.Load(), failed: stats.failed.Load()}
	if got != want {
		t.Fatalf("counters = ok:%d deferred:%d failed:%d, want %d/%d/%d",
			got.ok, got.deferred, got.failed, want.ok, want.deferred, want.failed)
	}
}

func requireQueueWaitRecorded(t *testing.T, stats *laneStats, wantPeak, wantSum uint64) {
	t.Helper()
	if peak := stats.queueMaxMicros.Load(); peak < wantPeak {
		t.Fatalf("queue high-water mark = %dµs, want at least %d", peak, wantPeak)
	}
	if sum := stats.queueMicros.Load(); sum < wantSum {
		t.Fatalf("queue wait sum = %dµs, want at least %d across four deliveries", sum, wantSum)
	}
}

func TestConsumeLaneCountsEveryOutcomeWithoutAnApplication(t *testing.T) {
	lane := testLane(nil, 2, zap.NewNop(), laneOutcomeByID)

	processQueued(&lane, 3*time.Millisecond, "ok", "deferred", "failed", "ok")

	requireLaneCounters(t, lane.stats, laneCounts{ok: 2, deferred: 1, failed: 1})
	requireQueueWaitRecorded(t, lane.stats, 3000, 12000)
}

func TestNewConsumeLaneSharesOneCounterSetPerLane(t *testing.T) {
	first := newConsumeLane(nil, "twitch.outgress.premium", nil, zap.NewNop())
	second := newConsumeLane(nil, "twitch.outgress.premium", nil, zap.NewNop())

	if first.stats == nil {
		t.Fatal("newConsumeLane left the lane without counters")
	}
	if first.stats != second.stats {
		t.Fatal("two units on one lane got separate sampling cursors")
	}
	if first.stats.sampleRate != consumeNRSampleRate {
		t.Fatalf("lane sample rate = %d, want the shipped rate %d", first.stats.sampleRate, consumeNRSampleRate)
	}
}
