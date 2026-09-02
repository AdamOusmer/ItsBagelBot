// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"strconv"
	"testing"

	discapi "ItsBagelBot/app/outgress/internal/discord"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"go.uber.org/zap"
)

func TestSetupGuildCreatesMissingRolesAndBindsChannels(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{}
	w := New(Config{Log: zap.NewNop(), Limiter: &scriptedLimiter{}})
	w.SetDiscord(guild, kv)

	got, err := w.SetupGuild(context.Background(), "guild-1", "", "42")
	if err != nil {
		t.Fatalf("SetupGuild: %v", err)
	}
	if got.Refused != "" {
		t.Fatalf("refused = %q", got.Refused)
	}
	if got.GuildID != "guild-1" || got.LiveChannelID == "" || got.ClipsChannelID == "" || got.VoiceHubID == "" {
		t.Fatalf("incomplete fill: %+v", got)
	}
	if kv.guild != "guild-1" || kv.twitch != "42" {
		t.Fatalf("reverse index = %s -> %s", kv.guild, kv.twitch)
	}
	wantRoles := map[string]bool{"Live": true, "Mods": true, "Regulars": true, "Member": true}
	for _, name := range guild.createdRo {
		delete(wantRoles, name)
	}
	if len(wantRoles) != 0 {
		t.Fatalf("missing roles %v", wantRoles)
	}
}

func TestSetupGuildRefusesALivedInServerButStillBinds(t *testing.T) {
	guild := &guildRecorder{}
	for i := 0; i < ddiscord.LivingCommunityMinChannels; i++ {
		guild.channels = append(guild.channels, discapi.Snowflake{
			ID:   "ch-" + strconv.Itoa(i),
			Name: "existing-" + strconv.Itoa(i),
		})
	}
	kv := &memLiveStore{}
	w := New(Config{Log: zap.NewNop(), Limiter: &scriptedLimiter{}})
	w.SetDiscord(guild, kv)

	got, err := w.SetupGuild(context.Background(), "guild-1", "", "42")
	if err != nil {
		t.Fatalf("SetupGuild: %v", err)
	}
	if got.Refused == "" {
		t.Fatal("lived-in guild must refuse the fill")
	}
	if got.GuildID != "guild-1" {
		t.Fatal("refused setup still returns the guild id so Connect can persist it")
	}
	if len(guild.createdCh) != 0 {
		t.Fatal("fill must not run on a lived-in guild")
	}
	if kv.guild != "guild-1" || kv.twitch != "42" {
		t.Fatal("connect still writes the reverse index")
	}
}
