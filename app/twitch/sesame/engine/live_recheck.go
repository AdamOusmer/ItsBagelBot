// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"go.uber.org/zap"
)

const (
	recheckKeyPrefix    = "live:recheck:"
	expiryRecheckWindow = 10 * time.Second

	demandRecheckKeyPrefix = "live:demand:"
	demandRecheckWindow    = 5 * time.Minute

	offlineConfirmKeyPrefix = "live:confirm:"
)

// Helix can trail a go-live by minutes: one early check may confirm a false offline.
var defaultOfflineConfirmDelays = []time.Duration{2 * time.Minute, 10 * time.Minute}

type recheckClaim struct {
	key           string
	window        time.Duration
	broadcasterID string
}

func (s *ValkeyLiveStore) claimRecheck(ctx context.Context, claim recheckClaim) {
	won, err := pkg_valkey.ClaimOnce(ctx, s.client, claim.key, claim.window)
	if err != nil || !won {
		return
	}
	if err := s.requestRecheck(ctx, claim.broadcasterID); err != nil {
		s.log.Warn("live: failed to publish re-check", zap.String("broadcaster_id", claim.broadcasterID), zap.Error(err))
	}
}

func (s *ValkeyLiveStore) armOfflineConfirm(ctx context.Context, broadcasterID uint64) {
	admissionFence, allowed := s.confirmationFence(ctx, broadcasterID)
	if !allowed {
		return
	}
	id := strconv.FormatUint(broadcasterID, 10)
	items := make([]pkg_valkey.MSet, 0, len(s.cfg.OfflineConfirmDelays)+1)
	if admissionFence != "" {
		items = append(items, pkg_valkey.MSet{Key: offlineConfirmKeyPrefix + "admission:" + id, Value: admissionFence, TTL: s.confirmationRetention()})
	}
	items = append(items, pkg_valkey.MSet{Key: demandRecheckKeyPrefix + id, Value: "1", TTL: demandRecheckWindow})
	for stage, delay := range s.cfg.OfflineConfirmDelays {
		key := offlineConfirmKeyPrefix + strconv.Itoa(stage) + ":" + id
		items = append(items, pkg_valkey.MSet{Key: key, Value: "1", TTL: delay})
	}
	if err := pkg_valkey.SetAll(ctx, s.client, items); err != nil {
		s.log.Warn("live: failed to arm offline confirmation", module.BIDField(broadcasterID), zap.Error(err))
	}
}

func (s *ValkeyLiveStore) confirmationFence(ctx context.Context, broadcasterID uint64) (string, bool) {
	if s.cfg.Runtime == nil {
		return "", true
	}
	state, err := s.cfg.Runtime.Get(ctx, broadcasterID)
	if err != nil || !state.AllowsRuntime() {
		return "", false
	}
	return runtimeFence(state), true
}

func (s *ValkeyLiveStore) confirmationRetention() time.Duration {
	retention := time.Minute
	for _, delay := range s.cfg.OfflineConfirmDelays {
		retention = max(retention, delay+time.Minute)
	}
	return retention
}

func (s *ValkeyLiveStore) liveKeyExists(ctx context.Context, broadcasterID uint64) bool {
	n, err := pkg_valkey.CountExisting(ctx, s.client, []string{liveKey(broadcasterID)})
	return err == nil && n > 0
}
