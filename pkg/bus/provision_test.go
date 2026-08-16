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

// streamManagerSpy stands in for the modern JetStream API. It implements the
// narrow streamProvisioner the reconciler takes, so a converge that reached for
// a delete or any other verb would not compile.
type streamManagerSpy struct {
	info         *jsapi.StreamInfo
	updateErr    error
	updateCalled bool
	updateCount  int
	updated      jsapi.StreamConfig
	addCalled    bool
}

// streamSpy satisfies jetstream.Stream by embedding the interface: every method
// the reconciler does not call panics rather than silently returning a zero
// value.
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

// liveStream models what STREAM.INFO returns for a converged stream: the
// request config after the broker's own rewrites. Building the fixture through
// serverNormalized is the whole point — a fixture built from the raw request
// shape hides every sentinel the server stores differently from what we sent.
func liveStream(cfg jsapi.StreamConfig) *jsapi.StreamInfo {
	return &jsapi.StreamInfo{Config: serverNormalized(cfg)}
}

func reconcile(t *testing.T, js *streamManagerSpy, spec StreamSpec) error {
	t.Helper()
	return reconcileStream(context.Background(), js, spec, zap.NewNop())
}

// mustReconcile converges a spec that has no reason to fail.
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
	// A stream provisioned under the old catalog: R1, pinned to one ordinal.
	// Both changes must converge in a single UpdateStream, with no delete and no
	// recreate — the runtime credentials cannot delete streams, and a second pass
	// would only run on the next reconnect.
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
	// A nil Placement is what clears the stored one: jetstream.StreamConfig omits it
	// from the request, and the server stores the update's config wholesale
	// rather than merging it over the old one.
	if js.updated.Placement != nil {
		t.Fatalf("update carried placement %v; a nil placement is what clears the stored tag",
			js.updated.Placement.Tags)
	}
	if js.updated.Replicas != 3 {
		t.Fatalf("update replicas = %d, want 3", js.updated.Replicas)
	}
	// Storage is fixed at creation, so the update must echo what the stream has.
	if js.updated.Storage != live.Storage {
		t.Fatalf("update storage = %v, want the live stream's %v", js.updated.Storage, live.Storage)
	}
}

// TestConvergedCatalogStreamsIssueNoUpdates is the regression gate for the
// reconcile-forever class of bug. The live fixture is the request config after
// the broker's own rewrites (serverNormalized), because that — not the request
// shape — is what STREAM.INFO returns. Modelling the live stream as the raw
// request is what hid MaxMsgsPerSubject 0 vs the stored -1 for three streams,
// each of which then issued an UpdateStream on every startup and every
// reconnect, forever.
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

// TestBatchAndScheduleFlagsConvergeInTheSameUpdate is the reason the reconcile
// runs on the modern API. A legacy StreamConfig cannot carry these flags, so a
// legacy update cleared them and a second pass re-added them — and while they
// were off the server destroyed every staged atomic batch and open fast session
// on the stream. One update, all flags, no window.
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

// TestReconcileNeverAsksToDisableOneWayFlags covers the three doors that only
// open one way. The server refuses an update that clears message TTLs or message
// schedules, so a spec that does not declare them must not try to take them off
// a stream that has them: the update would be rejected on every pass, and the
// retry timer would turn that into a permanent error loop.
func TestReconcileNeverAsksToDisableOneWayFlags(t *testing.T) {
	spec := ingressStreamSpec(t) // declares neither TTLs nor schedules
	live := streamConfig(spec)
	live.AllowMsgTTL = true
	live.AllowMsgSchedules = true
	js := &streamManagerSpy{info: liveStream(live)}

	mustReconcile(t, js, spec)
	if js.updateCount != 0 {
		t.Fatalf("update calls = %d; a flag the server will not let us clear is not drift", js.updateCount)
	}

	// And when something else genuinely drifts, the update still carries the
	// live values forward instead of asking for a rejection.
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

// TestGuardianRetriesFailedReconcile covers the other half of the batch-feature
// finding: a failed pass used to be a Warn and nothing else, so a stream that
// missed its converge stayed drifted until the next reconnect — possibly hours
// later — while publishers assumed the capabilities it was missing.
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

// TestIngressPartitionNarrowsBeforeItCreates covers the one ordering a rolling
// deploy cannot get wrong twice. The subject moves between two live streams, and
// the broker refuses both an overlap and offers no atomic move, so the narrowing
// UpdateStream has to precede the CreateStream that claims the subject.
// EnsureStreams walks its slice in order, which makes catalog order the
// enforcement mechanism — and makes it worth a test, because the failure mode of
// the reverse order is not a lost message but a permanent crashloop: the create
// is refused, the initial provision is fatal, and the narrowing that would clear
// the overlap never runs.
func TestIngressPartitionNarrowsBeforeItCreates(t *testing.T) {
	narrowed, created := catalogIndex(t, TwitchIngressStream.Name), catalogIndex(t, TwitchIngressStandardStream.Name)
	if narrowed > created {
		t.Fatalf("catalog reconciles %s before %s; the create would be refused for subject overlap",
			TwitchIngressStandardStream.Name, TwitchIngressStream.Name)
	}

	// The live shape a first converge meets: the pre-partition stream, still
	// wildcarded and still holding the whole gigabyte.
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
