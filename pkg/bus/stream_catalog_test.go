package bus

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

// fleetStreamSpecs returns the whole catalog: the reconciled data streams plus
// the two outgress streams their owners reconcile separately.
func fleetStreamSpecs() []StreamSpec {
	specs := append([]StreamSpec{}, DataStreams...)
	return append(specs, OutgressStream, OutgressSystemStream)
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

func catalogIndex(t *testing.T, name string) int {
	t.Helper()
	for i := range DataStreams {
		if DataStreams[i].Name == name {
			return i
		}
	}
	t.Fatalf("stream %s missing from the catalog", name)
	return -1
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

// TestCatalogStreamsClaimDisjointSubjects is the create-time gate for the whole
// catalog, not just the outgress pair. A subject may belong to exactly one
// stream: the broker refuses an overlapping create outright
// (JSStreamSubjectOverlap), and the failure lands at service startup where a
// failed initial provision is fatal. It also underwrites streamForTopic's
// first-match resolution — with disjoint filters the first match is the only
// one, so catalog ORDER cannot silently decide which stream a lane binds.
//
// Wildcard-aware in both directions, which is the case the ingress partition
// actually hits: twitch.ingress.event.standard is invisible to a literal
// comparison against twitch.ingress.event.>.
func TestCatalogStreamsClaimDisjointSubjects(t *testing.T) {
	specs := fleetStreamSpecs()
	for i := range specs {
		for j := i + 1; j < len(specs); j++ {
			requireDisjointSubjects(t, specs[i], specs[j])
		}
	}
}

func requireDisjointSubjects(t *testing.T, first, second StreamSpec) {
	t.Helper()
	for _, a := range first.Subjects {
		for _, b := range second.Subjects {
			if matchSubject(a, b) || matchSubject(b, a) {
				t.Fatalf("streams %s (%q) and %s (%q) both claim the same subject space",
					first.Name, a, second.Name, b)
			}
		}
	}
}

// TestIngressLanesResolveToTheirPartitions pins the subject→stream map the
// partition creates. Consumers bind the stream streamForTopic hands back (see
// targetForTopic), so this map IS the consumer binding: a standard-lane
// subscriber that still resolved to TWITCH_INGRESS would provision a consumer
// on a stream that no longer captures its subject and receive nothing, silently.
func TestIngressLanesResolveToTheirPartitions(t *testing.T) {
	t.Setenv("NATS_INGRESS_PARTITION", "on")
	for subject, want := range map[string]string{
		"twitch.ingress.event.standard":       TwitchIngressStandardStream.Name,
		"twitch.ingress.event.premium":        TwitchIngressStream.Name,
		"twitch.ingress.event.stream":         TwitchIngressStream.Name,
		"twitch.ingress.status.authz.revoked": TwitchIngressStream.Name,
		"twitch.ingress.retry.standard":       TwitchIngressRetryStream.Name,
	} {
		requireStreamForTopic(t, subject, want)
	}

	// The wildcard that used to catch every lane is gone, and nothing may quietly
	// re-introduce it: a stream still claiming twitch.ingress.event.> would
	// overlap the standard partition and fail to create.
	if got, err := streamForTopic("twitch.ingress.event.unknown"); err == nil {
		t.Fatalf("streamForTopic(unknown lane) = %q, want a refusal; a wildcard is back in the catalog", got)
	}
}

func requireStreamForTopic(t *testing.T, subject, want string) {
	t.Helper()
	got, err := streamForTopic(subject)
	if err != nil {
		t.Fatalf("streamForTopic(%q): %v", subject, err)
	}
	if got != want {
		t.Fatalf("stream for %q = %q, want %q", subject, got, want)
	}
}

// TestPartitionFlagOffKeepsThePrePartitionShape is the deploy-safety half of
// the partition gate: merging the partition code must change nothing until the
// operator flips NATS_INGRESS_PARTITION in its own window, after the fleet's
// ingress images all carry per-subject cohort staging.
func TestPartitionFlagOffKeepsThePrePartitionShape(t *testing.T) {
	t.Setenv("NATS_INGRESS_PARTITION", "off")
	lanes := IngressLaneSpecs()
	if len(lanes) != 1 || lanes[0].Name != TwitchIngressStream.Name {
		t.Fatalf("lane specs with the partition off = %#v, want the single legacy stream", lanes)
	}
	if got := lanes[0].Subjects; len(got) != 2 || got[0] != "twitch.ingress.event.>" {
		t.Fatalf("legacy subjects = %v, want the event wildcard restored", got)
	}
	if lanes[0].MaxBytes != 1<<30 {
		t.Fatalf("legacy MaxBytes = %d, want the whole gigabyte", lanes[0].MaxBytes)
	}
	for subject, want := range map[string]string{
		"twitch.ingress.event.standard": TwitchIngressStream.Name,
		"twitch.ingress.event.premium":  TwitchIngressStream.Name,
	} {
		got, err := streamForTopic(subject)
		if err != nil || got != want {
			t.Fatalf("streamForTopic(%q) = %q, %v; want %q on the legacy shape", subject, got, err, want)
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
		requireStreamForTopic(t, chat, OutgressStream.Name)
	}
}

func TestOutgressStreamHasSingleReconciler(t *testing.T) {
	for _, spec := range DataStreams {
		if spec.Name == OutgressStream.Name {
			t.Fatal("outgress stream must be reconciled only by outgress")
		}
	}
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

// TestRetryStreamCarriesTheSchedulePrerequisites keeps the delayed-redelivery
// lane usable. A scheduled retry sets Nats-Schedule-TTL on the emitted message,
// which the broker rejects outright unless the stream allows message TTLs, and
// the server forces rollups on any scheduling stream (schedule rows are replaced
// via Nats-Rollup: sub) — state both here so the stored config equals the
// requested one instead of drifting on the first reconcile.
func TestRetryStreamCarriesTheSchedulePrerequisites(t *testing.T) {
	cfg := streamConfig(TwitchIngressRetryStream)

	requireContract(t,
		contractClause{cfg.AllowMsgSchedules,
			"retry lane does not enable message schedules; there is no delay primitive left"},
		contractClause{cfg.AllowMsgTTL,
			"Nats-Schedule-TTL is rejected when message TTLs are disabled"},
		contractClause{cfg.AllowRollup && !cfg.DenyPurge,
			"scheduling forces rollups and permits purge; emitting the requested shape avoids permanent drift"},
		contractClause{cfg.Discard == jsapi.DiscardOld,
			"message scheduling cannot use discard new"},
	)
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

func TestFleetStreamStorageTiersAreExplicit(t *testing.T) {
	// Memory is the firehose tier (no per-event disk write); file is the
	// retention tier (survives a full-quorum restart). Both are stated in the
	// catalog, so a tier change is a visible edit rather than a dropped field.
	memory := map[string]bool{
		TwitchIngressStream.Name:         true,
		TwitchIngressStandardStream.Name: true,
		TwitchIngressRetryStream.Name:    true,
		OutgressStream.Name:              true,
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
