// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
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
	id := strconv.FormatUint(broadcasterID, 10)
	cmds := make(valkey.Commands, 0, len(s.cfg.OfflineConfirmDelays)+1)
	cmds = append(cmds, s.client.B().Set().Key(demandRecheckKeyPrefix+id).Value("1").Px(demandRecheckWindow).Build())
	for stage, delay := range s.cfg.OfflineConfirmDelays {
		key := offlineConfirmKeyPrefix + strconv.Itoa(stage) + ":" + id
		cmds = append(cmds, s.client.B().Set().Key(key).Value("1").Px(delay).Build())
	}
	for _, res := range s.client.DoMulti(ctx, cmds...) {
		if err := res.Error(); err != nil {
			s.log.Warn("live: failed to arm offline confirmation", module.BIDField(broadcasterID), zap.Error(err))
			return
		}
	}
}

func (s *ValkeyLiveStore) liveKeyExists(ctx context.Context, broadcasterID uint64) bool {
	n, err := s.client.Do(ctx, s.client.B().Exists().Key(liveKey(broadcasterID)).Build()).AsInt64()
	return err == nil && n > 0
}
