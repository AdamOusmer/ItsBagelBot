// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

type streamManagerSpy struct {
	info         *jsapi.StreamInfo
	updateErr    error
	updateCalled bool
	updateCount  int
	updated      jsapi.StreamConfig
	addCalled    bool
}

type streamSpy struct {
	jsapi.Stream
	info *jsapi.StreamInfo
}

func (s *streamSpy) CachedInfo() *jsapi.StreamInfo { return s.info }

func (s *streamManagerSpy) Stream(context.Context, string) (jsapi.Stream, error) {
	if s.info == nil {
		return nil, jsapi.ErrStreamNotFound
	}
	return &streamSpy{info: s.info}, nil
}

func (s *streamManagerSpy) UpdateStream(_ context.Context, cfg jsapi.StreamConfig) (jsapi.Stream, error) {
	s.updateCalled = true
	s.updateCount++
	s.updated = cfg
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return &streamSpy{info: &jsapi.StreamInfo{Config: cfg}}, nil
}

func (s *streamManagerSpy) CreateStream(_ context.Context, cfg jsapi.StreamConfig) (jsapi.Stream, error) {
	s.addCalled = true
	return &streamSpy{info: &jsapi.StreamInfo{Config: cfg}}, nil
}

func liveStream(cfg jsapi.StreamConfig) *jsapi.StreamInfo {
	return &jsapi.StreamInfo{Config: serverNormalized(cfg)}
}

func reconcile(t *testing.T, js *streamManagerSpy, spec StreamSpec) error {
	t.Helper()
	return reconcileStream(context.Background(), js, spec, zap.NewNop())
}

func mustReconcile(t *testing.T, js *streamManagerSpy, spec StreamSpec) {
	t.Helper()
	if err := reconcile(t, js, spec); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func TestReconcileStreamRequiresOperatorForRetentionMigration(t *testing.T) {
	live := streamConfig(OutgressStream)
	live.Retention = jsapi.LimitsPolicy
	js := &streamManagerSpy{info: liveStream(live)}

	err := reconcile(t, js, OutgressStream)
	if err == nil {
		t.Fatal("reconcile unexpectedly accepted a retention-policy migration")
	}
	if !strings.Contains(err.Error(), "operator-managed delete/recreate") {
		t.Fatalf("reconcile error = %v, want explicit operator migration", err)
	}
	if js.addCalled {
		t.Fatal("retention migration attempted to recreate the stream")
	}
	if js.updateCalled {
		t.Fatal("retention migration attempted to update the stream")
	}
}

func TestReconcileStreamNeverDeletesAfterRejectedWorkQueueUpdate(t *testing.T) {
	live := streamConfig(OutgressStream)
	live.MaxAge += time.Second
	js := &streamManagerSpy{
		info:      liveStream(live),
		updateErr: errors.New("update rejected"),
	}

	err := reconcile(t, js, OutgressStream)
	if err == nil {
		t.Fatal("reconcile unexpectedly accepted a rejected work-queue update")
	}
	if !strings.Contains(err.Error(), "operator-managed delete/recreate") {
		t.Fatalf("reconcile error = %v, want explicit operator migration", err)
	}
	if !js.updateCalled {
		t.Fatal("reconcile did not attempt the non-destructive stream update")
	}
	if js.addCalled {
		t.Fatal("rejected update triggered a stream recreation")
	}
}

func TestReconcileClearsPlacementAndScalesInOneUpdate(t *testing.T) {
	spec := ingressStreamSpec(t)
	live := streamConfig(spec)
	live.Replicas = 1
	live.Placement = &jsapi.Placement{Tags: []string{"nats-0"}}
	js := &streamManagerSpy{info: liveStream(live)}

	mustReconcile(t, js, spec)
	if js.updateCount != 1 {
		t.Fatalf("update calls = %d, want exactly one converging pass", js.updateCount)
	}
	if js.addCalled {
		t.Fatal("converging an R1-with-placement stream must not recreate it")
	}
	if js.updated.Placement != nil {
		t.Fatalf("update carried placement %v; a nil placement is what clears the stored tag",
			js.updated.Placement.Tags)
	}
	if js.updated.Replicas != 3 {
		t.Fatalf("update replicas = %d, want 3", js.updated.Replicas)
	}
	if js.updated.Storage != live.Storage {
		t.Fatalf("update storage = %v, want the live stream's %v", js.updated.Storage, live.Storage)
	}
}

func TestConvergedCatalogStreamsIssueNoUpdates(t *testing.T) {
	for _, spec := range fleetStreamSpecs() {
		t.Run(spec.Name, func(t *testing.T) {
			js := &streamManagerSpy{info: liveStream(streamConfig(spec))}

			mustReconcile(t, js, spec)
			if js.updateCount != 0 {
				t.Fatalf("converged stream issued %d update(s); the desired config never matches what the server stores",
					js.updateCount)
			}
			if js.addCalled {
				t.Fatal("converged stream was recreated")
			}
		})
	}
}

func TestBatchAndScheduleFlagsConvergeInTheSameUpdate(t *testing.T) {
	spec := TwitchIngressRetryStream
	live := streamConfig(spec)
	live.AllowAtomicPublish = false
	live.AllowBatchPublish = false
	live.AllowMsgSchedules = false
	js := &streamManagerSpy{info: liveStream(live)}

	mustReconcile(t, js, spec)
	if js.updateCount != 1 {
		t.Fatalf("update calls = %d, want exactly one converging pass", js.updateCount)
	}
	if !js.updated.AllowAtomicPublish || !js.updated.AllowBatchPublish {
		t.Fatal("converging update did not carry the batch publish flags")
	}
	if !js.updated.AllowMsgSchedules {
		t.Fatal("converging update did not carry message scheduling")
	}
}

func TestReconcileNeverAsksToDisableOneWayFlags(t *testing.T) {
	spec := ingressStreamSpec(t)
	live := streamConfig(spec)
	live.AllowMsgTTL = true
	live.AllowMsgSchedules = true
	js := &streamManagerSpy{info: liveStream(live)}

	mustReconcile(t, js, spec)
	if js.updateCount != 0 {
		t.Fatalf("update calls = %d; a flag the server will not let us clear is not drift", js.updateCount)
	}

	live.Replicas = 1
	js = &streamManagerSpy{info: liveStream(live)}
	mustReconcile(t, js, spec)
	if !js.updated.AllowMsgTTL || !js.updated.AllowMsgSchedules {
		t.Fatal("converging update tried to disable message TTLs or schedules; the server rejects that outright")
	}
	if js.updated.Replicas != 3 {
		t.Fatalf("update replicas = %d, want the drift converged", js.updated.Replicas)
	}
}

func TestGuardianRetriesFailedReconcile(t *testing.T) {
	spec := ingressStreamSpec(t)
	live := streamConfig(spec)
	live.Replicas = 1
	js := &streamManagerSpy{info: liveStream(live), updateErr: errors.New("no meta leader")}
	guardian := &streamGuardian{js: js, specs: []StreamSpec{spec}, log: zap.NewNop()}

	guardian.reconcileAll(context.Background())
	if !guardian.dirty.Load() {
		t.Fatal("a failed reconcile did not arm the retry")
	}

	js.updateErr = nil
	guardian.dirty.Store(false)
	guardian.reconcileAll(context.Background())
	if guardian.dirty.Load() {
		t.Fatal("a successful reconcile left the retry armed")
	}
	if js.updateCount != 2 {
		t.Fatalf("update calls = %d, want the failed pass plus its retry", js.updateCount)
	}
}

func TestIngressPartitionNarrowsBeforeItCreates(t *testing.T) {
	narrowed, created := catalogIndex(t, TwitchIngressStream.Name), catalogIndex(t, TwitchIngressStandardStream.Name)
	if narrowed > created {
		t.Fatalf("catalog reconciles %s before %s; the create would be refused for subject overlap",
			TwitchIngressStandardStream.Name, TwitchIngressStream.Name)
	}

	live := streamConfig(TwitchIngressStream)
	live.Subjects = []string{"twitch.ingress.event.>", "twitch.ingress.status.>"}
	live.MaxBytes = 1 << 30
	js := &streamManagerSpy{info: liveStream(live)}

	mustReconcile(t, js, TwitchIngressStream)
	if js.updateCount != 1 {
		t.Fatalf("update calls = %d, want one narrowing pass", js.updateCount)
	}
	if js.addCalled {
		t.Fatal("narrowing a live stream must not recreate it; the runtime credentials cannot delete streams")
	}
	if !sameSubjects(js.updated.Subjects, TwitchIngressStream.Subjects) {
		t.Fatalf("narrowing update carried subjects %v, want %v", js.updated.Subjects, TwitchIngressStream.Subjects)
	}
	for _, subject := range js.updated.Subjects {
		if matchSubject("twitch.ingress.event.standard", subject) {
			t.Fatalf("narrowing update still claims the standard lane through %q", subject)
		}
	}
}
