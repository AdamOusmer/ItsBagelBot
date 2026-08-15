package bus

import (
	"slices"
	"time"

	jsapi "github.com/nats-io/nats.go/jetstream"
)

// streamConfig translates the small fleet-owned stream contract into the NATS
// API shape. Keeping this pure mapping separate from the connection lifecycle
// makes placement and capacity defaults straightforward to test.
//
// The result is deliberately the *server-normalised* shape (serverNormalized),
// not the shape a human would write by hand: nats-server rewrites a request
// before storing it, and reconcileStream compares the desired config against
// what STREAM.INFO returns. Emitting the pre-rewrite shape means the comparison
// can never converge — that is exactly how MaxMsgsPerSubject 0 (stored as -1)
// made three streams issue an UpdateStream on every startup and every reconnect
// forever.
func streamConfig(spec StreamSpec) jsapi.StreamConfig {
	duplicateWindow := 2 * time.Minute
	if spec.Duplicates > 0 {
		duplicateWindow = spec.Duplicates
	}
	if spec.MaxAge > 0 && spec.MaxAge < duplicateWindow {
		// NATS rejects a duplicate window longer than the stream's MaxAge.
		duplicateWindow = spec.MaxAge
	}
	// Zero value is jsapi.FileStorage, so a spec that states file storage is
	// documenting its tier rather than changing anything; only memory is an
	// actual opt-in.
	storage := jsapi.FileStorage
	if spec.Storage != 0 {
		storage = jsapi.MemoryStorage
	}
	return serverNormalized(jsapi.StreamConfig{
		Name:     spec.Name,
		Subjects: spec.Subjects,
		Storage:  storage,
		// StreamSpec keeps the legacy nats.* enums so callers outside pkg/bus
		// keep spelling nats.WorkQueuePolicy; both packages encode the same
		// protocol integers, and stream_config_test pins that equivalence.
		Retention:         jsapi.RetentionPolicy(spec.Retention),
		Discard:           jsapi.DiscardOld,
		MaxAge:            spec.MaxAge,
		MaxBytes:          spec.MaxBytes,
		MaxMsgsPerSubject: spec.MaxMsgsPer,
		// Zero replicas becomes the safe single copy in serverNormalized; a spec
		// opts into RAFT replication explicitly. Every catalog stream does — the
		// default exists for ad-hoc specs in tests and tooling, not for the fleet.
		Replicas:   spec.Replicas,
		Duplicates: duplicateWindow,
		Placement:  placement(spec.PlacementTags),
		// Both batch wires are ordinary config fields here, which is the point of
		// running the reconcile on the modern API: they converge in the same
		// UpdateStream as everything else instead of being cleared by a legacy
		// update and re-added by a second pass. That window used to destroy every
		// staged atomic batch and open fast session on the stream.
		AllowAtomicPublish: spec.BatchPublish,
		AllowBatchPublish:  spec.BatchPublish,
		// Message scheduling drags AllowMsgTTL and AllowRollup with it; see the
		// StreamSpec field. Stating them here rather than letting the broker
		// coerce them keeps the stored config equal to the requested one.
		AllowMsgSchedules: spec.MsgSchedules,
		AllowMsgTTL:       spec.MsgSchedules,
	})
}

// serverNormalized mirrors nats-server 2.14.3's checkStreamCfg
// (server/stream.go:1625-2245): the rewrites the broker applies to a create or
// update request *before* storing it. Every field reconcileStream compares must
// be in the shape the server will hand back from STREAM.INFO, or the reconcile
// sees permanent drift and updates forever.
//
// It is idempotent by construction (each rule only fires on the un-normalised
// value), so applying it to a live config read back from the broker is a no-op —
// which is what lets the provision tests build a realistic "live" stream out of
// the same helper.
func serverNormalized(cfg jsapi.StreamConfig) jsapi.StreamConfig {
	// Unlimited is -1 on the wire, never 0: the server rewrites each of these
	// and stores the sentinel. The client's own JSON tags send the zero value
	// explicitly, so "unset" is never "leave it alone".
	cfg.MaxMsgs = unlimitedIfUnset(cfg.MaxMsgs)
	cfg.MaxMsgsPerSubject = unlimitedIfUnset(cfg.MaxMsgsPerSubject)
	cfg.MaxBytes = unlimitedIfUnset(cfg.MaxBytes)
	cfg.MaxMsgSize = int32(unlimitedIfUnset(int64(cfg.MaxMsgSize)))
	cfg.MaxConsumers = int(unlimitedIfUnset(int64(cfg.MaxConsumers)))
	if cfg.Replicas <= 0 {
		cfg.Replicas = 1
	}
	cfg.Duplicates = dedupWindow(cfg.Duplicates, cfg.MaxAge)
	if cfg.AllowMsgSchedules {
		// The server forces both on a scheduling stream: schedule rows are
		// replaced with Nats-Rollup: sub, and a fired one-shot purges its own
		// subject.
		cfg.AllowRollup, cfg.DenyPurge = true, false
	}
	cfg.Placement = normalizedPlacement(cfg.Placement)
	return cfg
}

// unlimitedIfUnset maps the "no limit" spellings a client may send onto the -1
// the server stores. Anything below -1 is normalised the same way.
func unlimitedIfUnset(value int64) int64 {
	if value == 0 || value < -1 {
		return -1
	}
	return value
}

// dedupWindow reproduces the server's duplicate-window defaulting: an unset
// window becomes the 2m default, clamped to MaxAge when the stream retains less
// than that.
func dedupWindow(window, maxAge time.Duration) time.Duration {
	if window != 0 {
		return window
	}
	if maxAge != 0 && maxAge < 2*time.Minute {
		return maxAge
	}
	return 2 * time.Minute
}

// placement maps the spec's tags to the NATS shape. No tags means a nil
// Placement, which is also how a live stream's stored placement is cleared:
// jetstream.StreamConfig.Placement is `json:",omitempty"`, so nil is omitted
// from the update request, and nats-server 2.14.3 unmarshals each update into a
// fresh config and stores that config wholesale (jsClusteredStreamUpdateRequest
// builds the new stream assignment from it) — nothing carries the old placement
// forward. Sending an empty &jsapi.Placement{} would be equivalent, because
// checkStreamCfg normalises an empty placement back to nil before the update is
// proposed; nil is the honest spelling, so that is what we send.
func placement(tags []string) *jsapi.Placement {
	if len(tags) == 0 {
		return nil
	}
	return &jsapi.Placement{Tags: slices.Clone(tags)}
}

// normalizedPlacement drops an empty placement object the same way the server
// does, so an empty struct and nil compare equal instead of drifting forever.
func normalizedPlacement(p *jsapi.Placement) *jsapi.Placement {
	if placementConstrainsNothing(p) {
		return nil
	}
	return p
}

// placementConstrainsNothing reports the placements the server stores as nil: a
// missing one, and one that names neither a cluster nor a tag. Both leave the
// stream free to be assigned anywhere, so both must normalise to the same value
// or samePlacement sees drift that no update can close.
func placementConstrainsNothing(p *jsapi.Placement) bool {
	if p == nil {
		return true
	}
	return p.Cluster == "" && len(p.Tags) == 0
}

func streamMatches(got, want jsapi.StreamConfig) bool {
	return sameStreamLimits(got, want) &&
		sameStreamFeatures(got, want) &&
		sameSubjects(got.Subjects, want.Subjects) &&
		samePlacement(got.Placement, want.Placement)
}

// sameStreamLimits compares the capacity contract. Replicas is updatable in
// place, so a drift here must trigger a reconcile (UpdateStream scales the
// stream); omitting it lets a live R1 stream stay R1 while the spec declares R3.
func sameStreamLimits(got, want jsapi.StreamConfig) bool {
	return got.Retention == want.Retention &&
		got.MaxAge == want.MaxAge &&
		got.MaxBytes == want.MaxBytes &&
		got.MaxMsgsPerSubject == want.MaxMsgsPerSubject &&
		got.Replicas == want.Replicas &&
		got.Duplicates == want.Duplicates
}

// sameStreamFeatures compares the 2.14 capability flags. They are only
// comparable because the whole reconcile runs on the modern API: the legacy
// JetStreamManager config cannot carry them, so a legacy update silently
// cleared allow_atomic/allow_batched and the comparison could never see it.
func sameStreamFeatures(got, want jsapi.StreamConfig) bool {
	return got.AllowAtomicPublish == want.AllowAtomicPublish &&
		got.AllowBatchPublish == want.AllowBatchPublish &&
		got.AllowMsgSchedules == want.AllowMsgSchedules &&
		got.AllowMsgTTL == want.AllowMsgTTL
}

// samePlacement compares placements in both directions on purpose. The
// interesting case today is a live stream that still carries the old ordinal
// placement tag against a catalog that declares none: without the nil-vs-set
// arm, reconcileStream would see no drift and the stale tag would keep
// constraining the stream forever.
func samePlacement(a, b *jsapi.Placement) bool {
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
