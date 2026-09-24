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

func boundStore(t *testing.T) *discordstore.Mem {
	t.Helper()
	store := discordstore.NewMem()
	if err := store.BindGuild(context.Background(), discordstore.Binding{Guild: discordstore.Guild{ID: "guild-1"}, Broadcaster: discordstore.Broadcaster{ID: "b1"}}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestRepostDeskDeletesThePreviousPanelAndRemembersTheNewOne(t *testing.T) {
	rec := newGuildRecorder()
	store := boundStore(t)
	ctx := context.Background()
	rememberDesk(t, store, "support", "old")
	w := setupWorker(rec, store)

	id, err := w.RepostDesk(ctx, DeskRepostRequest{
		GuildID: "guild-1", BroadcasterID: "b1",
		Panel: ddiscord.TicketPanelSpec{Title: "Need a hand?", Button: "Contact staff"},
	})
	if err != nil {
		t.Fatalf("RepostDesk: %v", err)
	}
	if id == "" {
		t.Fatal("the new panel's id is what the dashboard shows as confirmation")
	}

	wantDeletedPanel(t, rec, "old")
	wantSinglePanel(t, rec, panelWant{ChannelID: "support", Title: "Need a hand?", Button: "Contact staff"})
	wantRememberedDesk(t, store, "support", id)
}

func rememberDesk(t *testing.T, store *discordstore.Mem, channelID, messageID string) {
	t.Helper()
	panel := discordstore.DeskPanel{GuildID: "guild-1", ChannelID: channelID, MessageID: messageID}
	if err := store.RememberDesk(context.Background(), panel); err != nil {
		t.Fatal(err)
	}
}

func wantDeletedPanel(t *testing.T, rec *guildRecorder, messageID string) {
	t.Helper()
	if len(rec.deleted) != 1 {
		t.Fatalf("deleted = %v, want exactly the previous panel", rec.deleted)
	}
	if rec.deleted[0] != messageID {
		t.Fatalf("deleted = %q, want %q", rec.deleted[0], messageID)
	}
}

type panelWant struct {
	ChannelID string
	Title     string
	Button    string
}

func wantSinglePanel(t *testing.T, rec *guildRecorder, want panelWant) {
	t.Helper()
	if len(rec.panelPosts) != 1 {
		t.Fatalf("panel posts = %+v, want exactly one", rec.panelPosts)
	}
	if rec.panelPosts[0].ChannelID != want.ChannelID {
		t.Fatalf("panel channel = %q, want %q", rec.panelPosts[0].ChannelID, want.ChannelID)
	}
	if rec.panelPosts[0].Embed.Title != want.Title {
		t.Fatalf("panel embed = %+v, want title %q", rec.panelPosts[0].Embed, want.Title)
	}
	if len(rec.panelButtons) != 1 {
		t.Fatalf("panel buttons = %+v, want exactly one", rec.panelButtons)
	}
	if rec.panelButtons[0].Label != want.Button {
		t.Fatalf("button label = %q, want %q", rec.panelButtons[0].Label, want.Button)
	}
}

func wantRememberedDesk(t *testing.T, store *discordstore.Mem, channelID, messageID string) {
	t.Helper()
	got, ok := store.Desk(context.Background(), discordstore.Guild{ID: "guild-1"})
	if !ok {
		t.Fatal("no desk remembered after a repost")
	}
	if got.MessageID != messageID {
		t.Fatalf("remembered message = %q, want %q", got.MessageID, messageID)
	}
	if got.ChannelID != channelID {
		t.Fatalf("remembered channel = %q, want %q", got.ChannelID, channelID)
	}
}

func TestRepostDeskFillsBlankCopyWithTheDefaults(t *testing.T) {
	rec := newGuildRecorder()
	w := setupWorker(rec, boundStore(t))

	_, err := w.RepostDesk(context.Background(), DeskRepostRequest{
		GuildID: "guild-1", BroadcasterID: "b1", ChannelID: "support",
	})
	if err != nil {
		t.Fatalf("RepostDesk: %v", err)
	}
	if rec.panelPosts[0].Embed.Title != ddiscord.TicketPanelTitleDefault {
		t.Fatalf("title = %q", rec.panelPosts[0].Embed.Title)
	}
	if rec.panelButtons[0].Label != ddiscord.TicketPanelButtonDefault {
		t.Fatalf("button = %q", rec.panelButtons[0].Label)
	}
	if rec.panelPosts[0].Embed.Color != ddiscord.LiveColor {
		t.Fatalf("color = %d, want the brand default rather than black", rec.panelPosts[0].Embed.Color)
	}
}

func TestRepostDeskWithNowhereToPost(t *testing.T) {
	w := setupWorker(newGuildRecorder(), boundStore(t))

	_, err := w.RepostDesk(context.Background(), DeskRepostRequest{GuildID: "guild-1", BroadcasterID: "b1"})

	if !errors.Is(err, discapi.ErrBadRequest) {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
}

func TestRepostDeskRefusesSomeoneElsesGuild(t *testing.T) {
	w := setupWorker(newGuildRecorder(), boundStore(t))

	_, err := w.RepostDesk(context.Background(), DeskRepostRequest{
		GuildID: "guild-1", BroadcasterID: "someone-else", ChannelID: "support",
	})

	if !errors.Is(err, ErrNotBound) {
		t.Fatalf("err = %v, want ErrNotBound", err)
	}
}

func TestRepostDeskWithoutADiscordClient(t *testing.T) {
	w := New(Config{Store: boundStore(t)})

	_, err := w.RepostDesk(context.Background(), DeskRepostRequest{GuildID: "guild-1", BroadcasterID: "b1"})

	if !errors.Is(err, ErrDiscordUnavailable) {
		t.Fatalf("err = %v, want ErrDiscordUnavailable", err)
	}
}

func TestRepostDeskSurvivesAFailedDelete(t *testing.T) {
	rec := newGuildRecorder()
	rec.deleteErr = errors.New("unknown message")
	store := boundStore(t)
	ctx := context.Background()
	rememberDesk(t, store, "support", "old")
	w := setupWorker(rec, store)

	id, err := w.RepostDesk(ctx, DeskRepostRequest{GuildID: "guild-1", BroadcasterID: "b1"})

	if err != nil {
		t.Fatalf("RepostDesk: %v", err)
	}
	if id == "" {
		t.Fatal("the repost still has to answer with the new panel's id")
	}
}
