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

// testLane builds a lane whose sampling cursor and counters belong to the test
// alone, so nothing here mutates the package-level telemetry registry or starts
// its flusher.
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

// handlerSawTransaction records, per delivery, whether the handler was given a
// transaction. That is the observable difference between the sampled and the
// unsampled path: the trace join, the segment and the datastore spans all hang
// off the transaction in the message context.
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

	// The first delivery is always sampled so a quiet lane traces immediately.
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
	lane := testLane(app, consumeSampleRate(), zap.NewNop(), handlerSawTransaction(&seen, nil))

	messages := make([]*Message, 4)
	for i := range messages {
		messages[i] = NewMessage("id", nil)
		lane.process(messages[i])
	}

	// The shipped default is 1: every message keeps the full transaction it had
	// before sampling existed, so the change lands dark.
	if consumeSampleRate() != 1 {
		t.Fatalf("default sample rate = %d, want 1", consumeSampleRate())
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

	// A rate this high means only the warm-up delivery is sampled; the failure
	// lands squarely in the unsampled majority.
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
	// A trace id on the line is the proof that a transaction was built
	// retroactively: monitor.TraceLogger adds those fields only when it holds one.
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
	// Expected backpressure must stay free: an overload cannot be allowed to buy
	// back a transaction per delivery attempt.
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

func TestConsumeLaneCountsEveryOutcomeWithoutAnApplication(t *testing.T) {
	// A nil application is the local-development and unit-test shape: both the
	// sampled and unsampled paths must run clean through it.
	lane := testLane(nil, 2, zap.NewNop(), func(msg *Message) error {
		switch msg.UUID {
		case "deferred":
			return testExpectedNack{}
		case "failed":
			return errors.New("boom")
		default:
			return nil
		}
	})

	for _, id := range []string{"ok", "deferred", "failed", "ok"} {
		msg := NewMessage(id, nil)
		msg.receivedAt = time.Now().Add(-3 * time.Millisecond)
		lane.process(msg)
	}

	if ok, def, failed := lane.stats.ok.Load(), lane.stats.deferred.Load(), lane.stats.failed.Load(); ok != 2 || def != 1 || failed != 1 {
		t.Fatalf("counters = ok:%d deferred:%d failed:%d, want 2/1/1", ok, def, failed)
	}
	if peak := lane.stats.queueMaxMicros.Load(); peak < 3000 {
		t.Fatalf("queue high-water mark = %dµs, want at least 3000", peak)
	}
	if sum := lane.stats.queueMicros.Load(); sum < 12000 {
		t.Fatalf("queue wait sum = %dµs, want at least 12000 across four deliveries", sum)
	}
}

func TestNewConsumeLaneSharesOneCounterSetPerLane(t *testing.T) {
	// A nil application registers counters without starting the flusher, so this
	// exercises the real wiring without leaving a goroutine behind.
	first := newConsumeLane(nil, "twitch.outgress.premium", nil, zap.NewNop())
	second := newConsumeLane(nil, "twitch.outgress.premium", nil, zap.NewNop())

	if first.stats == nil {
		t.Fatal("newConsumeLane left the lane without counters")
	}
	// The weighted consumer builds one consumeLane per consumer unit; sharing the
	// cursor is what keeps the sample rate per lane instead of per unit.
	if first.stats != second.stats {
		t.Fatal("two units on one lane got separate sampling cursors")
	}
	if first.stats.sampleRate != consumeSampleRate() {
		t.Fatalf("lane sample rate = %d, want the process rate %d", first.stats.sampleRate, consumeSampleRate())
	}
}
