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
