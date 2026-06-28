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
