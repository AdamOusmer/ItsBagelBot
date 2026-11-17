// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"slices"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

func (r *Commands) publishDirtyUses(keys []commandKey) []commandKey {
	for len(keys) > 0 {
		n := usePublicationSize(keys)
		if err := r.publishUseChunk(keys[:n]); err != nil {
			return keys
		}
		keys = keys[n:]
	}
	return nil
}

func usePublicationSize(keys []commandKey) int {
	n, bytes := 0, 0
	for _, key := range keys {
		size := 32 + 4*len(key.name)
		if n == useMaxRows || bytes+size > useMaxBytes {
			break
		}
		n++
		bytes += size
	}
	return n
}

func (r *Commands) publishUseChunk(keys []commandKey) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	txn := r.app.StartTransaction("publish command uses")
	defer txn.End()
	ctx = newrelic.NewContext(ctx, txn)
	for _, userID := range distinctUsers(keys) {
		r.Invalidate(userID)
	}
	states, err := r.rowStates(ctx, keys)
	if err != nil {
		txn.NoticeError(err)
		r.log.Warn("failed to load rows after uses flush", zap.Error(err))
		return err
	}
	return r.publishUseStates(ctx, states)
}

func distinctUsers(items []commandKey) []uint64 {
	ids := make([]uint64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.userID)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func (r *Commands) publishUseStates(ctx context.Context, states map[commandKey]data.CommandChangedDTO) error {
	for key, dto := range states {
		if err := bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, dto); err != nil {
			r.log.Error("failed to publish command uses change",
				zap.Uint64("user_id", key.userID), zap.String("command", key.name), zap.Error(err))
			return err
		}
	}
	return nil
}
