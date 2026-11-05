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

func (s *RewardSnapshots) has(ctx context.Context, broadcasterID, rewardID string) (bool, error) {
	cmd := s.client.B().Hexists().Key(outgress.RewardSnapshotKey(broadcasterID)).Field(rewardID).Build()
	return s.client.Do(ctx, cmd).AsBool()
}

func (s *RewardSnapshots) saveFirst(ctx context.Context, broadcasterID, rewardID string, snapshot rewardSnapshot) error {
	body, err := codec.Marshal(snapshot)
	if err != nil {
		return err
	}
	key := outgress.RewardSnapshotKey(broadcasterID)
	results := s.client.DoMulti(ctx,
		s.client.B().Hsetnx().Key(key).Field(rewardID).Value(string(body)).Build(),
		s.client.B().Expire().Key(key).Seconds(int64(rewardSnapshotTTL.Seconds())).Build(),
	)
	for _, result := range results {
		if err := result.Error(); err != nil {
			return err
		}
	}
	return nil
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

func (s *RewardSnapshots) clear(ctx context.Context, broadcasterID, rewardID string) error {
	cmd := s.client.B().Hdel().Key(outgress.RewardSnapshotKey(broadcasterID)).Field(rewardID).Build()
	return s.client.Do(ctx, cmd).Error()
}

func (s *RewardSnapshots) restoreLock(broadcasterID, owner string) pkg_valkey.OwnerLock {
	return pkg_valkey.NewOwnerLock(s.client, "outgress:rewards:restore:"+broadcasterID, owner)
}
