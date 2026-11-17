// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey_test

import (
	"context"
	"testing"

	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func TestStreamDeliveryRefreshHonorsOwnership(t *testing.T) {
	for _, state := range []string{"owned", "transferred", "acked", "deleted"} {
		t.Run(state, func(t *testing.T) {
			client := valkeytest.Real(t)
			d := seedStreamDelivery(t, client)
			changeStreamDelivery(t, client, d, state)
			owned, err := d.Refresh(t.Context(), client)
			require.NoError(t, err)
			require.Equal(t, state == "owned", owned)
			if state != "owned" {
				return
			}
			pending, err := client.Do(t.Context(), client.B().Xpending().Key(d.Stream).Group(d.Group).Start(d.ID).End(d.ID).Count(1).Build()).ToArray()
			require.NoError(t, err)
			require.Len(t, pending, 1)
			fields, err := pending[0].ToArray()
			require.NoError(t, err)
			idle, err := fields[2].AsInt64()
			require.NoError(t, err)
			require.Less(t, idle, int64(30_000))
			retries, err := fields[3].AsInt64()
			require.NoError(t, err)
			require.EqualValues(t, 1, retries)
		})
	}
}

func seedStreamDelivery(t *testing.T, client valkey.Client) pkgvalkey.StreamDelivery {
	t.Helper()
	d := pkgvalkey.StreamDelivery{Stream: "test:stream-delivery:" + nuid.Next(), Group: "group", Consumer: "owner"}
	t.Cleanup(func() { client.Do(context.Background(), client.B().Del().Key(d.Stream).Build()) })
	require.NoError(t, pkgvalkey.Exec1(t.Context(), client, pkgvalkey.XGroupCreateCmd(client, d.Stream, d.Group)).Error())
	id, err := client.Do(t.Context(), client.B().Xadd().Key(d.Stream).Id("*").FieldValue().FieldValue("payload", "body").Build()).ToString()
	require.NoError(t, err)
	d.ID = id
	cmd := pkgvalkey.XReadGroupCmd(client, pkgvalkey.GroupRead{Stream: d.Stream, Group: d.Group, Consumer: d.Consumer, Count: 1, BlockMS: 1})
	require.NoError(t, pkgvalkey.Exec1(t.Context(), client, cmd).Error())
	return d
}

func changeStreamDelivery(t *testing.T, client valkey.Client, d pkgvalkey.StreamDelivery, state string) {
	t.Helper()
	var cmd valkey.Completed
	switch state {
	case "acked":
		cmd = client.B().Xack().Key(d.Stream).Group(d.Group).Id(d.ID).Build()
	case "deleted":
		cmd = client.B().Xdel().Key(d.Stream).Id(d.ID).Build()
	case "transferred":
		cmd = client.B().Xclaim().Key(d.Stream).Group(d.Group).Consumer("other").MinIdleTime("0").Id(d.ID).Justid().Build()
	default:
		cmd = client.B().Xclaim().Key(d.Stream).Group(d.Group).Consumer(d.Consumer).MinIdleTime("0").Id(d.ID).Idle(120_000).Justid().Build()
	}
	require.NoError(t, client.Do(t.Context(), cmd).Error())
}
