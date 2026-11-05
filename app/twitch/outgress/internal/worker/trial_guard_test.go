// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"maps"
	"testing"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/kvstate/kvtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func observedWorker() (*Worker, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.InfoLevel)
	log := zap.New(core)
	return &Worker{log: log, blocked: &BlockedLog{log: log, pending: map[blockedKey]*blockedTally{}}}, logs
}

func trialPipeline(t *testing.T) (*Worker, *scriptedTransport, *observer.ObservedLogs) {
	t.Helper()
	obs, logs := observedWorker()
	rt := &scriptedTransport{}
	w := pipelineWorker(t, rt, withLog(obs.log), withBlocked(obs.blocked), withBatchStore(NewJetStreamBatchStore(kvtest.New())))
	return w, rt, logs
}

func TestTrialOriginStopsDirectAndBatchChild(t *testing.T) {
	w, rt, logs := trialPipeline(t)
	direct := outgress.Message{Type: outgress.TypeChat, BroadcasterID: "42", Origin: "trial", TrialGeneration: 3, Payload: codec.RawMessage(`{"message":"hi"}`)}
	batch := testMessage{Type: "batch", Broadcaster: "42", Payload: `{"id":"b","items":[{"type":"chat","broadcaster_id":"42","origin":"trial","trial_generation":3,"payload":{"message":"hi"}}]}`}
	body, err := codec.Marshal(direct)
	require.NoError(t, err)

	require.NoError(t, w.Process(bus.NewMessage("trial-direct", body)))
	require.NoError(t, batch.send(w))

	assert.Zero(t, logs.Len(), "a blocked output must not log on its own")
	w.blocked.Flush()
	blocked := logs.FilterMessage("trial output blocked").All()
	require.Len(t, blocked, 1)
	assert.EqualValues(t, 2, blocked[0].ContextMap()["count"])
	assert.Empty(t, rt.recorded(), "trial output never reaches Twitch")
}

func TestProcessRefusesTrialOriginFromTheWire(t *testing.T) {
	w, logs := observedWorker()
	body, err := codec.Marshal(outgress.Message{Type: outgress.TypeChat, BroadcasterID: "42", Origin: "trial", TrialGeneration: 5, Payload: codec.RawMessage(`{"message":"hi"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Process(bus.NewMessage("trial-wire", body)); err != nil {
		t.Fatal(err)
	}
	w.blocked.Flush()
	blocked := logs.FilterMessage("trial output blocked").All()
	if len(blocked) != 1 || blocked[0].ContextMap()["trial_generation"] != uint64(5) {
		t.Fatalf("want the wire trial output blocked with its generation, got %+v", blocked)
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
	w, _, logs := trialPipeline(t)
	body, err := codec.Marshal(*blockedTrialBatch(t))
	require.NoError(t, err)
	require.NoError(t, w.Process(bus.NewMessage("trial-batch", body)))

	w.blocked.Flush()
	w.blocked.Flush()

	assert.Equal(t, 2, logs.FilterMessage("trial output blocked").Len(), "only the first minute's two lines")
}
