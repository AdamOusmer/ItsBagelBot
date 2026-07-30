package bus

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
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
//
// The whole reconcile runs on nats.go's current `jetstream` API, not the legacy
// JetStreamManager. That is not a style choice: the legacy StreamConfig stopped
// growing before allow_atomic / allow_batched / allow_msg_schedules existed, so
// a legacy UpdateStream is a full-replace that *clears* them — and clearing
// allow_atomic makes the server destroy every staged atomic batch and open fast
// session on the stream (deleteAtomicBatches/deleteFastBatches), with no
// advisory and no reply to the publishers waiting on them. One API, one update,
// no window where a converging stream has batching switched off.

const (
	// streamReconcileTimeout bounds one stream's INFO + UPDATE round trip. The
	// reconcile that matters most runs when every owner reconnects at the same
	// instant after a hub roll, while the meta leader is still being elected, so
	// the budget is deliberately several times the JetStream API default.
	streamReconcileTimeout = 20 * time.Second
	// streamReconcileRetry is how soon a failed reconcile is retried. Without it
	// the only retry is the next reconnect, which may be hours away: a stream
	// left un-converged is a stream whose declared capabilities (batch publish,
	// message schedules) the publishers are already assuming.
	streamReconcileRetry = 30 * time.Second
)

// EnsureStreams keeps the declared streams provisioned for the lifetime of the
// process, so the fleet self-heals when the broker restarts. It holds a NATS
// connection open until ctx is cancelled and reconciles the specs:
//
//   - once, synchronously, before returning — a failure here is fatal at
//     startup, because the service cannot run without its streams;
//   - again on every reconnect — if the broker restarted with empty JetStream
//     storage (or the streams were deleted), they are recreated automatically;
//   - and on a retry timer while any stream is still un-converged.
//
// The NATS client's own infinite reconnect (see options) then re-establishes
// publishers and durable consumers against the restored stream. Every replica
// of a stream's declared owner calls this function; reconciliation is
// idempotent and creation races resolve to success. Non-owners must not call it,
// because their credentials intentionally have no stream-management rights.
func EnsureStreams(ctx context.Context, url string, specs []StreamSpec, log *zap.Logger) error {
	guardian := &streamGuardian{specs: specs, log: log}

	nc, err := nats.Connect(busURL(url), busOptions("stream-guardian")...)
	if err != nil {
		return fmt.Errorf("bus: connect for provisioning: %w", err)
	}

	// Dialed at the leaf, so the stream API must target the hub domain.
	guardian.js, err = jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		nc.Close()
		return fmt.Errorf("bus: jetstream context: %w", err)
	}

	// Initial provisioning is synchronous and fatal: the service must not start
	// serving if its streams could not be established.
	for _, spec := range specs {
		if err := guardian.reconcile(ctx, spec); err != nil {
			nc.Close()
			return err
		}
	}

	// The reconnect handler is installed here rather than as a connect option on
	// purpose: RetryOnFailedConnect means Connect can return while the client is
	// still dialling, so a handler registered up front can fire on the client's
	// goroutine before guardian.js exists.
	nc.SetReconnectHandler(func(*nats.Conn) {
		log.Info("nats reconnected; re-provisioning jetstream streams")
		guardian.reconcileAll(ctx)
	})

	go guardian.retryUntilConverged(ctx)

	// Hold the connection open so the reconnect handler keeps firing, and
	// release it when the service shuts down.
	go func() {
		<-ctx.Done()
		nc.Close()
	}()

	return nil
}

// streamGuardian owns the reconcile loop for one service's streams. The dirty
// flag is the whole retry state: any failed spec marks it, and the retry timer
// re-runs the full (idempotent) pass until nothing is left drifted.
type streamGuardian struct {
	js    streamProvisioner
	specs []StreamSpec
	log   *zap.Logger
	dirty atomic.Bool
}

// streamProvisioner is the slice of the modern JetStream API the reconciler
// uses. Narrow on purpose: it keeps the provisioning tests honest about which
// calls a converge is allowed to make, and it makes the absence of a delete
// path structural rather than a promise in a comment.
type streamProvisioner interface {
	Stream(ctx context.Context, name string) (jsapi.Stream, error)
	CreateStream(ctx context.Context, cfg jsapi.StreamConfig) (jsapi.Stream, error)
	UpdateStream(ctx context.Context, cfg jsapi.StreamConfig) (jsapi.Stream, error)
}

func (g *streamGuardian) reconcileAll(ctx context.Context) {
	for _, spec := range g.specs {
		if err := g.reconcile(ctx, spec); err != nil {
			// Error, not Warn: an un-converged stream may be missing the batch
			// or scheduling capability its publishers already depend on, and the
			// only thing standing between that and a silent outage is this line.
			g.log.Error("jetstream stream reconcile failed; retrying",
				zap.String("stream", spec.Name),
				zap.Duration("retry_in", streamReconcileRetry),
				zap.Error(err))
			g.dirty.Store(true)
		}
	}
}

// reconcile converges one spec under a bounded child of the caller's context,
// so a shutdown cancels an in-flight reconcile instead of holding it open for
// its own private timeout.
func (g *streamGuardian) reconcile(ctx context.Context, spec StreamSpec) error {
	ctx, cancel := context.WithTimeout(ctx, streamReconcileTimeout)
	defer cancel()
	return reconcileStream(ctx, g.js, spec, g.log)
}

// retryUntilConverged re-runs the pass while anything is still drifted. It is a
// ticker rather than a per-failure goroutine so overlapping failures collapse
// into one retry, and it exits with the service.
func (g *streamGuardian) retryUntilConverged(ctx context.Context) {
	ticker := time.NewTicker(streamReconcileRetry)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if g.dirty.Swap(false) {
				g.reconcileAll(ctx)
			}
		}
	}
}

func reconcileStream(ctx context.Context, js streamProvisioner, spec StreamSpec, log *zap.Logger) error {
	desired := streamConfig(spec)

	stream, err := js.Stream(ctx, spec.Name)
	switch {
	case err == nil:
		info := stream.CachedInfo()
		if info == nil {
			return fmt.Errorf("bus: inspect stream %q: no cached configuration", spec.Name)
		}
		return convergeStream(ctx, js, spec, streamShape{live: info.Config, desired: desired}, log)

	case errors.Is(err, jsapi.ErrStreamNotFound):
		return createStream(ctx, js, spec, desired, log)

	default:
		return fmt.Errorf("bus: inspect stream %q: %w", spec.Name, err)
	}
}

// streamShape pairs the broker's stored config with the one the catalog wants,
// so the converge helpers take one argument instead of two easily-swapped ones.
type streamShape struct {
	live    jsapi.StreamConfig
	desired jsapi.StreamConfig
}

func createStream(
	ctx context.Context,
	js streamProvisioner,
	spec StreamSpec,
	desired jsapi.StreamConfig,
	log *zap.Logger,
) error {
	if _, err := js.CreateStream(ctx, desired); err != nil {
		// Another guardian won the create race.
		if errors.Is(err, jsapi.ErrStreamNameAlreadyInUse) {
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

func convergeStream(
	ctx context.Context,
	js streamProvisioner,
	spec StreamSpec,
	shape streamShape,
	log *zap.Logger,
) error {
	warnStorageDrift(spec, shape, log)

	// Everything converges in one update, including the two changes the R3
	// catalog introduces on a live broker: the replica bump (the meta leader
	// selects the additional peers and scales the RAFT group in place) and
	// clearing the old ordinal placement tag. Those two are only compatible in
	// this direction — the server rejects a *move* combined with a replica
	// change (JSStreamMoveAndScale), and a nil placement is not a move, so
	// dropping the tag while scaling up is accepted. Adding a tag back would
	// have to be its own reconcile pass; see PlacementTags in the catalog.
	update := effectiveDesired(shape)

	if streamMatches(shape.live, update) {
		return nil
	}

	if err := checkRetentionMigration(spec, shape); err != nil {
		return err
	}

	if _, err := js.UpdateStream(ctx, update); err != nil {
		// Work-queue replacement used to happen automatically here. Keep the
		// destructive fallback operator-only so a leaked runtime credential
		// cannot erase the stream it owns.
		if shape.desired.Retention == jsapi.WorkQueuePolicy {
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
		zap.String("retention", shape.desired.Retention.String()),
	)
	return nil
}

// warnStorageDrift reports a storage tier the guardian cannot converge. Storage
// type is fixed at creation; NATS rejects changing it via UpdateStream.
// Converting a live stream (e.g. one created before its spec moved to memory)
// means a delete+recreate in a maintenance window, which drops whatever it
// currently holds — safe for these perishable streams but disruptive enough that
// it is a deliberate operator step, not something the guardian does under
// traffic. Warn so the drift is visible; keep serving the existing stream.
func warnStorageDrift(spec StreamSpec, shape streamShape, log *zap.Logger) {
	if shape.live.Storage == shape.desired.Storage {
		return
	}
	log.Warn("jetstream stream storage differs from spec; manual recreate required to converge",
		zap.String("stream", spec.Name),
		zap.String("current", shape.live.Storage.String()),
		zap.String("desired", shape.desired.Storage.String()),
	)
}

// effectiveDesired folds in the fields a live stream will not let an update
// change away from, so the request the guardian sends is one the server can
// actually accept — and one whose result equals what it asked for, or the next
// reconcile sees the same drift again and updates forever.
//
// Three one-way doors, all enforced by configUpdateCheck:
//   - storage is immutable (see warnStorageDrift);
//   - message TTLs cannot be disabled once enabled ("message TTL status can not
//     be disabled");
//   - message schedules cannot be disabled once enabled ("message schedules can
//     not be disabled"), and a scheduling stream forces rollups on.
//
// Enabling any of them stays a normal catalog edit; only the removal is refused,
// and a spec that drops one is asking for a delete+recreate an operator has to
// perform.
func effectiveDesired(shape streamShape) jsapi.StreamConfig {
	desired := shape.desired
	desired.Storage = shape.live.Storage
	desired.AllowMsgTTL = desired.AllowMsgTTL || shape.live.AllowMsgTTL
	desired.AllowMsgSchedules = desired.AllowMsgSchedules || shape.live.AllowMsgSchedules
	return serverNormalized(desired)
}

// checkRetentionMigration refuses the one drift this reconciler cannot perform.
// NATS cannot update a stream to or from work-queue retention. Runtime
// credentials deliberately have no STREAM.DELETE permission, so this
// destructive migration must be performed explicitly by an operator.
func checkRetentionMigration(spec StreamSpec, shape streamShape) error {
	if shape.live.Retention == shape.desired.Retention {
		return nil
	}
	if shape.live.Retention != jsapi.WorkQueuePolicy && shape.desired.Retention != jsapi.WorkQueuePolicy {
		return nil
	}
	return fmt.Errorf(
		"bus: stream %q retention change from %s to %s requires an operator-managed delete/recreate; runtime credentials cannot delete streams",
		spec.Name, shape.live.Retention, shape.desired.Retention,
	)
}
