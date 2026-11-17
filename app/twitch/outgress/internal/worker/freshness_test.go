// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"testing"
	"time"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/kvstate/kvtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessRejectsStaleActionsAndBatchItems(t *testing.T) {
	old := time.Now().Add(-61 * time.Second).UnixMilli()
	fresh := time.Now().UnixMilli()
	chat := outgress.Message{Type: outgress.TypeChat, BroadcasterID: testBroadcaster, Payload: []byte(`{"message":"hello"}`)}
	for _, tc := range []struct {
		name  string
		outer int64
		items []int64
		want  int
		lane  Lane
	}{
		{"stale action", old, nil, 0, LanePremium},
		{"fresh action", fresh, nil, 1, LaneStandard},
		{"stale batch", old, []int64{fresh}, 0, LanePremium},
		{"mixed batch", fresh, []int64{old, fresh}, 1, LaneStandard},
		{"batch item inherits original time", fresh, []int64{0}, 1, LanePremium},
		{"system action", old, nil, 1, LaneSystem},
		{"system batch", old, []int64{old, 0}, 2, LaneSystem},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := &scriptedTransport{}
			w := pipelineWorker(t, rt, withBatchStore(NewJetStreamBatchStore(kvtest.New())), withLane(tc.lane))
			payload := chat
			payload.EventAt = tc.outer
			if tc.items != nil {
				batch := outgress.Batch{ID: "freshness"}
				for _, timestamp := range tc.items {
					item := chat
					item.EventAt = timestamp
					batch.Items = append(batch.Items, item)
				}
				payload.Type = outgress.TypeBatch
				body, err := codec.Marshal(batch)
				require.NoError(t, err)
				payload.Payload = body
			}
			body, err := codec.Marshal(payload)
			require.NoError(t, err)
			require.NoError(t, w.Process(bus.NewMessage("event", body)))
			assert.Len(t, rt.recorded(), tc.want)
		})
	}
}
