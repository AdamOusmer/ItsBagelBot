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
}

func NewRewardSnapshots(client valkey.Client) *RewardSnapshots {
	return &RewardSnapshots{client: pkg_valkey.Primary(client)}
}

type rewardRef struct {
	broadcasterID string
	rewardID      string
}

func (r rewardRef) stagedKey() string {
	return "outgress:rewards:staged:" + r.broadcasterID + ":" + r.rewardID
}

func (s *RewardSnapshots) has(ctx context.Context, reward rewardRef) (bool, error) {
	committed, err := s.client.Do(ctx, s.client.B().Hexists().Key(outgress.RewardSnapshotKey(reward.broadcasterID)).Field(reward.rewardID).Build()).AsBool()
	if err != nil || committed {
		return committed, err
	}
	return s.client.Do(ctx, s.client.B().Exists().Key(reward.stagedKey()).Build()).AsBool()
}

func (s *RewardSnapshots) stage(ctx context.Context, reward rewardRef, snapshot rewardSnapshot) error {
	body, err := codec.Marshal(snapshot)
	if err != nil {
		return err
	}
	cmd := s.client.B().Set().Key(reward.stagedKey()).Value(string(body)).Nx().Px(rewardStagedTTL).Build()
	if err := s.client.Do(ctx, cmd).Error(); !valkey.IsValkeyNil(err) {
		return err
	}
	return nil
}

func (s *RewardSnapshots) commit(ctx context.Context, reward rewardRef) error {
	staged := reward.stagedKey()
	body, err := s.client.Do(ctx, s.client.B().Get().Key(staged).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return nil
	}
	if err != nil {
		return err
	}
	key := outgress.RewardSnapshotKey(reward.broadcasterID)
	results := s.client.DoMulti(ctx,
		s.client.B().Hsetnx().Key(key).Field(reward.rewardID).Value(body).Build(),
		s.client.B().Expire().Key(key).Seconds(int64(rewardSnapshotTTL.Seconds())).Build(),
		s.client.B().Del().Key(staged).Build(),
	)
	for _, result := range results {
		if err := result.Error(); err != nil {
			return err
		}
	}
	return nil
}

func (s *RewardSnapshots) discard(ctx context.Context, reward rewardRef) error {
	return s.client.Do(ctx, s.client.B().Del().Key(reward.stagedKey()).Build()).Error()
}

func (s *RewardSnapshots) all(ctx context.Context, broadcasterID string) (map[string]rewardSnapshot, error) {
	cmd := s.client.B().Hgetall().Key(outgress.RewardSnapshotKey(broadcasterID)).Build()
	raw, err := s.client.Do(ctx, cmd).AsStrMap()
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
	cmd := s.client.B().Hdel().Key(outgress.RewardSnapshotKey(reward.broadcasterID)).Field(reward.rewardID).Build()
	return s.client.Do(ctx, cmd).Error()
}

func (s *RewardSnapshots) changeLock(broadcasterID, owner string) pkg_valkey.OwnerLock {
	return pkg_valkey.NewOwnerLock(s.client, "outgress:rewards:change:"+broadcasterID, owner)
}

func (s *RewardSnapshots) restoreLock(broadcasterID, owner string) pkg_valkey.OwnerLock {
	return pkg_valkey.NewOwnerLock(s.client, "outgress:rewards:restore:"+broadcasterID, owner)
}
