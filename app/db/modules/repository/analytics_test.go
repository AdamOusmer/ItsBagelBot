// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"strconv"
	"testing"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAnalyticsSeedMatchesWriteTimeFields(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	row := func(user uint64, name string, enabled bool) *ent.ModulesCreate {
		return client.Modules.Create().SetUserID(user).SetName(name).SetIsEnabled(enabled).SetRevision(0).SetConfigs([]byte(`{}`))
	}
	client.Modules.CreateBulk(
		row(1, "quotes", true), row(1, "loyalty", true), row(1, "bogus", true),
		row(2, "accountage", true), row(2, "alerts", true), row(2, "automod", true),
		row(2, "clip", true), row(2, "codm", true), row(2, "counters", true),
		row(3, "quotes", false),
	).ExecX(ctx)
	client.SpotifyCredential.Create().SetUserID(1).SetTokenEnc([]byte("token")).ExecX(ctx)
	client.SpotifyCredential.Create().SetUserID(2).ExecX(ctx)
	client.GoveeCredential.Create().SetUserID(1).SetKeyEnc([]byte("key")).ExecX(ctx)
	client.GoveeCredential.Create().SetUserID(3).SetKeyEnc([]byte("key")).ExecX(ctx)

	got, err := repo.AnalyticsSeed(ctx)
	require.NoError(t, err)
	require.Equal(t, analytics.Tally{
		"modules.rows|quotes|on":         1,
		"modules.rows|loyalty|on":        1,
		"modules.rows|accountage|on":     1,
		"modules.rows|alerts|on":         1,
		"modules.rows|automod|on":        1,
		"modules.rows|clip|on":           1,
		"modules.rows|codm|on":           1,
		"modules.rows|counters|on":       1,
		"modules.rows|quotes|off":        1,
		"modules.breadth|2":              1,
		"modules.breadth|6+":             1,
		"integrations.connected|spotify": 1,
		"integrations.connected|govee":   2,
	}, got)
}

func TestAnalyticsSeedPagesPastOneBatch(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	const users = 1100
	builders := make([]*ent.ModulesCreate, 0, users)
	for id := uint64(1); id <= users; id++ {
		builders = append(builders, client.Modules.Create().SetUserID(id).SetName("quotes").SetIsEnabled(true).SetRevision(0).SetConfigs([]byte(`{}`)))
	}
	client.Modules.CreateBulk(builders...).ExecX(ctx)

	got, err := repo.AnalyticsSeed(ctx)
	require.NoError(t, err)
	require.Equal(t, analytics.Tally{
		"modules.rows|quotes|on": users,
		"modules.breadth|1":      users,
	}, got)
}

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
