// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Dropped rejections also fail the follow-up chat reply, so they show two attempts.
func TestEveryFailedHelixAttemptCountsByClass(t *testing.T) {
	vc := valkeytest.Spawn(t)
	rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker())
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusNoContent} {
		reply := scriptedResponse{status: status, body: `{"message":"no"}`}
		observe := func(cfg *Config) { cfg.Twitch.SetAttemptObserver(CountHelixFailures(rec)) }
		w := pipelineWorker(t, &scriptedTransport{responses: []scriptedResponse{reply, reply}}, observe)
		_ = testMessage{Type: "channel_update", Payload: `{"field":"title","locale":"en","user":"alice"}`}.send(w)
	}
	rec.Close()

	counts, err := vc.Do(context.Background(), vc.B().Hgetall().Key(catalog.CounterKey(time.Now())).Build()).AsIntMap()
	require.NoError(t, err)
	require.Equal(t, map[string]int64{
		"outgress.failures|client":       2,
		"outgress.failures|unauthorized": 2,
		"outgress.failures|banned":       2,
		"outgress.failures|ratelimited":  1,
		"outgress.failures|server":       1,
	}, counts)
}
