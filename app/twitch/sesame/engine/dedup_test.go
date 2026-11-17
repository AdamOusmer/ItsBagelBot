// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/idempotency"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func usedCounts(t *testing.T, pub *rawPublisher) []int64 {
	t.Helper()
	var counts []int64
	for _, payload := range pub.payloads[data.SubjectCommandUsed] {
		var dto data.CommandUsedDTO
		require.NoError(t, codec.Unmarshal(payload, &dto))
		counts = append(counts, dto.Count)
	}
	return counts
}

func TestEventDedupGuardsOnlyTheEffectsThatCount(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		reader     fakeReader
		wantKey    string
		wantCounts []int64
	}{
		{
			name:       "a replayed command counts its use once",
			text:       "!foo",
			reader:     fakeReader{cmd: projection.Command{Name: "foo", Response: "hi", IsActive: true}, cmdFound: true},
			wantKey:    "m1:" + effectUse,
			wantCounts: []int64{1},
		},
		{name: "a plain chat message never consults the store", text: "just chatting, not a command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newRecordingStore()
			pub := &rawPublisher{}
			d := Deps{
				Proj: tc.reader, Live: liveAlways{}, Cooldown: NoopCooldown{},
				Pub: pub, Log: zap.NewNop(),
				Dedup: NewEventDedup(store, "sesame:seen:", time.Minute, zap.NewNop()),
			}
			p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{
				OutgressPremium: premiumSubj, OutgressStandard: standardSubj, CountUses: true,
			})

			require.NoError(t, p.Process(commandMsg(t, "m1", tc.text)))
			require.NoError(t, p.Process(commandMsg(t, "m1", tc.text)))
			p.Close()

			assert.Equal(t, tc.wantCounts, usedCounts(t, pub))
			if tc.wantKey == "" {
				assert.Empty(t, store.keys())
				return
			}
			assert.Contains(t, store.keys(), tc.wantKey)
		})
	}
}

type recordingStore struct {
	mu     sync.Mutex
	claims map[string]bool
	seen   []string
	owners map[string]string
}

func newRecordingStore() *recordingStore {
	return &recordingStore{claims: map[string]bool{}, owners: map[string]string{}}
}

func (r *recordingStore) UpdateLeases(_ context.Context, batch idempotency.LeaseBatch) ([]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	states := make([]int64, len(batch.Keys))
	for i, key := range batch.Keys {
		states[i] = r.updateLease(string(key), batch)
	}
	return states, nil
}

func (r *recordingStore) updateLease(key string, batch idempotency.LeaseBatch) int64 {
	if r.claims[key] {
		return 1
	}
	owner := r.owners[key]
	if owner != "" && owner != batch.Owner {
		return 2
	}
	switch batch.Mode {
	case "acquire":
		r.owners[key] = batch.Owner
	case "complete":
		r.claims[key] = true
		r.seen = append(r.seen, key)
		delete(r.owners, key)
		return 1
	case "release":
		delete(r.owners, key)
	}
	return 0
}

func (r *recordingStore) Seen(_ context.Context, key string, _ time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, key)
	if r.claims[key] {
		return true, nil
	}
	r.claims[key] = true
	return false, nil
}

func (r *recordingStore) Contains(_ context.Context, key string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.claims[key], nil
}

func (r *recordingStore) Release(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.claims, key)
	return nil
}

func (r *recordingStore) keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}
