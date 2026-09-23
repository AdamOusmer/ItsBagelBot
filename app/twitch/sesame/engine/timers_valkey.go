// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// timersModuleName is the ModuleView key the dashboard's Timers tab writes:
// its enable toggle is the master switch, and its Configs blob carries the
// list of repeating messages this store reads.
const timersModuleName = "timers"

// timerKeyPrefix is the Valkey schedule key for one timer: EX'd to its own
// interval. Its expiry, not a running goroutine, is the clock — the same
// idiom ValkeyLiveStore uses for its key-expiry re-check (live_valkey.go).
const timerKeyPrefix = "timer:"

// timerClaimPrefix guards one expiry so only one replica of the fleet fires
// it; nested under timerKeyPrefix the way live:recheck: nests under live:, so
// onExpired can tell its own claim keys apart from a schedule key.
const timerClaimPrefix = "timer:claim:"

const timerClaimTTL = 5 * time.Second

// minTimerInterval floors a configured interval. The dashboard already clamps
// to 60s; this only guards a hand-crafted RPC call from arming a tight
// expire/fire/re-arm loop.
const minTimerInterval = 30 * time.Second

// timerFirstFireJitter caps the random phase offset added to a timer's FIRST
// interval at arm time. It desynchronizes timers armed in the same instant
// (every timer at stream.online, or a freshly added same-interval one) so they
// don't all expire together and post as one wall of chat. Only the first fire
// is offset; re-arms use the exact interval, so the configured cadence holds.
// The offset is floored at the smaller of this cap and the interval itself, so
// a 30s timer never gets a 60s first wait.
const timerFirstFireJitter = 30 * time.Second

// rearmTimeout bounds one mid-stream rearm (IsLive read + ArmAll) triggered off
// a modules cache-invalidation, so a stalled Valkey/projector never pins the
// goroutine the rearm watcher spawns per message.
const rearmTimeout = 5 * time.Second

// timerFireTimeout bounds fireBounded's detached call to fire, the same
// off-the-watcher's-own-goroutine shape rearmTimeout gives RearmIfLive above.
// A message may name up to maxUrlFetchTokens (8) distinct {urlfetch}
// payloads, fanned out CONCURRENTLY (fetchUrlValues) rather than serially, so
// the wall-clock bound this needs is one customFetchRPCTimeout (gossip_rpc.go,
// 3.5s) plus headroom for the fan-out and the publish — not 8x it. 10s leaves
// that headroom generous while still short enough that a stuck fire cannot
// pin a goroutine open indefinitely.
const timerFireTimeout = 10 * time.Second

// reconcileInterval is how often the reconciler re-arms live broadcasters'
// timers, recovering any that silently stalled (a missed expiry notification).
// A stalled timer comes back within one interval, no stream restart needed.
const reconcileInterval = time.Minute

// reconcileClaimKey serializes the sweep to one replica per tick. It sorts
// under timerClaimPrefix so onExpired ignores its own expiry, and its short TTL
// only has to cover the skew between replicas' independent tickers — a rare
// double-sweep is harmless (NX arming dedups it), so this is efficiency, not
// correctness.
const reconcileClaimKey = timerClaimPrefix + "reconcile"

const reconcileClaimTTL = 30 * time.Second

// timerAuxPrefix is the gate and stop counters' key prefix (spec §5):
// timerx:lines:<bid> (chat activity), timerx:mark:<bid>:<tid> (a gated
// timer's watermark) and timerx:fires:<bid>:<tid> (the fire cap). It is
// deliberately NOT nested under timerKeyPrefix the way timerClaimPrefix is.
// onExpired routes every "timer:"-prefixed expiry into the fire path by
// design (it IS the clock), so an aux key sharing that prefix would only be
// rejected there by a failed id parse on "lines"/"mark"/"fires", a silent
// no-op today, but one bad refactor of parseTimerKey away from firing a
// message off a counter's own TTL. "timerx:" fails the plain HasPrefix(key,
// "timer:") check at the 6th byte ('x' vs ':'), so onExpired never sees these
// keys' expiries at all.
const timerAuxPrefix = "timerx:"

// timerAuxTTL bounds every gate/stop counter (lines, watermark, fire count).
// DisarmAll deletes all three on stream.offline (the normal path); the TTL is
// the safety net for the stream.offline that never arrives, a dropped
// message, a crash between the last event and the delete loop, so a stale
// counter cannot outlive a reasonable "this stream is clearly over" window.
// 48h is the number the spec fixes (§5): long enough that no live counter
// during a realistically long stream is ever caught by it, short enough that
// an orphaned counter does not linger for a week.
const timerAuxTTL = 48 * time.Hour

// timerDef is one broadcaster-authored repeating chat message.
//
// MinChatLines, MaxFires and EndsAt are the gate and stop fields from
// docs/specs/timer-conditions.md (D1-D6): a chat-activity gate, a per-stream
// fire cap, and an end date. All three are optional and additive (D11): a
// blob saved before this change decodes with every one at its zero value,
// which isGated/stopped/gatePasses (timers_rules.go) all read as "off," so an
// existing timer's behaviour is unchanged bit for bit.
type timerDef struct {
	ID       string `json:"id"`
	Message  string `json:"message"`
	Interval int    `json:"intervalSeconds"`
	Enabled  bool   `json:"enabled"`
	// MinChatLines gates a tick on chat activity: 0 (off) to 100 lines since
	// the timer's last fire (D2, D4).
	MinChatLines int `json:"minChatLines"`
	// MaxFires caps how many times this timer may fire in one stream: 0
	// (unlimited) to 100 (D5).
	MaxFires int `json:"maxFiresPerStream"`
	// EndsAt is an RFC 3339 UTC instant past which the timer stops; empty
	// means never (D6). Kept as the raw string, not a parsed time.Time: sesame
	// never writes this field back, and parseEndsAt's ok bool is what every
	// reader actually branches on.
	EndsAt string `json:"endsAt"`
}

// timersConfig is the "timers" module's Configs blob.
type timersConfig struct {
	Timers []timerDef `json:"timers"`
}

// ValkeyTimerStore arms a broadcaster's repeating messages for the length of
// one stream and fires them off Valkey key expiry: stream.online SETs one key
// per enabled timer (EX = its interval), stream.offline deletes them, and each
// expiry re-checks live state + config, posts the message, and re-arms.
//
// A missed expiry notification (the watcher's pub/sub connection drops and
// reconnects) silently stalls that one timer: its key is gone and nothing
// re-sets it. StartReconciler is the safety net, a once-a-minute sweep that
// re-arms every live broadcaster's timers with the same NX SET arming already
// uses, so a stalled timer resumes within one interval instead of staying
// down until the next stream.online.
type ValkeyTimerStore struct {
	client valkey.Client
	pub    bus.Publisher
	proj   projection.Reader
	live   IsLiveChecker

	// nc + rearmSubject drive the mid-stream arm-on-save path: a subscription to
	// the modules cache-invalidation subject that re-arms a live broadcaster's
	// timers the moment their dashboard save lands (StartRearmWatcher). nil nc or
	// empty subject leaves the watcher disabled — timers still arm on
	// stream.online, just not mid-stream.
	nc           *nats.Conn
	rearmSubject string

	outgressPremium  string
	outgressStandard string

	keyspaceDB int
	log        *zap.Logger

	// gatedCache memoizes "does this broadcaster have at least one enabled
	// timer with a chat activity gate," the answer CountChatLine's hot path
	// (the chat pipeline) needs on every message (D13). It is keyed by
	// broadcaster id the same way ValkeyLiveStore.cache is (live_valkey.go): a
	// hit costs one theine lookup, no allocation, no Valkey round trip. The
	// rearm watcher invalidates it in StartRearmWatcher's callback, which
	// already subscribes to the modules cache-invalidation subject a timers
	// save publishes to, no second NATS subscription. The TTL is a safety net
	// for a missed invalidation, not the primary freshness mechanism.
	gatedCache *cache.Keyed[uint64, bool]

	// now is read wherever a stop check needs the current instant (the end
	// date, D6). A field defaulting to time.Now in the constructor, rather than
	// a bare call at the read site, is the first clock injection in this
	// package (D16): it lets a test move "now" past an endsAt without a real
	// sleep, the same way lockFake's injected clock does for pkg/valkey's lock
	// tests.
	now func() time.Time

	// badEndsAtWarned dedups warnUnparsableEndsAt's log line per (broadcaster,
	// timer): a tick runs at least once per interval floor (30s), so without
	// this a broadcaster who saved a bad date would fill the log forever
	// instead of once (D16 says "log once"). Zero value is a ready-to-use
	// empty map; no constructor init needed. DisarmAll clears a timer's entry
	// so a broadcaster who fixes the date on the next stream gets a fresh
	// warning if they somehow break it again, not required for correctness but
	// cheap to keep the map from outliving the timer that caused the entry.
	badEndsAtWarned sync.Map

	// blankFireWarned dedups the "expansion left nothing to post" warning the
	// same way badEndsAtWarned dedups its own (D16: log once, not once per
	// tick) — a broadcaster whose message is now all conditionals/tokens that
	// happen to resolve empty would otherwise fill the log every interval.
	// Keyed by timerRef for the same reason: a bare timer id is not unique
	// across broadcasters.
	blankFireWarned sync.Map

	// pipeline expands a firing message's {token} spans through timerChain
	// (engine/timer_vars.go). It is nil until WirePipeline runs: main builds
	// this store before the pipeline it posts through even exists (the
	// expiry/rearm/reconciler watchers this constructor starts need to be
	// live before the consumer is), so the two are wired together once both
	// are built. Left nil (unit tests that build the struct literal directly,
	// or a wiring bug), fire posts the message exactly as saved, same as
	// before a chain existed for it at all — an unmounted scope leaves its
	// tokens literal, and a whole unwired chain is the same degrade one level
	// up.
	pipeline *Pipeline
}

// WirePipeline lets a timer's message expand {token} spans, the way a custom
// command's response does, once the pipeline that owns the scope chain
// exists. See the pipeline field's own comment for why this is a late setter
// rather than a constructor argument.
func (s *ValkeyTimerStore) WirePipeline(p *Pipeline) {
	s.pipeline = p
}

// TimersConfig wires a ValkeyTimerStore.
type TimersConfig struct {
	OutgressPremiumSubject  string
	OutgressStandardSubject string
	// KeyspaceDB is the Valkey db the expiry watcher listens on (default 0).
	KeyspaceDB int
	// NC is the core NATS connection the rearm watcher subscribes on. nil leaves
	// the watcher disabled.
	NC *nats.Conn
	// ModulesInvalidateSubject is the modules-scope cache-invalidation subject
	// (e.g. "bagel.cache.invalidate.modules") the rearm watcher listens on. Empty
	// leaves the watcher disabled.
	ModulesInvalidateSubject string
	// Log is the store's logger; a nil Log defaults to a no-op.
	Log *zap.Logger
}

// NewValkeyTimerStore builds a timer store. proj resolves a broadcaster's
// "timers" ModuleView and tier (for the outgress lane); live gates every fire
// and re-arm to the broadcaster's current live state.
func NewValkeyTimerStore(client valkey.Client, pub bus.Publisher, proj projection.Reader, live IsLiveChecker, cfg TimersConfig) *ValkeyTimerStore {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyTimerStore{
		client:           client,
		pub:              pub,
		proj:             proj,
		live:             live,
		nc:               cfg.NC,
		rearmSubject:     cfg.ModulesInvalidateSubject,
		outgressPremium:  cfg.OutgressPremiumSubject,
		outgressStandard: cfg.OutgressStandardSubject,
		keyspaceDB:       cfg.KeyspaceDB,
		log:              log,
		gatedCache:       cache.NewKeyed[uint64, bool](gatedCacheCapacity, gatedCacheTTL, gatedCacheKeyFn),
		now:              time.Now,
	}
}

// gatedCacheCapacity ceilings ValkeyTimerStore.gatedCache the same way
// liveCacheCapacity ceilings the live store's cache (live_valkey.go): one
// entry per broadcaster this pod has seen chat from recently, so a few
// thousand covers a pod's working set without holding cache.DefaultCapacity
// at rest for a bool.
const gatedCacheCapacity int64 = 4096

// gatedCacheTTL is the safety net for a missed rearm-watcher invalidation
// (D13's explicit invalidate is the primary path). Minutes, not seconds: a
// stale "no gated timer" answer costs a broadcaster's gate a few minutes of
// extra activity in its window on the next save, not a wrong fire, so this
// favors fewer Valkey reads over tight freshness.
const gatedCacheTTL = 10 * time.Minute

// gatedCacheKeyFn stringifies a broadcaster id for gatedCache's singleflight
// group. It runs only on a cache miss or an explicit Invalidate, never on a
// hit (see cache.Keyed's doc comment).
func gatedCacheKeyFn(broadcasterID uint64) string {
	return strconv.FormatUint(broadcasterID, 10)
}

// timerRef names one timer within one broadcaster's schedule: the
// (broadcaster, timer id) pair every per-timer key and log line in this file
// is derived from. Named the same way loyalty_valkey.go's counterRef is
// (the same fix for the same shape of finding): CodeScene's Primitive
// Obsession check flagged this file at 46% primitive-typed arguments against
// a 40% threshold, because nearly every method here threaded broadcasterID
// uint64 and either timerID string or a whole timerDef through separately.
// The two halves of a ref carry no meaning apart, so bundling them removes a
// repeated parameter from every per-timer call and reads as "one timer," not
// two values that happen to travel together.
type timerRef struct {
	broadcasterID uint64
	id            string
}

// scheduleKey is this timer's Valkey schedule key, EX'd to its interval.
func (r timerRef) scheduleKey() string {
	return cache.PairKey(timerKeyPrefix, r.broadcasterID, r.id)
}

// markKey is this timer's watermark: the linesKey value as of its last fire,
// or its arm if it has not fired yet this stream (D4).
func (r timerRef) markKey() string {
	return cache.PairKey(timerAuxPrefix+"mark:", r.broadcasterID, r.id)
}

// firesKey is this timer's per-stream fire count, the fire cap's counter (D5).
func (r timerRef) firesKey() string {
	return cache.PairKey(timerAuxPrefix+"fires:", r.broadcasterID, r.id)
}

// armedTimer pairs a timerRef with the timerDef instance a call is acting on.
// onExpired resolves both together (a schedule key parses into a ref before
// the config read that produces the def), and everything downstream of a tick
// (the stop check, the gate check, the fire) needs both: the ref for the aux
// keys, the def for the message and thresholds. Passing one armedTimer value
// instead of a (timerRef, timerDef) pair is the other half of the Primitive
// Obsession fix timerRef starts.
type armedTimer struct {
	ref timerRef
	def timerDef
}

// linesKey is the broadcaster-wide chat-activity counter a gated timer's
// watermark measures against (D3, D4). Broadcaster-scoped, not per timer, so
// it stays a plain function rather than a timerRef method.
func linesKey(broadcasterID uint64) string {
	return cache.UserKey(timerAuxPrefix+"lines:", broadcasterID)
}

// ArmAll SETs one Valkey key per enabled timer of an enabled "timers" module,
// each EX'd to its interval plus a small random phase offset (armJittered) so
// timers armed together here don't all fire in the same instant. NX means a
// timer already counting down is left alone, so this only starts the ones not
// yet armed — the freshly added timer on a mid-stream rearm, or every timer on
// a fresh stream.online.
func (s *ValkeyTimerStore) ArmAll(ctx context.Context, broadcasterID uint64) {
	cfg, ok := s.config(ctx, broadcasterID)
	if !ok {
		return
	}
	for _, td := range cfg.Timers {
		s.armOne(ctx, armedTimer{ref: timerRef{broadcasterID: broadcasterID, id: td.ID}, def: td})
	}
}

// armOne is ArmAll's per-timer step: skip a timer that has already stopped
// this stream, otherwise arm it and, for a gated timer, seed its watermark.
// The stop re-check here (not just in onExpired) is what keeps a capped or
// ended timer down: StartReconciler calls ArmAll every minute for every live
// broadcaster (D5), and without this check a timer that stopped between
// sweeps would come right back on the next one.
func (s *ValkeyTimerStore) armOne(ctx context.Context, at armedTimer) {
	if !at.def.Enabled || at.ref.id == "" {
		return
	}
	var fires int64
	if clampFireCap(at.def.MaxFires) > 0 {
		fires = s.fireCount(ctx, at.ref)
	}
	if stopped(at.def, fires, s.now()) {
		return
	}
	s.armJittered(ctx, at)
	if isGated(at.def) {
		s.seedWatermark(ctx, at)
	}
}

// RearmIfLive arms a broadcaster's enabled timers mid-stream, but only while the
// broadcaster is actually live. It backs the arm-on-save path: adding, enabling
// or editing a timer from the dashboard changes the broadcaster's modules blob,
// which is what StartRearmWatcher turns into this call — so a timer created
// after stream.online still starts counting down this session instead of
// sitting idle until the next stream.
//
// ArmAll's NX SET makes repeated calls safe: a timer already counting down keeps
// its clock, only a not-yet-armed one (the freshly added timer) gets a key. When
// the broadcaster is offline this is a no-op — the next stream.online arms
// everything fresh, and arming an offline broadcaster's keys would only set keys
// onExpired drops unfired on their first expiry.
func (s *ValkeyTimerStore) RearmIfLive(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 {
		return
	}
	live, err := s.live.IsLive(ctx, broadcasterID)
	if err != nil || !live {
		return
	}
	s.ArmAll(ctx, broadcasterID)
}

// StartReconciler periodically re-arms every live broadcaster's timers, so a
// timer that silently stalled comes back mid-stream without a stream restart.
// A stall happens when an expiry notification is lost — Valkey pub/sub is
// fire-and-forget with no replay, so a Sentinel failover, a watcher reconnect,
// or an onExpired that hit a transient error and returned without re-arming can
// leave a timer's key expired and never re-set. The expiry watcher alone never
// recovers that until the next stream.online; this sweep does.
//
// ArmAll's NX SET keeps it cheap and safe: a still-armed timer is untouched,
// only a missing (stalled) one is re-armed. One replica sweeps per tick (a
// fleet-wide NX claim), so the scan isn't multiplied across the pool. Runs
// until ctx is cancelled.
func (s *ValkeyTimerStore) StartReconciler(ctx context.Context) {
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	s.log.Info("timers: reconciler starting", zap.Duration("interval", reconcileInterval))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

// reconcile claims the sweep for this tick, then re-arms every live
// broadcaster's timers. RearmIfLive re-checks live state and NX-arms, so an
// already-armed timer is left counting down and only a stalled one restarts.
func (s *ValkeyTimerStore) reconcile(ctx context.Context) {
	if won, err := pkg_valkey.ClaimOnce(ctx, s.client, reconcileClaimKey, reconcileClaimTTL); err != nil || !won {
		return // another replica owns this tick, or the claim write failed
	}
	for _, id := range s.liveBroadcasters(ctx) {
		s.RearmIfLive(ctx, id)
	}
}

// liveBroadcasters SCANs the live-key set and returns the broadcaster ids
// currently live, skipping the recheck guard keys that share the live: prefix.
func (s *ValkeyTimerStore) liveBroadcasters(ctx context.Context) []uint64 {
	var ids []uint64
	cursor := uint64(0)
	for {
		entry, err := s.client.Do(ctx, s.client.B().Scan().Cursor(cursor).Match(livekey.KeyPrefix+"*").Count(200).Build()).AsScanEntry()
		if err != nil {
			s.log.Warn("timers: reconcile scan failed", zap.Error(err))
			return ids
		}
		for _, k := range entry.Elements {
			if strings.HasPrefix(k, recheckKeyPrefix) {
				continue
			}
			if id, err := strconv.ParseUint(strings.TrimPrefix(k, livekey.KeyPrefix), 10, 64); err == nil && id != 0 {
				ids = append(ids, id)
			}
		}
		cursor = entry.Cursor
		if cursor == 0 {
			return ids
		}
	}
}

// clampInterval turns a configured interval (seconds) into a Duration floored
// at minTimerInterval, the single place the floor is applied.
func clampInterval(seconds int) time.Duration {
	d := time.Duration(seconds) * time.Second
	if d < minTimerInterval {
		d = minTimerInterval
	}
	return d
}

// arm re-arms a timer at exactly its interval — the onExpired path, which must
// preserve the cadence set by the first (jittered) fire.
func (s *ValkeyTimerStore) arm(ctx context.Context, at armedTimer) {
	s.armAfter(ctx, at, clampInterval(at.def.Interval))
}

// armJittered arms a timer's first schedule key at its interval plus a random
// phase offset (see timerFirstFireJitter), so timers armed in the same instant
// don't all expire together. It is the ArmAll (stream.online + mid-stream
// rearm) path; onExpired re-arms via arm() at the exact interval, so the offset
// shifts only when the timer first fires this session, not its cadence after.
func (s *ValkeyTimerStore) armJittered(ctx context.Context, at armedTimer) {
	interval := clampInterval(at.def.Interval)
	spread := interval
	if spread > timerFirstFireJitter {
		spread = timerFirstFireJitter
	}
	offset := time.Duration(rand.Int64N(int64(spread.Seconds())+1)) * time.Second
	s.armAfter(ctx, at, interval+offset)
}

// armAfter SETs one timer's schedule key EX'd to ex. NX leaves an
// already-counting-down key alone: ArmAll must not reset the clock on a
// redelivered stream.online (or a mid-stream rearm), and onExpired's re-arm
// must not clobber a fresh key a concurrent ArmAll just set (the narrow race of
// a stream ending and restarting within the same instant).
func (s *ValkeyTimerStore) armAfter(ctx context.Context, at armedTimer, ex time.Duration) {
	if !at.def.Enabled || at.ref.id == "" {
		return
	}
	err := s.client.Do(ctx, s.client.B().Set().Key(at.ref.scheduleKey()).Value("1").Nx().ExSeconds(int64(ex.Seconds())).Build()).Error()
	if err != nil && !valkey.IsValkeyNil(err) {
		s.log.Warn("timers: failed to arm", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
}

// DisarmAll deletes every configured timer's key so a stream that just ended
// stops immediately rather than waiting out its longest-running timer's
// interval. Best-effort: a config read failure leaves the keys to expire and
// self-stop on their own (onExpired's live check fails the same way).
func (s *ValkeyTimerStore) DisarmAll(ctx context.Context, broadcasterID uint64) {
	cfg, ok := s.config(ctx, broadcasterID)
	if !ok {
		return
	}
	for _, td := range cfg.Timers {
		if td.ID == "" {
			continue
		}
		ref := timerRef{broadcasterID: broadcasterID, id: td.ID}
		s.delAuxKey(ctx, ref, ref.scheduleKey())
		s.delAuxKey(ctx, ref, ref.markKey())
		s.delAuxKey(ctx, ref, ref.firesKey())
		// Forget the "already warned" marks too, so a broadcaster who fixes
		// the problem and later reintroduces it on a future stream gets a
		// fresh warning instead of permanent silence.
		s.badEndsAtWarned.Delete(ref)
		s.blankFireWarned.Delete(ref)
	}
	// The chat-line counter is per broadcaster, not per timer (D3), so it is
	// deleted once here rather than inside the loop above. The empty id reads
	// as "no timer" in the warn log below, the same way it always has.
	s.delAuxKey(ctx, timerRef{broadcasterID: broadcasterID}, linesKey(broadcasterID))
}

// delAuxKey deletes one Valkey key belonging to ref (ref.id is empty for the
// broadcaster-wide chat line counter, which is not scoped to one timer).
// Shared by DisarmAll's four deletes so the same best-effort warn-and-continue
// posture the schedule-key delete already had doesn't have to be retyped per
// key.
func (s *ValkeyTimerStore) delAuxKey(ctx context.Context, ref timerRef, key string) {
	if err := s.client.Do(ctx, s.client.B().Del().Key(key).Build()).Error(); err != nil {
		s.log.Warn("timers: failed to disarm", module.BIDField(ref.broadcasterID), zap.String("timer_id", ref.id), zap.String("key", key), zap.Error(err))
	}
}

// config resolves the broadcaster's "timers" ModuleView, reporting false when
// the module is missing, disabled, unconfigured, or the read failed.
func (s *ValkeyTimerStore) config(ctx context.Context, broadcasterID uint64) (timersConfig, bool) {
	// Fails closed on everything but an enabled row: a missing row means no
	// timers were ever configured (absent -> ModuleOff), and a read failure
	// must not arm or fire anything, because a timer fired off a config we
	// could not read posts the wrong message into chat.
	view, state, err := ModuleLookup{Proj: s.proj, BroadcasterID: broadcasterID, Name: timersModuleName, Absent: ModuleOff}.Resolve(ctx)
	if err != nil {
		s.log.Warn("timers: failed to read module views", module.BIDField(broadcasterID), zap.Error(err))
	}
	if state != ModuleOn {
		return timersConfig{}, false
	}
	// An enabled module with an empty blob is "on but unconfigured": there is
	// nothing to arm, and it is kept distinct from a bad blob so it does not
	// log a warning on every tick of a freshly toggled-on module.
	if len(view.Configs) == 0 {
		return timersConfig{}, false
	}
	var cfg timersConfig
	if err := codec.Unmarshal(view.Configs, &cfg); err != nil {
		s.log.Warn("timers: bad config", module.BIDField(broadcasterID), zap.Error(err))
		return timersConfig{}, false
	}
	return cfg, true
}

// StartRearmWatcher subscribes to the modules cache-invalidation subject and
// arms a broadcaster's timers the moment a dashboard save changes their modules
// blob (RearmIfLive gates it to a live broadcaster). This is the arm-on-save
// path: without it, arming happens only on stream.online, so a timer added
// after the broadcaster went live would sit idle until their next stream.
//
// It rides the same "modules" invalidation event sesame's projection cache
// already consumes, so it needs no new NATS subject or account grant. A missed
// message (subscription drop) is self-correcting: the timer still arms on the
// next stream.online, matching the store's best-effort posture. The
// subscription is async — each message spawns a bounded goroutine so a slow
// IsLive/ArmAll never stalls delivery of the next invalidation. It runs until
// ctx is cancelled.
func (s *ValkeyTimerStore) StartRearmWatcher(ctx context.Context) {
	if s.nc == nil || s.rearmSubject == "" {
		return
	}
	sub, err := s.nc.Subscribe(s.rearmSubject, func(msg *nats.Msg) {
		var dto invalidate.DTO
		if err := codec.Unmarshal(msg.Data, &dto); err != nil {
			return
		}
		id, err := strconv.ParseUint(dto.BroadcasterID, 10, 64)
		if err != nil || id == 0 {
			return
		}
		// Drop the memoized "has a gated timer" answer on every modules save
		// for this broadcaster, not just a timers-scoped one: the DTO carries
		// no module name (invalidate.DTO is broadcaster-wide), and reusing the
		// subscription this watcher already holds (D13) means the memo can
		// only ever be as fresh as the coarsest save it rides along with,
		// which is what the TTL safety net (gatedCacheTTL) is for.
		s.gatedCache.Invalidate(id)
		go func() {
			rctx, cancel := context.WithTimeout(context.Background(), rearmTimeout)
			defer cancel()
			s.RearmIfLive(rctx, id)
		}()
	})
	if err != nil {
		s.log.Error("timers: failed to start rearm watcher", zap.String("subject", s.rearmSubject), zap.Error(err))
		return
	}
	s.log.Info("timers: rearm watcher starting", zap.String("subject", s.rearmSubject))
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()
}

// StartExpiryWatcher subscribes to Valkey key-expiry notifications and fires
// (or drops) each timer whose key expires. It runs until ctx is cancelled and
// reconnects on a dropped subscription, mirroring ValkeyLiveStore's watcher.
// Requires notify-keyspace-events to include expired-key events (Ex), already
// on for the live-recheck watcher this shares the deployment with.
func (s *ValkeyTimerStore) StartExpiryWatcher(ctx context.Context) {
	channel := "__keyevent@" + strconv.Itoa(s.keyspaceDB) + "__:expired"
	s.log.Info("timers: expiry watcher starting", zap.String("channel", channel))

	for ctx.Err() == nil {
		err := s.client.Receive(ctx, s.client.B().Subscribe().Channel(channel).Build(), func(msg valkey.PubSubMessage) {
			s.onExpired(ctx, msg.Message)
		})
		if ctx.Err() != nil {
			return
		}
		s.log.Warn("timers: expiry watcher dropped, reconnecting", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// onExpired handles one expired key. It ignores anything that is not a
// schedule key, claims the expiry so only one replica acts on it, then
// re-validates live state + config before firing and re-arming: a timer
// paused, deleted, or whose stream ended between arming and this expiry is
// dropped instead of fired.
func (s *ValkeyTimerStore) onExpired(ctx context.Context, key string) {
	ref, ok := parseTimerKey(key)
	if !ok {
		return
	}

	// One replica per expiry fires the timer.
	claimKey := timerClaimPrefix + strconv.FormatUint(ref.broadcasterID, 10) + ":" + ref.id
	if won, err := pkg_valkey.ClaimOnce(ctx, s.client, claimKey, timerClaimTTL); err != nil || !won {
		return
	}

	live, err := s.live.IsLive(ctx, ref.broadcasterID)
	if err != nil || !live {
		return // stream ended: stay stopped until the next stream.online arms fresh
	}

	at, ok := s.resolveArmed(ctx, ref)
	if !ok {
		return // disabled, deleted, or unreadable since arming: drop, don't re-arm
	}
	s.tick(ctx, at)
}

// parseTimerKey extracts the timerRef an expired Valkey key names, or reports
// ok=false for anything that is not one of this store's own schedule keys: a
// foreign key, a claim key (timerClaimPrefix), an aux key (timerAuxPrefix,
// see its doc comment for why those never reach here), or one whose id half
// will not parse.
func parseTimerKey(key string) (timerRef, bool) {
	if !strings.HasPrefix(key, timerKeyPrefix) || strings.HasPrefix(key, timerClaimPrefix) {
		return timerRef{}, false
	}
	rest := strings.TrimPrefix(key, timerKeyPrefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return timerRef{}, false
	}
	broadcasterID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || broadcasterID == 0 {
		return timerRef{}, false
	}
	return timerRef{broadcasterID: broadcasterID, id: parts[1]}, true
}

// resolveArmed resolves the timer ref names into an armedTimer from the
// broadcaster's CURRENT config, not whatever it was at arm time, so a timer
// disabled or deleted since arming is dropped instead of fired on stale
// settings.
func (s *ValkeyTimerStore) resolveArmed(ctx context.Context, ref timerRef) (armedTimer, bool) {
	cfg, ok := s.config(ctx, ref.broadcasterID)
	if !ok {
		return armedTimer{}, false
	}
	td, ok := findTimer(cfg.Timers, ref.id)
	if !ok || !td.Enabled {
		return armedTimer{}, false
	}
	return armedTimer{ref: ref, def: td}, true
}

// tick decides and acts on one expiry (spec §6): a stop wins outright (no
// fire, no re-arm, the timer stays down until the next stream), a gate that
// fails re-arms at the exact interval without firing (D8: a skip must not
// drift the cadence), and anything else fires before re-arming.
//
// fire runs off tick's own goroutine (fireBounded), not inline: tick is
// called from onExpired, which StartExpiryWatcher's Receive callback runs
// SYNCHRONOUSLY and serially for every broadcaster's expiry on this replica.
// Before timerChain existed, fire cost only a Valkey write and a publish —
// fast enough that inline never showed up. It can now spend real wall time
// expanding a message that names {urlfetch} (a gossip RPC per distinct
// payload, up to maxUrlFetchTokens of them) or any other scoped token, and an
// inline fire there would delay every OTHER broadcaster's expiry — and this
// timer's own re-arm below — by however long the slowest read takes.
// recordFire and arm stay inline: neither reads anything fire produces (a
// fire is "counted" and re-armed the moment tick decides to fire it, not
// after the message finishes expanding), so moving fire off this goroutine
// changes nothing about when the fire cap, the watermark or the next
// schedule key land.
func (s *ValkeyTimerStore) tick(ctx context.Context, at armedTimer) {
	if s.hasStopped(ctx, at) {
		return
	}
	if !s.gateOpen(ctx, at) {
		s.arm(ctx, at)
		return
	}
	s.fireBounded(at)
	s.recordFire(ctx, at)
	s.arm(ctx, at)
}

// fireBounded dispatches fire onto its own goroutine under its own bounded,
// detached context — the shape StartRearmWatcher already gives RearmIfLive
// (rearmTimeout), for the same reason tick's own comment gives. Detached from
// ctx (context.Background, not the watcher's) rather than inheriting it: a
// fire already in flight when the process starts shutting down should still
// get its own bounded window to finish publishing rather than being cut off
// the instant the watcher's ctx cancels.
func (s *ValkeyTimerStore) fireBounded(at armedTimer) {
	go func() {
		fctx, cancel := context.WithTimeout(context.Background(), timerFireTimeout)
		defer cancel()
		s.fire(fctx, at)
	}()
}

// hasStopped resolves at's stop conditions against fresh counters. The fire
// count is only read when a cap is actually set, a tick on an uncapped timer
// (the common case, D11) costs no extra Valkey round trip for a comparison
// that would always be false.
func (s *ValkeyTimerStore) hasStopped(ctx context.Context, at armedTimer) bool {
	s.warnUnparsableEndsAt(at)
	var fires int64
	if clampFireCap(at.def.MaxFires) > 0 {
		fires = s.fireCount(ctx, at.ref)
	}
	return stopped(at.def, fires, s.now())
}

// warnUnparsableEndsAt logs once total, not once per tick, when at carries a
// non-empty EndsAt that does not parse (D16 keeps this logging out of the
// pure stopped()/parseEndsAt() rules, which have no logger to write to, and
// D16 says "log once": a tick recurs at least every minTimerInterval, so
// without badEndsAtWarned's dedup this would fill the log for as long as the
// bad value sits saved). A blank EndsAt is the ordinary "never ends" case
// (D6) and never logs. badEndsAtWarned is keyed by timerRef directly: a timer
// id alone is not unique across broadcasters, and timerRef is already the
// pair that names one timer everywhere else in this file.
func (s *ValkeyTimerStore) warnUnparsableEndsAt(at armedTimer) {
	if at.def.EndsAt == "" {
		return
	}
	if _, ok := parseEndsAt(at.def.EndsAt); ok {
		return
	}
	if _, alreadyWarned := s.badEndsAtWarned.LoadOrStore(at.ref, struct{}{}); alreadyWarned {
		return
	}
	s.log.Warn("timers: unparsable end date, treating as never-ends",
		module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.String("ends_at", at.def.EndsAt))
}

// gateOpen resolves at's chat-activity gate (spec §6 step 2). No gate always
// opens, which also skips both Valkey reads below for the common case.
func (s *ValkeyTimerStore) gateOpen(ctx context.Context, at armedTimer) bool {
	if !isGated(at.def) {
		return true
	}
	lines := s.linesCount(ctx, at.ref.broadcasterID)
	mark := s.watermark(ctx, at.ref)
	return gatePasses(at.def, lines, mark)
}

// recordFire advances the two counters a fire moves: the fire cap (INCR) and,
// for a gated timer, the watermark (set to the current chat line count, D4).
// An ungated timer has no watermark to move.
func (s *ValkeyTimerStore) recordFire(ctx context.Context, at armedTimer) {
	if _, err := pkg_valkey.Incr(ctx, s.client, at.ref.firesKey(), timerAuxTTL); err != nil {
		s.log.Warn("timers: failed to record fire", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
	if isGated(at.def) {
		s.setWatermark(ctx, at)
	}
}

// fireCount reads a timer's per-stream fire count (D5), zero before its first
// fire this stream.
func (s *ValkeyTimerStore) fireCount(ctx context.Context, ref timerRef) int64 {
	n, err := pkg_valkey.GetInt(ctx, s.client, ref.firesKey())
	if err != nil {
		s.log.Warn("timers: failed to read fire count", module.BIDField(ref.broadcasterID), zap.String("timer_id", ref.id), zap.Error(err))
	}
	return n
}

// linesCount reads the broadcaster's chat-activity counter (D3), zero if
// nobody has chatted (or nobody counted it) since it last expired.
func (s *ValkeyTimerStore) linesCount(ctx context.Context, broadcasterID uint64) int64 {
	n, err := pkg_valkey.GetInt(ctx, s.client, linesKey(broadcasterID))
	if err != nil {
		s.log.Warn("timers: failed to read chat line count", module.BIDField(broadcasterID), zap.Error(err))
	}
	return n
}

// watermark reads a gated timer's line-count baseline (D4), zero if it has
// never been seeded (armOne seeds it at arm time, so this is the unusual
// case of a fire racing a not-yet-processed arm).
func (s *ValkeyTimerStore) watermark(ctx context.Context, ref timerRef) int64 {
	n, err := pkg_valkey.GetInt(ctx, s.client, ref.markKey())
	if err != nil {
		s.log.Warn("timers: failed to read gate watermark", module.BIDField(ref.broadcasterID), zap.String("timer_id", ref.id), zap.Error(err))
	}
	return n
}

// setWatermark overwrites a gated timer's watermark with the broadcaster's
// current chat line count. Unlike seedWatermark's NX (arm time, D4), a fire
// always overwrites: this IS the new baseline the next tick's delta measures
// from, and it must move even if something had already set the key.
func (s *ValkeyTimerStore) setWatermark(ctx context.Context, at armedTimer) {
	lines := s.linesCount(ctx, at.ref.broadcasterID)
	err := s.client.Do(ctx, s.client.B().Set().Key(at.ref.markKey()).
		Value(strconv.FormatInt(lines, 10)).Ex(timerAuxTTL).Build()).Error()
	if err != nil {
		s.log.Warn("timers: failed to set gate watermark", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
}

// seedWatermark sets a gated timer's watermark to the broadcaster's current
// chat line count, but only if it has no watermark yet (NX). armOne calls
// this on every arm, including a mid-stream rearm of a timer that has already
// fired this stream, NX is what stops that rearm from wiping out the
// existing watermark and handing the gate a free pass on its next tick (D4).
func (s *ValkeyTimerStore) seedWatermark(ctx context.Context, at armedTimer) {
	lines := s.linesCount(ctx, at.ref.broadcasterID)
	err := s.client.Do(ctx, s.client.B().Set().Key(at.ref.markKey()).
		Value(strconv.FormatInt(lines, 10)).Nx().Ex(timerAuxTTL).Build()).Error()
	if err != nil && !valkey.IsValkeyNil(err) {
		s.log.Warn("timers: failed to seed gate watermark", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
}

// CountChatLine increments the broadcaster's chat-activity counter (D3), the
// source every gated timer's watermark delta reads. It is a no-op for a
// broadcaster with no enabled chat-activity gate: hasGatedTimer's cache makes
// that the cheap path (one theine lookup) so a channel that never opens the
// feature costs nothing on its hot chat path beyond that lookup.
func (s *ValkeyTimerStore) CountChatLine(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 || !s.hasGatedTimer(ctx, broadcasterID) {
		return
	}
	// Off the message path: the INCR+EXPIRE round trip must not add Valkey
	// latency to every chat line on a channel with a gated timer, the same
	// reasoning ValkeyReputation.Bump gives for detaching its own INCR+EXPIRE
	// pair from the automod gate it used to sit inside.
	go func() {
		actx, cancel := context.WithTimeout(context.Background(), countChatLineTimeout)
		defer cancel()
		if _, err := pkg_valkey.Incr(actx, s.client, linesKey(broadcasterID), timerAuxTTL); err != nil {
			s.log.Debug("timers: chat line count failed", module.BIDField(broadcasterID), zap.Error(err))
		}
	}()
}

// hasGatedTimer answers CountChatLine's gate through gatedCache: does this
// broadcaster have at least one enabled timer with a chat activity gate. A
// loader error (a bad blob, an unreachable Valkey) reads as false rather than
// propagating, the caller is a fire-and-forget chat-path hook with nothing
// to do with an error, and the next chat line retries the same cache miss.
func (s *ValkeyTimerStore) hasGatedTimer(ctx context.Context, broadcasterID uint64) bool {
	gated, err := s.gatedCache.GetOrLoad(ctx, broadcasterID, func(ctx context.Context) (bool, error) {
		cfg, ok := s.config(ctx, broadcasterID)
		if !ok {
			return false, nil
		}
		for _, td := range cfg.Timers {
			if td.Enabled && isGated(td) {
				return true, nil
			}
		}
		return false, nil
	})
	return err == nil && gated
}

// countChatLineTimeout bounds CountChatLine's detached INCR, mirroring
// rearmTimeout's role for the rearm watcher's own detached call: a stalled
// Valkey must not leak one goroutine per chat line on a channel with a gated
// timer.
const countChatLineTimeout = 5 * time.Second

// findTimer stays a linear scan on purpose. cfg.Timers is decoded fresh from the
// module blob by config() on the same call that scans it, so an id-keyed map
// could not outlive one fire: building it would cost a map alloc plus one insert
// per timer to save a walk over the same handful of entries, which is strictly
// more work than the scan. This is the opposite case to the module set, which is
// cached across events and therefore worth keying.
func findTimer(timers []timerDef, id string) (timerDef, bool) {
	for _, td := range timers {
		if td.ID == id {
			return td, true
		}
	}
	return timerDef{}, false
}

// fire posts one timer's message the same way the pipeline posts any module
// Output: the send-time floor guard first (the config was already floor-
// checked at save time; this only covers drift), then whichever premium/
// standard lane the broadcaster's own tier resolves to.
//
// A message naming a {token} expands through timerChain first (see
// engine/timer_vars.go for which scopes a tick mounts and why); one with none
// costs expandTimerText one Lex and nothing else. The rendered text then
// fans out through chatLines exactly as a command response does — multi-line,
// blank-line-dropping, per-line floor recheck — because an expanded span
// (urlfetch's answer, a quote) is exactly as untrusted as a command's own
// token values are.
func (s *ValkeyTimerStore) fire(ctx context.Context, at armedTimer) {
	if term, hit := moderation.CheckFloor(at.def.Message); hit {
		s.log.Warn("timers: suppressed message carrying floor content",
			module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.String("term", term))
		return
	}

	idStr := strconv.FormatUint(at.ref.broadcasterID, 10)
	subject := s.outgressStandard
	locale := ""
	if u, err := s.proj.User(ctx, at.ref.broadcasterID); err == nil {
		if u.Premium() {
			subject = s.outgressPremium
		}
		locale = u.Locale
	}

	text := s.timerText(ctx, at, locale)
	outputs := s.timerOutputs(idStr, text)
	if len(outputs) == 0 {
		// tick's own recordFire/arm already ran (fireBounded's own comment):
		// the fire slot is spent whether or not anything was left to post, so
		// this is the difference between a silently quiet timer and a visibly
		// blank one.
		s.warnBlankFire(at)
		return
	}
	for _, out := range outputs {
		s.publishFired(ctx, at, subject, out)
	}
}

// warnBlankFire logs once per timer (D16: log once, not once per tick — see
// warnUnparsableEndsAt's identical shape) that an expansion left nothing to
// post: every line blank (a template that is now all conditionals or tokens
// resolving empty) or the floor guard catching an expanded span. DisarmAll
// forgets the mark so a broadcaster who edits the message and later
// reintroduces the problem sees a fresh warning instead of permanent silence.
func (s *ValkeyTimerStore) warnBlankFire(at armedTimer) {
	if _, already := s.blankFireWarned.LoadOrStore(at.ref, struct{}{}); already {
		return
	}
	s.log.Warn("timers: expansion left nothing to post, fire slot spent",
		module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id))
}

// timerText answers what fire posts, before line-splitting: the stored
// message unchanged when no pipeline is wired to expand it, its expansion
// otherwise.
// timerText answers what fire posts, before line-splitting. The unwired
// branch is exactly at.def.Message, untouched, because it never reaches
// Translate (timerOutputs skips chatLines with no pipeline) and so was never
// a moderation surface. The wired branch now IS one — timerOutputs' chatLines
// call runs every published line through Translate, token or not — so every
// line's own leading slash is defanged first (defangTimerSlashLines, see its
// own comment for why that is not vars.go's sanitizeVar).
func (s *ValkeyTimerStore) timerText(ctx context.Context, at armedTimer, locale string) string {
	if s.pipeline == nil {
		return at.def.Message
	}
	message := defangTimerSlashLines(at.def.Message)
	run := timerRun{ref: at.ref, locale: locale, firedAt: s.now()}
	return s.pipeline.expandTimerText(ctx, run, message)
}

// timerOutputs fans text into the chat messages fire actually publishes.
// With no pipeline wired this is the single raw message fire has always sent
// (no split, no slash-verb routing — the exact pre-expansion behavior, kept
// for the unit tests that build a store literal with no pipeline). Wired, it
// reuses chatLines — the same multi-line split, blank-line drop and per-line
// floor recheck a command response gets — so a template typed with more than
// one line, or an expanded span that smuggled one in, behaves identically
// either way.
func (s *ValkeyTimerStore) timerOutputs(idStr, text string) []module.Output {
	out := &module.Output{Type: outgress.TypeChat, BroadcasterID: idStr, Text: text}
	if s.pipeline == nil {
		return []module.Output{*out}
	}
	return s.pipeline.chatLines(out)
}

// publishFired builds and sends one already-prepared chat output, logging and
// skipping it on a marshal or publish failure — one bad line must not drop
// its siblings from the same fire.
func (s *ValkeyTimerStore) publishFired(ctx context.Context, at armedTimer, subject string, out module.Output) {
	body, err := buildOutgress(&out)
	if err != nil {
		s.log.Warn("timers: failed to build outgress message", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
		return
	}
	if err := bus.PublishRaw(ctx, s.pub, subject, body); err != nil {
		s.log.Warn("timers: failed to publish", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
}
