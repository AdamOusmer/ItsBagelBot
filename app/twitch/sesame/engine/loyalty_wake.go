// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strings"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/cache"
	pkg_valkey "ItsBagelBot/pkg/valkey"
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
	pkg_valkey.ExpiryHubOr(s.expiry, s.client, s.keyspaceDB, s.log).Watch(ctx, s.onExpired, loyaltyTickKeyPrefix)
}

func (s *ValkeyLoyaltyClock) StartRearmWatcher(ctx context.Context) {
	if s.invalidations == nil {
		return
	}
	s.invalidations.Watch(invalidate.ScopeModules, func(e cache.Event) {
		select {
		case s.rearms <- e.ID:
		default:
			s.nudge()
		}
	})
}
