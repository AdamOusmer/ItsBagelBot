package main

import (
	"strings"
	"testing"

	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

// TestConsumeModePlumbsFromEnvAndFlag is the A/B knob's wiring. run.sh sets the
// pod env and the flag from one variable, so both routes have to land on the
// same field, and the flag has to win when they are given together.
func TestConsumeModePlumbsFromEnvAndFlag(t *testing.T) {
	opts, err := parseOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	// The default must stay flow: pull is the experiment, not the shipped shape.
	if opts.consumer.Mode != consumeModeFlow {
		t.Fatalf("default mode = %q, want %q", opts.consumer.Mode, consumeModeFlow)
	}

	t.Setenv("CONSUME_MODE", consumeModePull)
	opts, err = parseOptions([]string{"-role", roleConsume})
	if err != nil {
		t.Fatal(err)
	}
	if opts.consumer.Mode != consumeModePull {
		t.Fatalf("env mode = %q, want %q", opts.consumer.Mode, consumeModePull)
	}

	opts, err = parseOptions([]string{"-role", roleConsume, "-consume-mode", consumeModeFlow})
	if err != nil {
		t.Fatal(err)
	}
	if opts.consumer.Mode != consumeModeFlow {
		t.Fatalf("flag mode = %q, want the flag to win over the env", opts.consumer.Mode)
	}
}

// TestUnknownConsumeModeIsRefused guards the reported number's honesty: a mode
// that silently fell back to flow would be published as a pull measurement.
func TestUnknownConsumeModeIsRefused(t *testing.T) {
	_, err := parseOptions([]string{"-role", roleConsume, "-consume-mode", "puull"})
	if err == nil || !strings.Contains(err.Error(), "unknown -consume-mode") {
		t.Fatalf("error = %v, want a refusal", err)
	}
	if !validConsumeMode(consumeModeFlow) || !validConsumeMode(consumeModePull) {
		t.Fatal("a supported mode was refused")
	}
	if validConsumeMode("") {
		t.Fatal("an empty mode was accepted")
	}
}

// TestLagPollerTracksTheModeUnderTest keeps the instrumentation apples-to-apples.
// The lag curve is read off the consumer the poller finds, and the ack policy is
// the one field the two modes' consumers can never share — so a poller that
// looked for the wrong one would report a flat zero-lag curve for a lane that
// was falling behind.
func TestLagPollerTracksTheModeUnderTest(t *testing.T) {
	flow := &consumeState{cfg: consumerConfig{Mode: consumeModeFlow}}
	pull := &consumeState{cfg: consumerConfig{Mode: consumeModePull}}

	if flow.laneAckPolicy() != jsapi.AckFlowControlPolicy {
		t.Fatalf("flow poller looks for %v", flow.laneAckPolicy())
	}
	if pull.laneAckPolicy() != jsapi.AckAllPolicy {
		t.Fatalf("pull poller looks for %v", pull.laneAckPolicy())
	}
}

// TestConsumerSummaryRecordsTheMode keeps a captured run self-describing. A
// delivered rate means opposite things in the two modes (every pod receiving the
// whole lane versus the lane divided across pods), so an unlabelled summary is
// not comparable to anything.
func TestConsumerSummaryRecordsTheMode(t *testing.T) {
	state, closeStore, err := newConsumeState(consumerConfig{
		Mode: consumeModePull, Subject: stressHotSubject, Group: "nats-stress",
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()

	if got := state.summary().ConsumeMode; got != consumeModePull {
		t.Fatalf("summary consume_mode = %q, want %q", got, consumeModePull)
	}
}
