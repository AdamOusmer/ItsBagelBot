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
	if err := store.RememberDesk(ctx, discordstore.DeskPanel{GuildID: "guild-1", ChannelID: "support", MessageID: "old"}); err != nil {
		t.Fatal(err)
	}
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
	if len(rec.deleted) != 1 || rec.deleted[0] != "old" {
		t.Fatalf("deleted = %v, want the previous panel", rec.deleted)
	}
	// The channel came from the remembered panel, not from the request.
	if len(rec.panelPosts) != 1 || rec.panelPosts[0].ChannelID != "support" {
		t.Fatalf("panel posts = %+v", rec.panelPosts)
	}
	if rec.panelPosts[0].Embed.Title != "Need a hand?" {
		t.Fatalf("panel embed = %+v", rec.panelPosts[0].Embed)
	}
	if len(rec.panelButtons) != 1 || rec.panelButtons[0].Label != "Contact staff" {
		t.Fatalf("panel buttons = %+v", rec.panelButtons)
	}
	got, ok := store.Desk(ctx, discordstore.Guild{ID: "guild-1"})
	if !ok || got.MessageID != id || got.ChannelID != "support" {
		t.Fatalf("remembered desk = %+v, %v", got, ok)
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

	// ErrNotBound, not ErrGuildBoundElsewhere: every dashboard-facing verb
	// now takes the strict check, which collapses "no binding" and "somebody
	// else's binding" into one refusal so a caller cannot map guild ids by
	// probing. Only the setup verb, where the distinction is a real screen,
	// still reports "bound elsewhere".
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

// A panel the previous delete could not remove (somebody deleted it by hand)
// must not stop the repost: the new panel is the point.
func TestRepostDeskSurvivesAFailedDelete(t *testing.T) {
	rec := newGuildRecorder()
	rec.deleteErr = errors.New("unknown message")
	store := boundStore(t)
	ctx := context.Background()
	_ = store.RememberDesk(ctx, discordstore.DeskPanel{GuildID: "guild-1", ChannelID: "support", MessageID: "old"})
	w := setupWorker(rec, store)

	id, err := w.RepostDesk(ctx, DeskRepostRequest{GuildID: "guild-1", BroadcasterID: "b1"})

	if err != nil || id == "" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
}
