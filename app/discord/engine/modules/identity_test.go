// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func identityFor(status string, applied *fakeApplied) (*modules.Identity, *[]ddiscord.Command) {
	published := &[]ddiscord.Command{}
	return &modules.Identity{
		Resolve: func(context.Context, uint64) []discordstore.GuildConfigOf {
			return []discordstore.GuildConfigOf{{Guild: discordstore.Guild{ID: "g1"}, Config: ddiscord.Config{GuildID: "g1"}}}
		},
		Status:  func(context.Context, uint64) (string, bool) { return status, status != "" },
		Applied: applied,
		Publish: func(_ context.Context, c ddiscord.Command) error {
			*published = append(*published, c)
			return nil
		},
		Log: zap.NewNop(),
	}, published
}

func identityTiers(t *testing.T, cmds []ddiscord.Command) []string {
	t.Helper()
	var tiers []string
	for _, c := range cmds {
		require.Equal(t, ddiscord.TypeSetGuildIdentity, c.Type)
		require.Equal(t, ddiscord.LaneDefault, ddiscord.Lane(c.Type), "identity rides the default lane, not moderation")
		var payload ddiscord.IdentityPayload
		require.NoError(t, codec.Unmarshal(c.Payload, &payload))
		if payload.Identity.Premium {
			tiers = append(tiers, "premium")
		} else {
			tiers = append(tiers, "standard")
		}
	}
	return tiers
}

func connectGuild(t *testing.T, i *modules.Identity) []ddiscord.Command {
	t.Helper()
	var emitted []ddiscord.Command
	c := &module.Context{
		Event:  ddiscord.Event{Type: "GUILD_CREATE", GuildID: "g1"},
		Config: ddiscord.Config{GuildID: "g1"}, BroadcasterID: "999", Log: zap.NewNop(),
	}
	handler := modules.IdentityModule(i).Events[ddiscord.SubjectEventGuild]
	require.NoError(t, handler(context.Background(), c, func(cmd ddiscord.Command) { emitted = append(emitted, cmd) }))
	return emitted
}

func TestIdentityOnGuildConnect(t *testing.T) {
	cases := []struct {
		name        string
		statuses    []string
		recordErr   error
		want        []string
		wantRecords int
	}{
		{name: "a paid guild gets the premium identity", statuses: []string{"paid"}, want: []string{"premium"}, wantRecords: 1},
		{name: "vip is treated as premium", statuses: []string{"vip"}, want: []string{"premium"}, wantRecords: 1},
		{name: "a free guild clears the override", statuses: []string{"free"}, want: []string{"standard"}, wantRecords: 1},
		{name: "a second connect emits nothing", statuses: []string{"paid", "paid"}, want: []string{"premium"}, wantRecords: 1},
		{name: "an upgrade after apply emits again", statuses: []string{"free", "paid"}, want: []string{"standard", "premium"}, wantRecords: 2},
		{name: "an unprojected account leaves the guild alone", statuses: []string{""}},
		{name: "a record failure still emits", statuses: []string{"paid"}, recordErr: errors.New("valkey down"), want: []string{"premium"}, wantRecords: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applied := &fakeApplied{seen: map[string]string{}, recordErr: tc.recordErr}
			var emitted []ddiscord.Command

			for _, status := range tc.statuses {
				i, _ := identityFor(status, applied)
				emitted = append(emitted, connectGuild(t, i)...)
			}

			require.Equal(t, tc.want, identityTiers(t, emitted))
			require.Equal(t, tc.wantRecords, applied.records)
		})
	}
}

func TestIdentityOnUserChanged(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    []string
	}{
		{name: "a tier upgrade applies immediately", payload: `{"user_id": 999, "status": "paid"}`, want: []string{"premium"}},
		{name: "a malformed payload is acked and publishes nothing", payload: `{not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i, published := identityFor("free", &fakeApplied{seen: map[string]string{}})

			err := i.HandleUserChanged(bus.NewMessage("m", []byte(tc.payload)))

			require.NoError(t, err, "an error would nack the message")
			require.Equal(t, tc.want, identityTiers(t, *published))
		})
	}
}
