package bus

import (
	"slices"
	"time"

	"github.com/nats-io/nats.go"
)

// streamConfig translates the small fleet-owned stream contract into the NATS
// API shape. Keeping this pure mapping separate from the connection lifecycle
// makes placement and capacity defaults straightforward to test.
func streamConfig(spec StreamSpec) *nats.StreamConfig {
	duplicateWindow := 2 * time.Minute
	if spec.Duplicates > 0 {
		duplicateWindow = spec.Duplicates
	}
	if spec.MaxAge > 0 && spec.MaxAge < duplicateWindow {
		// NATS rejects a duplicate window longer than the stream's MaxAge.
		duplicateWindow = spec.MaxAge
	}
	// Zero value is nats.FileStorage; a spec opts into memory explicitly.
	storage := nats.FileStorage
	if spec.Storage != 0 {
		storage = spec.Storage
	}
	// Zero replicas means the safe single-copy default; a spec opts into RAFT
	// replication explicitly.
	replicas := spec.Replicas
	if replicas <= 0 {
		replicas = 1
	}
	return &nats.StreamConfig{
		Name:              spec.Name,
		Subjects:          spec.Subjects,
		Storage:           storage,
		Retention:         spec.Retention,
		Discard:           nats.DiscardOld,
		MaxAge:            spec.MaxAge,
		MaxBytes:          spec.MaxBytes,
		MaxMsgsPerSubject: spec.MaxMsgsPer,
		Replicas:          replicas,
		Duplicates:        duplicateWindow,
		Placement:         placement(spec.PlacementTags),
	}
}

func placement(tags []string) *nats.Placement {
	if len(tags) == 0 {
		return nil
	}
	return &nats.Placement{Tags: slices.Clone(tags)}
}

func streamMatches(got, want nats.StreamConfig) bool {
	return sameSubjects(got.Subjects, want.Subjects) &&
		got.Retention == want.Retention &&
		got.MaxAge == want.MaxAge &&
		got.MaxBytes == want.MaxBytes &&
		got.MaxMsgsPerSubject == want.MaxMsgsPerSubject &&
		// Replicas is updatable in place, so a drift here must trigger a reconcile
		// (UpdateStream scales the stream); omitting it lets a live R3 stream stay
		// R3 while the spec declares R1.
		got.Replicas == want.Replicas &&
		got.Duplicates == want.Duplicates &&
		samePlacement(got.Placement, want.Placement)
}

func samePlacement(a, b *nats.Placement) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Cluster == b.Cluster && sameSubjects(a.Tags, b.Tags)
}

func sameSubjects(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
