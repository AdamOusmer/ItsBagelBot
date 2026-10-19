// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/discordapi"
	catalog "ItsBagelBot/internal/domain/analytics"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHandleCountsInteractionsEventsAndRemovals(t *testing.T) {
	vc := valkeytest.Spawn(t)
	h := newHarness(t, ddiscord.Config{GuildID: testGuild, WelcomeEnabled: "off"})
	h.d.Analytics = analytics.New(vc, zap.NewNop(), h.d.Registry.AnalyticsNames(), analytics.WithoutTicker())

	h.interact(interaction{data: map[string]any{"name": "daily"}, member: map[string]any{"user": map[string]any{"id": "u1"}}})
	h.interact(interaction{channelID: testSupportCh, data: map[string]any{"custom_id": discordapi.CustomDailyClaim}, member: map[string]any{"user": map[string]any{"id": "u1"}}})
	h.send("GUILD_MEMBER_ADD", memberPayload(testGuild))
	h.send("GUILD_DELETE", map[string]any{"id": testGuild, "unavailable": true})
	h.send("GUILD_DELETE", map[string]any{"id": testGuild})
	h.d.Analytics.Close()

	counts, err := vc.Do(context.Background(), vc.B().Hgetall().Key(catalog.CounterKey(time.Now())).Build()).AsIntMap()
	require.NoError(t, err)
	require.Equal(t, map[string]int64{
		"discord.interactions|daily":              1,
		"discord.interactions|bagel:crumbs:daily": 1,
		"discord.events|guild_member_add":         1,
		"lifecycle|guild_removed":                 1,
	}, counts)
}
