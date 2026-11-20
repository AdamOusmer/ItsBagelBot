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
	"ItsBagelBot/internal/domain/invalidate"
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

type ValkeyLoyaltyStore struct {
	client   valkey.Client
	primary  valkey.Client
	rpc      *LoyaltyRPC
	reporter *LoyaltyReporter
	scopes   *cache.Cache[counterScope]
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
		scopes:   cache.New[counterScope](scopeCacheCapacity, scopeCacheTTL),
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

func (s *ValkeyLoyaltyStore) Earn(e PointEarning) {
	s.reporter.Earn(e)
}

func (s *ValkeyLoyaltyStore) scope(ctx context.Context, broadcasterID uint64, name string) (counterScope, error) {
	key := counterRef{broadcasterID, name}.scopeKey()
	return s.scopes.GetOrLoad(ctx, key, func(ctx context.Context) (counterScope, error) {
		c, found, err := s.rpc.CounterGet(ctx, CounterTarget{BroadcasterID: broadcasterID, Name: name})
		if err != nil {
			return "", err
		}
		if !found {
			return data.CounterScopeChannel, nil
		}
		switch c.Scope {
		case data.CounterScopeBot, data.CounterScopeViewer, data.CounterScopeCommand, data.CounterScopeViewerCommand:
			return counterScope(c.Scope), nil
		default:
			return data.CounterScopeChannel, nil
		}
	})
}

type counterScope string

func entryField(scope counterScope, viewerID uint64, command string) string {
	if bucketedScope(scope) {
		return command + ":" + strconv.FormatUint(viewerID, 10)
	}
	return strconv.FormatUint(viewerID, 10)
}

func bucketedScope(scope counterScope) bool {
	return scope == data.CounterScopeCommand || scope == data.CounterScopeViewerCommand
}

func rowScoped(scope counterScope) bool {
	return scope == data.CounterScopeChannel || scope == data.CounterScopeBot
}

func bumpTarget(scope counterScope, viewerID uint64, command string) (counterScope, uint64, string) {
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

type resolvedBump struct {
	scope    counterScope
	viewerID uint64
	command  string
}

func (s *ValkeyLoyaltyStore) CounterBump(ctx context.Context, b CounterBump) (int64, error) {
	name := NormalizeCounterName(b.Name)
	if name == "" || b.Delta == 0 {
		return 0, nil
	}
	if data.SystemCounter(name) {
		return 0, ErrReservedCounter
	}
	target, err := s.scope(ctx, b.BroadcasterID, name)
	if err != nil {
		return 0, err
	}
	scope, viewerID, command := bumpTarget(target, b.Viewer.ID, NormalizeCounterName(b.Command))
	resolved := resolvedBump{scope: scope, viewerID: viewerID, command: command}

	value, err := s.bumpValue(ctx, b, name, resolved)
	if err != nil {
		return 0, err
	}

	s.reporter.BumpContext(ctx, CounterBumpTarget{
		BroadcasterID: b.BroadcasterID,
		Name:          name,
		Scope:         string(scope),
		Viewer:        bumpViewer(b.Viewer, viewerID),
		Command:       command,
	}, b.Delta)
	return value, nil
}

func (s *ValkeyLoyaltyStore) bumpValue(ctx context.Context, b CounterBump, name string, resolved resolvedBump) (int64, error) {
	if rowScoped(resolved.scope) {
		return s.bumpChannel(ctx, b.BroadcasterID, name, b.Delta)
	}
	return s.bumpEntry(ctx, b, name, resolved)
}

func bumpViewer(viewer Viewer, viewerID uint64) Viewer {
	if viewerID == 0 {
		return Viewer{ID: viewerID}
	}
	viewer.ID = viewerID
	return viewer
}

func (s *ValkeyLoyaltyStore) bumpChannel(ctx context.Context, broadcasterID uint64, name string, delta int64) (int64, error) {
	key := counterRef{broadcasterID, name}.channelKey()
	counter := pkg_valkey.SeededCounter{Key: key, TTL: counterTTL}
	return counter.Bump(ctx, s.client, delta, func() int64 {
		if c, found, loadErr := s.rpc.CounterGet(ctx, CounterTarget{BroadcasterID: broadcasterID, Name: name}); loadErr == nil && found {
			return c.Value
		}
		return 0
	})
}

func (s *ValkeyLoyaltyStore) bumpEntry(ctx context.Context, b CounterBump, name string, resolved resolvedBump) (int64, error) {
	broadcasterID, viewerID, command, delta := b.BroadcasterID, resolved.viewerID, resolved.command, b.Delta
	field := entryField(resolved.scope, viewerID, command)
	key := counterRef{broadcasterID, name}.viewerKey()
	counter := pkg_valkey.SeededCounter{Key: key, Field: field, TTL: counterTTL}
	return counter.Bump(ctx, s.client, delta, func() int64 {
		if c, found, loadErr := s.rpc.CounterGet(ctx, CounterTarget{BroadcasterID: broadcasterID, Name: name, ViewerID: viewerID, Command: command}); loadErr == nil && found {
			return c.Value
		}
		return 0
	})
}

func (s *ValkeyLoyaltyStore) CounterPeek(ctx context.Context, target CounterTarget) (loyaltyrpc.Counter, bool, error) {
	target.Name = NormalizeCounterName(target.Name)
	if target.Name == "" {
		return loyaltyrpc.Counter{}, false, nil
	}
	target.Command = NormalizeCounterName(target.Command)
	scope, err := s.scope(ctx, target.BroadcasterID, target.Name)
	if err != nil {
		return s.rpc.CounterGet(ctx, target)
	}

	if v, ok := s.peekView(ctx, target, scope); ok {
		return loyaltyrpc.Counter{Name: target.Name, Scope: string(scope), Value: v}, true, nil
	}
	return s.rpc.CounterGet(ctx, target)
}

func (s *ValkeyLoyaltyStore) peekView(ctx context.Context, target CounterTarget, scope counterScope) (int64, bool) {
	broadcasterID, name, viewerID, command := target.BroadcasterID, target.Name, target.ViewerID, target.Command
	var (
		v   int64
		ok  bool
		err error
	)
	ref := counterRef{broadcasterID, name}
	if viewScope, viewViewer, viewCmd := bumpTarget(scope, viewerID, command); !rowScoped(viewScope) {
		field := entryField(viewScope, viewViewer, viewCmd)
		v, ok, err = pkg_valkey.ReadHashInt(ctx, s.primary, ref.viewerKey(), field)
	} else {
		v, ok, err = pkg_valkey.NewKV(s.primary).ReadInt(ctx, ref.channelKey())
	}
	if err != nil {
		s.log.Debug("loyalty: counter view read failed", zap.String("counter", name), zap.Error(err))
		return 0, false
	}
	return v, ok
}

func (s *ValkeyLoyaltyStore) CounterInvalidate(ctx context.Context, broadcasterID uint64, name string) {
	name = NormalizeCounterName(name)
	if name == "" {
		return
	}
	ref := counterRef{broadcasterID, name}
	s.invalidateCounter(ctx, ref)
}

func (s *ValkeyLoyaltyStore) invalidateCounter(ctx context.Context, ref counterRef) {
	if err := pkg_valkey.DelKeys(ctx, s.client, ref.channelKey(), ref.viewerKey()); err != nil {
		s.log.Warn("loyalty: failed to invalidate counter view",
			module.BIDField(ref.broadcasterID), zap.String("counter", ref.name), zap.Error(err))
	}
	s.scopes.Invalidate(ref.scopeKey())
}

func (s *ValkeyLoyaltyStore) BalanceGet(ctx context.Context, broadcasterID, viewerID uint64) (loyaltyrpc.Balance, error) {
	key := balanceKey(broadcasterID, viewerID)
	kv := pkg_valkey.NewKV(s.client)
	if raw, ok := kv.GetString(ctx, key); ok {
		if points, watch, ok := decodeBalance(raw); ok {
			return loyaltyrpc.Balance{ViewerID: strconv.FormatUint(viewerID, 10), Points: points, WatchSeconds: watch}, nil
		}
	}
	bal, err := s.rpc.BalanceGet(ctx, broadcasterID, viewerID)
	if err != nil {
		return loyaltyrpc.Balance{}, err
	}
	_ = kv.Set(ctx, pkg_valkey.Key{Name: key, TTL: balanceTTL},
		strconv.FormatInt(bal.Points, 10)+":"+strconv.FormatUint(bal.WatchSeconds, 10))
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

func (s *ValkeyLoyaltyStore) BalanceCredit(ctx context.Context, credit PointCredit) (CreditOutcome, error) {
	out, err := s.rpc.BalanceCredit(ctx, credit)
	if err != nil {
		return out, err
	}
	if err := pkg_valkey.DelKeys(ctx, s.client, balanceKey(credit.BroadcasterID, credit.ViewerID)); err != nil {
		s.log.Warn("loyalty: failed to invalidate balance view", module.BIDField(credit.BroadcasterID), zap.Error(err))
	}
	s.invalidateTop(cache.Event{ID: credit.BroadcasterID})
	return out, nil
}

func (s *ValkeyLoyaltyStore) BalanceCountAtLeast(ctx context.Context, broadcasterID uint64, mins BalanceMinimums) (loyaltyrpc.BalanceCount, error) {
	return s.rpc.BalanceCountAtLeast(ctx, broadcasterID, mins)
}

func (s *ValkeyLoyaltyStore) BalanceSnapshot(ctx context.Context, broadcasterID, viewerID uint64) (loyaltyrpc.BalanceSnapshot, error) {
	return s.rpc.BalanceSnapshot(ctx, broadcasterID, viewerID)
}

func (s *ValkeyLoyaltyStore) WatchStatus(ctx context.Context) (loyaltyrpc.WatchStatus, error) {
	return s.rpc.WatchStatus(ctx)
}

func (s *ValkeyLoyaltyStore) BalanceAdjustViewer(ctx context.Context, adjustment BalanceAdjustment) (loyaltyrpc.Balance, bool, error) {
	bal, found, err := s.rpc.BalanceAdjustViewer(ctx, adjustment)
	if err != nil || !found {
		return bal, found, err
	}
	s.invalidateTop(cache.Event{ID: adjustment.BroadcasterID})
	return bal, true, nil
}

func (s *ValkeyLoyaltyStore) BalanceTransfer(ctx context.Context, t PointTransfer) (bal loyaltyrpc.Balance, found, moved bool, err error) {
	bal, _, found, moved, err = s.rpc.BalanceTransfer(ctx, t)
	if err != nil || !found {
		return bal, found, moved, err
	}
	s.invalidateTop(cache.Event{ID: t.BroadcasterID})
	return bal, true, moved, nil
}

const topCacheTTL = time.Minute

const topCacheCapacity int64 = 4096

func (s *ValkeyLoyaltyStore) Top(ctx context.Context, broadcasterID uint64, limit int) ([]loyaltyrpc.Balance, error) {
	key := strconv.FormatUint(broadcasterID, 10) + ":" + strconv.Itoa(limit)
	return s.top.GetOrLoadTTL(ctx, key, func(ctx context.Context) ([]loyaltyrpc.Balance, time.Duration, error) {
		reply, err := s.rpc.topReply(ctx, broadcasterID, limit)
		return reply.Top, freezeTTL(reply.FreezeStamp, topCacheTTL), err
	})
}

func (s *ValkeyLoyaltyStore) CounterCreate(ctx context.Context, broadcasterID uint64, name, scope string) (loyaltyrpc.Counter, error) {
	c, err := s.rpc.CounterCreate(ctx, counterRef{broadcasterID, name}, counterScope(scope))
	if err != nil {
		return loyaltyrpc.Counter{}, err
	}
	s.CounterInvalidate(ctx, broadcasterID, name)
	return c, nil
}

func (s *ValkeyLoyaltyStore) CounterSet(ctx context.Context, set CounterAssignment) (bool, error) {
	found, err := s.rpc.CounterSet(ctx, set)
	if err != nil || !found {
		return found, err
	}
	s.CounterInvalidate(ctx, set.BroadcasterID, set.Name)
	return true, nil
}

func (s *ValkeyLoyaltyStore) CounterDelete(ctx context.Context, broadcasterID uint64, name string) error {
	if err := s.rpc.CounterDelete(ctx, counterRef{broadcasterID, name}); err != nil {
		return err
	}
	s.CounterInvalidate(ctx, broadcasterID, name)
	return nil
}

func (s *ValkeyLoyaltyStore) CounterList(ctx context.Context, broadcasterID uint64) ([]loyaltyrpc.Counter, error) {
	return s.rpc.CounterList(ctx, broadcasterID)
}

func (s *ValkeyLoyaltyStore) BalanceWager(ctx context.Context, wager PointWager) (WagerOutcome, error) {
	out, err := s.rpc.BalanceWager(ctx, wager)
	if err == nil && out.Found {
		s.invalidateTop(cache.Event{ID: wager.BroadcasterID})
	}
	return out, err
}

const balanceInvalidateTimeout = 5 * time.Second

func (s *ValkeyLoyaltyStore) BindInvalidations(h cache.Feed) {
	h.Watch(invalidate.ScopeLoyalty, s.invalidateTop)
	h.Watch(invalidate.ScopeLoyaltyCounters, s.onCounterInvalidation)
	h.WatchGap(s.top.Clear)
	h.WatchGap(s.scopes.Clear)
}

func (s *ValkeyLoyaltyStore) invalidateTop(e cache.Event) {
	topPrefix := strconv.FormatUint(e.ID, 10) + ":"
	s.top.InvalidateWhere(func(key string) bool { return strings.HasPrefix(key, topPrefix) })
}

func (s *ValkeyLoyaltyStore) onCounterInvalidation(e cache.Event) {
	if len(e.Keys) == 0 {
		prefix := cache.PairKey("scope:", e.ID, "")
		s.scopes.InvalidateWhere(func(key string) bool { return strings.HasPrefix(key, prefix) })
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), balanceInvalidateTimeout)
	defer cancel()
	for _, name := range e.Keys {
		s.invalidateCounter(ctx, counterRef{e.ID, name})
	}
}

func (s *ValkeyLoyaltyStore) Close() {
	s.scopes.Close()
	s.top.Close()
}
