package bus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"

	"go.uber.org/zap"
)

// JetStream streams are part of the broker's contract, not something a client
// should invent from a subject on the fly. The fleet declares its streams in
// the catalog (streams.go) and reconciles them idempotently at startup here: a
// fresh deployment provisions its own streams, and a drifted one converges,
// with no out-of-band ops step.

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
// publishers and durable consumers against the restored stream. Every replica
// of a stream's declared owner calls this function; reconciliation is
// idempotent and creation races resolve to success. Non-owners must not call it,
// because their credentials intentionally have no stream-management rights.
func EnsureStreams(ctx context.Context, url string, specs []StreamSpec, log *zap.Logger) error {
	var js nats.JetStreamManager
	var batchJS jsapi.JetStream

	reconcileAll := func() {
		for _, spec := range specs {
			if err := reconcileStream(js, spec, log); err != nil {
				log.Warn("jetstream stream reconcile failed; will retry on next reconnect",
					zap.String("stream", spec.Name), zap.Error(err))
				continue
			}
			if err := reconcileBatchFeatures(batchJS, spec); err != nil {
				log.Warn("jetstream batch feature reconcile failed; will retry on next reconnect",
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
	batchJS, err = jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		nc.Close()
		return fmt.Errorf("bus: modern jetstream context: %w", err)
	}

	// Initial provisioning is synchronous and fatal: the service must not start
	// serving if its streams could not be established.
	for _, spec := range specs {
		if err := reconcileStream(js, spec, log); err != nil {
			nc.Close()
			return err
		}
		if err := reconcileBatchFeatures(batchJS, spec); err != nil {
			nc.Close()
			return fmt.Errorf("bus: enable batch publishing on %q: %w", spec.Name, err)
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

// reconcileBatchFeatures uses nats.go's current JetStream API because the
// legacy JetStreamManager StreamConfig intentionally stopped growing before
// AllowAtomicPublish/AllowBatchPublish were added. Keeping the ordinary stream
// reconciler in place avoids a risky consumer/provisioning migration while this
// narrow second pass makes the NATS 2.14 capability authoritative.
func reconcileBatchFeatures(js jsapi.JetStream, spec StreamSpec) error {
	if !spec.BatchPublish {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := js.Stream(ctx, spec.Name)
	if err != nil {
		return err
	}
	info := stream.CachedInfo()
	if info == nil {
		return errors.New("stream info was not cached")
	}
	config := info.Config
	if config.AllowAtomicPublish && config.AllowBatchPublish {
		return nil
	}
	config.AllowAtomicPublish = true
	config.AllowBatchPublish = true
	_, err = js.UpdateStream(ctx, config)
	return err
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
		if info.Config.Storage != desired.Storage {
			// Storage type is fixed at creation; NATS rejects changing it via
			// UpdateStream. Converting a live stream (e.g. one created before its
			// spec moved to memory) means a delete+recreate in a maintenance
			// window, which drops whatever it currently holds — safe for these
			// perishable streams but disruptive enough that it is a deliberate
			// operator step, not something the guardian does under traffic. Warn
			// so the drift is visible; keep serving the existing stream.
			log.Warn("jetstream stream storage differs from spec; manual recreate required to converge",
				zap.String("stream", spec.Name),
				zap.String("current", info.Config.Storage.String()),
				zap.String("desired", desired.Storage.String()),
			)
		}

		if streamMatches(info.Config, *desired) {
			return nil
		}

		// NATS cannot update a stream to or from work-queue retention. Runtime
		// credentials deliberately have no STREAM.DELETE permission, so this
		// destructive migration must be performed explicitly by an operator.
		if info.Config.Retention != desired.Retention &&
			(info.Config.Retention == nats.WorkQueuePolicy || desired.Retention == nats.WorkQueuePolicy) {
			return fmt.Errorf(
				"bus: stream %q retention change from %s to %s requires an operator-managed delete/recreate; runtime credentials cannot delete streams",
				spec.Name, info.Config.Retention, desired.Retention,
			)
		}

		// Never attempt to flip storage in place — NATS rejects it, which would
		// wedge this reconcile on every reconnect. Converge every other drifted
		// field against the stream's existing storage; the drift warning above
		// covers the storage difference until it is recreated by hand.
		update := *desired
		update.Storage = info.Config.Storage

		if _, err := js.UpdateStream(&update); err != nil {
			// Work-queue replacement used to happen automatically here. Keep the
			// destructive fallback operator-only so a leaked runtime credential
			// cannot erase the stream it owns.
			if desired.Retention == nats.WorkQueuePolicy {
				return fmt.Errorf(
					"bus: update work-queue stream %q (operator-managed delete/recreate required; runtime credentials cannot delete streams): %w",
					spec.Name, err,
				)
			}
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
