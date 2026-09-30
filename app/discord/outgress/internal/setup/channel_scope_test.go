// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"
	"errors"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func TestConfigRejectsForeignChannelsBeforeSave(t *testing.T) {
	for _, configure := range []func(*ddiscord.Config){
		func(c *ddiscord.Config) { c.LiveChannelID = "foreign" },
		func(c *ddiscord.Config) { c.ClipsChannelID = "foreign" },
		func(c *ddiscord.Config) { c.WelcomeChannelID = "foreign" },
		func(c *ddiscord.Config) { c.LogChannelID = "foreign" },
		func(c *ddiscord.Config) { c.TicketChannelID = "foreign" },
		func(c *ddiscord.Config) { c.TicketLogChannelID = "foreign" },
		func(c *ddiscord.Config) { c.TicketArchiveCategoryID = "foreign" },
	} {
		w, store, rest := boundWorker(t, "guild-1")
		rest.channelGuilds = map[string]string{"foreign": "guild-2"}
		cfg := ddiscord.Config{}
		configure(&cfg)
		_, err := w.SetGuildConfig(context.Background(), GuildConfigWrite{GuildID: "guild-1", BroadcasterID: "42", Config: cfg})
		if !errors.Is(err, discapi.ErrForbidden) {
			t.Fatalf("expected refusal: %v", err)
		}
		_, _, found := store.GuildConfig(context.Background(), discordstore.Guild{ID: "guild-1"})
		if found {
			t.Fatal("foreign configuration persisted")
		}
	}
}

func TestRepostRefusesForeignTargetAndRememberedPanelBeforeDelete(t *testing.T) {
	for _, remembered := range []bool{false, true} {
		rest := newGuildRecorder()
		rest.channelGuilds = map[string]string{"safe": "guild-1", "foreign": "guild-2"}
		store := boundStore(t)
		w := setupWorker(rest, store)
		target := "foreign"
		previous := "safe"
		if remembered {
			target = "safe"
			previous = "foreign"
		}
		rememberDesk(t, store, previous, "old")
		_, err := w.RepostDesk(context.Background(), DeskRepostRequest{GuildID: "guild-1", BroadcasterID: "b1", ChannelID: target})
		if !errors.Is(err, discapi.ErrForbidden) {
			t.Fatalf("expected refusal: %v", err)
		}
		if len(rest.deleted) != 0 || len(rest.panelPosts) != 0 {
			t.Fatal("performed side effects before membership check")
		}
	}
}
