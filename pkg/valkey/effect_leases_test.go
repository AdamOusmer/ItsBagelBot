// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey_test

import (
	"slices"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
)

func TestEffectLeaseOwnershipAndExpiry(t *testing.T) {
	for _, mode := range []string{"renew", "complete", "release", "expire"} {
		t.Run(mode, func(t *testing.T) {
			client := valkeytest.Spawn(t)
			batch := valkey.EffectLeases{Keys: []string{"first", "second"}, Owner: "pod-a", TTL: 50 * time.Millisecond, Mode: "acquire"}
			states, err := valkey.UpdateEffectLeases(t.Context(), client, batch)
			require.NoError(t, err)
			require.Equal(t, []int64{0, 0}, states)
			peer := batch
			peer.Owner = "pod-b"
			states, err = valkey.UpdateEffectLeases(t.Context(), client, peer)
			require.NoError(t, err)
			require.Equal(t, []int64{2, 2}, states)
			batch.Mode, batch.TTL = mode, time.Second
			if mode != "expire" {
				_, err = valkey.UpdateEffectLeases(t.Context(), client, batch)
				require.NoError(t, err)
			}
			want := map[string]int64{"renew": 2, "complete": 1, "release": 0, "expire": 0}[mode]
			require.Eventually(t, func() bool {
				states, err = valkey.UpdateEffectLeases(t.Context(), client, peer)
				return err == nil && slices.Equal(states, []int64{want, want})
			}, time.Second, time.Millisecond)
		})
	}
}

func TestCompleteLeaseAfterEvictionOrOwnershipChange(t *testing.T) {
	for _, owner := range []string{"", "pod-b"} {
		t.Run("owner="+owner, func(t *testing.T) {
			client := valkeytest.Spawn(t)
			if owner != "" {
				require.NoError(t, client.Do(t.Context(), client.B().Set().Key("effect").Value(owner).Build()).Error())
			}
			states, err := valkey.UpdateEffectLeases(t.Context(), client, valkey.EffectLeases{
				Keys: []string{"effect"}, Owner: "pod-a", TTL: time.Minute, Mode: "complete",
			})
			require.NoError(t, err)
			value, err := client.Do(t.Context(), client.B().Get().Key("effect").Build()).ToString()
			require.NoError(t, err)
			want := struct {
				State int64
				Value string
			}{1, "1"}
			if owner != "" {
				want.State, want.Value = 2, owner
			}
			require.Equal(t, want, struct {
				State int64
				Value string
			}{states[0], value})
		})
	}
}
