package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/cache"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// Valkey key shapes. Counters keep MySQL (the loyalty service) as the source
// of truth and Valkey as a shared live view: reads seed from the service on a
// cold key, writes INCR the shared key (atomic across sesame replicas, so the
// chat-visible count is exact) while the same delta rides the reporter to the
// service. A lost event only drifts the Valkey view until the key's TTL
// retires it and the next read re-seeds from DB truth.
const (
	// loyalCounterChannelPrefix keys one channel-scoped counter (a string).
	loyalCounterChannelPrefix = "loyal:cnt:c:"
	// loyalCounterViewerPrefix keys one entry-scoped counter: a single hash
	// per counter, fields "<viewer>" (viewer scope) or "<command>:<viewer>"
	// (viewer+command scope). One key per counter keeps invalidation a DEL.
	loyalCounterViewerPrefix = "loyal:cnt:v:"
	// loyalBalancePrefix keys one viewer's cached balance reply.
	loyalBalancePrefix = "loyal:bal:"
)

// counterTTL bounds a counter's live view between re-seeds. Long, because a
// death counter mid-marathon should not lose its local increments to an
// expiry; refreshed on every write.
const counterTTL = 12 * time.Hour

// balanceTTL bounds how stale a !points reply can be: accruals land through
// the reporter + service flush windows, so a short TTL keeps the answer
// within a minute of truth without a read RPC per command.
const balanceTTL = time.Minute

// scopeCacheTTL bounds the in-process (broadcaster, counter) -> scope cache.
// Scope changes only through delete + recreate, so minutes of staleness on
// OTHER replicas is acceptable; the acting replica invalidates its own.
const scopeCacheTTL = 5 * time.Minute

// scopeCacheCapacity ceilings that cache. It is keyed per (broadcaster, counter),
// a handful per broadcaster, so a few thousand covers the working set within the
// TTL without holding the generic cache.DefaultCapacity ten thousand at rest.
const scopeCacheCapacity int64 = 4096

// These scripts keep a warm counter bump to one master round trip. A missing
// key/field returns nil without mutating anything so the caller can obtain the
// authoritative seed from the loyalty service. The second execution installs
// that seed only when the key is still cold, then applies this caller's delta.
// Concurrent seeders are safe: one creates the value and every caller's INCR
// still lands exactly once.
var (
	counterTTLArg     = strconv.FormatInt(int64(counterTTL.Seconds()), 10)
	bumpChannelScript = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  if ARGV[1] == '' then return false end
  redis.call('SET', KEYS[1], ARGV[1])
end
local value = redis.call('INCRBY', KEYS[1], ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
return value`)
	bumpEntryScript = valkey.NewLuaScript(`
if redis.call('HEXISTS', KEYS[1], ARGV[1]) == 0 then
  if ARGV[2] == '' then return false end
  redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
end
local value = redis.call('HINCRBY', KEYS[1], ARGV[1], ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return value`)
)

// The bump-ONCE scripts fold the idempotency claim (KEYS[2], the same key
// idempotency.ValkeyStore.Seen would SET) into the increment so a redelivered
// counter bump is deduped in the SAME atomic master round trip that increments —
// never the crash-exposed "claim, then a separate INCRBY" the split guard ran.
// A crash between those two ops used to leave the claim set but the counter
// un-incremented, so the redelivery saw a duplicate and rendered a stale value:
// the bump was lost. Folded, either both land or neither does.
//
// Return is a two-element array {value, flag}:
//
//	flag  1 = applied   (this delivery incremented; value is the new count)
//	flag  0 = duplicate (a replay; value is the current count, read back warm)
//	flag -1 = needseed  (fresh but the counter is cold and no seed was supplied;
//	                     NOTHING was mutated — crucially the claim was NOT set —
//	                     so the caller loads the authoritative seed and re-execs)
//	flag -2 = dup-cold  (a replay whose counter view is cold; value is unknown,
//	                     so the caller falls back to the authoritative peek)
//
// KEYS[2] is optional: with the dedup key absent (numkeys 1, the kill switch)
// the scripts never claim and simply apply, matching the guard's fail-open.
var (
	bumpChannelOnceScript = valkey.NewLuaScript(`
local dedup = KEYS[2]
if dedup and dedup ~= '' and redis.call('EXISTS', dedup) == 1 then
  if redis.call('EXISTS', KEYS[1]) == 1 then
    return {redis.call('GET', KEYS[1]), 0}
  end
  return {0, -2}
end
if redis.call('EXISTS', KEYS[1]) == 0 then
  if ARGV[1] == '' then return {0, -1} end
  redis.call('SET', KEYS[1], ARGV[1])
end
local value = redis.call('INCRBY', KEYS[1], ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
if dedup and dedup ~= '' then
  redis.call('SET', dedup, '1', 'EX', ARGV[4])
end
return {value, 1}`)
	bumpEntryOnceScript = valkey.NewLuaScript(`
local dedup = KEYS[2]
if dedup and dedup ~= '' and redis.call('EXISTS', dedup) == 1 then
  if redis.call('HEXISTS', KEYS[1], ARGV[1]) == 1 then
    return {redis.call('HGET', KEYS[1], ARGV[1]), 0}
  end
  return {0, -2}
end
if redis.call('HEXISTS', KEYS[1], ARGV[1]) == 0 then
  if ARGV[2] == '' then return {0, -1} end
  redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
end
local value = redis.call('HINCRBY', KEYS[1], ARGV[1], ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[4])
if dedup and dedup ~= '' then
  redis.call('SET', dedup, '1', 'EX', ARGV[5])
end
return {value, 1}`)
)

// The flag half of a bump-once reply. See the script comment for the states.
const (
	bumpFlagApplied  = 1
	bumpFlagWarmDup  = 0
	bumpFlagNeedSeed = -1
	bumpFlagDupCold  = -2
)

// errCounterDupCold marks a deduplicated bump whose live counter view was cold:
// the increment already landed on the original delivery, so the caller renders
// the authoritative value via a peek rather than trusting a stale zero. It is a
// sentinel, not a fault, so the reporter delta is NOT re-applied for it.
var errCounterDupCold = errors.New("loyalty: counter bump deduplicated, view cold")

// bumpOnceOutcome is the decoded result of one folded claim+increment.
type bumpOnceOutcome struct {
	value   int64 // the counter value (valid unless dupCold)
	applied bool  // true = this delivery incremented; false = duplicate replay
	dupCold bool  // duplicate AND the view was cold: the caller must peek
}

// bumpOnceTarget is the fully-resolved counter one bump-once addresses, so the
// script wiring and the reporter/fail-open settle both read one value instead of
// a long parameter list.
type bumpOnceTarget struct {
	broadcasterID uint64
	name          string
	scope         string
	field         string // hash field for entry scopes; unused for row scopes
	viewerID      uint64
	command       string
	delta         int64
	dedupKey      string        // "" = kill switch: apply without claiming
	dedupTTL      time.Duration // claim lifetime; ignored when dedupKey is ""
}

// ValkeyLoyaltyStore is the worker-side loyalty surface: counter bumps/reads
// with a Valkey live view over the loyalty service, cached balance peeks, and
// pass-through management verbs. It implements LoyaltyStore.
type ValkeyLoyaltyStore struct {
	client valkey.Client
	// primary serves the counter view. Bumps are exact because their script
	// runs on the master; peeking the same counter from a lagging node-local
	// replica would contradict that, and would keep serving a value
	// CounterInvalidate already deleted instead of re-seeding from the service.
	// The balance cache deliberately does not use this: its staleness budget is
	// balanceTTL, which dwarfs replication lag.
	primary  valkey.Client
	rpc      *LoyaltyRPC
	reporter *LoyaltyReporter
	scopes   *cache.Cache[string]
	log      *zap.Logger
	// bumpFailOpen counts folded bumps admitted through a Valkey/script fault, so
	// the fail-open rate is observable the way idempotency.ValkeyStore exposes its.
	bumpFailOpen atomic.Int64
}

// NewValkeyLoyaltyStore builds the store. reporter carries every mutation to
// the loyalty service; rpc is the cold-read loader and the management path.
func NewValkeyLoyaltyStore(client valkey.Client, rpc *LoyaltyRPC, reporter *LoyaltyReporter, log *zap.Logger) *ValkeyLoyaltyStore {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyLoyaltyStore{
		client:   client,
		primary:  pkg_valkey.Primary(client),
		rpc:      rpc,
		reporter: reporter,
		scopes:   cache.New[string](scopeCacheCapacity, scopeCacheTTL),
		log:      log,
	}
}

// NormalizeCounterName is the worker-side mirror of the loyalty service's
// counter key normalization: bare name, lower-cased, no leading "!".
func NormalizeCounterName(name string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "!")))
}

func counterChannelKey(broadcasterID uint64, name string) string {
	return loyalCounterChannelPrefix + strconv.FormatUint(broadcasterID, 10) + ":" + name
}

func counterViewerKey(broadcasterID uint64, name string) string {
	return loyalCounterViewerPrefix + strconv.FormatUint(broadcasterID, 10) + ":" + name
}

func balanceKey(broadcasterID, viewerID uint64) string {
	return loyalBalancePrefix + strconv.FormatUint(broadcasterID, 10) + ":" + strconv.FormatUint(viewerID, 10)
}

// Earn hands one accrual to the reporter (fire-and-forget; the balance cache
// is deliberately not touched — its short TTL absorbs the lag).
func (s *ValkeyLoyaltyStore) Earn(broadcasterID, viewerID uint64, login, name string, points int64, watchSeconds uint64) {
	s.reporter.Earn(broadcasterID, viewerID, login, name, points, watchSeconds)
}

// scope resolves a counter's scope, creating nothing: an unknown counter
// defaults to channel scope and materializes in the service on its first
// flushed bump.
func (s *ValkeyLoyaltyStore) scope(ctx context.Context, broadcasterID uint64, name string) string {
	key := "scope:" + strconv.FormatUint(broadcasterID, 10) + ":" + name
	scope, err := s.scopes.GetOrLoad(ctx, key, func(ctx context.Context) (string, error) {
		c, found, err := s.rpc.CounterGet(ctx, broadcasterID, name, 0, "")
		if err != nil {
			return "", err
		}
		if !found {
			return data.CounterScopeChannel, nil
		}
		switch c.Scope {
		case data.CounterScopeBot, data.CounterScopeViewer, data.CounterScopeCommand, data.CounterScopeViewerCommand:
			return c.Scope, nil
		default:
			return data.CounterScopeChannel, nil
		}
	})
	if err != nil {
		s.log.Debug("loyalty: scope resolve failed, defaulting to channel",
			zap.Uint64("broadcaster_id", broadcasterID), zap.String("counter", name), zap.Error(err))
		return data.CounterScopeChannel
	}
	return scope
}

// entryField is the hash field one entry-scoped value lives under: the viewer
// id alone for viewer scope, "<command>:<viewer>" for viewer+command scope,
// "<command>:0" for the pooled command scope (mirroring its viewer_id=0 row).
// The viewer id is digits-only, so the encoding never collides.
func entryField(scope string, viewerID uint64, command string) string {
	if bucketedScope(scope) {
		return command + ":" + strconv.FormatUint(viewerID, 10)
	}
	return strconv.FormatUint(viewerID, 10)
}

// bucketedScope reports whether a scope keys its buckets by command.
func bucketedScope(scope string) bool {
	return scope == data.CounterScopeCommand || scope == data.CounterScopeViewerCommand
}

// rowScoped reports whether a scope keeps its value on the counter row (the
// plain Valkey string) rather than in per-bucket entries.
func rowScoped(scope string) bool {
	return scope == data.CounterScopeChannel || scope == data.CounterScopeBot
}

// bumpTarget normalizes the (scope, viewer, command) triple a bump lands
// under: bot and channel values ignore both; command scope pools every viewer
// into the command bucket. An unaddressable bump falls back to the channel
// value rather than dropping: a viewer scope without a viewer (should not
// happen from chat), or a command scope bumped by a nameless source.
func bumpTarget(scope string, viewerID uint64, command string) (string, uint64, string) {
	switch scope {
	case data.CounterScopeBot:
		return scope, 0, ""
	case data.CounterScopeCommand:
		if command == "" {
			return data.CounterScopeChannel, 0, ""
		}
		return scope, 0, command
	case data.CounterScopeViewer, data.CounterScopeViewerCommand:
		if viewerID == 0 {
			return data.CounterScopeChannel, 0, ""
		}
		if scope == data.CounterScopeViewer {
			command = ""
		}
		return scope, viewerID, command
	default:
		return data.CounterScopeChannel, 0, ""
	}
}

// CounterBump increments a counter and returns the new chat-visible value.
// viewer is the acting chatter and command the triggering command's
// canonical name; the counter's own scope decides which of them key the
// value. The Valkey key is seeded from the service on first touch so the
// increment continues the stored count instead of restarting at zero. Bot
// scope rides broadcasterID 0 (the reserved bot namespace) and is reachable
// only from admin/system callers — template and chat paths never pass 0.
func (s *ValkeyLoyaltyStore) CounterBump(ctx context.Context, broadcasterID uint64, name string, viewer Viewer, command string, delta int64) (int64, error) {
	name = NormalizeCounterName(name)
	if name == "" || delta == 0 {
		return 0, nil
	}
	scope, viewerID, command := bumpTarget(s.scope(ctx, broadcasterID, name), viewer.ID, NormalizeCounterName(command))
	if viewerID == 0 {
		// The bump fell back to a non-viewer bucket; a stray identity must
		// not ride a key it does not belong to.
		viewer = Viewer{}
	}
	viewer.ID = viewerID

	var value int64
	var err error
	if rowScoped(scope) {
		value, err = s.bumpChannel(ctx, broadcasterID, name, delta)
	} else {
		value, err = s.bumpEntry(ctx, broadcasterID, name, entryField(scope, viewerID, command), viewerID, command, delta)
	}
	if err != nil {
		return 0, err
	}

	s.reporter.Bump(broadcasterID, name, scope, viewer, command, delta)
	return value, nil
}

// bumpChannel increments and refreshes a warm shared string atomically in one
// master round trip. Only a cold key pays the loyalty RPC and a second script
// call to seed it; the seed race across replicas is benign and every delta is
// still applied exactly once.
func (s *ValkeyLoyaltyStore) bumpChannel(ctx context.Context, broadcasterID uint64, name string, delta int64) (int64, error) {
	key := counterChannelKey(broadcasterID, name)
	deltaArg := strconv.FormatInt(delta, 10)

	value, err := bumpChannelScript.Exec(ctx, s.client, []string{key}, []string{"", deltaArg, counterTTLArg}).AsInt64()
	if err == nil {
		return value, nil
	}
	if !valkey.IsValkeyNil(err) {
		return 0, err
	}

	seed := int64(0)
	if c, found, loadErr := s.rpc.CounterGet(ctx, broadcasterID, name, 0, ""); loadErr == nil && found {
		seed = c.Value
	}
	return bumpChannelScript.Exec(ctx, s.client, []string{key}, []string{
		strconv.FormatInt(seed, 10), deltaArg, counterTTLArg,
	}).AsInt64()
}

// bumpEntry is the hash-field equivalent of bumpChannel: warm increment and
// TTL refresh are one atomic master call, while a cold field is seeded from
// the loyalty service before the caller's delta is applied.
func (s *ValkeyLoyaltyStore) bumpEntry(ctx context.Context, broadcasterID uint64, name, field string, viewerID uint64, command string, delta int64) (int64, error) {
	key := counterViewerKey(broadcasterID, name)
	deltaArg := strconv.FormatInt(delta, 10)

	value, err := bumpEntryScript.Exec(ctx, s.client, []string{key}, []string{field, "", deltaArg, counterTTLArg}).AsInt64()
	if err == nil {
		return value, nil
	}
	if !valkey.IsValkeyNil(err) {
		return 0, err
	}

	seed := int64(0)
	if c, found, loadErr := s.rpc.CounterGet(ctx, broadcasterID, name, viewerID, command); loadErr == nil && found {
		seed = c.Value
	}
	return bumpEntryScript.Exec(ctx, s.client, []string{key}, []string{
		field, strconv.FormatInt(seed, 10), deltaArg, counterTTLArg,
	}).AsInt64()
}

// CounterBumpOnce is CounterBump with the idempotency claim folded into the same
// atomic script, so a redelivered command bumps the counter (and the summed
// loyalty delta) exactly once even if the pod dies mid-flight: the claim and the
// increment now land together or not at all. dedupKey is the exact key the guard
// would claim ("" disables dedup — the kill switch — and simply applies once).
// Returns (value, applied): applied is true when this delivery incremented (the
// reporter fired), false on a deduplicated replay (it did not). A cold-view
// replay and any Valkey/script fault return a non-nil error so the caller reads
// the authoritative value back with a peek; a fault fails OPEN, applying the
// durable accrual once so an outage never drops a live bump.
func (s *ValkeyLoyaltyStore) CounterBumpOnce(ctx context.Context, broadcasterID uint64, name string, viewer Viewer, command string, delta int64, dedupKey string, dedupTTL time.Duration) (int64, bool, error) {
	name = NormalizeCounterName(name)
	if name == "" || delta == 0 {
		return 0, false, nil
	}
	scope, viewerID, command := bumpTarget(s.scope(ctx, broadcasterID, name), viewer.ID, NormalizeCounterName(command))
	if viewerID == 0 {
		viewer = Viewer{}
	}
	viewer.ID = viewerID

	t := bumpOnceTarget{
		broadcasterID: broadcasterID, name: name, scope: scope,
		field:    entryField(scope, viewerID, command),
		viewerID: viewerID, command: command, delta: delta,
		dedupKey: dedupKey, dedupTTL: dedupTTL,
	}
	out, err := s.atomicBumpOnce(ctx, t)
	return s.settleBumpOnce(t, viewer, out, err)
}

// settleBumpOnce maps a folded-bump outcome to the caller's contract and gates
// the loss-tolerant summed delta: reporter.Bump fires exactly once on a real
// apply and once on a fail-open fault (never dropping the accrual), and never on
// a duplicate replay.
func (s *ValkeyLoyaltyStore) settleBumpOnce(t bumpOnceTarget, viewer Viewer, out bumpOnceOutcome, err error) (int64, bool, error) {
	switch {
	case err != nil:
		// Fail OPEN: a Valkey/script fault must not drop a live bump, so apply the
		// durable accrual once and let the caller render via the authoritative peek.
		s.reporter.Bump(t.broadcasterID, t.name, t.scope, viewer, t.command, t.delta)
		s.noteBumpFailOpen(t.name, err)
		return 0, true, err
	case out.dupCold:
		return 0, false, errCounterDupCold
	case out.applied:
		s.reporter.Bump(t.broadcasterID, t.name, t.scope, viewer, t.command, t.delta)
		return out.value, true, nil
	default: // warm duplicate: value known, no second apply
		return out.value, false, nil
	}
}

// atomicBumpOnce runs the folded claim+increment against the right key shape for
// the counter's scope.
func (s *ValkeyLoyaltyStore) atomicBumpOnce(ctx context.Context, t bumpOnceTarget) (bumpOnceOutcome, error) {
	if rowScoped(t.scope) {
		return s.bumpChannelOnce(ctx, t)
	}
	return s.bumpEntryOnce(ctx, t)
}

// bumpChannelOnce folds the claim into the shared-string bump; a cold counter on
// a fresh (non-duplicate) delivery is seeded from the service and re-run, exactly
// as bumpChannel does — the needseed pass writes no claim, so the re-exec still
// takes the fresh path.
func (s *ValkeyLoyaltyStore) bumpChannelOnce(ctx context.Context, t bumpOnceTarget) (bumpOnceOutcome, error) {
	keys := bumpOnceKeys(counterChannelKey(t.broadcasterID, t.name), t.dedupKey)
	deltaArg, ttlArg := strconv.FormatInt(t.delta, 10), dedupTTLArg(t.dedupTTL)
	return runBumpOnce(
		func() int64 { return s.seedCounter(ctx, t.broadcasterID, t.name, 0, "") },
		func(seed string) (int64, int64, error) {
			return decodeBumpOnce(bumpChannelOnceScript.Exec(ctx, s.client, keys,
				[]string{seed, deltaArg, counterTTLArg, ttlArg}))
		},
	)
}

// bumpEntryOnce is the hash-field equivalent of bumpChannelOnce.
func (s *ValkeyLoyaltyStore) bumpEntryOnce(ctx context.Context, t bumpOnceTarget) (bumpOnceOutcome, error) {
	keys := bumpOnceKeys(counterViewerKey(t.broadcasterID, t.name), t.dedupKey)
	deltaArg, ttlArg := strconv.FormatInt(t.delta, 10), dedupTTLArg(t.dedupTTL)
	return runBumpOnce(
		func() int64 { return s.seedCounter(ctx, t.broadcasterID, t.name, t.viewerID, t.command) },
		func(seed string) (int64, int64, error) {
			return decodeBumpOnce(bumpEntryOnceScript.Exec(ctx, s.client, keys,
				[]string{t.field, seed, deltaArg, counterTTLArg, ttlArg}))
		},
	)
}

// seedCounter loads a cold counter's authoritative value from the loyalty
// service; a load failure seeds zero (the same benign default bumpChannel uses).
func (s *ValkeyLoyaltyStore) seedCounter(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string) int64 {
	if c, found, err := s.rpc.CounterGet(ctx, broadcasterID, name, viewerID, command); err == nil && found {
		return c.Value
	}
	return 0
}

// runBumpOnce runs the folded script once and, only on the cold-needseed signal,
// loads the seed and re-runs — the two-exec cold path, preserved. exec applies
// the script for a seed argument ("" probes without seeding) and returns the
// decoded (value, flag).
func runBumpOnce(loadSeed func() int64, exec func(seed string) (int64, int64, error)) (bumpOnceOutcome, error) {
	value, flag, err := exec("")
	if err != nil {
		return bumpOnceOutcome{}, err
	}
	if flag != bumpFlagNeedSeed {
		return outcomeFor(value, flag), nil
	}
	value, flag, err = exec(strconv.FormatInt(loadSeed(), 10))
	if err != nil {
		return bumpOnceOutcome{}, err
	}
	return outcomeFor(value, flag), nil
}

// outcomeFor maps a settled (value, flag) reply to an outcome. needseed is
// resolved before this by runBumpOnce, so only applied/dup/dup-cold reach here.
func outcomeFor(value, flag int64) bumpOnceOutcome {
	return bumpOnceOutcome{
		value:   value,
		applied: flag == bumpFlagApplied,
		dupCold: flag == bumpFlagDupCold,
	}
}

// bumpOnceKeys assembles the script's key list: the counter key alone (kill
// switch, numkeys 1) or the counter key plus the claim key (numkeys 2).
func bumpOnceKeys(counterKey, dedupKey string) []string {
	if dedupKey == "" {
		return []string{counterKey}
	}
	return []string{counterKey, dedupKey}
}

// decodeBumpOnce reads the {value, flag} array a bump-once script returns.
func decodeBumpOnce(r valkey.ValkeyResult) (value, flag int64, err error) {
	vals, err := r.AsIntSlice()
	return splitBumpReply(vals, err)
}

// splitBumpReply validates a bump-once reply's arity and splits it, kept pure so
// the array contract is unit-tested without a live interpreter.
func splitBumpReply(vals []int64, err error) (value, flag int64, _ error) {
	if err != nil {
		return 0, 0, err
	}
	if len(vals) != 2 {
		return 0, 0, fmt.Errorf("loyalty: bump-once reply arity %d, want 2", len(vals))
	}
	return vals[0], vals[1], nil
}

// dedupTTLArg formats a claim TTL in whole seconds, floored at one so a
// sub-second window never publishes a claim with no expiry.
func dedupTTLArg(ttl time.Duration) string {
	secs := int64(ttl.Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.FormatInt(secs, 10)
}

// noteBumpFailOpen counts and (throttled) logs a fail-open bump.
func (s *ValkeyLoyaltyStore) noteBumpFailOpen(name string, err error) {
	n := s.bumpFailOpen.Add(1)
	if n == 1 || n%1000 == 0 {
		s.log.Warn("loyalty: counter bump-once failed; applying without dedup (fail-open)",
			zap.String("counter", name), zap.Int64("fail_open_total", n), zap.Error(err))
	}
}

// CounterPeek reads a counter without bumping it: the live Valkey view when
// present, the service otherwise. found=false means the counter exists
// nowhere. command selects the bucket of a viewer+command counter.
func (s *ValkeyLoyaltyStore) CounterPeek(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string) (loyaltyrpc.Counter, bool, error) {
	name = NormalizeCounterName(name)
	if name == "" {
		return loyaltyrpc.Counter{}, false, nil
	}
	scope := s.scope(ctx, broadcasterID, name)
	command = NormalizeCounterName(command)

	if v, ok := s.peekView(ctx, broadcasterID, name, scope, viewerID, command); ok {
		return loyaltyrpc.Counter{Name: name, Scope: scope, Value: v}, true, nil
	}
	return s.rpc.CounterGet(ctx, broadcasterID, name, viewerID, command)
}

// peekView reads the live Valkey view of one counter value: the entry hash
// field for the entry scopes (when a viewer is known), the plain string
// otherwise. ok=false means the view is cold (or unreadable) and the caller
// should fall back to the service. It reads the primary so a peek agrees with
// the bump that produced the value and so CounterInvalidate's delete is
// actually observed as a cold view rather than as the pre-delete count.
func (s *ValkeyLoyaltyStore) peekView(ctx context.Context, broadcasterID uint64, name, scope string, viewerID uint64, command string) (int64, bool) {
	var (
		v   int64
		err error
	)
	if viewScope, viewViewer, viewCmd := bumpTarget(scope, viewerID, command); !rowScoped(viewScope) {
		field := entryField(viewScope, viewViewer, viewCmd)
		v, err = s.primary.Do(ctx, s.primary.B().Hget().Key(counterViewerKey(broadcasterID, name)).Field(field).Build()).AsInt64()
	} else {
		v, err = s.primary.Do(ctx, s.primary.B().Get().Key(counterChannelKey(broadcasterID, name)).Build()).AsInt64()
	}
	if err != nil {
		if !valkey.IsValkeyNil(err) {
			s.log.Debug("loyalty: counter view read failed", zap.String("counter", name), zap.Error(err))
		}
		return 0, false
	}
	return v, true
}

// CounterInvalidate drops a counter's live view (both shapes) and the local
// scope cache entry — the write-through for the authoritative management verbs.
func (s *ValkeyLoyaltyStore) CounterInvalidate(ctx context.Context, broadcasterID uint64, name string) {
	name = NormalizeCounterName(name)
	if name == "" {
		return
	}
	if err := s.client.Do(ctx, s.client.B().Del().
		Key(counterChannelKey(broadcasterID, name), counterViewerKey(broadcasterID, name)).
		Build()).Error(); err != nil {
		s.log.Warn("loyalty: failed to invalidate counter view",
			zap.Uint64("broadcaster_id", broadcasterID), zap.String("counter", name), zap.Error(err))
	}
	s.scopes.Invalidate("scope:" + strconv.FormatUint(broadcasterID, 10) + ":" + name)
}

// BalanceGet returns one viewer's standing through a short-TTL Valkey cache.
func (s *ValkeyLoyaltyStore) BalanceGet(ctx context.Context, broadcasterID, viewerID uint64) (loyaltyrpc.Balance, error) {
	key := balanceKey(broadcasterID, viewerID)
	if raw, err := s.client.Do(ctx, s.client.B().Get().Key(key).Build()).ToString(); err == nil {
		if points, watch, ok := decodeBalance(raw); ok {
			return loyaltyrpc.Balance{ViewerID: strconv.FormatUint(viewerID, 10), Points: points, WatchSeconds: watch}, nil
		}
	}
	bal, err := s.rpc.BalanceGet(ctx, broadcasterID, viewerID)
	if err != nil {
		return loyaltyrpc.Balance{}, err
	}
	_ = s.client.Do(ctx, s.client.B().Set().Key(key).
		Value(strconv.FormatInt(bal.Points, 10)+":"+strconv.FormatUint(bal.WatchSeconds, 10)).
		ExSeconds(int64(balanceTTL.Seconds())).Build()).Error()
	return bal, nil
}

func decodeBalance(raw string) (points int64, watch uint64, ok bool) {
	p, w, found := strings.Cut(raw, ":")
	if !found {
		return 0, 0, false
	}
	points, err := strconv.ParseInt(p, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	watch, err = strconv.ParseUint(w, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return points, watch, true
}

// BalanceAdjust passes a mod grant through to the service and drops the
// target's cached balance so their next !points shows the new value.
func (s *ValkeyLoyaltyStore) BalanceAdjust(ctx context.Context, broadcasterID uint64, viewerLogin string, value int64, absolute bool) (loyaltyrpc.Balance, bool, error) {
	bal, found, err := s.rpc.BalanceAdjust(ctx, broadcasterID, viewerLogin, value, absolute)
	if err != nil || !found {
		return bal, found, err
	}
	if viewerID, perr := strconv.ParseUint(bal.ViewerID, 10, 64); perr == nil && viewerID != 0 {
		_ = s.client.Do(ctx, s.client.B().Del().Key(balanceKey(broadcasterID, viewerID)).Build()).Error()
	}
	return bal, true, nil
}

// CounterCreate passes through to the service and refreshes the local view.
func (s *ValkeyLoyaltyStore) CounterCreate(ctx context.Context, broadcasterID uint64, name, scope string) (loyaltyrpc.Counter, error) {
	c, err := s.rpc.CounterCreate(ctx, broadcasterID, name, scope)
	if err != nil {
		return loyaltyrpc.Counter{}, err
	}
	s.CounterInvalidate(ctx, broadcasterID, c.Name)
	return c, nil
}

// CounterSet passes through to the service and drops the live view so the
// next read serves the new value.
func (s *ValkeyLoyaltyStore) CounterSet(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string, value int64) (bool, error) {
	found, err := s.rpc.CounterSet(ctx, broadcasterID, name, viewerID, command, value)
	if err != nil || !found {
		return found, err
	}
	s.CounterInvalidate(ctx, broadcasterID, name)
	return true, nil
}

// CounterDelete passes through to the service and drops the live view.
func (s *ValkeyLoyaltyStore) CounterDelete(ctx context.Context, broadcasterID uint64, name string) error {
	if err := s.rpc.CounterDelete(ctx, broadcasterID, name); err != nil {
		return err
	}
	s.CounterInvalidate(ctx, broadcasterID, name)
	return nil
}

// CounterList passes through to the service (management/list is not a hot
// path, so no cache).
func (s *ValkeyLoyaltyStore) CounterList(ctx context.Context, broadcasterID uint64) ([]loyaltyrpc.Counter, error) {
	return s.rpc.CounterList(ctx, broadcasterID)
}
