package bus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
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

func TestOutgressStreamIsPerishableWorkQueue(t *testing.T) {
	cfg := streamConfig(OutgressStream)

	if cfg.Retention != jsapi.WorkQueuePolicy {
		t.Fatalf("retention = %v, want work queue", cfg.Retention)
	}
	if cfg.MaxAge != 5*time.Second {
		t.Fatalf("max age = %v, want 5s", cfg.MaxAge)
	}
	if cfg.Duplicates > cfg.MaxAge {
		t.Fatalf("duplicate window %v exceeds max age %v", cfg.Duplicates, cfg.MaxAge)
	}
}

func TestOutgressSystemStreamIsDurableWorkQueue(t *testing.T) {
	cfg := streamConfig(OutgressSystemStream)

	if cfg.Retention != jsapi.WorkQueuePolicy {
		t.Fatalf("retention = %v, want work queue (ack removes, not replay)", cfg.Retention)
	}
	// Control jobs (EventSub enroll, stream_status) must outlive the chat lane's
	// 5s so a rollout gap or transient nack does not silently drop an enrollment.
	if cfg.MaxAge <= 5*time.Second {
		t.Fatalf("max age = %v, want longer than the chat lane's 5s", cfg.MaxAge)
	}
	if cfg.Duplicates > cfg.MaxAge {
		t.Fatalf("duplicate window %v exceeds max age %v", cfg.Duplicates, cfg.MaxAge)
	}
}

func TestOutgressStreamsDoNotOverlap(t *testing.T) {
	// A subject may belong to exactly one stream; overlap makes AddStream fail.
	for _, chat := range OutgressStream.Subjects {
		for _, sys := range OutgressSystemStream.Subjects {
			if chat == sys {
				t.Fatalf("chat and system streams both claim %q", chat)
			}
		}
	}
}

func TestSystemSubjectResolvesToSystemStream(t *testing.T) {
	got, err := streamForTopic("twitch.outgress.system")
	if err != nil {
		t.Fatalf("streamForTopic: %v", err)
	}
	if got != OutgressSystemStream.Name {
		t.Fatalf("stream = %q, want %q", got, OutgressSystemStream.Name)
	}
	for _, chat := range []string{"twitch.outgress.premium", "twitch.outgress.standard"} {
		got, err := streamForTopic(chat)
		if err != nil {
			t.Fatalf("streamForTopic(%q): %v", chat, err)
		}
		if got != OutgressStream.Name {
			t.Fatalf("stream for %q = %q, want %q", chat, got, OutgressStream.Name)
		}
	}
}

func TestOutgressStreamHasSingleReconciler(t *testing.T) {
	for _, spec := range DataStreams {
		if spec.Name == OutgressStream.Name {
			t.Fatal("outgress stream must be reconciled only by outgress")
		}
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

// ingressStreamSpec returns the TWITCH_INGRESS spec from DataStreams, failing the
// test if it is missing. Shared by the tests that assert on the firehose spec.
func ingressStreamSpec(t *testing.T) StreamSpec {
	t.Helper()
	for i := range DataStreams {
		if DataStreams[i].Name == "TWITCH_INGRESS" {
			return DataStreams[i]
		}
	}
	t.Fatal("TWITCH_INGRESS stream spec missing")
	return StreamSpec{}
}

func TestIngressStreamIsolatesLanesPerSubject(t *testing.T) {
	cfg := streamConfig(ingressStreamSpec(t))
	// The premium/standard/stream lanes are distinct literal subjects on one
	// stream; MaxBytes eviction alone is oldest-first stream-wide, letting a
	// standard flood evict premium. The per-subject cap makes a flooded lane
	// wrap itself instead.
	if cfg.MaxMsgsPerSubject <= 0 {
		t.Fatal("ingress lanes need a per-subject cap so one lane cannot evict the others")
	}
	if cfg.MaxBytes <= 0 {
		t.Fatal("ingress stream still needs its global byte backstop")
	}
	// Fleet publishers deliberately omit Nats-Msg-Id. Keep a short bounded
	// stream window for rolling-upgrade or externally published messages that
	// may still carry the header; it is inert for the normal hot path.
	if cfg.Duplicates <= 0 || cfg.Duplicates > time.Minute {
		t.Fatalf("duplicate window = %v, want a short non-zero dedup window", cfg.Duplicates)
	}
}

func TestEveryFleetStreamEnablesBatchPublishing(t *testing.T) {
	// Also the thing that keeps R3 affordable: the atomic and fast-ingest wires
	// pay one quorum round-trip per batch instead of one per message.
	for _, spec := range fleetStreamSpecs() {
		if !spec.BatchPublish {
			t.Fatalf("stream %s does not enable shared batch publishing", spec.Name)
		}
	}
}

// fleetStreamSpecs returns the whole catalog: the reconciled data streams plus
// the two outgress streams their owners reconcile separately.
func fleetStreamSpecs() []StreamSpec {
	specs := append([]StreamSpec{}, DataStreams...)
	return append(specs, OutgressStream, OutgressSystemStream)
}

func TestEveryFleetStreamIsReplicated(t *testing.T) {
	// The hub is a three-peer quorum, so R3 is the only factor that survives
	// losing a peer. An R1 stream here means its sole copy — and every consumer
	// bound to it — disappears with whichever peer happened to hold it.
	for _, spec := range fleetStreamSpecs() {
		if got := streamConfig(spec).Replicas; got != 3 {
			t.Fatalf("stream %s replicas = %d, want 3", spec.Name, got)
		}
	}

	// A zero-value Replicas defaults to a single copy, never 0 (which NATS rejects).
	if got := streamConfig(StreamSpec{Name: "X", Subjects: []string{"x.>"}}).Replicas; got != 1 {
		t.Fatalf("default replicas = %d, want 1", got)
	}

	// streamMatches must be replica-sensitive, or a live stream still at R1 stays
	// R1 while the spec declares R3 — invisible drift, since nothing else differs.
	want := streamConfig(ingressStreamSpec(t)) // R3
	drifted := want
	drifted.Replicas = 1
	if streamMatches(drifted, want) {
		t.Fatal("streamMatches ignored a replica drift; live R1 would never converge to R3")
	}
}

func TestFleetStreamsCarryNoPlacement(t *testing.T) {
	// Hub server tags are per-pod ordinals, each present on exactly one server.
	// An R3 stream pinned to one of them is unsatisfiable: the meta leader cannot
	// find three peers carrying a one-peer tag.
	for _, spec := range fleetStreamSpecs() {
		if cfg := streamConfig(spec); cfg.Placement != nil {
			t.Fatalf("R3 stream %s carries placement %v; an ordinal tag cannot satisfy three peers",
				spec.Name, cfg.Placement.Tags)
		}
	}

	// Drift must be detected in both directions. Live-set vs spec-nil is the case
	// this catalog change actually hits: streams provisioned under the old spec
	// still carry an ordinal tag, and missing it leaves them constrained forever.
	want := streamConfig(ingressStreamSpec(t))
	stale := want
	stale.Placement = &jsapi.Placement{Tags: []string{"nats-0"}}
	if streamMatches(stale, want) {
		t.Fatal("streamMatches ignored a stale placement on a live stream")
	}
	if streamMatches(want, stale) {
		t.Fatal("streamMatches ignored a placement the spec asks for but the stream lacks")
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

	if err := reconcile(t, js, spec); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
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

			if err := reconcile(t, js, spec); err != nil {
				t.Fatalf("reconcile: %v", err)
			}
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

// TestStreamConfigEmitsServerSentinels pins the direction of the fix: the
// desired config carries the server's spelling of "unlimited" so the comparison
// can converge. streamMatches deliberately does not paper over the difference —
// if it did, a real drift between 0 and -1 in a future field would be invisible.
func TestStreamConfigEmitsServerSentinels(t *testing.T) {
	cfg := streamConfig(BagelDataStream) // no per-subject cap declared
	if cfg.MaxMsgsPerSubject != -1 {
		t.Fatalf("max msgs per subject = %d, want the server's -1 sentinel", cfg.MaxMsgsPerSubject)
	}
	for name, got := range map[string]int64{
		"max_msgs":      cfg.MaxMsgs,
		"max_msg_size":  int64(cfg.MaxMsgSize),
		"max_consumers": int64(cfg.MaxConsumers),
	} {
		if got != -1 {
			t.Fatalf("%s = %d, want the server's -1 sentinel", name, got)
		}
	}

	unnormalized := cfg
	unnormalized.MaxMsgsPerSubject = 0
	if streamMatches(cfg, unnormalized) {
		t.Fatal("streamMatches treats 0 and -1 as equal; a per-subject cap could then be silently dropped")
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

	if err := reconcile(t, js, spec); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
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

	if err := reconcile(t, js, spec); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if js.updateCount != 0 {
		t.Fatalf("update calls = %d; a flag the server will not let us clear is not drift", js.updateCount)
	}

	// And when something else genuinely drifts, the update still carries the
	// live values forward instead of asking for a rejection.
	live.Replicas = 1
	js = &streamManagerSpy{info: liveStream(live)}
	if err := reconcile(t, js, spec); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !js.updated.AllowMsgTTL || !js.updated.AllowMsgSchedules {
		t.Fatal("converging update tried to disable message TTLs or schedules; the server rejects that outright")
	}
	if js.updated.Replicas != 3 {
		t.Fatalf("update replicas = %d, want the drift converged", js.updated.Replicas)
	}
}

// TestRetryStreamCarriesTheSchedulePrerequisites keeps the delayed-redelivery
// lane usable. A scheduled retry sets Nats-Schedule-TTL on the emitted message,
// which the broker rejects outright unless the stream allows message TTLs, and
// the server forces rollups on any scheduling stream (schedule rows are replaced
// via Nats-Rollup: sub) — state both here so the stored config equals the
// requested one instead of drifting on the first reconcile.
func TestRetryStreamCarriesTheSchedulePrerequisites(t *testing.T) {
	cfg := streamConfig(TwitchIngressRetryStream)

	if !cfg.AllowMsgSchedules {
		t.Fatal("retry lane does not enable message schedules; there is no delay primitive left")
	}
	if !cfg.AllowMsgTTL {
		t.Fatal("Nats-Schedule-TTL is rejected when message TTLs are disabled")
	}
	if !cfg.AllowRollup || cfg.DenyPurge {
		t.Fatal("scheduling forces rollups and permits purge; emitting the requested shape avoids permanent drift")
	}
	if cfg.Discard != jsapi.DiscardOld {
		t.Fatal("message scheduling cannot use discard new")
	}
	// The schedule row lives in this stream: if MaxAge evicts it before it
	// fires, the retry is silently cancelled.
	if cfg.MaxAge < time.Minute {
		t.Fatalf("max age = %v, too tight to hold a delayed retry", cfg.MaxAge)
	}
	if got, err := streamForTopic("twitch.ingress.retry.premium"); err != nil || got != cfg.Name {
		t.Fatalf("streamForTopic(retry) = %q, %v; want %q", got, err, cfg.Name)
	}
}

// TestLegacySpecEnumsMatchModernEnums pins the conversion StreamSpec relies on:
// callers keep spelling nats.WorkQueuePolicy / nats.MemoryStorage while the
// reconcile speaks the modern API. Both encode the same protocol integers, and a
// silent divergence would turn a work-queue spec into a limits stream.
func TestLegacySpecEnumsMatchModernEnums(t *testing.T) {
	if jsapi.RetentionPolicy(nats.WorkQueuePolicy) != jsapi.WorkQueuePolicy ||
		jsapi.RetentionPolicy(nats.InterestPolicy) != jsapi.InterestPolicy ||
		jsapi.RetentionPolicy(nats.LimitsPolicy) != jsapi.LimitsPolicy {
		t.Fatal("retention enums diverged between the legacy and modern clients")
	}
	if streamConfig(StreamSpec{Name: "M", Subjects: []string{"m.>"}, Storage: nats.MemoryStorage}).Storage != jsapi.MemoryStorage {
		t.Fatal("memory storage did not survive the spec conversion")
	}
	if streamConfig(StreamSpec{Name: "F", Subjects: []string{"f.>"}}).Storage != jsapi.FileStorage {
		t.Fatal("the default storage tier is no longer file")
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

func TestFleetStreamStorageTiersAreExplicit(t *testing.T) {
	// Memory is the firehose tier (no per-event disk write); file is the
	// retention tier (survives a full-quorum restart). Both are stated in the
	// catalog, so a tier change is a visible edit rather than a dropped field.
	memory := map[string]bool{
		TwitchIngressStream.Name:      true,
		TwitchIngressRetryStream.Name: true,
		OutgressStream.Name:           true,
	}
	for _, spec := range fleetStreamSpecs() {
		want := jsapi.FileStorage
		if memory[spec.Name] {
			want = jsapi.MemoryStorage
		}
		if got := streamConfig(spec).Storage; got != want {
			t.Fatalf("stream %s storage = %v, want %v", spec.Name, got, want)
		}
	}
}

func TestMemoryStreamsFitTheHubMemoryBudget(t *testing.T) {
	// Under R3 every hub peer holds a full replica, so the per-node floor is the
	// sum of every memory-backed stream's MaxBytes — not a single node's share.
	//
	// max_mem accounts for that sum and nothing else, which is why this test
	// models the terms the broker leaves out. On a memory stream each peer also
	// carries a memStore RAFT WAL (createRaftGroup's else-branch), which between
	// snapshots approaches a second copy of the stream; staged atomic batches for
	// R>1 are a newMemStore that RegisterStorageUpdates never sees; and every
	// stream has its own ingest queue bounded by max_buffered_size. None of that
	// is registered with account storage, so max_mem cannot back-pressure it —
	// only the pod's memory limit can, by OOM-killing the member.
	//
	// Keep these three constants in step with deploy/k8s/nats-server.conf and the
	// nats StatefulSet's limits.
	const (
		maxMem          = 4 << 30   // jetstream.max_mem
		podMemoryLimit  = 6 << 30   // nats container limits.memory
		bufferedPerNode = 128 << 20 // jetstream.max_buffered_size, per stream
		runtimeReserve  = 1 << 30   // Go heap, connection buffers, dedup ids
	)

	var streamBytes int64
	var streams int64
	for _, spec := range fleetStreamSpecs() {
		cfg := streamConfig(spec)
		streams++
		if cfg.Storage != jsapi.MemoryStorage {
			continue
		}
		if cfg.MaxBytes <= 0 {
			t.Fatalf("memory stream %s has no byte cap; it can exhaust max_mem", spec.Name)
		}
		streamBytes += cfg.MaxBytes
	}

	// What max_mem sees.
	if streamBytes >= maxMem/2 {
		t.Fatalf("memory-backed streams reserve %d bytes per node, leaving too little of the %d max_mem for broker state",
			streamBytes, int64(maxMem))
	}

	// What the kernel sees: stream bytes + an equal-sized RAFT WAL ceiling +
	// one ingest queue per stream + room for the process itself.
	worstCase := 2*streamBytes + streams*bufferedPerNode + runtimeReserve
	if worstCase >= podMemoryLimit {
		t.Fatalf("worst-case per-member memory %d (streams %d + WAL + %d ingest queues) exceeds the %d pod limit",
			worstCase, streamBytes, streams, int64(podMemoryLimit))
	}
}

func TestReplaceConsumerCarriesAckFloor(t *testing.T) {
	desired := laneConsumerConfig(
		"twitch.ingress.event.premium",
		"worker",
		"worker_twitch_ingress_event_premium",
		6,
	)

	// A predecessor that acked through stream seq 41 must hand the successor a
	// start at 42: DeliverAll here would replay every retained message (up to
	// MaxAge) to the whole group.
	carryAckFloor(desired, &nats.ConsumerInfo{
		AckFloor: nats.SequenceInfo{Stream: 41},
	})
	if desired.DeliverPolicy != nats.DeliverByStartSequencePolicy {
		t.Fatalf("deliver policy = %v, want by-start-sequence", desired.DeliverPolicy)
	}
	if desired.OptStartSeq != 42 {
		t.Fatalf("start seq = %d, want ack floor + 1", desired.OptStartSeq)
	}

	// No acks yet: starting from the beginning is the correct resume point.
	fresh := laneConsumerConfig("twitch.ingress.event.standard", "worker", "w", 6)
	carryAckFloor(fresh, &nats.ConsumerInfo{})
	if fresh.DeliverPolicy != nats.DeliverAllPolicy || fresh.OptStartSeq != 0 {
		t.Fatal("zero ack floor must keep the original delivery policy")
	}
}

func TestFleetSubscriberHasBoundedPacedRedelivery(t *testing.T) {
	// A plain NACK redelivers immediately; with a four-digit budget a poison
	// message used to grind the whole fleet. The budget must stay small and the
	// pacing non-zero.
	if fleetMaxRedeliveries == 0 || fleetMaxRedeliveries > 10 {
		t.Fatalf("fleet redeliveries = %d, want a small bounded budget", fleetMaxRedeliveries)
	}
	if fleetNakDelay <= 0 {
		t.Fatalf("fleet nak delay = %v, want paced redelivery", fleetNakDelay)
	}
}

func TestLaneConsumerHasBoundedDeliveryBudget(t *testing.T) {
	cfg := laneConsumerConfig(
		"twitch.outgress.premium",
		"outgress-premium",
		"outgress-premium_twitch_outgress_premium",
		4,
	)

	if cfg.MaxDeliver != 4 {
		t.Fatalf("max deliver = %d, want initial delivery plus 3 redeliveries", cfg.MaxDeliver)
	}
	// A consumer BackOff clamps AckWait to backoff[0] on the server, which
	// redelivers still-in-flight slow handlers to other replicas and duplicates
	// the job fleet-wide (the !clip reply incident). NACK pacing belongs to the
	// subscriber's per-message NakWithDelay, never to the consumer.
	if len(cfg.BackOff) != 0 {
		t.Fatalf("backoff = %v, want none: it would clamp ack wait to its first step", cfg.BackOff)
	}
	if cfg.AckWait != 4*time.Second {
		t.Fatalf("ack wait = %v, want 4s bounded by the output dedup window", cfg.AckWait)
	}
	if cfg.AckPolicy != nats.AckExplicitPolicy {
		t.Fatalf("ack policy = %v, want explicit", cfg.AckPolicy)
	}
	if cfg.DeliverGroup != "outgress-premium" {
		t.Fatalf("delivery group = %q, want shared replica queue", cfg.DeliverGroup)
	}
	if cfg.Metadata[managedConsumerMetadata] != "true" {
		t.Fatal("consumer is not marked as server-managed")
	}
}
