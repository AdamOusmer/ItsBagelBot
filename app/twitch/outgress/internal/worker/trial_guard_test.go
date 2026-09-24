package worker

import (
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
	"context"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func observedWorker() (*Worker, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.InfoLevel)
	return &Worker{log: zap.New(core)}, logs
}

func TestTrialOriginStopsDirectAndBatchChild(t *testing.T) {
	w, logs := observedWorker()
	msg := &outgress.Message{Type: outgress.TypeChat, BroadcasterID: "42", Origin: "trial", TrialGeneration: 3, Payload: codec.RawMessage(`{"message":"hi"}`)}
	if err := w.processPayload(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if err := w.processBatchItem(context.Background(), *msg, "42"); err != nil {
		t.Fatal(err)
	}
	if n := logs.FilterMessage("trial output blocked").Len(); n != 2 {
		t.Fatalf("want 2 blocked logs, got %d", n)
	}
}

func TestUntaggedOutputSkipsTrialGuard(t *testing.T) {
	w, logs := observedWorker()
	msg := &outgress.Message{Type: outgress.TypeChat, BroadcasterID: "42"}
	if w.rejectTrialOutput(context.Background(), msg) {
		t.Fatal("untagged output was refused")
	}
	if logs.Len() != 0 {
		t.Fatal("untagged output was logged as blocked")
	}
	msg.Origin = "trial"
	if !w.rejectTrialOutput(context.Background(), msg) {
		t.Fatal("trial output was not refused")
	}
}

func TestBlockedTrialBatchLogsEachWouldBeSend(t *testing.T) {
	w, logs := observedWorker()
	items := []outgress.Message{
		{Type: outgress.TypeChat, BroadcasterID: "42", Origin: "trial", Payload: codec.RawMessage(`{"message":"one"}`)},
		{Type: outgress.TypeChat, BroadcasterID: "42", Origin: "trial", Payload: codec.RawMessage(`{"message":"two"}`)},
	}
	body, err := codec.Marshal(outgress.Batch{ID: "b1", Items: items})
	if err != nil {
		t.Fatal(err)
	}
	batch := &outgress.Message{Type: outgress.TypeBatch, BroadcasterID: "42", Origin: "trial", Payload: body}
	if !w.rejectTrialOutput(context.Background(), batch) {
		t.Fatal("trial batch was not refused")
	}
	blocked := logs.FilterMessage("trial output blocked").All()
	if len(blocked) != 2 {
		t.Fatalf("want one log per child, got %d", len(blocked))
	}
	if got := string(blocked[1].ContextMap()["payload"].(string)); got != `{"message":"two"}` {
		t.Fatalf("payload not logged: %q", got)
	}
}
