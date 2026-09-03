// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"strconv"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"go.uber.org/zap"
)

func setupWorker(guild *guildRecorder, kv discordLiveStore) *Worker {
	w := New(Config{Log: zap.NewNop(), Limiter: &scriptedLimiter{}})
	w.SetDiscord(guild, kv)
	return w
}

func setupGuild1(t *testing.T, w *Worker, broadcasterID string) GuildSetupResult {
	t.Helper()
	got, err := w.SetupGuild(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: broadcasterID})
	if err != nil {
		t.Fatalf("SetupGuild: %v", err)
	}
	return got
}

// foreignChannels fakes a lived-in server: enough channels the template
// does not know about.
func foreignChannels() []discapi.Snowflake {
	out := make([]discapi.Snowflake, 0, ddiscord.LivingCommunityMinChannels)
	for i := 0; i < ddiscord.LivingCommunityMinChannels; i++ {
		out = append(out, discapi.Snowflake{ID: "ch-" + strconv.Itoa(i), Name: "existing-" + strconv.Itoa(i)})
	}
	return out
}

func assertBound(t *testing.T, kv *memLiveStore, broadcasterID string) {
	t.Helper()
	if kv.guild != "guild-1" {
		t.Fatalf("reverse guild = %s, want guild-1", kv.guild)
	}
	if kv.twitch != broadcasterID {
		t.Fatalf("reverse twitch = %s, want %s", kv.twitch, broadcasterID)
	}
}

func assertFilled(t *testing.T, got GuildSetupResult) {
	t.Helper()
	if got.Refused != "" {
		t.Fatalf("refused = %q", got.Refused)
	}
	if msg := filledGaps(got); msg != "" {
		t.Fatal(msg)
	}
}

func filledGaps(got GuildSetupResult) string {
	if got.GuildID != "guild-1" {
		return "guild = " + got.GuildID
	}
	for _, slot := range []struct {
		id   string
		name string
	}{
		{got.LiveChannelID, "live channel"},
		{got.ClipsChannelID, "clips channel"},
		{got.VoiceHubID, "voice hub"},
		{got.LogChannelID, "logs channel"},
		{got.TicketChannelID, "ticket channel"},
		{got.TicketCategoryID, "ticket category"},
	} {
		if slot.id == "" {
			return "missing " + slot.name
		}
	}
	return ""
}

func TestSetupGuildCreatesMissingRolesAndBindsChannels(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{}

	got := setupGuild1(t, setupWorker(guild, kv), "42")

	assertFilled(t, got)
	assertBound(t, kv, "42")
	wantRoles := map[string]bool{"Live": true, "Mods": true, "Regulars": true, "Member": true}
	for _, name := range guild.createdRo {
		delete(wantRoles, name)
	}
	if len(wantRoles) != 0 {
		t.Fatalf("missing roles %v", wantRoles)
	}
	if len(guild.panels) == 0 {
		t.Fatal("setup must post the ticket desk button")
	}
	if guild.panels[0] != discapi.CustomTicketOpen {
		t.Fatalf("desk button = %v", guild.panels)
	}
}

func TestSetupGuildRefusesALivedInServerButStillBinds(t *testing.T) {
	guild := &guildRecorder{channels: foreignChannels()}
	kv := &memLiveStore{}

	got := setupGuild1(t, setupWorker(guild, kv), "42")

	if got.Refused == "" {
		t.Fatal("lived-in guild must refuse the fill")
	}
	if len(guild.createdCh) != 0 {
		t.Fatal("lived-in guild must not create channels")
	}
	if got.GuildID != "guild-1" {
		t.Fatalf("refused setup returns the guild id: %+v", got)
	}
	if got.ClipsChannelID != "" {
		t.Fatalf("refused setup adopts nothing here: %+v", got)
	}
	assertBound(t, kv, "42")
}

func TestSetupGuildRefusesAGuildBoundToAnotherBroadcasterBeforeAnyWrite(t *testing.T) {
	guild := &guildRecorder{}
	kv := &memLiveStore{guild: "guild-1", twitch: "7"}

	_, err := setupWorker(guild, kv).SetupGuild(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})

	if err != ErrGuildBoundElsewhere {
		t.Fatalf("err = %v, want ErrGuildBoundElsewhere", err)
	}
	if len(guild.createdCh)+len(guild.createdRo) != 0 {
		t.Fatal("a refused caller must not touch the server")
	}
	assertBound(t, kv, "7")
}

func TestSetupGuildCompletesAPartialFill(t *testing.T) {
	guild := &guildRecorder{}
	// A fill cut short by a timeout left eight template channels behind.
	for _, name := range []string{"Welcome", "welcome", "rules", "Announcements", "now-live", "clips", "announcements", "Community"} {
		guild.channels = append(guild.channels, discapi.Snowflake{ID: "old-" + name, Name: name})
	}
	kv := &memLiveStore{guild: "guild-1", twitch: "42"}

	got := setupGuild1(t, setupWorker(guild, kv), "42")

	assertFilled(t, got)
	if got.LiveChannelID != "old-now-live" {
		t.Fatalf("existing live channel must be reused: %+v", got)
	}
	if got.ClipsChannelID != "old-clips" {
		t.Fatalf("existing clips channel must be reused: %+v", got)
	}
	for _, name := range guild.createdCh {
		if name == "now-live" {
			t.Fatalf("%s was created twice", name)
		}
		if name == "clips" {
			t.Fatalf("%s was created twice", name)
		}
	}
}

func TestSetupGuildAdoptsMatchingChannelsOnALivedInServer(t *testing.T) {
	guild := &guildRecorder{channels: append(foreignChannels(), discapi.Snowflake{ID: "their-clips", Name: "Clips"})}

	got := setupGuild1(t, setupWorker(guild, &memLiveStore{}), "42")

	if got.Refused == "" {
		t.Fatal("lived-in guild must refuse the fill")
	}
	if len(guild.createdCh) != 0 {
		t.Fatal("lived-in guild must not create channels")
	}
	if got.ClipsChannelID != "their-clips" {
		t.Fatalf("clips channel must be adopted by name, got %q", got.ClipsChannelID)
	}
}

func TestUnbindGuildOnlyForTheBoundBroadcaster(t *testing.T) {
	kv := &memLiveStore{guild: "guild-1", twitch: "42"}
	w := setupWorker(&guildRecorder{}, kv)

	if err := w.UnbindGuild(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "7"}); err != ErrGuildBoundElsewhere {
		t.Fatalf("err = %v, want ErrGuildBoundElsewhere", err)
	}
	if err := w.UnbindGuild(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"}); err != nil {
		t.Fatalf("UnbindGuild: %v", err)
	}
	if _, ok := kv.GetGuild(context.Background(), GuildSetupRequest{GuildID: "guild-1"}); ok {
		t.Fatal("binding survived unbind")
	}
}

func TestGuildLayoutRequiresTheBinding(t *testing.T) {
	guild := &guildRecorder{channels: []discapi.Snowflake{{ID: "c1", Name: "general"}}}
	w := setupWorker(guild, &memLiveStore{guild: "guild-1", twitch: "42"})

	if _, err := w.GuildLayout(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "7"}); err != ErrGuildBoundElsewhere {
		t.Fatalf("err = %v, want ErrGuildBoundElsewhere", err)
	}
	layout, err := w.GuildLayout(context.Background(), GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})
	if err != nil {
		t.Fatalf("GuildLayout: %v", err)
	}
	if len(layout.Channels) != 1 {
		t.Fatalf("channels = %+v", layout.Channels)
	}
	if layout.Channels[0].ID != "c1" {
		t.Fatalf("channels = %+v", layout.Channels)
	}
}
