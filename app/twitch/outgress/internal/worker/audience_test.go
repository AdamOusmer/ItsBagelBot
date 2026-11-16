// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type audiencePoll struct {
	viewers     int
	broadcaster string
	band        string
	offline     bool
	status      int
}

func TestProcessStreamAudienceSizes(t *testing.T) {
	for _, tc := range []audiencePoll{
		{viewers: 0, band: "v0_9"}, {viewers: 9, band: "v0_9"},
		{viewers: 10, band: "v10_49"}, {viewers: 49, band: "v10_49"},
		{viewers: 50, band: "v50_199"}, {viewers: 199, band: "v50_199"},
		{viewers: 200, band: "v200_999"}, {viewers: 999, band: "v200_999"},
		{viewers: 1000, band: "v1kp"},
		{viewers: 50, broadcaster: "invalid"},
		{viewers: 50, broadcaster: "18446744073709551616"},
		{viewers: 50, offline: true},
		{viewers: 50, status: 503},
	} {
		t.Run(fmt.Sprintf("%+v", tc), func(t *testing.T) {
			checkAudiencePoll(t, tc)
		})
	}
}

func checkAudiencePoll(t *testing.T, tc audiencePoll) {
	t.Helper()
	vc := valkeytest.Spawn(t)
	now := time.Now().UTC()
	rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker(), analytics.WithClock(func() time.Time { return now }))
	t.Cleanup(rec.Close)
	w := pipelineWorker(t, &scriptedTransport{responses: audienceResponses(tc)})
	w.SetLiveWriter(NewLiveWriter(vc, nil, "", time.Hour, zap.NewNop()))
	w.SetAnalytics(rec)
	store := projection.NewStore(vc)
	w.SetStreamInfoStore(store)
	require.NoError(t, store.SetStreamInfo(t.Context(), testBroadcaster, projection.StreamInfo{PeakViewers: 10000}))
	msg := testMessage{Type: outgress.TypeStreamStatus, Broadcaster: tc.broadcaster}
	for range 2 {
		require.Equal(t, tc.status == 503, msg.send(w) != nil)
	}
	rec.Flush(t.Context())
	checkAudienceCounts(t, audienceCountCheck{client: vc, at: now, band: tc.band})
}

func audienceResponses(tc audiencePoll) []scriptedResponse {
	body := fmt.Sprintf(`{"data":[{"type":"live","viewer_count":%d}]}`, tc.viewers)
	if tc.offline {
		body = `{"data":[]}`
	}
	status := tc.status
	if status == 0 {
		status = 200
	}
	response := scriptedResponse{status: status, body: body}
	return []scriptedResponse{response, response}
}

type audienceCountCheck struct {
	client valkey.Client
	at     time.Time
	band   string
}

func checkAudienceCounts(t *testing.T, check audienceCountCheck) {
	t.Helper()
	for _, window := range []catalog.Window{catalog.Day, catalog.Week, catalog.Month} {
		checkAudienceWindow(t, check, window)
	}
}

func checkAudienceWindow(t *testing.T, check audienceCountCheck, window catalog.Window) {
	t.Helper()
	want, got := map[string]int64{}, map[string]int64{}
	for _, band := range catalog.Enum(catalog.KindSize) {
		key := catalog.DistinctKey(window, check.at, catalog.Field(catalog.ChannelsSize, band))
		n, err := pkgvalkey.SetCard(t.Context(), check.client, key)
		require.NoError(t, err)
		got[band], want[band] = n, 0
	}
	if window != catalog.Day && check.band != "" {
		want[check.band] = 1
	}
	require.Equal(t, want, got, "window %v", window)
}
