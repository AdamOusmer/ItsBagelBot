// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency

import (
	"context"
	"time"

	pkgvalkey "ItsBagelBot/pkg/valkey"
	valkeygo "github.com/valkey-io/valkey-go"
)

type CompletedBatch struct {
	Keys []EffectKey
	TTL  time.Duration
}

type BatchStore interface {
	CompleteBatch(context.Context, CompletedBatch) []error
}

func CompleteBatch(ctx context.Context, store Store, batch CompletedBatch) []error {
	if writer, ok := store.(BatchStore); ok {
		return writer.CompleteBatch(ctx, batch)
	}
	errs := make([]error, len(batch.Keys))
	for i, key := range batch.Keys {
		_, errs[i] = store.Seen(ctx, string(key), batch.TTL)
	}
	return errs
}

func (s *ValkeyStore) CompleteBatch(ctx context.Context, batch CompletedBatch) []error {
	cmds := make(valkeygo.Commands, len(batch.Keys))
	for i, key := range batch.Keys {
		cmds[i] = s.client.B().Set().Key(Key(s.prefix, key)).Value("1").Nx().PxMilliseconds(millis(batch.TTL)).Build()
	}
	results := pkgvalkey.ExecMulti(ctx, s.client, cmds...)
	errs := make([]error, len(results))
	for i, result := range results {
		errs[i] = completedResult(result)
	}
	return errs
}

func completedResult(result valkeygo.ValkeyResult) error {
	err := result.Error()
	if valkeygo.IsValkeyNil(err) {
		return nil
	}
	return err
}

func (l *lruStore) CompleteBatch(ctx context.Context, batch CompletedBatch) []error {
	errs := CompleteBatch(ctx, l.inner, batch)
	for i, key := range batch.Keys {
		if errs[i] == nil {
			l.cache.add(claimKey(key), batch.TTL)
		}
	}
	return errs
}
