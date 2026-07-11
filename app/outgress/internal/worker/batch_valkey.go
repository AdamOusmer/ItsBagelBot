package worker

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

const batchKeyPrefix = "outgress:batch:"

// ValkeyBatchStore is the infrastructure adapter for BatchStore. Batch
// orchestration depends only on the interface in batch.go; this file owns the
// Valkey commands and key layout.
type ValkeyBatchStore struct {
	client valkey.Client
}

func NewValkeyBatchStore(client valkey.Client) *ValkeyBatchStore {
	return &ValkeyBatchStore{client: client}
}

func (s *ValkeyBatchStore) Acquire(ctx context.Context, batchID, owner string, ttl time.Duration) (bool, error) {
	res := s.client.Do(ctx, s.client.B().Set().Key(batchLockKey(batchID)).Value(owner).Nx().Px(ttl).Build())
	value, err := res.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}
	return value == "OK", nil
}

func (s *ValkeyBatchStore) Next(ctx context.Context, batchID string) (int, error) {
	value, err := s.client.Do(ctx, s.client.B().Get().Key(batchProgressKey(batchID)).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return 0, nil
		}
		return 0, err
	}
	return strconv.Atoi(value)
}

func (s *ValkeyBatchStore) SaveNext(ctx context.Context, batchID string, next int, ttl time.Duration) error {
	return s.client.Do(ctx, s.client.B().Set().Key(batchProgressKey(batchID)).
		Value(strconv.Itoa(next)).Px(ttl).Build()).Error()
}

func (s *ValkeyBatchStore) Release(ctx context.Context, batchID, owner string) error {
	const releaseIfOwner = `if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end`
	return s.client.Do(ctx, s.client.B().Eval().Script(releaseIfOwner).
		Numkeys(1).Key(batchLockKey(batchID)).Arg(owner).Build()).Error()
}

func batchLockKey(batchID string) string     { return batchKeyPrefix + batchID + ":lock" }
func batchProgressKey(batchID string) string { return batchKeyPrefix + batchID + ":next" }
