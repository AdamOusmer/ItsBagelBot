package bus

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	Name      string               // valid JetStream stream name (no dots/spaces/wildcards)
	Subjects  []string             // subjects captured by the stream
	Retention nats.RetentionPolicy // zero value is the ordinary limits policy
	MaxAge    time.Duration        // hard lifetime limit for stored messages
	MaxBytes  int64                // hard cap so one stream cannot exhaust the instance
}

// OutgressStream is owned and reconciled by outgress itself. Keeping it out of
// DataStreams prevents every producer replica from racing the one-time
// limits-to-work-queue migration.
var OutgressStream = StreamSpec{
	Name:      "TWITCH_OUTGRESS",
	Subjects:  []string{"twitch.outgress.>"},
	Retention: nats.WorkQueuePolicy,
	// Outgress commands are perishable work, not an event log. ACK/TERM removes
	// them immediately; this ceiling also removes an orphan if no consumer is
	// available during a rollout.
	MaxAge:   5 * time.Second,
	MaxBytes: 256 << 20, // 256 MiB
}

// DataStreams backs the replayable event bus: user, command, module and
// transaction change events plus Twitch ingress events. Outgress commands are
// deliberately excluded because they are perishable work, not event history.
var DataStreams = []StreamSpec{
	{
		Name:     "BAGEL_DATA",
		Subjects: []string{"data.>"},
		MaxAge:   5 * time.Minute,
		MaxBytes: 512 << 20, // 512 MiB
	},
	{
		Name:     "TWITCH_INGRESS",
		Subjects: []string{"twitch.ingress.event.>", "twitch.ingress.status.>"},
		MaxAge:   5 * time.Minute,
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

	opts := append(busOptions("stream-guardian"),
		nats.ReconnectHandler(func(*nats.Conn) {
			log.Info("nats reconnected; re-provisioning jetstream streams")
			reconcileAll()
		}),
	)

	nc, err := nats.Connect(busURL(url), opts...)
	if err != nil {
		return fmt.Errorf("bus: connect for provisioning: %w", err)
	}

	// Dialed at the leaf, so the stream API must target the hub domain.
	js, err = nc.JetStream(jsDomainOption()...)
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
	desired := streamConfig(spec)

	add := func() error {
		if _, err := js.AddStream(desired); err != nil {
			// Another guardian won the create race.
			if errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
				return nil
			}
			return fmt.Errorf("bus: create stream %q: %w", spec.Name, err)
		}
		log.Info("provisioned jetstream stream",
			zap.String("stream", spec.Name),
			zap.Strings("subjects", spec.Subjects),
			zap.String("retention", desired.Retention.String()),
		)
		return nil
	}

	info, err := js.StreamInfo(spec.Name)
	switch {
	case err == nil:
		if streamMatches(info.Config, *desired) {
			return nil
		}

		// NATS cannot update a stream to or from work-queue retention. This
		// one-time migration intentionally purges the old replay log: outgress
		// commands that were already sent must never be replayed.
		if info.Config.Retention != desired.Retention &&
			(info.Config.Retention == nats.WorkQueuePolicy || desired.Retention == nats.WorkQueuePolicy) {
			if err := js.DeleteStream(spec.Name); err != nil && !errors.Is(err, nats.ErrStreamNotFound) {
				return fmt.Errorf("bus: replace stream %q: %w", spec.Name, err)
			}
			return add()
		}

		if _, err := js.UpdateStream(desired); err != nil {
			return fmt.Errorf("bus: update stream %q: %w", spec.Name, err)
		}
		log.Info("converged jetstream stream",
			zap.String("stream", spec.Name),
			zap.Strings("subjects", spec.Subjects),
			zap.String("retention", desired.Retention.String()),
		)
		return nil

	case errors.Is(err, nats.ErrStreamNotFound):
		return add()

	default:
		return fmt.Errorf("bus: inspect stream %q: %w", spec.Name, err)
	}
}

func streamConfig(spec StreamSpec) *nats.StreamConfig {
	duplicateWindow := 2 * time.Minute
	if spec.MaxAge > 0 && spec.MaxAge < duplicateWindow {
		// NATS rejects a duplicate window longer than the stream's MaxAge.
		duplicateWindow = spec.MaxAge
	}
	return &nats.StreamConfig{
		Name:       spec.Name,
		Subjects:   spec.Subjects,
		Storage:    nats.FileStorage,
		Retention:  spec.Retention,
		Discard:    nats.DiscardOld,
		MaxAge:     spec.MaxAge,
		MaxBytes:   spec.MaxBytes,
		Replicas:   1,
		Duplicates: duplicateWindow,
	}
}

func streamMatches(got, want nats.StreamConfig) bool {
	return sameSubjects(got.Subjects, want.Subjects) &&
		got.Retention == want.Retention &&
		got.MaxAge == want.MaxAge &&
		got.MaxBytes == want.MaxBytes
}

func sameSubjects(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
