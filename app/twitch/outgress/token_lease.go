// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"go.uber.org/zap"
)

const mintLeaseKeyPrefix = "outgress:token:mint:"

const mintLeaseTTL = 15 * time.Second

const mintLeaseReleaseTimeout = 2 * time.Second

func (d *deps) newMintLease(accountID string) twitch.MintLease {
	log := d.log
	lock := pkg_valkey.NewOwnerLock(d.valkey, mintLeaseKeyPrefix+accountID, d.host)

	return twitch.MintLease{
		Acquire: func(ctx context.Context) (func(), bool, bool) {
			ok, err := lock.Acquire(ctx, mintLeaseTTL)
			if err != nil {
				log.Warn("mint lease backend unavailable; minting immediately, uncoordinated",
					zap.String("account_id", accountID), zap.Error(err))
				return nil, false, true
			}
			if !ok {
				return nil, false, false
			}
			return func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), mintLeaseReleaseTimeout)
				defer cancel()
				if err := lock.Release(releaseCtx); err != nil {
					log.Warn("mint lease release failed; it will expire on its own",
						zap.String("account_id", accountID), zap.Error(err))
				}
			}, true, false
		},
	}
}
