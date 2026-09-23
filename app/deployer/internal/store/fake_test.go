// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package store

import (
	"context"
	"encoding/json"
	"time"

	"ItsBagelBot/app/deployer/internal/ports"
)

// fakeKV mirrors the JetStream KV rules the store relies on: one revision
// sequence shared by every key (it is the stream sequence), create refuses a
// live key, update and a revisioned remove refuse a stale revision.
type fakeKV struct {
	seq  ports.Revision
	data map[kvKey]record
}

func (f *fakeKV) get(_ context.Context, key kvKey) (record, error) {
	rec, ok := f.data[key]
	if !ok {
		return record{}, ports.ErrNotFound
	}
	return rec, nil
}

func (f *fakeKV) create(_ context.Context, key kvKey, value []byte) (ports.Revision, error) {
	if _, ok := f.data[key]; ok {
		return 0, ports.ErrConflict
	}
	return f.write(key, value), nil
}

func (f *fakeKV) update(_ context.Context, key kvKey, value []byte, rev ports.Revision) (ports.Revision, error) {
	if f.data[key].rev != rev {
		return 0, ports.ErrConflict
	}
	return f.write(key, value), nil
}

func (f *fakeKV) remove(_ context.Context, key kvKey, rev ports.Revision) error {
	if rev != 0 && f.data[key].rev != rev {
		return ports.ErrConflict
	}
	f.seq++
	delete(f.data, key)
	return nil
}

func (f *fakeKV) write(key kvKey, value []byte) ports.Revision {
	f.seq++
	f.data[key] = record{value: value, rev: f.seq}
	return f.seq
}

// seed writes v as JSON, bypassing the store, to stage states a crashed Put
// would leave behind.
func (f *fakeKV) seed(key kvKey, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	f.write(key, b)
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

const testLockTTL = 2 * time.Minute

func testStore() (*Store, *fakeKV, *fakeClock) {
	kv := &fakeKV{data: map[kvKey]record{}}
	clk := &fakeClock{now: time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)}
	return newStore(kv, testLockTTL, clk), kv, clk
}
