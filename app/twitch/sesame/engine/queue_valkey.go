// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/pkg/cache"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type QueueStore interface {
	SetOpen(ctx context.Context, broadcasterID uint64, open bool) error
	IsOpen(ctx context.Context, broadcasterID uint64) (bool, error)
	Join(ctx context.Context, broadcasterID uint64, login string) (pos, size int64, joined bool, err error)
	Remove(ctx context.Context, broadcasterID uint64, login string) (bool, error)
	Pop(ctx context.Context, broadcasterID uint64) (login string, remaining int64, err error)
	List(ctx context.Context, broadcasterID uint64, n int64) (entries []string, total int64, err error)
	Clear(ctx context.Context, broadcasterID uint64) error
}

const (
	queueOpenPrefix = "queue:open:"
	queueLinePrefix = "queue:line:"
)

type ValkeyQueueStore struct {
	client valkey.Client
	ttl    time.Duration
	log    *zap.Logger
}

func NewValkeyQueueStore(client valkey.Client, ttl time.Duration, log *zap.Logger) *ValkeyQueueStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyQueueStore{client: pkg_valkey.Primary(client), ttl: ttl, log: log}
}

func queueOpenKey(id uint64) string { return cache.UserKey(queueOpenPrefix, id) }
func queueLineKey(id uint64) string { return cache.UserKey(queueLinePrefix, id) }

func (s *ValkeyQueueStore) SetOpen(ctx context.Context, broadcasterID uint64, open bool) error {
	if !open {
		return s.client.Do(ctx, s.client.B().Del().Key(queueOpenKey(broadcasterID)).Build()).Error()
	}
	seconds := int64(s.ttl.Seconds())
	for _, r := range s.client.DoMulti(ctx,
		s.client.B().Set().Key(queueOpenKey(broadcasterID)).Value("1").ExSeconds(seconds).Build(),
		s.client.B().Expire().Key(queueLineKey(broadcasterID)).Seconds(seconds).Build(),
	) {
		if err := r.Error(); err != nil && !valkey.IsValkeyNil(err) {
			return err
		}
	}
	return nil
}

func (s *ValkeyQueueStore) IsOpen(ctx context.Context, broadcasterID uint64) (bool, error) {
	n, err := s.client.Do(ctx, s.client.B().Exists().Key(queueOpenKey(broadcasterID)).Build()).AsInt64()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *ValkeyQueueStore) Join(ctx context.Context, broadcasterID uint64, login string) (pos, size int64, joined bool, err error) {
	key := queueLineKey(broadcasterID)
	seconds := int64(s.ttl.Seconds())
	resps := s.client.DoMulti(ctx,
		s.client.B().Zadd().Key(key).Nx().ScoreMember().ScoreMember(float64(time.Now().UnixMilli()), login).Build(),
		s.client.B().Zrank().Key(key).Member(login).Build(),
		s.client.B().Zcard().Key(key).Build(),
		s.client.B().Expire().Key(key).Seconds(seconds).Build(),
		s.client.B().Expire().Key(queueOpenKey(broadcasterID)).Seconds(seconds).Build(),
	)
	added, err := resps[0].AsInt64()
	if err != nil {
		return 0, 0, false, err
	}
	rank, err := resps[1].AsInt64()
	if err != nil {
		return 0, 0, false, err
	}
	size, err = resps[2].AsInt64()
	if err != nil {
		return 0, 0, false, err
	}
	return rank + 1, size, added > 0, nil
}

func (s *ValkeyQueueStore) Remove(ctx context.Context, broadcasterID uint64, login string) (bool, error) {
	n, err := s.client.Do(ctx, s.client.B().Zrem().Key(queueLineKey(broadcasterID)).Member(login).Build()).AsInt64()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *ValkeyQueueStore) Pop(ctx context.Context, broadcasterID uint64) (login string, remaining int64, err error) {
	key := queueLineKey(broadcasterID)
	resps := s.client.DoMulti(ctx,
		s.client.B().Zpopmin().Key(key).Count(1).Build(),
		s.client.B().Zcard().Key(key).Build(),
	)
	popped, err := resps[0].AsZScores()
	if err != nil {
		return "", 0, err
	}
	remaining, err = resps[1].AsInt64()
	if err != nil {
		return "", 0, err
	}
	if len(popped) == 0 {
		return "", remaining, nil
	}
	return popped[0].Member, remaining, nil
}

func (s *ValkeyQueueStore) List(ctx context.Context, broadcasterID uint64, n int64) (entries []string, total int64, err error) {
	if n <= 0 {
		return nil, 0, nil
	}
	key := queueLineKey(broadcasterID)
	resps := s.client.DoMulti(ctx,
		s.client.B().Zrange().Key(key).Min("0").Max(strconv.FormatInt(n-1, 10)).Build(),
		s.client.B().Zcard().Key(key).Build(),
	)
	entries, err = resps[0].AsStrSlice()
	if err != nil {
		return nil, 0, err
	}
	total, err = resps[1].AsInt64()
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

func (s *ValkeyQueueStore) Clear(ctx context.Context, broadcasterID uint64) error {
	return s.client.Do(ctx, s.client.B().Del().Key(queueLineKey(broadcasterID)).Build()).Error()
}
