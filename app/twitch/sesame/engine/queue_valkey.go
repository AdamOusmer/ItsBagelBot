// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
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

func (s *ValkeyQueueStore) queue(broadcasterID uint64) pkg_valkey.SimpleQueue {
	return pkg_valkey.SimpleQueue{Line: queueLineKey(broadcasterID), Open: queueOpenKey(broadcasterID)}
}

func (s *ValkeyQueueStore) SetOpen(ctx context.Context, broadcasterID uint64, open bool) error {
	q := s.queue(broadcasterID)
	if !open {
		return q.SetClosed(ctx, s.client)
	}
	return q.SetOpen(ctx, s.client, s.ttl)
}

func (s *ValkeyQueueStore) IsOpen(ctx context.Context, broadcasterID uint64) (bool, error) {
	return s.queue(broadcasterID).IsOpen(ctx, s.client)
}

func (s *ValkeyQueueStore) Join(ctx context.Context, broadcasterID uint64, login string) (pos, size int64, joined bool, err error) {
	return s.queue(broadcasterID).Join(ctx, s.client, pkg_valkey.QueueEntry{Member: login, At: time.Now(), TTL: s.ttl})
}

func (s *ValkeyQueueStore) Remove(ctx context.Context, broadcasterID uint64, login string) (bool, error) {
	return s.queue(broadcasterID).Remove(ctx, s.client, login)
}

func (s *ValkeyQueueStore) Pop(ctx context.Context, broadcasterID uint64) (login string, remaining int64, err error) {
	return s.queue(broadcasterID).Pop(ctx, s.client)
}

func (s *ValkeyQueueStore) List(ctx context.Context, broadcasterID uint64, n int64) (entries []string, total int64, err error) {
	if n <= 0 {
		return nil, 0, nil
	}
	return s.queue(broadcasterID).List(ctx, s.client, n)
}

func (s *ValkeyQueueStore) Clear(ctx context.Context, broadcasterID uint64) error {
	return s.queue(broadcasterID).Clear(ctx, s.client)
}
