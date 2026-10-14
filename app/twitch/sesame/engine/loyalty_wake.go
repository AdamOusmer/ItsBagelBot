// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

func (s *ValkeyLoyaltyClock) nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *ValkeyLoyaltyClock) onExpired(_ context.Context, key string) {
	if strings.HasPrefix(key, loyaltyTickKeyPrefix) && !strings.HasPrefix(key, loyaltyTickClaimPrefix) {
		s.nudge()
	}
}

func (s *ValkeyLoyaltyClock) StartExpiryWatcher(ctx context.Context) {
	expiryHubOr(s.expiry, s.client, s.keyspaceDB, s.log).Watch(ctx, s.onExpired, loyaltyTickKeyPrefix)
}

func (s *ValkeyLoyaltyClock) StartRearmWatcher(ctx context.Context) {
	if s.nc == nil || s.rearmSubject == "" {
		return
	}
	sub, err := s.nc.Subscribe(s.rearmSubject, func(msg *nats.Msg) {
		var dto invalidate.DTO
		if codec.Unmarshal(msg.Data, &dto) != nil {
			return
		}
		if id, err := strconv.ParseUint(dto.BroadcasterID, 10, 64); err == nil && id != 0 {
			select {
			case s.rearms <- id:
			default:
				s.nudge()
			}
		}
	})
	if err != nil {
		s.log.Error("loyalty: rearm watcher unavailable", zap.Error(err))
		return
	}
	defer sub.Unsubscribe()
	<-ctx.Done()
}
