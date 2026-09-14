// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"encoding/json"
	"time"

	"ItsBagelBot/pkg/kvstate"
)

// JetStreamBatchStore keeps lock and progress in one revision-fenced record.
// It is the sole batch authority, not a fallback that could replay a checkpoint
// from a different backend. Its bucket TTL must equal batchStateTTL.
type JetStreamBatchStore struct{ store kvstate.Store }
type batchRecord struct {
	Owner string
	Next  int
}

func NewJetStreamBatchStore(store kvstate.Store) *JetStreamBatchStore {
	return &JetStreamBatchStore{store: store}
}

func decodeBatchRecord(value kvstate.Value) (batchRecord, error) {
	var record batchRecord
	if len(value.Data) == 0 {
		return record, nil
	}
	err := json.Unmarshal(value.Data, &record)
	return record, err
}

func (s *JetStreamBatchStore) change(ctx context.Context, id string, edit func(*batchRecord) error) error {
	_, err := kvstate.Change(ctx, s.store, kvstate.Key(id), func(value kvstate.Value) ([]byte, error) {
		record, err := decodeBatchRecord(value)
		if err != nil {
			return nil, err
		}
		if err := edit(&record); err != nil {
			return nil, err
		}
		return json.Marshal(record)
	})
	return err
}

func (s *JetStreamBatchStore) Acquire(ctx context.Context, lease BatchLease, _ time.Duration) (bool, error) {
	err := s.change(ctx, lease.ID, func(record *batchRecord) error {
		if record.Owner != "" {
			return errBatchBusy
		}
		record.Owner = lease.Owner
		return nil
	})
	if err == errBatchBusy {
		return false, nil
	}
	return err == nil, err
}

func (s *JetStreamBatchStore) Next(ctx context.Context, id string) (int, error) {
	var next int
	// Confirm with a CAS: a follower's stale direct read must never replay an
	// already-checkpointed item after another replica hands over the lease.
	err := s.change(ctx, id, func(record *batchRecord) error { next = record.Next; return nil })
	return next, err
}

func (s *JetStreamBatchStore) SaveNext(ctx context.Context, lease BatchLease, next int, _ time.Duration) error {
	return s.change(ctx, lease.ID, func(record *batchRecord) error {
		if record.Owner == "" || record.Owner != lease.Owner {
			return errBatchBusy
		}
		record.Next = max(record.Next, next)
		return nil
	})
}

func (s *JetStreamBatchStore) Release(ctx context.Context, lease BatchLease) error {
	return s.change(ctx, lease.ID, func(record *batchRecord) error {
		if record.Owner != lease.Owner {
			return errBatchBusy
		}
		record.Owner = ""
		return nil
	})
}
