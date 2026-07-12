package bus

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestOutgressStreamIsPerishableWorkQueue(t *testing.T) {
	cfg := streamConfig(OutgressStream)

	if cfg.Retention != nats.WorkQueuePolicy {
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

	if cfg.Retention != nats.WorkQueuePolicy {
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
	// Ingress publishes set Nats-Msg-Id so broker dedup collapses publish
	// retries and Twitch EventSub redeliveries; both happen within seconds,
	// and the window bounds the broker's per-id tracking state.
	if cfg.Duplicates <= 0 || cfg.Duplicates > time.Minute {
		t.Fatalf("duplicate window = %v, want a short non-zero dedup window", cfg.Duplicates)
	}
}

func TestEveryFleetStreamEnablesBatchPublishing(t *testing.T) {
	specs := append([]StreamSpec{}, DataStreams...)
	specs = append(specs, OutgressStream, OutgressSystemStream)
	for _, spec := range specs {
		if !spec.BatchPublish {
			t.Fatalf("stream %s does not enable shared batch publishing", spec.Name)
		}
	}
}

func TestStreamReplicasAreExplicitAndEnforced(t *testing.T) {
	ingress := ingressStreamSpec(t)

	// The firehose is R1: its async PubAck-bound producer must not pay a per-publish
	// RAFT quorum. The control lane stays R3 because a lost enroll job is invisible.
	if got := streamConfig(ingress).Replicas; got != 1 {
		t.Fatalf("TWITCH_INGRESS replicas = %d, want 1 (R1 firehose)", got)
	}
	if got := streamConfig(OutgressSystemStream).Replicas; got != 3 {
		t.Fatalf("TWITCH_OUTGRESS_SYSTEM replicas = %d, want 3 (durable control lane)", got)
	}

	// A zero-value Replicas defaults to a single copy, never 0 (which NATS rejects).
	if got := streamConfig(StreamSpec{Name: "X", Subjects: []string{"x.>"}}).Replicas; got != 1 {
		t.Fatalf("default replicas = %d, want 1", got)
	}

	// streamMatches must be replica-sensitive, or a live stream hand-edited to R3
	// stays R3 while the spec declares R1 — the invisible drift this change fixes.
	want := *streamConfig(ingress) // R1
	drifted := want
	drifted.Replicas = 3
	if streamMatches(drifted, want) {
		t.Fatal("streamMatches ignored a replica drift; live R3 would never converge to R1")
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
	if cfg.AckWait != 30*time.Second {
		t.Fatalf("ack wait = %v, want 30s of in-flight tolerance", cfg.AckWait)
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
