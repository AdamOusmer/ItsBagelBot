// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
)

func rememberDesk(t *testing.T, store *discordstore.Mem, channelID, messageID string) {
	t.Helper()
	panel := discordstore.DeskPanel{GuildID: "guild-1", ChannelID: channelID, MessageID: messageID}
	require.NoError(t, store.RememberDesk(context.Background(), panel))
}

func deskStore(t *testing.T, remembered bool) *discordstore.Mem {
	t.Helper()
	store := boundStore(owners{"guild-1": "b1"})
	if remembered {
		rememberDesk(t, store, "support", "old")
	}
	return store
}

type deskPost struct {
	Channel string
	Title   string
	Color   int
	Button  string
}

type deskView struct {
	ID         string
	Deleted    []discapi.Message
	Posted     []deskPost
	Remembered discordstore.DeskPanel
}

func viewDesk(id string, d *fakeDiscord, store *discordstore.Mem) deskView {
	view := deskView{ID: id, Deleted: d.deleted}
	for _, p := range d.panels {
		view.Posted = append(view.Posted, deskPost{
			Channel: p.Post.ChannelID, Title: p.Post.Embed.Title, Color: p.Post.Embed.Color, Button: p.Buttons[0].Label,
		})
	}
	view.Remembered, _ = store.Desk(context.Background(), discordstore.Guild{ID: "guild-1"})
	return view
}

func TestRepostDesk(t *testing.T) {
	previous := discordstore.DeskPanel{GuildID: "guild-1", ChannelID: "support", MessageID: "old"}
	reposted := discordstore.DeskPanel{GuildID: "guild-1", ChannelID: "support", MessageID: "panel-in-support"}
	defaultPost := deskPost{Channel: "support", Title: ddiscord.TicketPanelTitleDefault, Color: ddiscord.LiveColor, Button: ddiscord.TicketPanelButtonDefault}
	cases := []struct {
		name       string
		caller     string
		channel    string
		panel      ddiscord.TicketPanelSpec
		remembered bool
		offline    bool
		deleteErr  error
		wantErr    error
		want       deskView
	}{{
		name:       "replaces the previous panel in its channel and remembers the new one",
		caller:     "b1",
		panel:      ddiscord.TicketPanelSpec{Title: "Need a hand?", Button: "Contact staff"},
		remembered: true,
		want: deskView{
			ID: "panel-in-support", Deleted: []discapi.Message{{ChannelID: "support", ID: "old"}},
			Posted:     []deskPost{{Channel: "support", Title: "Need a hand?", Color: ddiscord.LiveColor, Button: "Contact staff"}},
			Remembered: reposted,
		},
	}, {
		name:    "fills blank copy with the defaults",
		caller:  "b1",
		channel: "support",
		want:    deskView{ID: "panel-in-support", Posted: []deskPost{defaultPost}, Remembered: reposted},
	}, {
		name:       "still answers with the new panel when the old one cannot be deleted",
		caller:     "b1",
		remembered: true,
		deleteErr:  errors.New("unknown message"),
		want: deskView{
			ID: "panel-in-support", Deleted: []discapi.Message{{ChannelID: "support", ID: "old"}},
			Posted: []deskPost{defaultPost}, Remembered: reposted,
		},
	}, {
		name:    "refuses when there is nowhere to post",
		caller:  "b1",
		wantErr: discapi.ErrBadRequest,
	}, {
		name:       "refuses someone else's guild",
		caller:     "someone-else",
		channel:    "support",
		remembered: true,
		wantErr:    setup.ErrNotBound,
		want:       deskView{Remembered: previous},
	}, {
		name:    "refuses without a discord client",
		caller:  "b1",
		offline: true,
		wantErr: setup.ErrDiscordUnavailable,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDiscord{deleteErr: tc.deleteErr}
			store := deskStore(t, tc.remembered)
			req := setup.DeskRepostRequest{GuildID: "guild-1", BroadcasterID: tc.caller, ChannelID: tc.channel, Panel: tc.panel}

			id, err := workerFor(d, store, tc.offline).RepostDesk(context.Background(), req)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, viewDesk(id, d, store))
		})
	}
}

func TestRepostRefusesForeignTargetAndRememberedPanelBeforeDelete(t *testing.T) {
	for _, remembered := range []bool{false, true} {
		rest := &fakeDiscord{channelGuilds: map[string]string{"safe": "guild-1", "foreign": "guild-2"}}
		store := boundStore(owners{"guild-1": "b1"})
		w := newWorker(rest, store)
		target := "foreign"
		previous := "safe"
		if remembered {
			target = "safe"
			previous = "foreign"
		}
		rememberDesk(t, store, previous, "old")
		_, err := w.RepostDesk(context.Background(), setup.DeskRepostRequest{GuildID: "guild-1", BroadcasterID: "b1", ChannelID: target})
		require.ErrorIs(t, err, discapi.ErrForbidden, "expected refusal")
		require.Empty(t, rest.deleted, "deleted before membership check")
		require.Empty(t, rest.panels, "posted before membership check")
	}
}
