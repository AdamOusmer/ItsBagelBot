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
	if got.ClipsChannelID != "" {
		t.Fatal("no template-named channel exists, nothing to adopt")
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

func TestSetupGuildRefusesAGuildBoundToAnotherBroadcasterBeforeAnyWrite(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{guild: "guild-1", twitch: "7"}
	w := New(Config{Log: zap.NewNop(), Limiter: &scriptedLimiter{}})
	w.SetDiscord(guild, kv)

	_, err := w.SetupGuild(context.Background(), "guild-1", "", "42")
	if err != ErrGuildBoundElsewhere {
		t.Fatalf("err = %v, want ErrGuildBoundElsewhere", err)
	}
	if len(guild.createdCh) != 0 || len(guild.createdRo) != 0 {
		t.Fatal("a refused caller must not touch the server")
	}
	if kv.twitch != "7" {
		t.Fatal("the existing binding was overwritten")
	}
}

func TestSetupGuildCompletesAPartialFill(t *testing.T) {
	guild := &guildRecorder{}
	// A fill cut short by a timeout left eight template channels behind.
	for _, name := range []string{"Welcome", "welcome", "rules", "Announcements", "now-live", "clips", "announcements", "Community"} {
		guild.channels = append(guild.channels, discapi.Snowflake{ID: "old-" + name, Name: name})
	}
	kv := &memLiveStore{guild: "guild-1", twitch: "42"}
	w := New(Config{Log: zap.NewNop(), Limiter: &scriptedLimiter{}})
	w.SetDiscord(guild, kv)

	got, err := w.SetupGuild(context.Background(), "guild-1", "", "42")
	if err != nil {
		t.Fatalf("SetupGuild: %v", err)
	}
	if got.Refused != "" {
		t.Fatalf("template-named channels must not read as lived-in: %q", got.Refused)
	}
	if got.LiveChannelID != "old-now-live" || got.ClipsChannelID != "old-clips" {
		t.Fatalf("existing template channels must be reused: %+v", got)
	}
	if got.VoiceHubID == "" {
		t.Fatal("the missing half of the template was not created")
	}
	for _, name := range guild.createdCh {
		if name == "now-live" || name == "clips" {
			t.Fatalf("%s was created twice", name)
		}
	}
}

func TestSetupGuildAdoptsMatchingChannelsOnALivedInServer(t *testing.T) {
	guild := &guildRecorder{}
	for i := 0; i < ddiscord.LivingCommunityMinChannels; i++ {
		guild.channels = append(guild.channels, discapi.Snowflake{ID: "ch-" + strconv.Itoa(i), Name: "existing-" + strconv.Itoa(i)})
	}
	guild.channels = append(guild.channels, discapi.Snowflake{ID: "their-clips", Name: "Clips"})
	w := New(Config{Log: zap.NewNop(), Limiter: &scriptedLimiter{}})
	w.SetDiscord(guild, &memLiveStore{})

	got, err := w.SetupGuild(context.Background(), "guild-1", "", "42")
	if err != nil {
		t.Fatalf("SetupGuild: %v", err)
	}
	if got.Refused == "" || len(guild.createdCh) != 0 {
		t.Fatal("lived-in guild must refuse the fill")
	}
	if got.ClipsChannelID != "their-clips" {
		t.Fatalf("clips channel must be adopted by name, got %q", got.ClipsChannelID)
	}
}

func TestUnbindGuildOnlyForTheBoundBroadcaster(t *testing.T) {
	kv := &memLiveStore{guild: "guild-1", twitch: "42"}
	w := New(Config{Log: zap.NewNop(), Limiter: &scriptedLimiter{}})
	w.SetDiscord(&guildRecorder{}, kv)

	if err := w.UnbindGuild(context.Background(), "guild-1", "7"); err != ErrGuildBoundElsewhere {
		t.Fatalf("err = %v, want ErrGuildBoundElsewhere", err)
	}
	if err := w.UnbindGuild(context.Background(), "guild-1", "42"); err != nil {
		t.Fatalf("UnbindGuild: %v", err)
	}
	if _, ok := kv.GetGuild(context.Background(), "guild-1"); ok {
		t.Fatal("binding survived unbind")
	}
}

func TestGuildLayoutRequiresTheBinding(t *testing.T) {
	guild := &guildRecorder{channels: []discapi.Snowflake{{ID: "c1", Name: "general"}}}
	w := New(Config{Log: zap.NewNop(), Limiter: &scriptedLimiter{}})
	w.SetDiscord(guild, &memLiveStore{guild: "guild-1", twitch: "42"})

	if _, err := w.GuildLayout(context.Background(), "guild-1", "7"); err != ErrGuildBoundElsewhere {
		t.Fatalf("err = %v, want ErrGuildBoundElsewhere", err)
	}
	layout, err := w.GuildLayout(context.Background(), "guild-1", "42")
	if err != nil {
		t.Fatalf("GuildLayout: %v", err)
	}
	if len(layout.Channels) != 1 || layout.Channels[0].ID != "c1" {
		t.Fatalf("channels = %+v", layout.Channels)
	}
}
