// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const counterRPCPrefix = "test.loyalty"

type counterStoreRig struct {
	store    *ValkeyLoyaltyStore
	reporter *LoyaltyReporter
	pub      *rawPublisher
	id       uint64
}

func counterRigFor(t *testing.T, def loyaltyrpc.Counter) counterStoreRig {
	t.Helper()
	nc := testnats.Connect(t)
	_, err := nc.Subscribe(counterRPCPrefix+".counter.get", func(m *nats.Msg) {
		var req loyaltyrpc.Request
		_ = codec.Unmarshal(m.Data, &req)
		reply := loyaltyrpc.Reply{}
		if req.Name == def.Name {
			reply.Found, reply.Counter = true, &def
		}
		body, _ := codec.Marshal(reply)
		_ = m.Respond(body)
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	pub := &rawPublisher{}
	reporter := NewLoyaltyReporter(pub, zap.NewNop())
	store := NewValkeyLoyaltyStore(newHotPathTestClient(t), NewLoyaltyRPC(nc, counterRPCPrefix), reporter, nil)
	rig := counterStoreRig{store: store, reporter: reporter, pub: pub, id: freshChannel()}
	t.Cleanup(func() { store.CounterInvalidate(context.Background(), rig.id, def.Name) })
	return rig
}

type counterBumpStep struct {
	viewer  uint64
	command string
	delta   int64
}

type counterPeekStep struct {
	viewer  uint64
	command string
	want    int64
}

type publishedBump struct {
	Scope    string
	ViewerID uint64
	Command  string
	Delta    int64
}

func (rig counterStoreRig) published(t *testing.T) []publishedBump {
	t.Helper()
	rig.reporter.Close()
	var got []publishedBump
	for _, payload := range rig.pub.payloads[data.SubjectLoyaltyCounters] {
		var dto data.CounterBumpedDTO
		require.NoError(t, codec.Unmarshal(payload, &dto))
		for _, b := range dto.Bumps {
			got = append(got, publishedBump{b.Scope, b.ViewerID, b.Command, b.Delta})
		}
	}
	return got
}

type counterBumpCase struct {
	name   string
	def    loyaltyrpc.Counter
	bumps  []counterBumpStep
	peeks  []counterPeekStep
	wantPB []publishedBump
}

func pooledCounterCases() []counterBumpCase {
	return []counterBumpCase{
		{
			name:   "a channel counter pools every viewer's bumps",
			def:    loyaltyrpc.Counter{Name: "deaths", Scope: data.CounterScopeChannel},
			bumps:  []counterBumpStep{{7, "", 2}, {8, "", 3}},
			peeks:  []counterPeekStep{{7, "", 5}, {99, "", 5}},
			wantPB: []publishedBump{{data.CounterScopeChannel, 0, "", 5}},
		},
		{
			name:   "a viewer counter keeps one value per viewer and ignores the command",
			def:    loyaltyrpc.Counter{Name: "hugs", Scope: data.CounterScopeViewer},
			bumps:  []counterBumpStep{{7, "", 1}, {8, "", 3}, {7, "raid", 1}},
			peeks:  []counterPeekStep{{7, "", 2}, {8, "", 3}, {7, "raid", 2}},
			wantPB: []publishedBump{{data.CounterScopeViewer, 7, "", 2}, {data.CounterScopeViewer, 8, "", 3}},
		},
		{
			name:   "a viewer command counter keeps one value per viewer and command",
			def:    loyaltyrpc.Counter{Name: "uses", Scope: data.CounterScopeViewerCommand},
			bumps:  []counterBumpStep{{7, "hug", 4}, {7, "pet", 1}, {7, "hug", 1}},
			peeks:  []counterPeekStep{{7, "hug", 5}, {7, "pet", 1}},
			wantPB: []publishedBump{{data.CounterScopeViewerCommand, 7, "hug", 5}, {data.CounterScopeViewerCommand, 7, "pet", 1}},
		},
	}
}

func degradedCounterCases() []counterBumpCase {
	return []counterBumpCase{
		{
			name:   "a command counter pools viewers per command and a bump without a command degrades to the channel",
			def:    loyaltyrpc.Counter{Name: "raid", Scope: data.CounterScopeCommand},
			bumps:  []counterBumpStep{{7, "raid", 1}, {8, "raid", 2}, {7, "", 4}},
			peeks:  []counterPeekStep{{7, "raid", 3}, {8, "", 4}},
			wantPB: []publishedBump{{data.CounterScopeChannel, 0, "", 4}, {data.CounterScopeCommand, 0, "raid", 3}},
		},
		{
			name:   "a viewer counter bumped without a viewer degrades to the channel",
			def:    loyaltyrpc.Counter{Name: "waves", Scope: data.CounterScopeViewer},
			bumps:  []counterBumpStep{{0, "", 2}},
			peeks:  []counterPeekStep{{0, "", 2}},
			wantPB: []publishedBump{{data.CounterScopeChannel, 0, "", 2}},
		},
		{
			name:   "an unknown scope degrades to the channel",
			def:    loyaltyrpc.Counter{Name: "mystery", Scope: "galaxy"},
			bumps:  []counterBumpStep{{7, "", 1}},
			peeks:  []counterPeekStep{{7, "", 1}},
			wantPB: []publishedBump{{data.CounterScopeChannel, 0, "", 1}},
		},
		{
			name:  "a bot counter bumped for a channel is kept in Valkey but never reported",
			def:   loyaltyrpc.Counter{Name: "feeds", Scope: data.CounterScopeBot},
			bumps: []counterBumpStep{{7, "", 2}},
			peeks: []counterPeekStep{{7, "", 2}},
		},
		{
			name:   "the first bump seeds from the persisted value",
			def:    loyaltyrpc.Counter{Name: "seeded", Scope: data.CounterScopeChannel, Value: 40},
			bumps:  []counterBumpStep{{7, "", 2}},
			peeks:  []counterPeekStep{{7, "", 42}},
			wantPB: []publishedBump{{data.CounterScopeChannel, 0, "", 2}},
		},
	}
}

func TestValkeyCounterBumpLandsOnTheTargetItsScopeNames(t *testing.T) {
	for _, tc := range slices.Concat(pooledCounterCases(), degradedCounterCases()) {
		t.Run(tc.name, func(t *testing.T) {
			rig := counterRigFor(t, tc.def)
			ctx := context.Background()
			for _, step := range tc.bumps {
				_, err := rig.store.CounterBump(ctx, CounterBump{
					BroadcasterID: rig.id, Name: tc.def.Name, Viewer: Viewer{ID: step.viewer}, Command: step.command, Delta: step.delta,
				})
				require.NoError(t, err)
			}

			var got []counterPeekStep
			for _, step := range tc.peeks {
				counter, found, err := rig.store.CounterPeek(ctx, CounterTarget{
					BroadcasterID: rig.id, Name: tc.def.Name, ViewerID: step.viewer, Command: step.command,
				})
				require.NoError(t, err)
				require.True(t, found)
				got = append(got, counterPeekStep{step.viewer, step.command, counter.Value})
			}

			assert.Equal(t, tc.peeks, got)
			assert.ElementsMatch(t, tc.wantPB, rig.published(t))
		})
	}
}
