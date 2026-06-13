package bus

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/nats-io/nats.go"

	"go.uber.org/zap"
)

// JetStream streams are part of the broker's contract, not something a client
// should invent on the fly. watermill's AutoProvision names a stream after the
// (dotted) topic, which JetStream rejects, so it can never stand a stream up in
// production. Instead the fleet declares the streams it depends on here and
// reconciles them idempotently at startup: a fresh deployment provisions its
// own streams, and a drifted one converges, with no out-of-band ops step.

// StreamSpec is the desired state of one JetStream stream. It is intentionally
// small: the operational knobs that matter for the shared HeatWave-sized
// broker (retention window and a hard size cap) are explicit, the rest take
// safe defaults in reconcileStream.
type StreamSpec struct {
	Name     string        // valid JetStream stream name (no dots/spaces/wildcards)
	Subjects []string      // subjects captured by the stream
	MaxAge   time.Duration // how long messages are retained for replay/rebuild
	MaxBytes int64         // hard cap so one stream cannot exhaust the instance
}

// DataStreams backs the event bus (the data.> plane from ADR 0007): user,
// command, module and transaction change events plus reprojection requests all
// land in one limits-retention stream that the per-service durable consumers
// and the projector read from. One stream keeps the broker surface small; the
// durable consumers created by the subscribers do the per-subject fan-out.
var DataStreams = []StreamSpec{
	{
		Name:     "BAGEL_DATA",
		Subjects: []string{"data.>"},
		MaxAge:   7 * 24 * time.Hour,
		MaxBytes: 512 << 20, // 512 MiB
	},
	{
		Name:     "TWITCH_INGRESS",
		Subjects: []string{"twitch.ingress.event.>", "twitch.ingress.status.>"},
		MaxAge:   24 * time.Hour,
		MaxBytes: 256 << 20, // 256 MiB
	},
	{
		Name:     "TWITCH_OUTGRESS",
		Subjects: []string{"twitch.outgress.>"},
		MaxAge:   24 * time.Hour,
		MaxBytes: 256 << 20, // 256 MiB
	},
}

// EnsureStreams keeps the declared streams provisioned for the lifetime of the
// process, so the fleet self-heals when the broker restarts. It holds a NATS
// connection open until ctx is cancelled and reconciles the specs:
//
//   - once, synchronously, before returning — a failure here is fatal at
//     startup, because the service cannot run without its streams;
//   - again on every reconnect — if the broker restarted with empty JetStream
//     storage (or the streams were deleted), they are recreated automatically.
//
// The NATS client's own infinite reconnect (see options) then re-establishes
// publishers and durable consumers against the restored stream. It is safe to
// call from every instance of every service: reconciliation is idempotent and
// creation races resolve to success.
func EnsureStreams(ctx context.Context, url string, specs []StreamSpec, log *zap.Logger) error {
	var js nats.JetStreamManager

	reconcileAll := func() {
		for _, spec := range specs {
			if err := reconcileStream(js, spec, log); err != nil {
				log.Warn("jetstream stream reconcile failed; will retry on next reconnect",
					zap.String("stream", spec.Name), zap.Error(err))
			}
		}
	}

	opts := append(options("stream-guardian"),
		nats.ReconnectHandler(func(*nats.Conn) {
			log.Info("nats reconnected; re-provisioning jetstream streams")
			reconcileAll()
		}),
	)

	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return fmt.Errorf("bus: connect for provisioning: %w", err)
	}

	js, err = nc.JetStream()
	if err != nil {
		nc.Close()
		return fmt.Errorf("bus: jetstream context: %w", err)
	}

	// Initial provisioning is synchronous and fatal: the service must not start
	// serving if its streams could not be established.
	for _, spec := range specs {
		if err := reconcileStream(js, spec, log); err != nil {
			nc.Close()
			return err
		}
	}

	// Hold the connection open so the reconnect handler keeps firing, and
	// release it when the service shuts down.
	go func() {
		<-ctx.Done()
		nc.Close()
	}()

	return nil
}

func reconcileStream(js nats.JetStreamManager, spec StreamSpec, log *zap.Logger) error {
	desired := &nats.StreamConfig{
		Name:       spec.Name,
		Subjects:   spec.Subjects,
		Storage:    nats.FileStorage,
		Retention:  nats.LimitsPolicy,
		Discard:    nats.DiscardOld,
		MaxAge:     spec.MaxAge,
		MaxBytes:   spec.MaxBytes,
		Replicas:   1,
		Duplicates: 2 * time.Minute,
	}

	info, err := js.StreamInfo(spec.Name)
	switch {
	case err == nil:
		// Stream exists: converge only if the captured subjects drifted, so a
		// new subject domain added to the spec rolls out on the next deploy.
		if sameSubjects(info.Config.Subjects, spec.Subjects) {
			return nil
		}
		if _, err := js.UpdateStream(desired); err != nil {
			return fmt.Errorf("bus: update stream %q: %w", spec.Name, err)
		}
		log.Info("converged jetstream stream",
			zap.String("stream", spec.Name),
			zap.Strings("subjects", spec.Subjects),
		)
		return nil

	case errors.Is(err, nats.ErrStreamNotFound):
		if _, err := js.AddStream(desired); err != nil {
			// Another instance won the race between StreamInfo and AddStream.
			if errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
				return nil
			}
			return fmt.Errorf("bus: create stream %q: %w", spec.Name, err)
		}
		log.Info("provisioned jetstream stream",
			zap.String("stream", spec.Name),
			zap.Strings("subjects", spec.Subjects),
		)
		return nil

	default:
		return fmt.Errorf("bus: inspect stream %q: %w", spec.Name, err)
	}
}

func sameSubjects(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
