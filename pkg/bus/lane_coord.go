// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

// The pull lane durable dropped to a single replica (pull_consumer.go)
// because replicated ack state costs a raft proposal per delivered message,
// which is the one cost this lane shape exists to avoid. R1 buys that back at
// the price R3 used to hide: a peer holding the durable can go down without
// the consumer ever reporting gone, so the fetch loop's only trigger — the
// durable no longer resolving — never fires, and the lane fetches into a
// black hole for as long as that peer stays down.
//
// This file is the fix: a small NATS KV bucket ("lane_coord") every pod in
// the fleet can reach, used for exactly two things —
//
//   - a floor checkpoint, so a rebuilt durable resumes where the dead one left
//     off instead of replaying the retained window or, on DeliverNew, silently
//     skipping whatever the fleet had not yet acked;
//   - a create-if-absent lock, so a fleet of pods that all notice the same
//     dead durable at once do not all delete-and-recreate it concurrently,
//     each one racing the last one's create.
//
// Every operation here degrades to the pre-existing lock-free rebuild
// (pull_consumer.go's rebuildConsumer) the instant the bucket cannot be
// trusted: never provisioned, a denied ACL (which nats.go surfaces as a
// context deadline, not a permissions error, so a timeout is treated
// identically to an outright failure), a partition, all of it. KV is
// coordination, never correctness, and nothing here may block delivery.
const (
	// laneCoordBucket is the one fleet-wide coordination bucket. Not a knob:
	// every pod must agree on this name for the lock and floor keys below to
	// mean anything across pods.
	laneCoordBucket = "lane_coord"
	// laneFloorPrefix and laneRebuildPrefix key the two purposes this bucket
	// exists for (see above) inside that one bucket, per durable name, so
	// unrelated lanes never contend on each other's lock or floor.
	laneFloorPrefix   = "floor."
	laneRebuildPrefix = "rebuild."

	defaultPullProbeTimeout = time.Second
	// defaultPullLockStale must clear laneReplaceTimeout (pull_consumer.go,
	// currently 20s): floorAwareRebind's delete+recreate runs under that one
	// shared budget, not two separate ones, so a lock stolen before the
	// budget can even be spent would pull it out from under a winner that is
	// still legitimately working. 30s leaves margin above the full 20s.
	defaultPullLockStale = 30 * time.Second

	laneCoordProvisionTimeout = 5 * time.Second
	// laneCoordOpTimeout bounds every ordinary Get/Put/Create/Update/Delete.
	// Short on purpose: these calls run inline on the ticker and the
	// fetch-error path, and a slow bucket must degrade fast rather than hold
	// either one open.
	laneCoordOpTimeout = 2 * time.Second
)

func laneFloorKey(name string) string   { return laneFloorPrefix + name }
func laneRebuildKey(name string) string { return laneRebuildPrefix + name }

// pullProbeTimeout bounds the INFO call noteFetchError uses to tell a
// peer-down R1 consumer apart from an ordinary transient error. It has to
// stay well under maxWait's retry cadence or the probe itself becomes the
// spin this mechanism exists to avoid.
func pullProbeTimeout() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_PROBE_TIMEOUT", defaultPullProbeTimeout), defaultPullProbeTimeout)
}

// pullLockStale is how old a held rebuild lock has to be before another pod
// will steal it. It has to clear an ordinary rebuild's own round trip — the
// delete and the create both run under floorAwareRebind's single
// laneReplaceTimeout budget (pull_consumer.go), not a budget each — with room
// to spare, or a pod still legitimately working would lose its own lock
// mid-rebuild.
func pullLockStale() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_LOCK_STALE", defaultPullLockStale), defaultPullLockStale)
}

// laneCoordinator is the slice of the KV API the rebuild lock and floor
// checkpoint use. Narrow for the same reason pullConsumerProvisioner is: it
// keeps the coordination tests honest about which calls this mechanism may
// make, and it lets a test drive a lock race or a floor read without a
// broker.
type laneCoordinator interface {
	Get(ctx context.Context, key string) (jsapi.KeyValueEntry, error)
	Put(ctx context.Context, key string, value []byte) (uint64, error)
	Create(ctx context.Context, key string, value []byte) (uint64, error)
	Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error)
	// Delete is always revision-checked: releaseRebuildLock's whole point is
	// to refuse deleting a lock this pod no longer owns (see there), so the
	// interface offers no unconditional delete to reach for by mistake.
	Delete(ctx context.Context, key string, revision uint64) error
}

// realLaneCoordinator adapts jetstream.KeyValue to laneCoordinator. The
// adapter exists only because KeyValue.Create carries a variadic options
// parameter this mechanism never uses, which keeps *kvs from satisfying the
// narrower interface directly.
type realLaneCoordinator struct{ kv jsapi.KeyValue }

func (r realLaneCoordinator) Get(ctx context.Context, key string) (jsapi.KeyValueEntry, error) {
	return r.kv.Get(ctx, key)
}

func (r realLaneCoordinator) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	return r.kv.Put(ctx, key, value)
}

func (r realLaneCoordinator) Create(ctx context.Context, key string, value []byte) (uint64, error) {
	return r.kv.Create(ctx, key, value)
}

func (r realLaneCoordinator) Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error) {
	return r.kv.Update(ctx, key, value, revision)
}

func (r realLaneCoordinator) Delete(ctx context.Context, key string, revision uint64) error {
	return r.kv.Delete(ctx, key, jsapi.LastRevision(revision))
}

// laneCoordProvisioner is the slice of the KV manager the lazy bucket
// provision uses.
type laneCoordProvisioner interface {
	CreateKeyValue(ctx context.Context, cfg jsapi.KeyValueConfig) (jsapi.KeyValue, error)
	KeyValue(ctx context.Context, bucket string) (jsapi.KeyValue, error)
}

func laneCoordConfig() jsapi.KeyValueConfig {
	return jsapi.KeyValueConfig{
		Bucket:      laneCoordBucket,
		Description: "ItsBagelBot pull-lane rebuild coordination: floor checkpoints and rebuild locks",
		History:     1,
		Replicas:    3,
		Storage:     jsapi.MemoryStorage,
	}
}

// ensureLaneCoord provisions the bucket idempotently and binds a handle to
// it, once, at pull-subscriber construction — never at package init, so a
// broker that is not reachable yet at import time cannot fail a process that
// has not even tried to connect.
//
// Every failure returns nil instead of an error the caller must branch on:
// every call site already treats a nil coordinator as "run lock-free," which
// is what this whole mechanism degrades to the instant it cannot be trusted.
func ensureLaneCoord(nc *nats.Conn, log *zap.Logger) laneCoordinator {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		log.Warn("lane coordination bucket unavailable; rebuilds fall back to lock-free", zap.Error(err))
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), laneCoordProvisionTimeout)
	defer cancel()
	kv, err := provisionLaneCoordBucket(ctx, js, laneCoordConfig())
	if err != nil {
		log.Warn("lane coordination bucket unavailable; rebuilds fall back to lock-free", zap.Error(err))
		return nil
	}
	return realLaneCoordinator{kv: kv}
}

// provisionLaneCoordBucket is create-only, tolerant of another pod having
// already won the create — the same pattern streamReconciler.create uses for
// the stream catalog (provision.go). A denied create surfaces as a context
// deadline (see the package doc's ACL note above), which propagates here like
// any other provisioning failure; the caller degrades the same way regardless
// of which.
func provisionLaneCoordBucket(ctx context.Context, js laneCoordProvisioner, cfg jsapi.KeyValueConfig) (jsapi.KeyValue, error) {
	kv, err := js.CreateKeyValue(ctx, cfg)
	if err == nil {
		return kv, nil
	}
	if errors.Is(err, jsapi.ErrBucketExists) {
		return js.KeyValue(ctx, cfg.Bucket)
	}
	return nil, err
}

// recordAckedSequence captures the stream sequence advanceFloor has just
// confirmed acked, for checkpointFloor to persist. It runs on the ack cadence
// (once per batch or once per tick, pull_consumer.go), never per delivered
// message, so the extra Metadata() call here costs nothing the hot path was
// built to avoid.
//
// The caller only reaches this after wire.Ack() returned nil — never before,
// and never on an ack error. Ack is fire-and-forget over the wire, so a
// failure means the broker may never have seen it; checkpointing that
// sequence would let a rebuild resume past a message the fleet never
// actually acknowledged, which is a skip rather than the redelivery this
// whole mechanism is allowed to cost.
func (s *pullSubscriber) recordAckedSequence(wire jsapi.Msg) {
	metadata, err := wire.Metadata()
	if err != nil || metadata.Sequence.Stream == 0 {
		return
	}
	s.ackedStreamSeq.Store(metadata.Sequence.Stream)
}

// checkpointFloor persists the newest acked stream sequence to the
// coordination bucket, but only when it moved since the last write: a Put is
// a raft proposal on the bucket's own R3 stream, and writing one every tick
// whether or not the floor advanced would reintroduce, at tick cadence
// instead of message cadence, the exact per-proposal cost the parent change
// removes from the hot path. Called only from advanceFloorPeriodically's
// ticker goroutine — see the field doc on pullSubscriber.checkpointedSeq.
func (s *pullSubscriber) checkpointFloor() {
	if s.coord == nil {
		return
	}
	seq := s.ackedStreamSeq.Load()
	if seq == 0 || seq == s.checkpointedSeq.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), laneCoordOpTimeout)
	defer cancel()
	value := []byte(strconv.FormatUint(seq, 10))
	if _, err := s.coord.Put(ctx, laneFloorKey(s.name), value); err != nil {
		s.noteCheckpointErr(err)
		return
	}
	s.checkpointedSeq.Store(seq)
}

func (s *pullSubscriber) noteCheckpointErr(err error) {
	if count := s.checkpointErrs.Add(1); count == 1 || count%1_000 == 0 {
		s.log.Warn("lane floor checkpoint failed",
			zap.String("subject", s.subject),
			zap.Int64("checkpoint_errors", count),
			zap.Error(err))
	}
}

// consumerUnavailable decides whether a fetch failure justifies a rebuild.
// consumerGone (pull_consumer.go) is unambiguous and never needs the probe
// below. Everything else is only worth probing while the client itself has a
// live connection — with the transport down the probe would just fail on
// that, telling the caller nothing the fetch error had not already said, and
// a rebuild attempted over a dead connection cannot succeed anyway.
func (s *pullSubscriber) consumerUnavailable(err error) bool {
	if consumerGone(err) {
		return true
	}
	if s.connected == nil || !s.connected() {
		return false
	}
	return s.probeConsumerGone()
}

// probeConsumerGone is the R1-era gap this file closes: a peer holding the
// durable can go down without the durable ever reporting missing to
// ConsumerNotFound/Deleted, so an ordinary fetch just times out forever. A
// bounded INFO call tells that apart from an election or a slow responder
// that is still going to answer — a probe success means the fetch failure was
// transient and the existing pause/retry already covers it.
func (s *pullSubscriber) probeConsumerGone() bool {
	ctx, cancel := context.WithTimeout(context.Background(), pullProbeTimeout())
	defer cancel()
	_, err := s.consumer.Info(ctx)
	if err == nil {
		return false
	}
	return consumerGone(err) || errors.Is(err, context.DeadlineExceeded)
}

// coordinatedRebuild is noteFetchError's entry point in place of a direct
// rebuildConsumer call. Without it, every pod that notices the same gone
// durable in the same cycle deletes and recreates it independently, each one
// racing the last one's create — the exact stampede a fleet-wide durable
// cannot afford under load.
//
// The lock lives in the coordination bucket rather than on the durable
// itself: the durable is precisely what may be missing or unreachable, so
// there is nowhere on it to stake a claim. Every branch that cannot reach the
// bucket falls through to the pre-existing lock-free rebuild — coordination
// only ever adds safety on top of that fallback, never gates it.
func (s *pullSubscriber) coordinatedRebuild() {
	if s.coord == nil {
		s.rebuildConsumer()
		return
	}
	lock := s.acquireRebuildLock()
	switch lock.result {
	case rebuildLockWon:
		s.floorAwareRebuild()
		s.releaseRebuildLock(lock.revision)
	case rebuildLockUnavailable:
		s.rebuildConsumer()
	case rebuildLockLost:
		// Another pod already owns this cycle's rebuild; the ordinary pause
		// noteFetchError already takes is this pod's wait.
	}
}

// rebuildLockResult is acquireRebuildLock's verdict.
type rebuildLockResult int

const (
	rebuildLockWon rebuildLockResult = iota
	rebuildLockLost
	rebuildLockUnavailable
)

// rebuildLockAcquisition pairs the verdict with the revision this pod's own
// Create/Update landed at (zero when it did not win). releaseRebuildLock
// needs that exact revision: a plain unconditional delete would just as
// happily delete a lock a different pod has since won by stealing this one,
// see there.
type rebuildLockAcquisition struct {
	result   rebuildLockResult
	revision uint64
}

// acquireRebuildLock is the create-if-absent lock with a staleness steal.
// Create is atomic on the broker, so exactly one racing pod ever sees
// rebuildLockWon for a given cycle.
func (s *pullSubscriber) acquireRebuildLock() rebuildLockAcquisition {
	ctx, cancel := context.WithTimeout(context.Background(), laneCoordOpTimeout)
	defer cancel()
	key := laneRebuildKey(s.name)
	rev, err := s.coord.Create(ctx, key, rebuildLockValue())
	if err == nil {
		return rebuildLockAcquisition{result: rebuildLockWon, revision: rev}
	}
	if !errors.Is(err, jsapi.ErrKeyExists) {
		return rebuildLockAcquisition{result: rebuildLockUnavailable}
	}
	return s.tryStealRebuildLock(ctx, key)
}

// tryStealRebuildLock reads the held lock and steals it via a CAS update once
// it is older than pullLockStale — old enough that its winner most likely
// crashed mid-rebuild rather than still being at work. Losing the CAS means
// another pod's steal (or the true owner's own retry) landed first; that pod
// owns this cycle now.
func (s *pullSubscriber) tryStealRebuildLock(ctx context.Context, key string) rebuildLockAcquisition {
	entry, err := s.coord.Get(ctx, key)
	if err != nil {
		return rebuildLockAcquisition{result: rebuildLockUnavailable}
	}
	if !rebuildLockStale(entry.Value()) {
		return rebuildLockAcquisition{result: rebuildLockLost}
	}
	rev, err := s.coord.Update(ctx, key, rebuildLockValue(), entry.Revision())
	if err != nil {
		return rebuildLockAcquisition{result: rebuildLockLost}
	}
	return rebuildLockAcquisition{result: rebuildLockWon, revision: rev}
}

// releaseRebuildLock always fires after a winner's rebuild attempt, success
// or failure: holding the lock past that point only delays the next pod that
// notices the same failure, and the stale-steal above already bounds the
// blast radius of never releasing it at all.
//
// The delete is revision-checked against the exact Create/Update this pod's
// own acquireRebuildLock landed at — never a bare delete-by-key. A rebuild
// slow enough to cross pullLockStale can have its lock stolen by another pod
// mid-work; an unconditional delete issued after that pod's own rebuild
// would delete THEIR lock, reopening it to a third pod while the second is
// still working. Losing this CAS means exactly that happened, and the
// correct response is to leave the (someone else's) lock alone.
func (s *pullSubscriber) releaseRebuildLock(revision uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), laneCoordOpTimeout)
	defer cancel()
	_ = s.coord.Delete(ctx, laneRebuildKey(s.name), revision)
}

// rebuildLockValue stamps this pod's identity and the instant it took the
// lock. The identity is diagnostic only; the instant is what rebuildLockStale
// judges a stuck winner by.
func rebuildLockValue() []byte {
	return []byte(podIdentity() + " " + strconv.FormatInt(time.Now().UnixNano(), 10))
}

// rebuildLockStale reports whether a held lock is old enough to steal. A
// value this mechanism did not write (wrong shape, an unparsable instant) is
// treated as stale rather than as a permanent block: a foreign value here
// means the key was never in this mechanism's exclusive care to begin with,
// and stale-and-stealable is the safer read of that than stale-and-stuck.
func rebuildLockStale(value []byte) bool {
	fields := strings.Fields(string(value))
	if len(fields) != 2 {
		return true
	}
	took, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return true
	}
	return time.Since(time.Unix(0, took)) > pullLockStale()
}

// floorAwareRebuild is the lock winner's rebuild. Unlike rebuildConsumer's
// lock-free path (pull_consumer.go, always DeliverNew), it resumes from the
// fleet's floor checkpoint when one exists, which is the entire reason a
// winner is worth electing instead of every pod just racing rebuildConsumer.
func (s *pullSubscriber) floorAwareRebuild() {
	s.takePending()
	consumer, err := s.floorRebind()
	s.completeRebuild(consumer, err)
}

// floorAwareRebind performs the delete+recreate directly, reusing
// replacePullConsumer (pull_consumer.go) rather than bindPullConsumer's
// update-first path: that path exists to convert a push consumer left
// occupying the name, and it echoes whatever DeliverPolicy/OptStartSeq the
// occupant currently has — which for a rebuild is precisely the dead
// consumer's own position, the one thing rebuildDesiredConfig exists to
// override.
func (s *pullSubscriber) floorAwareRebind() (jsapi.Consumer, error) {
	js, err := jsapi.NewWithDomain(s.nc, JSDomain())
	if err != nil {
		return nil, fmt.Errorf("bus: modern jetstream context: %w", err)
	}
	return replacePullConsumer(js, s.stream, s.rebuildDesiredConfig(), errors.New("bus: probe-triggered rebuild"))
}

// rebuildDesiredConfig resumes the rebuilt durable from the fleet's floor
// checkpoint instead of the dead consumer's own position: that position
// belongs to the consumer this rebuild exists because it can no longer be
// trusted, and inheriting it would either replay the whole retained window or,
// on DeliverNew, silently skip whatever the fleet had not yet acked.
func (s *pullSubscriber) rebuildDesiredConfig() jsapi.ConsumerConfig {
	desired := s.desired
	floor, ok := s.readFloorCheckpoint()
	if !ok {
		desired.DeliverPolicy = jsapi.DeliverNewPolicy
		desired.OptStartSeq = 0
		return desired
	}
	desired.DeliverPolicy = jsapi.DeliverByStartSequencePolicy
	desired.OptStartSeq = floor + 1
	return desired
}

func (s *pullSubscriber) readFloorCheckpoint() (uint64, bool) {
	if s.coord == nil {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), laneCoordOpTimeout)
	defer cancel()
	entry, err := s.coord.Get(ctx, laneFloorKey(s.name))
	if err != nil {
		return 0, false
	}
	seq, err := strconv.ParseUint(string(entry.Value()), 10, 64)
	if err != nil {
		return 0, false
	}
	return seq, true
}
