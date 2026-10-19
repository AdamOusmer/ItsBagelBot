// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"strconv"
	"testing"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestWritesShiftAnalyticsLevelsByNetDelta(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	vk := valkeytest.Spawn(t)
	rec := analytics.New(vk, zap.NewNop(), nil, analytics.WithoutTicker())
	govee := repository.NewGoveeCreds(client, dbtest.NewPacker(t))
	repo.SetAnalytics(rec)
	govee.SetAnalytics(rec)

	_, err := repo.SetNow(ctx, 1, "quotes", true, nil)
	require.NoError(t, err)
	_, err = repo.Patch(ctx, 1, "loyalty", true, nil, nil)
	require.NoError(t, err)
	_, err = repo.SetNow(ctx, 2, "quotes", true, nil)
	require.NoError(t, err)
	_, err = repo.SetNow(ctx, 1, "quotes", false, nil)
	require.NoError(t, err)
	require.NoError(t, repo.DeleteAllForUser(ctx, 2))
	require.NoError(t, govee.SetKey(ctx, 1, "key"))
	require.NoError(t, govee.SetKey(ctx, 3, "key"))
	require.NoError(t, govee.ClearKey(ctx, 3))

	rec.Close()

	got, err := vk.Do(ctx, vk.B().Hgetall().Key("an:lvl").Build()).AsStrMap()
	require.NoError(t, err)
	nonZero := map[string]string{}
	for field, value := range got {
		if n, _ := strconv.ParseInt(value, 10, 64); n != 0 {
			nonZero[field] = value
		}
	}
	require.Equal(t, map[string]string{
		"modules.rows|quotes|off":      "1",
		"modules.rows|loyalty|on":      "1",
		"modules.breadth|1":            "1",
		"integrations.connected|govee": "1",
	}, nonZero)
}
