// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/cache"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	loyalCounterChannelPrefix = "loyal:cnt:c:"
	loyalCounterViewerPrefix  = "loyal:cnt:v:"
	loyalBalancePrefix        = "loyal:bal:"
)

const (
	counterTTL               = 12 * time.Hour
	balanceTTL               = time.Minute
	scopeCacheTTL            = 5 * time.Minute
	scopeCacheCapacity int64 = 4096
)

var ErrReservedCounter = errors.New("reserved counter name")

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

type ValkeyLoyaltyStore struct {
	client   valkey.Client
	primary  valkey.Client
	rpc      *LoyaltyRPC
	reporter *LoyaltyReporter
	scopes   *cache.Cache[string]
	top      *cache.Cache[[]loyaltyrpc.Balance]
	log      *zap.Logger
}

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
		top:      cache.New[[]loyaltyrpc.Balance](topCacheCapacity, topCacheTTL),
		log:      log,
	}
}

func NormalizeCounterName(name string) string {
	return scope.NormalizeName(name)
}

type counterRef struct {
	broadcasterID uint64
	name          string
}

func (r counterRef) channelKey() string {
	return cache.PairKey(loyalCounterChannelPrefix, r.broadcasterID, r.name)
}

func (r counterRef) viewerKey() string {
	return cache.PairKey(loyalCounterViewerPrefix, r.broadcasterID, r.name)
}

func (r counterRef) scopeKey() string {
	return cache.PairKey("scope:", r.broadcasterID, r.name)
}

func balanceKey(broadcasterID, viewerID uint64) string {
	return cache.PairKey(loyalBalancePrefix, broadcasterID, strconv.FormatUint(viewerID, 10))
}

func (s *ValkeyLoyaltyStore) Earn(broadcasterID, viewerID uint64, login, name string, points int64, watchSeconds uint64) {
	s.reporter.Earn(broadcasterID, viewerID, login, name, points, watchSeconds)
}

func (s *ValkeyLoyaltyStore) scope(ctx context.Context, broadcasterID uint64, name string) string {
	key := counterRef{broadcasterID, name}.scopeKey()
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
			module.BIDField(broadcasterID), zap.String("counter", name), zap.Error(err))
		return data.CounterScopeChannel
	}
	return scope
}

func entryField(scope string, viewerID uint64, command string) string {
	if bucketedScope(scope) {
		return command + ":" + strconv.FormatUint(viewerID, 10)
	}
	return strconv.FormatUint(viewerID, 10)
}

func bucketedScope(scope string) bool {
	return scope == data.CounterScopeCommand || scope == data.CounterScopeViewerCommand
}

func rowScoped(scope string) bool {
	return scope == data.CounterScopeChannel || scope == data.CounterScopeBot
}

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

func (s *ValkeyLoyaltyStore) CounterBump(ctx context.Context, b CounterBump) (int64, error) {
	name := NormalizeCounterName(b.Name)
	if name == "" || b.Delta == 0 {
		return 0, nil
	}
	if data.SystemCounter(name) {
		return 0, ErrReservedCounter
	}
	scope, viewerID, command := bumpTarget(s.scope(ctx, b.BroadcasterID, name), b.Viewer.ID, NormalizeCounterName(b.Command))
	viewer := b.Viewer
	if viewerID == 0 {
		viewer = Viewer{}
	}
	viewer.ID = viewerID

	var value int64
	var err error
	if rowScoped(scope) {
		value, err = s.bumpChannel(ctx, b.BroadcasterID, name, b.Delta)
	} else {
		value, err = s.bumpEntry(ctx, b.BroadcasterID, name, entryField(scope, viewerID, command), viewerID, command, b.Delta)
	}
	if err != nil {
		return 0, err
	}

	s.reporter.Bump(CounterBumpTarget{
		BroadcasterID: b.BroadcasterID,
		Name:          name,
		Scope:         scope,
		Viewer:        viewer,
		Command:       command,
	}, b.Delta)
	return value, nil
}

func (s *ValkeyLoyaltyStore) bumpChannel(ctx context.Context, broadcasterID uint64, name string, delta int64) (int64, error) {
	key := counterRef{broadcasterID, name}.channelKey()
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

func (s *ValkeyLoyaltyStore) bumpEntry(ctx context.Context, broadcasterID uint64, name, field string, viewerID uint64, command string, delta int64) (int64, error) {
	key := counterRef{broadcasterID, name}.viewerKey()
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

func (s *ValkeyLoyaltyStore) CounterPeek(ctx context.Context, target CounterTarget) (loyaltyrpc.Counter, bool, error) {
	name := NormalizeCounterName(target.Name)
	if name == "" {
		return loyaltyrpc.Counter{}, false, nil
	}
	broadcasterID, viewerID := target.BroadcasterID, target.ViewerID
	scope := s.scope(ctx, broadcasterID, name)
	command := NormalizeCounterName(target.Command)

	if v, ok := s.peekView(ctx, broadcasterID, name, scope, viewerID, command); ok {
		return loyaltyrpc.Counter{Name: name, Scope: scope, Value: v}, true, nil
	}
	return s.rpc.CounterGet(ctx, broadcasterID, name, viewerID, command)
}

func (s *ValkeyLoyaltyStore) peekView(ctx context.Context, broadcasterID uint64, name, scope string, viewerID uint64, command string) (int64, bool) {
	var (
		v   int64
		err error
	)
	ref := counterRef{broadcasterID, name}
	if viewScope, viewViewer, viewCmd := bumpTarget(scope, viewerID, command); !rowScoped(viewScope) {
		field := entryField(viewScope, viewViewer, viewCmd)
		v, err = s.primary.Do(ctx, s.primary.B().Hget().Key(ref.viewerKey()).Field(field).Build()).AsInt64()
	} else {
		v, err = s.primary.Do(ctx, s.primary.B().Get().Key(ref.channelKey()).Build()).AsInt64()
	}
	if err != nil {
		if !valkey.IsValkeyNil(err) {
			s.log.Debug("loyalty: counter view read failed", zap.String("counter", name), zap.Error(err))
		}
		return 0, false
	}
	return v, true
}

func (s *ValkeyLoyaltyStore) CounterInvalidate(ctx context.Context, broadcasterID uint64, name string) {
	name = NormalizeCounterName(name)
	if name == "" {
		return
	}
	ref := counterRef{broadcasterID, name}
	if err := s.client.Do(ctx, s.client.B().Del().
		Key(ref.channelKey(), ref.viewerKey()).
		Build()).Error(); err != nil {
		s.log.Warn("loyalty: failed to invalidate counter view",
			module.BIDField(broadcasterID), zap.String("counter", name), zap.Error(err))
	}
	s.scopes.Invalidate(ref.scopeKey())
}

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

func (s *ValkeyLoyaltyStore) BalanceAdjust(ctx context.Context, broadcasterID uint64, viewerLogin string, value int64, absolute bool) (loyaltyrpc.Balance, bool, error) {
	bal, found, err := s.rpc.BalanceAdjust(ctx, broadcasterID, viewerLogin, value, absolute)
	if err != nil || !found {
		return bal, found, err
	}
	s.dropBalanceCache(ctx, broadcasterID, bal.ViewerID)
	return bal, true, nil
}

func (s *ValkeyLoyaltyStore) BalanceSpend(ctx context.Context, broadcasterID uint64, viewerLogin string, amount int64) (loyaltyrpc.Balance, bool, bool, error) {
	bal, found, spent, err := s.rpc.BalanceSpend(ctx, broadcasterID, viewerLogin, amount)
	if err != nil || !found {
		return bal, found, spent, err
	}
	s.dropBalanceCache(ctx, broadcasterID, bal.ViewerID)
	return bal, true, spent, nil
}

func (s *ValkeyLoyaltyStore) BalanceTransfer(ctx context.Context, broadcasterID, fromViewerID uint64, targetLogin string, amount int64) (bal loyaltyrpc.Balance, found, moved bool, err error) {
	bal, target, found, moved, err := s.rpc.BalanceTransfer(ctx, broadcasterID, fromViewerID, targetLogin, amount)
	if err != nil || !found {
		return bal, found, moved, err
	}
	s.dropBalanceCache(ctx, broadcasterID, bal.ViewerID)
	if moved && target != nil {
		s.dropBalanceCache(ctx, broadcasterID, target.ViewerID)
	}
	return bal, true, moved, nil
}

const topCacheTTL = time.Minute

const topCacheCapacity int64 = 4096

func (s *ValkeyLoyaltyStore) Top(ctx context.Context, broadcasterID uint64, limit int) ([]loyaltyrpc.Balance, error) {
	key := strconv.FormatUint(broadcasterID, 10) + ":" + strconv.Itoa(limit)
	return s.top.GetOrLoad(ctx, key, func(ctx context.Context) ([]loyaltyrpc.Balance, error) {
		return s.rpc.Top(ctx, broadcasterID, limit)
	})
}

func (s *ValkeyLoyaltyStore) dropBalanceCache(ctx context.Context, broadcasterID uint64, viewerID string) {
	if id, err := strconv.ParseUint(viewerID, 10, 64); err == nil && id != 0 {
		_ = s.client.Do(ctx, s.client.B().Del().Key(balanceKey(broadcasterID, id)).Build()).Error()
	}
}

func (s *ValkeyLoyaltyStore) CounterCreate(ctx context.Context, broadcasterID uint64, name, scope string) (loyaltyrpc.Counter, error) {
	c, err := s.rpc.CounterCreate(ctx, broadcasterID, name, scope)
	if err != nil {
		return loyaltyrpc.Counter{}, err
	}
	s.CounterInvalidate(ctx, broadcasterID, c.Name)
	return c, nil
}

func (s *ValkeyLoyaltyStore) CounterSet(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string, value int64) (bool, error) {
	found, err := s.rpc.CounterSet(ctx, broadcasterID, name, viewerID, command, value)
	if err != nil || !found {
		return found, err
	}
	s.CounterInvalidate(ctx, broadcasterID, name)
	return true, nil
}

func (s *ValkeyLoyaltyStore) CounterDelete(ctx context.Context, broadcasterID uint64, name string) error {
	if err := s.rpc.CounterDelete(ctx, broadcasterID, name); err != nil {
		return err
	}
	s.CounterInvalidate(ctx, broadcasterID, name)
	return nil
}

func (s *ValkeyLoyaltyStore) CounterList(ctx context.Context, broadcasterID uint64) ([]loyaltyrpc.Counter, error) {
	return s.rpc.CounterList(ctx, broadcasterID)
}
