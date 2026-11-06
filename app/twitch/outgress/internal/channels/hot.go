package channels

import (
	"context"
	"strconv"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"
)

const hotIndexKey = "outgress:channels:hot"
const hotRetention = 7 * 24 * time.Hour
const hotBatch = 500
const hotRenewWindow = 24 * time.Hour
const hotRenewInterval = time.Hour
const hotRenewLockKey = "outgress:channels:hot:renew"
const hotMigrationCursorKey = "outgress:channels:migration-cursor"

func (r *Registry) retainHot(ctx context.Context, id string) error {
	score := time.Now().Add(hotRetention).Unix()
	return pkg_valkey.RetainIfPresent{Key: keyPrefix + id, Index: hotIndexKey, Member: id, TTL: hotRetention, Score: score}.Run(ctx, r.client)
}
func (r *Registry) RenewHot(ctx context.Context, keep func(context.Context, string) bool) error {
	claimed, err := r.claimRenewPass(ctx)
	if err != nil || !claimed {
		return err
	}
	due, err := r.renewalCandidates(ctx)
	if err != nil {
		return err
	}
	for _, id := range due {
		if err := r.renewIfKept(ctx, id, keep); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) claimRenewPass(ctx context.Context) (bool, error) {
	return pkg_valkey.ClaimOnce(ctx, r.client, hotRenewLockKey, hotRenewInterval.Truncate(time.Second))
}

func (r *Registry) renewalCandidates(ctx context.Context) ([]string, error) {
	before := strconv.FormatInt(time.Now().Add(hotRenewWindow).Unix(), 10)
	var due []string
	for offset := int64(0); ; offset += hotBatch {
		ids, err := pkg_valkey.ZRangeByScoreLimit(ctx, r.client, pkg_valkey.ScorePage{Key: hotIndexKey, Min: "-inf", Max: before, Offset: offset, Count: hotBatch})
		if err != nil {
			return nil, err
		}
		due = append(due, ids...)
		if len(ids) < hotBatch {
			return due, nil
		}
	}
}

func (r *Registry) renewIfKept(ctx context.Context, id string, keep func(context.Context, string) bool) error {
	if !keep(ctx, id) {
		return nil
	}
	return r.retainHot(ctx, id)
}

func (r *Registry) PruneHot(ctx context.Context) error {
	migration := pkg_valkey.RetentionMigration{
		Source: indexKey, Index: hotIndexKey, Cursor: hotMigrationCursorKey,
		Batch: hotBatch, KeyPrefix: keyPrefix, FloorTTL: hotRetention, Now: time.Now(),
	}
	if _, err := migration.Run(ctx, r.client); err != nil {
		return err
	}
	sweep := pkg_valkey.RetentionSweep{
		Index: hotIndexKey, SetIdx: []string{indexKey, unhealthyKey}, KeyPrefix: keyPrefix,
		Now: time.Now(), Batch: hotBatch, FloorTTL: hotRetention,
	}
	_, err := sweep.Run(ctx, r.client)
	return err
}

func (r *Registry) EraseIncarnation(ctx context.Context, id string, incarnation int64) error {
	erase := pkg_valkey.GuardedErase{
		Key: keyPrefix + id, SortedIdx: hotIndexKey, SetIdx: []string{indexKey, unhealthyKey},
		GuardField: "account_created_at", Expect: strconv.FormatInt(incarnation, 10), Member: id,
	}
	if _, err := erase.Run(ctx, r.client); err != nil {
		return err
	}
	r.cache.Invalidate(id)
	return nil
}
func (r *Registry) SetIncarnation(ctx context.Context, id string, incarnation int64) error {
	return pkg_valkey.HashField{Key: keyPrefix + id, Field: "account_created_at"}.Set(ctx, r.client, strconv.FormatInt(incarnation, 10))
}
