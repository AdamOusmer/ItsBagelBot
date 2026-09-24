// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const chattersSnapshotPrefix = "chatters:"

const chattersFetchLockPrefix = "chatters:fetch:"

const chattersSnapshotTTL = watchTickInterval + watchTickJitter

const (
	chattersFetchLockTTL = 10 * time.Second
	chattersSnapshotCap  = 5000
)

type chattersSnapshotEntry struct {
	ID    uint64 `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

type ValkeyChatters struct {
	client valkey.Client
	log    *zap.Logger
}

func NewValkeyChatters(client valkey.Client, log *zap.Logger) *ValkeyChatters {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyChatters{client: client, log: log}
}

func chattersSnapshotKey(broadcasterID uint64) string {
	return cache.UserKey(chattersSnapshotPrefix, broadcasterID)
}

func chattersFetchLockKey(broadcasterID uint64) string {
	return cache.UserKey(chattersFetchLockPrefix, broadcasterID)
}

func (v *ValkeyChatters) Snapshot(ctx context.Context, broadcasterID uint64) (entries []chattersSnapshotEntry, ok bool, err error) {
	if v == nil || v.client == nil {
		return nil, false, nil
	}
	raw, err := v.client.Do(ctx, v.client.B().Get().Key(chattersSnapshotKey(broadcasterID)).Build()).AsBytes()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var out []chattersSnapshotEntry
	if err := codec.Unmarshal(raw, &out); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (v *ValkeyChatters) Store(ctx context.Context, broadcasterID uint64, entries []chattersSnapshotEntry) {
	if v == nil || v.client == nil {
		return
	}
	if len(entries) > chattersSnapshotCap {
		entries = entries[:chattersSnapshotCap]
	}
	body, err := codec.Marshal(entries)
	if err != nil {
		v.log.Warn("chatters: snapshot encode failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	seconds := int64(chattersSnapshotTTL.Seconds())
	err = v.client.Do(ctx, v.client.B().Set().Key(chattersSnapshotKey(broadcasterID)).Value(string(body)).ExSeconds(seconds).Build()).Error()
	if err != nil {
		v.log.Warn("chatters: snapshot write failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

func (v *ValkeyChatters) TryFetchLock(ctx context.Context, broadcasterID uint64) bool {
	if v == nil || v.client == nil {
		return false
	}
	seconds := int64(chattersFetchLockTTL.Seconds())
	got, err := v.client.Do(ctx, v.client.B().Set().Key(chattersFetchLockKey(broadcasterID)).Value("1").Nx().ExSeconds(seconds).Build()).ToString()
	return err == nil && got == "OK"
}

func (v *ValkeyChatters) ReleaseFetchLock(ctx context.Context, broadcasterID uint64) {
	if v == nil || v.client == nil {
		return
	}
	err := v.client.Do(ctx, v.client.B().Del().Key(chattersFetchLockKey(broadcasterID)).Build()).Error()
	if err != nil {
		v.log.Warn("chatters: fetch lock release failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}
