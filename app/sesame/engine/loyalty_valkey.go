package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/cache"

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

// ValkeyLoyaltyStore is the worker-side loyalty surface: counter bumps/reads
// with a Valkey live view over the loyalty service, cached balance peeks, and
// pass-through management verbs. It implements LoyaltyStore.
type ValkeyLoyaltyStore struct {
	client   valkey.Client
	rpc      *LoyaltyRPC
	reporter *LoyaltyReporter
	scopes   *cache.Cache[string]
	log      *zap.Logger
}

// NewValkeyLoyaltyStore builds the store. reporter carries every mutation to
// the loyalty service; rpc is the cold-read loader and the management path.
func NewValkeyLoyaltyStore(client valkey.Client, rpc *LoyaltyRPC, reporter *LoyaltyReporter, log *zap.Logger) *ValkeyLoyaltyStore {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyLoyaltyStore{
		client:   client,
		rpc:      rpc,
		reporter: reporter,
		scopes:   cache.New[string](cache.DefaultCapacity, scopeCacheTTL),
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
		case data.CounterScopeViewer, data.CounterScopeViewerCommand:
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
// id alone for viewer scope, "<command>:<viewer>" for viewer+command scope.
// The viewer id is digits-only, so the encoding never collides.
func entryField(scope string, viewerID uint64, command string) string {
	if scope == data.CounterScopeViewerCommand {
		return command + ":" + strconv.FormatUint(viewerID, 10)
	}
	return strconv.FormatUint(viewerID, 10)
}

// CounterBump increments a counter and returns the new chat-visible value.
// viewerID is the acting chatter and command the triggering command's
// canonical name; the counter's own scope decides which of them key the
// value (the three modes: channel-global, per user, per user+command). The
// Valkey key is seeded from the service on first touch so the increment
// continues the stored count instead of restarting at zero.
func (s *ValkeyLoyaltyStore) CounterBump(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string, delta int64) (int64, error) {
	name = NormalizeCounterName(name)
	if name == "" || delta == 0 {
		return 0, nil
	}
	scope := s.scope(ctx, broadcasterID, name)
	command = NormalizeCounterName(command)

	var value int64
	var err error
	switch {
	case scope == data.CounterScopeChannel || viewerID == 0:
		// An entry-scoped counter bumped without a viewer (should not happen
		// from chat) falls back to the channel value rather than dropping.
		scope = data.CounterScopeChannel
		viewerID = 0
		command = ""
		value, err = s.bumpChannel(ctx, broadcasterID, name, delta)
	default:
		if scope == data.CounterScopeViewer {
			command = ""
		}
		value, err = s.bumpEntry(ctx, broadcasterID, name, entryField(scope, viewerID, command), viewerID, command, delta)
	}
	if err != nil {
		return 0, err
	}

	s.reporter.Bump(broadcasterID, name, scope, viewerID, command, delta)
	return value, nil
}

// bumpChannel seeds (SET NX from the service) then INCRs the shared string.
// The seed race across replicas is benign: one SET NX wins, both INCRs apply.
func (s *ValkeyLoyaltyStore) bumpChannel(ctx context.Context, broadcasterID uint64, name string, delta int64) (int64, error) {
	key := counterChannelKey(broadcasterID, name)

	exists, err := s.client.Do(ctx, s.client.B().Exists().Key(key).Build()).AsInt64()
	if err != nil {
		return 0, err
	}
	if exists == 0 {
		seed := int64(0)
		if c, found, err := s.rpc.CounterGet(ctx, broadcasterID, name, 0, ""); err == nil && found {
			seed = c.Value
		}
		// NX: a concurrent seeder winning is fine, the value is the same.
		_ = s.client.Do(ctx, s.client.B().Set().Key(key).Value(strconv.FormatInt(seed, 10)).Nx().ExSeconds(int64(counterTTL.Seconds())).Build()).Error()
	}

	value, err := s.client.Do(ctx, s.client.B().Incrby().Key(key).Increment(delta).Build()).AsInt64()
	if err != nil {
		return 0, err
	}
	_ = s.client.Do(ctx, s.client.B().Expire().Key(key).Seconds(int64(counterTTL.Seconds())).Build()).Error()
	return value, nil
}

// bumpEntry seeds one entry-scoped hash field (HSETNX from the service) then
// HINCRBYs it, refreshing the whole hash's TTL.
func (s *ValkeyLoyaltyStore) bumpEntry(ctx context.Context, broadcasterID uint64, name, field string, viewerID uint64, command string, delta int64) (int64, error) {
	key := counterViewerKey(broadcasterID, name)

	exists, err := s.client.Do(ctx, s.client.B().Hexists().Key(key).Field(field).Build()).AsBool()
	if err != nil {
		return 0, err
	}
	if !exists {
		seed := int64(0)
		if c, found, err := s.rpc.CounterGet(ctx, broadcasterID, name, viewerID, command); err == nil && found {
			seed = c.Value
		}
		_ = s.client.Do(ctx, s.client.B().Hsetnx().Key(key).Field(field).Value(strconv.FormatInt(seed, 10)).Build()).Error()
	}

	value, err := s.client.Do(ctx, s.client.B().Hincrby().Key(key).Field(field).Increment(delta).Build()).AsInt64()
	if err != nil {
		return 0, err
	}
	_ = s.client.Do(ctx, s.client.B().Expire().Key(key).Seconds(int64(counterTTL.Seconds())).Build()).Error()
	return value, nil
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

	if scope != data.CounterScopeChannel && viewerID != 0 {
		field := entryField(scope, viewerID, command)
		v, err := s.client.Do(ctx, s.client.B().Hget().Key(counterViewerKey(broadcasterID, name)).Field(field).Build()).AsInt64()
		if err == nil {
			return loyaltyrpc.Counter{Name: name, Scope: scope, Value: v}, true, nil
		}
		if !valkey.IsValkeyNil(err) {
			s.log.Debug("loyalty: counter hash read failed", zap.String("counter", name), zap.Error(err))
		}
	} else {
		v, err := s.client.Do(ctx, s.client.B().Get().Key(counterChannelKey(broadcasterID, name)).Build()).AsInt64()
		if err == nil {
			return loyaltyrpc.Counter{Name: name, Scope: scope, Value: v}, true, nil
		}
		if !valkey.IsValkeyNil(err) {
			s.log.Debug("loyalty: counter read failed", zap.String("counter", name), zap.Error(err))
		}
	}
	return s.rpc.CounterGet(ctx, broadcasterID, name, viewerID, command)
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
