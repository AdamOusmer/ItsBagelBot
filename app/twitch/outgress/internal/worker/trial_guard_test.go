package worker

import (
	"ItsBagelBot/internal/domain/outgress"
	"context"
	"testing"
)

func TestTrialOriginStopsDirectAndBatchChild(t *testing.T) {
	w := &Worker{}
	msg := &outgress.Message{Type: outgress.TypeChat, BroadcasterID: "42", Origin: "trial", TrialGeneration: 3}
	if err := w.processPayload(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if err := w.processBatchItem(context.Background(), *msg, "42"); err != nil {
		t.Fatal(err)
	}
}

// Untagged output never consults trial state, so a Valkey outage cannot stall
// ordinary channels.
func TestUntaggedOutputSkipsTrialGuard(t *testing.T) {
	w := &Worker{}
	msg := &outgress.Message{Type: outgress.TypeChat, BroadcasterID: "42"}
	if w.rejectTrialOutput(context.Background(), msg) {
		t.Fatal("untagged output was refused")
	}
	msg.Origin = "trial"
	if !w.rejectTrialOutput(context.Background(), msg) {
		t.Fatal("trial output was not refused")
	}
}
