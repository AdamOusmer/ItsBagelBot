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
	Storage: nats.MemoryStorage,
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
	Name:      "TWITCH_OUTGRESS_SYSTEM",
	Subjects:  []string{"twitch.outgress.system"},
	Retention: nats.WorkQueuePolicy,
	MaxAge:    5 * time.Minute,
	MaxBytes:  64 << 20, // 64 MiB: control jobs are small and low-volume
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
		// Memory-backed: the stream is perishable (a replay window that never
		// needs to survive a restart), so memory storage drops the per-event disk
		// write that capped synchronous PubAck throughput to a few thousand
		// events/second. Keep it R1: R3 RAFT consensus is leader-bound and, with a
		// replica on the 2-core node1, that voter would gate the whole cluster; a
		// lost leader drops only in-flight perishable chat. Requires the server
		// max_mem headroom in nats-server.conf.
		//
		// MaxBytes is 1 GiB so the memory-backed stream fits the broker's 2GB
		// max_mem (capped by node1) alongside TWITCH_OUTGRESS and dedup state.
		// MaxAge is moot under load: MaxBytes (stream-wide, oldest-first) evicts
		// first, and 1 GiB is the consumer lag budget in bytes (~6 s at 100k/s).
		// Raising toward the 150-200k target means larger MaxBytes, which needs
		// the broker off node1 and on >=8GiB nodes first.
		Storage:  nats.MemoryStorage,
		MaxBytes: 1 << 30, // 1 GiB
		// The premium, standard and stream lanes are distinct literal subjects
		// sharing this stream, and MaxBytes eviction is stream-wide oldest-first:
		// without a per-subject cap a standard-lane flood fills the stream and
		// evicts retained premium and stream.online events. 400k messages per
		// lane makes a flooded lane wrap itself while the other lanes keep their
		// retention (and stays within the 1 GiB stream cap).
		MaxMsgsPer: 400_000,
		// Ingress publishes carry Nats-Msg-Id (derived from Twitch's message id)
		// so publish retries and Twitch's own EventSub redeliveries collapse at
		// the broker. Both happen within seconds; 10s covers them while bounding
		// the broker's dedup-id state on the firehose — at 200k/s a 30s window
		// would track ~6M ids, a 10s window ~2M.
		Duplicates: 10 * time.Second,
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

		// Never attempt to flip storage in place — NATS rejects it, which would
		// wedge this reconcile on every reconnect. Converge every other drifted
		// field against the stream's existing storage; the drift warning above
		// covers the storage difference until it is recreated by hand.
		update := *desired
		update.Storage = info.Config.Storage

		if _, err := js.UpdateStream(&update); err != nil {
			// A work-queue stream is perishable, so replacing it is safe when an
			// in-place update is rejected — notably narrowing subjects out from
			// under an existing consumer's filter (the one-time migration that
			// splits the control lane onto its own stream). No replay to preserve;
			// the consumers are re-created against the converged stream.
			if desired.Retention == nats.WorkQueuePolicy {
				log.Warn("work-queue stream update rejected; replacing",
					zap.String("stream", spec.Name), zap.Error(err))
				if derr := js.DeleteStream(spec.Name); derr != nil && !errors.Is(derr, nats.ErrStreamNotFound) {
					return fmt.Errorf("bus: replace stream %q after failed update: %w", spec.Name, derr)
				}
				return add()
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
	return &nats.StreamConfig{
		Name:              spec.Name,
		Subjects:          spec.Subjects,
		Storage:           storage,
		Retention:         spec.Retention,
		Discard:           nats.DiscardOld,
		MaxAge:            spec.MaxAge,
		MaxBytes:          spec.MaxBytes,
		MaxMsgsPerSubject: spec.MaxMsgsPer,
		Replicas:          1,
		Duplicates:        duplicateWindow,
	}
}

func streamMatches(got, want nats.StreamConfig) bool {
	return sameSubjects(got.Subjects, want.Subjects) &&
		got.Retention == want.Retention &&
		got.MaxAge == want.MaxAge &&
		got.MaxBytes == want.MaxBytes &&
		got.MaxMsgsPerSubject == want.MaxMsgsPerSubject &&
		got.Duplicates == want.Duplicates
}

func sameSubjects(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
