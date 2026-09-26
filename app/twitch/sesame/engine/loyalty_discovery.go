// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	livekey "ItsBagelBot/internal/domain/live"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// Discovery persists its pending SCAN page and cursor before arming channels.
// A slow tenant does not make the next reconciliation restart from zero.
func (s *ValkeyLoyaltyClock) reconcile(ctx context.Context) {
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !s.claimDiscovery(rctx) {
		return
	}
	completedScan := false
	for rctx.Err() == nil {
		pending, err := s.armDiscoveredChannel(rctx)
		if err != nil {
			return
		}
		if pending {
			continue
		}
		if completedScan {
			return
		}
		completedScan, err = s.discoverLivePage(rctx)
		if err != nil {
			return
		}
	}
}
func (s *ValkeyLoyaltyClock) armDiscoveredChannel(ctx context.Context) (bool, error) {
	raw, err := s.client.Do(ctx, s.client.B().Lindex().Key(loyaltyDiscoveryQueueKey).Index(0).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	id, parseErr := strconv.ParseUint(raw, 10, 64)
	if parseErr == nil {
		if !s.arm(ctx, loyaltyArmRequest{broadcaster: id}) {
			return true, errors.New("watch admission unavailable during discovery")
		}
	}
	_, err = s.eval(ctx, loyaltyDiscoveryPopScript, []string{loyaltyDiscoveryQueueKey}, raw)
	return true, err
}
func (s *ValkeyLoyaltyClock) discoverLivePage(ctx context.Context) (bool, error) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(loyaltyDiscoveryCursorKey).Build()).ToString()
	if err != nil && !valkey.IsValkeyNil(err) {
		return false, err
	}
	cursor, _ := strconv.ParseUint(raw, 10, 64)
	scan, err := s.client.Do(ctx, s.client.B().Scan().Cursor(cursor).Match(livekey.KeyPrefix+"*").Count(200).Build()).AsScanEntry()
	if err != nil {
		s.log.Warn("loyalty: live schedule discovery failed", zap.Error(err))
		return false, err
	}
	_, err = s.eval(ctx, loyaltyDiscoverySaveScript, []string{loyaltyDiscoveryCursorKey, loyaltyDiscoveryQueueKey}, loyaltyDiscoveryArgs(scan)...)
	return scan.Cursor == 0, err
}
func loyaltyDiscoveryArgs(scan valkey.ScanEntry) []string {
	args := []string{strconv.FormatUint(scan.Cursor, 10)}
	for _, key := range scan.Elements {
		if id, ok := parseLiveKey(key); ok {
			args = append(args, strconv.FormatUint(id, 10))
		}
	}
	return args
}
func parseLiveKey(key string) (uint64, bool) {
	if !strings.HasPrefix(key, livekey.KeyPrefix) || strings.HasPrefix(key, recheckKeyPrefix) {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(key, livekey.KeyPrefix), 10, 64)
	return id, err == nil && id != 0
}

func (s *ValkeyLoyaltyClock) claimDiscovery(ctx context.Context) bool {
	won, err := pkg_valkey.ClaimOnce(ctx, s.client, loyaltyReconcileClaimKey, loyaltyReconcileClaimTTL)
	return err == nil && won
}
