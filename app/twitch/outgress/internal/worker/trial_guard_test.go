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
	log := zap.New(core)
	return &Worker{log: log, blocked: &BlockedLog{log: log, pending: map[blockedKey]*blockedTally{}}}, logs
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
	if logs.Len() != 0 {
		t.Fatal("a blocked output must not log on its own")
	}
	w.blocked.Flush()
	blocked := logs.FilterMessage("trial output blocked").All()
	if len(blocked) != 1 || blocked[0].ContextMap()["count"] != int64(2) {
		t.Fatalf("want one summary counting 2, got %+v", blocked)
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

func TestBlockedTrialOutputsSummarizePerChannelAndType(t *testing.T) {
	w, logs := observedWorker()
	items := []outgress.Message{
		{Type: outgress.TypeChat, BroadcasterID: "42", Origin: "trial", Payload: codec.RawMessage(`{"message":"one"}`)},
		{Type: outgress.TypeChat, BroadcasterID: "42", Origin: "trial", Payload: codec.RawMessage(`{"message":"two"}`)},
		{Type: outgress.TypeClip, BroadcasterID: "42", Origin: "trial"},
	}
	body, err := codec.Marshal(outgress.Batch{ID: "b1", Items: items})
	if err != nil {
		t.Fatal(err)
	}
	for range 50 {
		if !w.rejectTrialOutput(context.Background(), &outgress.Message{Type: outgress.TypeBatch, BroadcasterID: "42", Origin: "trial", Payload: body}) {
			t.Fatal("trial batch was not refused")
		}
	}
	w.blocked.Flush()
	summaries := map[string]map[string]any{}
	for _, entry := range logs.FilterMessage("trial output blocked").All() {
		summaries[entry.ContextMap()["type"].(string)] = entry.ContextMap()
	}
	if len(summaries) != 2 {
		t.Fatalf("want one line per type, got %d", len(summaries))
	}
	if summaries["chat"]["count"] != int64(100) || summaries["clip"]["count"] != int64(50) {
		t.Fatalf("counts: chat=%v clip=%v", summaries["chat"]["count"], summaries["clip"]["count"])
	}
	if summaries["chat"]["sample_payload"] != `{"message":"one"}` {
		t.Fatalf("sample: %v", summaries["chat"]["sample_payload"])
	}
	w.blocked.Flush()
	if n := logs.FilterMessage("trial output blocked").Len(); n != 2 {
		t.Fatalf("an empty minute must not log, got %d lines", n)
	}
}
