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
// should invent from a subject on the fly. The fleet declares its streams here
// and reconciles them idempotently at startup: a fresh deployment provisions
// its own streams, and a drifted one converges, with no out-of-band ops step.

// StreamSpec is the desired state of one JetStream stream. It is intentionally
// small: the operational knobs that matter for the shared HeatWave-sized
// broker (retention window and a hard size cap) are explicit, the rest take
// safe defaults in reconcileStream.
type StreamSpec struct {
	Name       string               // valid JetStream stream name (no dots/spaces/wildcards)
	Subjects   []string             // subjects captured by the stream
	Retention  nats.RetentionPolicy // zero value is the ordinary limits policy
	MaxAge     time.Duration        // hard lifetime limit for stored messages
	MaxBytes   int64                // hard cap so one stream cannot exhaust the instance
	MaxMsgsPer int64                // per-subject cap (0 = unlimited); lane isolation on shared streams
	// Duplicates overrides the Nats-Msg-Id dedup window (0 = the 2m default,
	// clamped to MaxAge). The broker tracks one id per message inside the
	// window, so a high-rate stream wants it as short as its producers' retry
	// horizon, not the default.
	Duplicates time.Duration
	// Storage selects the backing store. The zero value is nats.FileStorage
	// (on disk). A transient, size-capped, short-retention stream should use
	// nats.MemoryStorage: the per-message disk write (and, for replicas, the
	// synchronous consensus flush) is the dominant publish-side cost, so a
	// perishable firehose that never needs to survive a broker restart is far
	// cheaper in memory. Storage is fixed at creation — see reconcileStream.
	Storage nats.StorageType
	// BatchPublish enables both reliable atomic microbatches and NATS 2.14
	// flow-controlled fast-ingest batches. All fleet-owned streams opt in so
	// every pkg/bus publisher benefits; RPC subjects are Core NATS and never
	// enter these streams.
	BatchPublish bool
	// Replicas is the RAFT replication factor (0 defaults to 1). Unlike Storage,
	// replica count IS updatable in place, so reconcileStream converges a drifted
	// stream via UpdateStream — this is the field streamMatches must compare, or a
	// live stream hand-edited to R3 sticks at R3 forever while the spec says R1.
	// R1 is the throughput choice for the perishable firehose: R3 makes every
	// publish wait on a RAFT quorum before its PubAck, which is pure ack-latency
	// inflation on an ack-bound producer. Reserve R3 for control/data streams
	// whose loss on a single broker restart actually matters.
	Replicas int
	// PlacementTags constrain JetStream replicas to servers carrying every tag.
	// We use the stable StatefulSet ordinal tag for R1 streams so the hot
	// firehoses never land on worker1's WAN-connected peer after a restart.
	// R3 streams cannot use an ordinal placement tag because all three peers are
	// required; their preferred leader is managed operationally.
	PlacementTags []string
}

// OutgressStream carries the perishable chat lanes (premium/standard). It is
// owned and reconciled by outgress itself; keeping it out of DataStreams
// prevents every producer replica from racing the one-time limits-to-work-queue
// migration. The control lane (twitch.outgress.system) is deliberately NOT here
// — it lives on OutgressSystemStream with a longer lifetime; see that spec.
var OutgressStream = StreamSpec{
	Name:      "TWITCH_OUTGRESS",
	Subjects:  []string{"twitch.outgress.premium", "twitch.outgress.standard"},
	Retention: nats.WorkQueuePolicy,
	// Chat sends are perishable work, not an event log. ACK/TERM removes them
	// immediately; this 5s ceiling also drops a message that outlived its
	// usefulness (a chat line older than the retry budget must never be sent
	// late) and removes an orphan if no consumer is available during a rollout.
	MaxAge:   5 * time.Second,
	MaxBytes: 256 << 20, // 256 MiB
	// A 5s work queue never outlives a broker restart, so paying disk I/O per
	// send is pure overhead. Memory-backed removes the write bottleneck; the
	// 256 MiB MaxBytes caps the memory it can hold.
	Storage:      nats.MemoryStorage,
	BatchPublish: true,
	// R1: perishable 5s chat work. A dropped in-flight send is re-driven by the
	// pipeline; RAFT replication would only add ack latency to the send path.
	Replicas:      1,
	PlacementTags: []string{"nats-1"},
}

// OutgressSystemStream carries the outgress control lane: EventSub enroll
// (enable/disable/reconnect) jobs and stream_status live re-checks. Unlike chat
// these are control-plane work that MUST survive until acknowledged — an enroll
// silently dropped on the floor leaves a channel un-ingested with nobody the
// wiser. It stays a work-queue (ACK removes the message, so this is
// acknowledgment, not a replayable log) but with a generous MaxAge so a job
// published during a rollout gap, or nacked on a transient infra error, is
// retried instead of purged at the chat lane's 5s. Same subject namespace as the
// chat lanes, so producers and the NATS ACLs are unchanged; only the stream that
// captures twitch.outgress.system differs.
var OutgressSystemStream = StreamSpec{
	Name:         "TWITCH_OUTGRESS_SYSTEM",
	Subjects:     []string{"twitch.outgress.system"},
	Retention:    nats.WorkQueuePolicy,
	MaxAge:       5 * time.Minute,
	MaxBytes:     64 << 20, // 64 MiB: control jobs are small and low-volume
	BatchPublish: true,
	// R3, unlike the chat lanes: an EventSub enroll/disable or stream re-check
	// silently lost on a broker restart leaves a channel un-ingested with nobody
	// the wiser. This lane is low-volume, so the RAFT cost is negligible and the
	// durability is worth it. This is the one stream that stays replicated.
	Replicas: 3,
}

// BagelDataStream is the replayable application-data event bus. The users
// service owns its stream reconciliation; other services only publish to it or
// manage their own consumers. Keeping the owner explicit lets the broker ACL
// grant STREAM.CREATE/UPDATE to one credential instead of every BUS user.
var BagelDataStream = StreamSpec{
	Name:         "BAGEL_DATA",
	Subjects:     []string{"data.>"},
	MaxAge:       5 * time.Minute,
	MaxBytes:     512 << 20, // 512 MiB
	BatchPublish: true,
	// R1: a low-rate, 5-minute replay buffer. A broker restart drops at most a
	// few minutes of change events, which the projector re-derives from the
	// data services' RPC projections — not worth a per-publish RAFT quorum.
	Replicas:      1,
	PlacementTags: []string{"nats-1"},
}

// TwitchIngressStream is the replayable Twitch ingress firehose. Sesame owns
// its stream reconciliation because it is the primary lane consumer; ingress
// itself only publishes captured subjects and needs no JetStream API access.
var TwitchIngressStream = StreamSpec{
	Name:     "TWITCH_INGRESS",
	Subjects: []string{"twitch.ingress.event.>", "twitch.ingress.status.>"},
	MaxAge:   5 * time.Minute,
	// Memory-backed: the stream is perishable (a replay window that never
	// needs to survive a restart), so memory storage drops the per-event disk
	// write that capped synchronous PubAck throughput to a few thousand
	// events/second. Requires the server max_mem headroom in nats-server.conf.
	Storage: nats.MemoryStorage,
	// R1 (explicit, and now enforced by streamMatches). The firehose producer
	// is async PubAck-bound — its ceiling is max_pending / ack_latency — so R3
	// RAFT consensus per publish is pure ack-latency inflation. A lost leader
	// drops only in-flight perishable chat (5s/5m retention), the accepted
	// trade for the throughput. Every hub node (node2/node3/worker1) is
	// capable, so replication buys nothing here that offsets the latency cost.
	Replicas:      1,
	PlacementTags: []string{"nats-0"},
	// MaxBytes is 1 GiB so the memory-backed stream fits the broker's 4GB
	// max_mem alongside TWITCH_OUTGRESS and dedup state. MaxAge is moot under
	// load: MaxBytes (stream-wide, oldest-first) evicts first, and 1 GiB is the
	// consumer lag budget in bytes (~6s at 100k/s, ~4s at 150k/s). Raising
	// toward the 150-200k target means larger MaxBytes + more max_mem.
	MaxBytes: 1 << 30, // 1 GiB
	// The premium, standard and stream lanes are distinct literal subjects
	// sharing this stream, and MaxBytes eviction is stream-wide oldest-first:
	// without a per-subject cap a standard-lane flood fills the stream and
	// evicts retained premium and stream.online events. 400k messages per
	// lane makes a flooded lane wrap itself while the other lanes keep their
	// retention (and stays within the 1 GiB stream cap).
	MaxMsgsPer: 400_000,
	// The dedup window only applies to messages that carry a Nats-Msg-Id.
	// Production ingress runs INGRESS_PUBLISH_DEDUP=off (the per-message
	// dedup insert measured ~27% of single-stream ingest capacity, and
	// EventSub websockets never redeliver), so lane events are unindexed
	// and this window costs nothing for them. It stays at 10s to bound
	// dedup state for any id-carrying publisher on these subjects — at
	// 200k/s a 30s window would track ~6M ids, a 10s window ~2M.
	Duplicates:   10 * time.Second,
	BatchPublish: true,
}

// DataStreams is the complete replayable stream catalog. It remains available
// to tests and operator tooling; runtime services reconcile only the named
// stream they own above. Outgress commands are deliberately excluded because
// they are perishable work, not event history.
var DataStreams = []StreamSpec{BagelDataStream, TwitchIngressStream}

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
