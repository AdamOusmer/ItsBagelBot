package bus

import (
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
)

// This file is the fleet's stream catalog: the single authority on which
// JetStream streams exist, what subjects they capture and how they are tuned.
// The reconcile machinery that converges a broker to this catalog lives in
// provision.go; the subscribers that bind to these streams live in bus.go.
// Adding or re-tuning a stream is an edit here only.

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
	MaxMsgsPer int64                // per-subject cap (0 = unlimited, sent as the server's -1); lane isolation on shared streams
	// Duplicates overrides the Nats-Msg-Id dedup window (0 = the 2m default,
	// clamped to MaxAge). The broker tracks one id per message inside the
	// window, so a high-rate stream wants it as short as its producers' retry
	// horizon, not the default.
	Duplicates time.Duration
	// Storage selects the backing store and splits the catalog into two tiers.
	// The zero value is nats.FileStorage (on disk); specs that want it state it
	// explicitly, because "which tier is this" is the first question an operator
	// asks about a stream.
	//
	//   - nats.MemoryStorage is the firehose tier: a size-capped, short-retention
	//     lane whose contents are worthless once they age out. The per-message
	//     disk write is the dominant publish-side cost, so keeping it out of the
	//     filesystem is what makes six-figure ingest rates reachable. Memory is
	//     not the same as unreplicated: an R3 memory stream keeps a full copy on
	//     every hub peer and a restarted peer re-syncs its copy from the leader,
	//     so only losing the whole quorum at once loses the window.
	//   - nats.FileStorage is the retention tier: anything whose loss is silent
	//     (a control job nobody re-issues, a replay window a consumer resumes
	//     into) pays disk so a full-quorum restart is survivable.
	//
	// Storage is fixed at creation — see reconcileStream.
	Storage nats.StorageType
	// BatchPublish enables both reliable atomic microbatches and NATS 2.14
	// flow-controlled fast-ingest batches. All fleet-owned streams opt in so
	// every pkg/bus publisher benefits; RPC subjects are Core NATS and never
	// enter these streams.
	//
	// Both flags ride the same UpdateStream as every other field (see
	// reconcileStream), so a converge can never leave a stream with batching
	// half-disabled.
	BatchPublish bool
	// MsgSchedules enables server-side message scheduling (allow_msg_schedules,
	// NATS 2.14 / API level 2): a publish carrying Nats-Schedule is stored as a
	// schedule row, and the server emits the payload to Nats-Schedule-Target
	// when it fires. It is the delay primitive the retry lane is built on, so
	// nothing in the fleet has to hold a timer for a message it already handed
	// to the broker.
	//
	// It carries two server-imposed companions, both set explicitly by
	// streamConfig rather than left to the broker's silent coercion:
	//
	//   - AllowMsgTTL, because a non-empty Nats-Schedule-TTL (the per-retry
	//     lifetime on the *emitted* message) is rejected with the TTL-disabled
	//     error when message TTLs are off;
	//   - AllowRollup (and DenyPurge cleared), because the server forces both on
	//     any scheduling stream — a schedule row is replaced via Nats-Rollup:
	//     sub, and a one-shot schedule purges its own subject after it fires.
	//
	// Discard must stay DiscardOld (the server rejects scheduling with discard
	// new), which is what streamConfig emits for every stream.
	MsgSchedules bool
	// Replicas is the RAFT replication factor (0 defaults to 1). Unlike Storage,
	// replica count IS updatable in place, so reconcileStream converges a drifted
	// stream via UpdateStream — this is the field streamMatches must compare, or a
	// live stream left at R1 sticks at R1 forever while the spec declares R3.
	//
	// Every fleet stream is R3. The hub is a three-peer quorum, so R3 is the only
	// factor that survives losing one peer; an R1 stream's sole copy disappears
	// with the peer that held it, taking every unconsumed message and the
	// consumers bound to it. R1 used to be the throughput choice because R3 makes
	// each publish wait on a quorum before its PubAck — that cost is now
	// amortized, not paid per message: the ADR-050 atomic wire and the NATS 2.14
	// fast-ingest wire commit a whole batch behind one quorum round-trip, and
	// AckFlowControl consumers take the matching cost off the ack path.
	Replicas int
	// PlacementTags constrain JetStream replicas to servers carrying every tag.
	// The catalog deliberately sets none. Hub tags are per-pod ordinals
	// (server_tags: $POD_NAME in nats-server.conf), and an ordinal tag exists on
	// exactly one server, so an R3 stream constrained to it is unsatisfiable —
	// the meta leader cannot find three peers carrying a one-peer tag. Preferred
	// leadership is an operator action (a step-down), not a spec field.
	//
	// The field stays because "no tags" is also how a live stream's stored
	// placement is cleared: streamConfig emits a nil Placement, and nats-server
	// 2.14.3 stores the update's config wholesale, so nil replaces whatever was
	// there. One ordering constraint if tags are ever reintroduced: the server
	// treats a non-nil placement that differs from the stored one as a move, and
	// rejects a move and a replica change in the same update
	// (JSStreamMoveAndScale). Never add a tag in the same edit that touches
	// Replicas.
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
	MaxAge: 5 * time.Second,
	// 256 MiB is also the per-node memory this stream can occupy: under R3 every
	// hub peer holds a full replica, so the budget against the broker's 4GB
	// max_mem is 1 GiB (the TWITCH_INGRESS lane pair, which splits that gigabyte
	// rather than adding to it) + 256 MiB here ≈ 1.3 GiB on every member, not on
	// one. See nats-server.conf.
	MaxBytes: 256 << 20, // 256 MiB
	// A 5s work queue never outlives a broker restart, so paying disk I/O per
	// send is pure overhead. Memory-backed removes the write bottleneck; the
	// 256 MiB MaxBytes caps the memory it can hold.
	Storage:      nats.MemoryStorage,
	BatchPublish: true,
	// R3: perishable is not the same as expendable. A single-copy chat lane
	// vanishes with its peer, dropping every queued send and stalling the
	// consumers bound to it until the stream is re-created; R3 keeps the lane
	// serving through any one-peer loss, and a restarted peer re-syncs its
	// memory copy from the leader. The quorum round-trip is paid per batch by
	// the atomic/fast-ingest wires, not per send. No placement: an ordinal tag
	// cannot satisfy three peers (see PlacementTags).
	Replicas: 3,
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
	// Retention tier, explicitly on disk. The chat lanes next door are memory
	// because their contents are worthless in five seconds; a control job is the
	// opposite — an EventSub enroll/disable or stream re-check lost to a
	// full-quorum restart leaves a channel un-ingested with nobody the wiser.
	// Low volume, so the disk write is not on any hot path.
	Storage:      nats.FileStorage,
	BatchPublish: true,
	// R3, like every fleet stream: a silently-lost control job has no observer
	// to re-drive it, so a copy on each peer is the cheapest insurance the hub
	// offers at this volume.
	Replicas: 3,
}

// BagelDataStream is the replayable application-data event bus. The users
// service owns its stream reconciliation; other services only publish to it or
// manage their own consumers. Keeping the owner explicit lets the broker ACL
// grant STREAM.CREATE/UPDATE to one credential instead of every BUS user.
var BagelDataStream = StreamSpec{
	Name:     "BAGEL_DATA",
	Subjects: []string{"data.>"},
	MaxAge:   5 * time.Minute,
	MaxBytes: 512 << 20, // 512 MiB
	// Durable replay tier, explicitly on disk: this is a replay buffer, and a
	// buffer that empties on a restart turns every consumer's resume point into
	// a gap the consumer cannot see it has. The projector can re-derive from the
	// data services' RPC projections, but only if something tells it to. Low
	// rate, so disk costs nothing that matters here. Under R3 each peer keeps
	// 512 MiB of this stream on disk, well inside the 2GB max_file.
	Storage:      nats.FileStorage,
	BatchPublish: true,
	// R3: a full copy on every hub peer, so losing one peer neither empties the
	// replay window nor stalls the publishers bound to it.
	Replicas: 3,
}

// TwitchIngressStream is the replayable Twitch ingress firehose, minus the
// standard lane: that lane is partitioned onto TwitchIngressStandardStream, and
// this stream keeps premium, the stream (live) lane and the status subjects.
// Sesame owns the reconciliation of both because it is the primary lane
// consumer; ingress itself only publishes captured subjects and needs no
// JetStream API access.
var TwitchIngressStream = StreamSpec{
	Name: "TWITCH_INGRESS",
	// Enumerated rather than twitch.ingress.event.>, and that is forced rather
	// than chosen: two streams may not claim the same subject
	// (JSStreamSubjectOverlap), so the wildcard cannot survive the standard lane
	// moving out from under it. The cost is that an unforeseen
	// twitch.ingress.event.* subject is now captured by NOTHING rather than
	// landing here — a lane subject renamed through NATS_SUBJECT_LANE_* without
	// the matching catalog edit publishes into no stream at all. Adding a lane
	// means adding it here (or to a partition of its own).
	Subjects: []string{
		"twitch.ingress.event.premium",
		"twitch.ingress.event.stream",
		"twitch.ingress.status.>",
	},
	// Chat is live or it is nothing. This was 5 minutes, and on 2026-07-27 that
	// window is what turned a recovery into a chat flood: ingress kept publishing
	// while sesame crashlooped on a JetStream quorum loss, five minutes of events
	// accumulated, and sesame replied to every one of them the moment it came
	// back. Nothing in the bus distinguishes an event that arrived two seconds
	// ago from one that arrived four minutes ago, so the retention window IS the
	// staleness policy.
	//
	// 10s is a delivery hiccup, not a replay buffer. A consumer that misses more
	// than that has missed the conversation, and answering late is worse than not
	// answering. The durable replay guarantees stay on BAGEL_DATA, which is where
	// state that must survive an outage actually lives.
	//
	// TWO CONSEQUENCES, both deliberate:
	//
	//  1. twitch.ingress.status.> shares this stream, so authorization grants and
	//     revocations now also expire after 10s. If outgress is down longer than
	//     that, a revocation is dropped rather than applied late. The revocation
	//     path re-derives state on the next grant (see the EventSub revocation
	//     handling), so this degrades rather than corrupts. Splitting the status
	//     subjects into their own longer-lived stream is the clean fix and is
	//     worth doing before this window is trusted for anything but chat.
	//  2. streamConfig clamps the dedup window to MaxAge, which narrows it from
	//     30s to 10s. This costs nothing here: ingress is the only publisher to
	//     these subjects and it deliberately attaches no Nats-Msg-Id (see
	//     app/ingress/lib/ingress/nats.ex — EventSub websocket delivery is not
	//     replayed, so the broker-side dedup index was pure overhead and was
	//     measured at ~27% of per-message cost). There is no dedup to lose.
	MaxAge: 10 * time.Second,
	// Memory-backed: the stream is perishable (a replay window that never needs
	// to survive a restart), so memory storage drops the per-event disk write
	// that capped synchronous PubAck throughput to a few thousand events/second.
	// Requires the server max_mem headroom in nats-server.conf.
	Storage: nats.MemoryStorage,
	// R3 (explicit, and enforced by streamMatches). Memory-backed and replicated
	// are independent axes, and this stream wants both: memory keeps the
	// per-event disk write off the ingest path, R3 keeps a full copy on all
	// three hub peers so losing one neither empties the replay window nor
	// strands the lane consumers. A restarted peer re-syncs its memory copy from
	// the leader; only a simultaneous full-quorum loss drops the window.
	//
	// The per-publish quorum latency that made R1 the throughput choice is gone
	// from the per-message path: the ADR-050 atomic wire and the NATS 2.14
	// fast-ingest wire commit a whole batch behind one quorum round-trip. What
	// remains is the R3 single-stream ceiling — one serialized RAFT proposal
	// loop per stream, measured end to end at ~98k msg/s on this hub. That
	// ceiling is per STREAM, not per cluster, which is the whole reason
	// TwitchIngressStandardStream exists: two streams elect two leaders and run
	// two loops, live-measured at ~171.7k msg/s across the pair.
	//
	// No placement: an R3 stream needs all three peers, and the hub's tags are
	// per-pod ordinals that exist on one server each (see PlacementTags).
	Replicas: 3,
	// MaxBytes is 384 MiB, and under R3 that is 384 MiB on *every* hub peer, not
	// on one: each member holds a full replica. It was 1 GiB before the standard
	// lane was partitioned out, and the partition SPLITS that gigabyte rather
	// than adding to it — 384 MiB here plus 640 MiB on
	// TwitchIngressStandardStream — so the memory-backed budget against the
	// broker's 4GB max_mem is unchanged at 1 GiB of ingress + 256 MiB
	// (TWITCH_OUTGRESS) ≈ 1.3 GiB per node. See the max_mem block in
	// nats-server.conf. MaxAge is moot under load: MaxBytes (stream-wide,
	// oldest-first) evicts first, so this is the consumer lag budget in bytes.
	//
	// The 3:5 share against the standard lane follows the traffic. What is left
	// here is premium (premium broadcasters plus the special IDs) and two
	// trickles — the stream lane carries only stream.online/offline, and the
	// status subjects carry authorization lifecycle events.
	MaxBytes: 384 << 20, // 384 MiB
	// The premium, stream and status subjects share this stream, and MaxBytes
	// eviction is stream-wide oldest-first: without a per-subject cap a
	// premium-lane flood fills the stream and evicts retained stream.online and
	// authz events. The cap stays at 400k messages, which at the ~865 B measured
	// on the wire per event is ~330 MiB — still under the 384 MiB stream cap, so
	// a flooded premium lane wraps itself before it can evict its neighbours.
	// That inequality (MaxMsgsPer × wire bytes < MaxBytes) is the invariant this
	// field exists for; re-check it whenever either number moves.
	MaxMsgsPer: 400_000,
	// The dedup window only applies to messages that carry a Nats-Msg-Id.
	// Production ingress attaches none (see the MaxAge note above), so lane
	// events are unindexed and this window costs nothing for them. It stays at
	// 10s to bound dedup state for any id-carrying publisher on these subjects —
	// at 200k/s a 30s window would track ~6M ids, a 10s window ~2M. streamConfig
	// clamps it to MaxAge regardless, so the two cannot drift apart.
	Duplicates:   10 * time.Second,
	BatchPublish: true,
}

// TwitchIngressStandardStream is the standard lane, partitioned onto a stream of
// its own. It exists for one reason: a JetStream stream has ONE serialized RAFT
// proposal loop, so an R3 stream tops out around 98k msg/s end to end on this
// hub however the publishers are shaped. Two streams elect two leaders and run
// two loops — live-measured at ~171.7k msg/s across the pair — and standard is
// the only lane big enough to be worth moving: it carries every non-premium
// broadcaster plus every event with no extractable broadcaster, while premium,
// the stream lane and the status subjects stay on TwitchIngressStream.
//
// Everything not stated below is deliberately identical to the lane it split
// from — memory tier, R3, the 10s staleness window, batch publishing — and
// sesame owns it for the same reason it owns the other: one runtime owner per
// stream is what keeps the STREAM.CREATE/UPDATE grants narrow (nats-auth.conf).
//
// MIGRATION ORDER, and it is the whole safety argument. The subject moves from
// one live stream to another, and the broker lets neither an overlap nor a gap
// be expressed atomically, so the two API calls must run in this order:
//
//  1. UpdateStream TWITCH_INGRESS, dropping twitch.ingress.event.standard;
//  2. CreateStream TWITCH_INGRESS_STANDARD, claiming it.
//
// EnsureStreams reconciles its spec slice in order, so the ordering is enforced
// by listing TwitchIngressStream first — here in DataStreams and in the owner's
// slice (app/sesame/main.go). Between the two calls no stream claims the
// subject: that window is one JetStream API round trip, on the first pod that
// converges, and publishes inside it get no PubAck and are dropped by ingress's
// dedup-free rule. Every later pod finds both streams converged and writes
// nothing.
//
// The reverse order is not a smaller window, it is a deadlock. Creating this
// stream while TWITCH_INGRESS still captures twitch.ingress.event.> is refused
// with JSStreamSubjectOverlap; EnsureStreams returns that error; the owner
// treats a failed initial provision as fatal. The narrowing that would clear the
// overlap then never runs, on any pod, ever.
//
// An old-image pod reconciling mid-rollout cannot undo step 1 either: its wider
// TWITCH_INGRESS update now overlaps this stream, so the server refuses it and
// the pod logs a reconcile error on its retry timer until it is replaced.
var TwitchIngressStandardStream = StreamSpec{
	Name: "TWITCH_INGRESS_STANDARD",
	// Exactly one subject, and it must be the one TwitchIngressStream no longer
	// enumerates. Claimed by both streams it is a create error; claimed by
	// neither it is a publish into nothing.
	Subjects: []string{"twitch.ingress.event.standard"},
	// The same 10s window as the lane it split from, and not independently
	// tunable in practice: the window IS the staleness policy for chat, so two
	// halves of one firehose expiring on different clocks would let a standard
	// answer outlive a premium one.
	MaxAge:  10 * time.Second,
	Storage: nats.MemoryStorage,
	// R3 like every fleet stream. The point of the partition is two leaders, not
	// fewer replicas: an R1 half would take its whole window and its consumers
	// with whichever peer held it.
	Replicas: 3,
	// 640 MiB of the 1 GiB the unpartitioned stream held, with 384 MiB left on
	// TWITCH_INGRESS. The partition must not grow the hub's memory-backed
	// footprint, so this is a split of a fixed gigabyte rather than an addition
	// to it, and the larger share follows the larger lane. At the ~865 B
	// measured on the wire per event, 640 MiB is ~776k events of consumer lag —
	// ~7.8s at 100k/s, ~5.2s at 150k/s.
	MaxBytes: 640 << 20, // 640 MiB
	// No MaxMsgsPer, deliberately. A per-subject cap exists to stop one lane's
	// flood from evicting its neighbours, and this stream has no neighbours:
	// carrying the sibling's 400k over would only add a second ceiling (~330 MiB
	// at the measured wire size) that binds before MaxBytes and cuts the lag
	// budget this stream was just given roughly in half.
	//
	// Dedup window as on the lane it split from: ingress attaches no
	// Nats-Msg-Id, so it is inert on the hot path and only bounds dedup state
	// for an id-carrying publisher. streamConfig clamps it to MaxAge anyway.
	Duplicates:   10 * time.Second,
	BatchPublish: true,
}

// TwitchIngressRetryStream is the delayed-redelivery lane for the hot ingress
// consumers. An AckFlowControl consumer has no per-message pending state to NAK
// against — the server advances one replicated ack floor per window — so a
// handler failure cannot ask the broker to redeliver. The lane consumer instead
// hands the failed event back to the broker as a *scheduled* message here, and
// the broker delivers it when the delay elapses. Nothing in sesame holds a timer
// for work it has already handed off, so a pod that dies during the delay does
// not take the retry with it.
//
// NOT PROVISIONED TODAY. Membership of DataStreams resolves subjects to streams;
// it does not create anything. Every service reconciles an explicit list, and
// sesame's carries only the two ingress lane streams — deliberately, because
// receipt-level lane consumers ship disabled (NATS_CONSUME_FLOW defaults off), so
// nothing schedules onto this lane and a stream nobody drains would only consume
// hub memory. Provisioning it is part of enabling flow consumption, not of
// defining the spec, and it is owned by sesame when that happens: the retry lane
// is meaningless without the consumer that drains it, and one owner per stream is
// what keeps STREAM.CREATE/UPDATE grants narrow (see nats-auth.conf).
var TwitchIngressRetryStream = StreamSpec{
	Name:     "TWITCH_INGRESS_RETRY",
	Subjects: []string{"twitch.ingress.retry.>"},
	// 2 minutes is the whole budget, and it is NOT a retention target — it is the
	// floor a schedule row needs to outlive its own delay. MaxAge evicts the row,
	// and an evicted row is a silently cancelled schedule: the scheduler's only
	// cleanup for a missing message is dropping the wheel entry. So this bounds
	// the row, while the retry delay itself is bounded by the 10s lane window
	// above — a retry that lands after the event it replays would have aged out
	// is a late answer, which the lane's staleness policy says is worse than no
	// answer. Retry delays therefore have to stay well inside 10s, not inside 2m.
	MaxAge: 2 * time.Minute,
	// 32 MiB per member under R3. This lane only ever holds the exceptional
	// path — the events a handler already failed — so it is sized as an
	// incident buffer, not a firehose: at ~1 KiB an event that is ~32k retries
	// in flight, far past the point where the fix is upstream rather than here.
	MaxBytes: 32 << 20, // 32 MiB
	// Memory, like the lane it serves. A retry is worth less than the event it
	// replays and both are perishable, so paying a disk write per retry buys
	// nothing. Memory storage also rescans its messages to rebuild the schedule
	// wheel on start, so the schedules survive exactly as long as the messages do.
	Storage: nats.MemoryStorage,
	// R3, like every fleet stream. A single-copy retry lane would drop every
	// in-flight retry with its peer, and those are precisely the deliveries
	// that already failed once.
	Replicas: 3,
	// The reason this stream exists: the delay is the broker's, not ours.
	MsgSchedules: true,
	BatchPublish: true,
}

// DataStreams is the complete replayable stream catalog. It remains available
// to tests and operator tooling; runtime services reconcile only the named
// stream they own above. Outgress commands are deliberately excluded because
// they are perishable work, not event history.
//
// TwitchIngressStream precedes TwitchIngressStandardStream and that order is
// load-bearing wherever this slice is reconciled: the narrowing must land before
// the create, or the create is refused for overlap. See the migration note on
// TwitchIngressStandardStream.
var DataStreams = []StreamSpec{
	BagelDataStream,
	TwitchIngressStream,
	TwitchIngressStandardStream,
	TwitchIngressRetryStream,
}

// IngressPartitionEnabled gates the standard-lane partition. It exists so that
// merging the partition code is a zero-behaviour deploy: the fleet's images
// roll in whatever order Flux lands them, and until every ingress publisher
// carries the per-subject cohort staging, the first sesame pod to execute the
// partition would turn each of old ingress's mixed premium/standard atomic
// batches into a silent JSAtomicPublishIncompleteBatch drop. The flip is an
// operator action taken after the fleet has converged, in its own window
// (NATS-PULL-ROLLOUT.md section 6), exactly like the pull-consumer canary.
func IngressPartitionEnabled() bool {
	return env.Get("NATS_INGRESS_PARTITION", "off") == "on"
}

// IngressLaneSpecs is the ingress lane catalog the owner reconciles and the
// subject resolver consults, in reconcile order. Partition off is the
// pre-partition production shape, derived from the partitioned spec rather
// than kept as a second literal so the two shapes cannot drift apart in any
// field other than the two the partition changes.
func IngressLaneSpecs() []StreamSpec {
	if IngressPartitionEnabled() {
		return []StreamSpec{TwitchIngressStream, TwitchIngressStandardStream}
	}
	return []StreamSpec{legacyTwitchIngressStream()}
}

// legacyTwitchIngressStream is the unpartitioned lane: the event wildcard and
// the whole gigabyte, exactly what production runs before the flip. Reconciling
// this shape while the partitioned stream exists would be refused for subject
// overlap, which is the correct failure: the flag must not be flipped back
// after the partition has been created without deleting the partition first.
func legacyTwitchIngressStream() StreamSpec {
	spec := TwitchIngressStream
	spec.Subjects = []string{"twitch.ingress.event.>", "twitch.ingress.status.>"}
	spec.MaxBytes = 1 << 30
	return spec
}

// streamForTopic resolves the catalog stream that captures a subject, so
// subscribers can bind explicitly instead of paying an account-wide lookup.
// Every catalog subject set is disjoint (the broker enforces it, and
// TestCatalogStreamsClaimDisjointSubjects pins it), so the first match is the
// only match and iteration order does not decide the answer. The ingress lanes
// come from IngressLaneSpecs rather than DataStreams: before the partition
// flip the standard subject must resolve to TWITCH_INGRESS, where it still
// lives, or every standard consumer would bind a stream that does not exist.
func streamForTopic(topic string) (string, error) {
	specs := make([]StreamSpec, 0, len(DataStreams)+2)
	specs = append(specs, BagelDataStream)
	specs = append(specs, IngressLaneSpecs()...)
	specs = append(specs, TwitchIngressRetryStream, OutgressStream, OutgressSystemStream)

	for _, spec := range specs {
		if matchesAnySubject(topic, spec.Subjects) {
			return spec.Name, nil
		}
	}
	return "", fmt.Errorf("bus: no stream matches subject %q", topic)
}

// matchSubject reports whether subject falls under filter ('>' matches any
// suffix).
func matchSubject(subject, filter string) bool {
	if strings.HasSuffix(filter, ">") {
		return strings.HasPrefix(subject, strings.TrimSuffix(filter, ">"))
	}
	return subject == filter
}

func matchesAnySubject(topic string, filters []string) bool {
	for _, filter := range filters {
		if matchSubject(topic, filter) {
			return true
		}
	}
	return false
}
