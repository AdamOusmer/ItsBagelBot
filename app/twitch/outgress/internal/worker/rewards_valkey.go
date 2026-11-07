// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const (
	rewardSnapshotTTL    = 30 * 24 * time.Hour
	rewardRestoreLockTTL = 2 * time.Minute
	rewardChangeLockWait = 2 * time.Second
	rewardChangeLockPoll = 25 * time.Millisecond
	// Outlives redelivery of one change, short enough that an abandoned one cannot outlast a later edit by much.
	rewardStagedTTL = 15 * time.Minute
)

type rewardSnapshot struct {
	Enabled bool `json:"enabled"`
	Cost    int  `json:"cost"`
}

type RewardSnapshots struct {
	client valkey.Client
	kv     pkg_valkey.KV
}

func NewRewardSnapshots(client valkey.Client) *RewardSnapshots {
	primary := pkg_valkey.Primary(client)
	return &RewardSnapshots{client: primary, kv: pkg_valkey.NewKV(primary)}
}

type rewardRef struct {
	broadcasterID string
	rewardID      string
}

func (r rewardRef) stagedKey() string {
	return "outgress:rewards:staged:" + r.broadcasterID + ":" + r.rewardID
}

func (r rewardRef) field() pkg_valkey.HashField {
	return pkg_valkey.HashField{Key: outgress.RewardSnapshotKey(r.broadcasterID), Field: r.rewardID}
}

func (s *RewardSnapshots) has(ctx context.Context, reward rewardRef) (bool, error) {
	committed, err := reward.field().Exists(ctx, s.client)
	if err != nil || committed {
		return committed, err
	}
	staged, err := pkg_valkey.CountExisting(ctx, s.client, []string{reward.stagedKey()})
	return staged > 0, err
}

func (s *RewardSnapshots) stage(ctx context.Context, reward rewardRef, snapshot rewardSnapshot) error {
	body, err := codec.Marshal(snapshot)
	if err != nil {
		return err
	}
	return s.kv.SetIfAbsent(ctx, pkg_valkey.Key{Name: reward.stagedKey(), TTL: rewardStagedTTL}, string(body))
}

func (s *RewardSnapshots) commit(ctx context.Context, reward rewardRef) error {
	return reward.field().CommitStaged(ctx, s.client, pkg_valkey.StagedCommit{Staged: reward.stagedKey(), HashTTL: rewardSnapshotTTL})
}

func (s *RewardSnapshots) discard(ctx context.Context, reward rewardRef) error {
	return s.kv.Del(ctx, reward.stagedKey())
}

func (s *RewardSnapshots) all(ctx context.Context, broadcasterID string) (map[string]rewardSnapshot, error) {
	raw, err := pkg_valkey.HashAll(ctx, s.client, outgress.RewardSnapshotKey(broadcasterID))
	if err != nil {
		return nil, err
	}
	saved := make(map[string]rewardSnapshot, len(raw))
	for rewardID, body := range raw {
		var snapshot rewardSnapshot
		if codec.Unmarshal([]byte(body), &snapshot) == nil {
			saved[rewardID] = snapshot
		}
	}
	return saved, nil
}

func (s *RewardSnapshots) clear(ctx context.Context, reward rewardRef) error {
	return reward.field().Delete(ctx, s.client)
}

// A success overwrites and a failure only fills an empty field, so a redelivery can never turn a success into a failure.
func (s *RewardSnapshots) recordResult(ctx context.Context, payload *outgress.Message, result string) error {
	field := pkg_valkey.HashField{Key: outgress.RewardResultKey(payload.BroadcasterID, payload.RunID), Field: payload.ActionSlot}
	return field.Write(ctx, s.client, pkg_valkey.HashWrite{Value: result, Replace: result == outgress.RewardResultOK, TTL: outgress.RewardResultTTL})
}

func (s *RewardSnapshots) changeLock(broadcasterID, owner string) pkg_valkey.OwnerLock {
	return pkg_valkey.NewOwnerLock(s.client, "outgress:rewards:change:"+broadcasterID, owner)
}

func (s *RewardSnapshots) restoreLock(broadcasterID, owner string) pkg_valkey.OwnerLock {
	return pkg_valkey.NewOwnerLock(s.client, "outgress:rewards:restore:"+broadcasterID, owner)
}
