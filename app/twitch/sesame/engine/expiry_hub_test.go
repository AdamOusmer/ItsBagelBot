// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestExpiryHubDeliversAfterDrainingToZeroWatchers(t *testing.T) {
	client := valkeytest.Real(t)
	hub := NewExpiryHub(client, 0, zap.NewNop())
	for round := 0; round < 2; round++ {
		ctx, cancel := context.WithCancel(context.Background())
		keys := make(chan string, 1)
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			hub.Watch(ctx, func(_ context.Context, key string) { keys <- key }, "hubtest:")
		}()
		key := "hubtest:" + time.Now().Format("150405.000000000")
		require.Eventually(t, func() bool {
			client.Do(ctx, client.B().Set().Key(key).Value("1").PxMilliseconds(50).Build())
			select {
			case got := <-keys:
				return got == key
			case <-time.After(300 * time.Millisecond):
				return false
			}
		}, 10*time.Second, 10*time.Millisecond)
		cancel()
		<-finished
	}
}
