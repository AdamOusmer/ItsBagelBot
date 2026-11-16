// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
)

func TestKeyCopyReplacesFieldsWithAnAbsoluteExpiry(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	copy := pkgvalkey.KeyCopy{Source: "source", Destination: "snapshot", ExpireAt: time.Now().Add(time.Hour).Truncate(time.Second)}
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(copy.Source).FieldValue().FieldValue("total", "100").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(copy.Destination).FieldValue().FieldValue("stale", "1").Build()).Error())

	require.NoError(t, copy.Replace(ctx, client))
	fields, err := pkgvalkey.HashAllInts(ctx, client, copy.Destination)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"total": 100}, fields)

	expiry, err := client.Do(ctx, client.B().Expiretime().Key(copy.Destination).Build()).AsInt64()
	require.NoError(t, err)
	require.Equal(t, copy.ExpireAt.Unix(), expiry)
	_, err = client.Do(ctx, client.B().Hset().Key(copy.Source).FieldValue().FieldValue("total", "200").Build()).ToInt64()
	require.NoError(t, err)
	fields, err = pkgvalkey.HashAllInts(ctx, client, copy.Destination)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"total": 100}, fields)
}

func TestKeyCopyHandlesMissingSourceAndReportsCopyErrors(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	copy := pkgvalkey.KeyCopy{Source: "missing", Destination: "snapshot", ExpireAt: time.Now().Add(time.Hour)}
	require.NoError(t, copy.Replace(ctx, client))
	count, err := pkgvalkey.CountExisting(ctx, client, []string{copy.Destination})
	require.NoError(t, err)
	require.Zero(t, count)

	copy.Source = "source"
	copy.Destination = copy.Source
	require.NoError(t, client.Do(ctx, client.B().Set().Key(copy.Source).Value("value").Build()).Error())
	require.Error(t, copy.Replace(ctx, client))
}
