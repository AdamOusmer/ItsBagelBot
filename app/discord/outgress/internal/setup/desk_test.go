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
	// The channel came from the remembered panel, not from the request.
	wantSinglePanel(t, rec, "support", "Need a hand?", "Contact staff")
	wantRememberedDesk(t, store, "support", id)
}

// rememberDesk seeds the panel a repost is expected to replace.
func rememberDesk(t *testing.T, store *discordstore.Mem, channelID, messageID string) {
	t.Helper()
	panel := discordstore.DeskPanel{GuildID: "guild-1", ChannelID: channelID, MessageID: messageID}
	if err := store.RememberDesk(context.Background(), panel); err != nil {
		t.Fatal(err)
	}
}

// wantDeletedPanel asserts the repost removed exactly the previous panel:
// leaving it up gives a guild two live ticket buttons, one of them stale.
func wantDeletedPanel(t *testing.T, rec *guildRecorder, messageID string) {
	t.Helper()
	if len(rec.deleted) != 1 {
		t.Fatalf("deleted = %v, want exactly the previous panel", rec.deleted)
	}
	if rec.deleted[0] != messageID {
		t.Fatalf("deleted = %q, want %q", rec.deleted[0], messageID)
	}
}

// wantSinglePanel asserts one panel was posted, in channelID, with the copy the
// request asked for.
func wantSinglePanel(t *testing.T, rec *guildRecorder, channelID, title, button string) {
	t.Helper()
	if len(rec.panelPosts) != 1 {
		t.Fatalf("panel posts = %+v, want exactly one", rec.panelPosts)
	}
	if rec.panelPosts[0].ChannelID != channelID {
		t.Fatalf("panel channel = %q, want %q", rec.panelPosts[0].ChannelID, channelID)
	}
	if rec.panelPosts[0].Embed.Title != title {
		t.Fatalf("panel embed = %+v, want title %q", rec.panelPosts[0].Embed, title)
	}
	if len(rec.panelButtons) != 1 {
		t.Fatalf("panel buttons = %+v, want exactly one", rec.panelButtons)
	}
	if rec.panelButtons[0].Label != button {
		t.Fatalf("button label = %q, want %q", rec.panelButtons[0].Label, button)
	}
}

// wantRememberedDesk asserts the store now points at the new panel: the next
// repost deletes whatever this remembers, so a stale entry orphans a button.
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
	rememberDesk(t, store, "support", "old")
	w := setupWorker(rec, store)

	id, err := w.RepostDesk(ctx, DeskRepostRequest{GuildID: "guild-1", BroadcasterID: "b1"})

	if err != nil || id == "" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
}
