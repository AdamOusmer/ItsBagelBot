package worker

import (
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
	"context"
	"maps"
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

func blockedTrialBatch(t *testing.T) *outgress.Message {
	t.Helper()
	items := []outgress.Message{
		{Type: outgress.TypeChat, BroadcasterID: "42", Origin: "trial", Payload: codec.RawMessage(`{"message":"one"}`)},
		{Type: outgress.TypeChat, BroadcasterID: "42", Origin: "trial", Payload: codec.RawMessage(`{"message":"two"}`)},
		{Type: outgress.TypeClip, BroadcasterID: "42", Origin: "trial"},
	}
	body, err := codec.Marshal(outgress.Batch{ID: "b1", Items: items})
	if err != nil {
		t.Fatal(err)
	}
	return &outgress.Message{Type: outgress.TypeBatch, BroadcasterID: "42", Origin: "trial", Payload: body}
}

func blockedSummaries(logs *observer.ObservedLogs) map[string]map[string]any {
	summaries := map[string]map[string]any{}
	for _, entry := range logs.FilterMessage("trial output blocked").All() {
		summaries[entry.ContextMap()["type"].(string)] = entry.ContextMap()
	}
	return summaries
}

func TestBlockedTrialOutputsSummarizePerChannelAndType(t *testing.T) {
	w, logs := observedWorker()
	for range 50 {
		w.rejectTrialOutput(context.Background(), blockedTrialBatch(t))
	}
	w.blocked.Flush()
	summaries := blockedSummaries(logs)
	counts := map[string]any{}
	for kind, summary := range summaries {
		counts[kind] = summary["count"]
	}
	if want := map[string]any{"chat": int64(100), "clip": int64(50)}; !maps.Equal(counts, want) {
		t.Fatalf("want %v, got %v", want, counts)
	}
	if summaries["chat"]["sample_payload"] != `{"message":"one"}` {
		t.Fatalf("sample: %v", summaries["chat"]["sample_payload"])
	}
}

func TestAnEmptyMinuteLogsNothing(t *testing.T) {
	w, logs := observedWorker()
	w.rejectTrialOutput(context.Background(), blockedTrialBatch(t))
	w.blocked.Flush()
	w.blocked.Flush()
	if n := logs.FilterMessage("trial output blocked").Len(); n != 2 {
		t.Fatalf("want only the first minute's two lines, got %d", n)
	}
}
