// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package relay_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/discord/ingress/internal/gateway"
	"ItsBagelBot/app/discord/ingress/internal/relay"
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type published struct {
	Subject string
	Event   ddiscord.Event
}

type recorder struct {
	restErr error
	calls   []any
	ids     []string
}

func (r *recorder) InteractionCallback(_ context.Context, cb discordapi.Callback) error {
	r.calls = append(r.calls, cb)
	return r.restErr
}

func (r *recorder) PublishOwned(_ context.Context, subject string, payload []byte) error {
	var ev ddiscord.Event
	if err := codec.Unmarshal(payload, &ev); err != nil {
		return err
	}
	ev.ReceivedAtUnixMs = 0
	r.calls = append(r.calls, published{Subject: subject, Event: ev})
	return nil
}

func (r *recorder) PublishOwnedWithID(ctx context.Context, subject, id string, payload []byte) error {
	r.ids = append(r.ids, id)
	return r.PublishOwned(ctx, subject, payload)
}

func (r *recorder) Flush(context.Context) error { return nil }
func (r *recorder) Close() error                { return nil }

type routeIDs struct{ Guild, Channel, User string }

func dispatch(t *testing.T, rec *recorder, ev gateway.Event) []any {
	t.Helper()
	r := &relay.Relay{REST: rec, Pub: rec}
	ctx := context.Background()
	require.NoError(t, r.Ready(ctx, gateway.Identity{ApplicationID: "app-1"}))
	require.NoError(t, r.Dispatch(ctx, ev))
	return rec.calls
}

func TestDispatchRoutesEventsToTheirSubject(t *testing.T) {
	member := []byte(`{"guild_id":"g1","user":{"id":"u1"}}`)
	voice := []byte(`{"guild_id":"g1","channel_id":"c1","user_id":"u1"}`)
	message := []byte(`{"guild_id":"g1","channel_id":"c1","author":{"id":"u1"}}`)
	deleted := []byte(`{"guild_id":"g1","channel_id":"c1","id":"m1"}`)
	cases := []struct {
		name    string
		typ     string
		raw     []byte
		subject string
		ids     routeIDs
	}{
		{"member join carries guild and user", "GUILD_MEMBER_ADD", member, ddiscord.SubjectEventMember, routeIDs{"g1", "", "u1"}},
		{"member leave carries guild and user", "GUILD_MEMBER_REMOVE", member, ddiscord.SubjectEventMember, routeIDs{"g1", "", "u1"}},
		{"voice state carries channel and user", "VOICE_STATE_UPDATE", voice, ddiscord.SubjectEventVoice, routeIDs{"g1", "c1", "u1"}},
		{"message create takes the author", "MESSAGE_CREATE", message, ddiscord.SubjectEventMessage, routeIDs{"g1", "c1", "u1"}},
		{"message update takes the author", "MESSAGE_UPDATE", message, ddiscord.SubjectEventMessage, routeIDs{"g1", "c1", "u1"}},
		{"message delete has no user", "MESSAGE_DELETE", deleted, ddiscord.SubjectEventMessage, routeIDs{"g1", "c1", ""}},
		{"guild create takes the payload id as the guild", "GUILD_CREATE", []byte(`{"id":"g1"}`), ddiscord.SubjectEventGuild, routeIDs{"g1", "", ""}},
		{"audit log entry goes to the audit subject", "GUILD_AUDIT_LOG_ENTRY_CREATE", voice, ddiscord.SubjectEventAudit, routeIDs{"g1", "c1", "u1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := published{tc.subject, ddiscord.Event{
				Type: tc.typ, GuildID: tc.ids.Guild, ChannelID: tc.ids.Channel, UserID: tc.ids.User, Raw: tc.raw,
			}}
			assert.Equal(t, []any{want}, dispatch(t, &recorder{}, gateway.Event{Type: tc.typ, Raw: tc.raw}))
		})
	}
}

func TestDispatchDefersInteractionsBeforePublishing(t *testing.T) {
	interaction := []byte(`{"id":"int-1","token":"tok-1","guild_id":"g1","channel_id":"c1","member":{"user":{"id":"u1"}}}`)
	deferred := discordapi.Callback{Interaction: discordapi.Interaction{ID: "int-1", Token: "tok-1"}, Type: 5}
	cases := []struct {
		name    string
		event   gateway.Event
		restErr error
		want    []any
	}{
		{
			name:  "interaction is deferred, then published with the member user",
			event: gateway.Event{Type: "INTERACTION_CREATE", Raw: interaction},
			want: []any{deferred, published{ddiscord.SubjectEventInteraction, ddiscord.Event{
				Type: "INTERACTION_CREATE", GuildID: "g1", ChannelID: "c1", UserID: "u1", Raw: interaction,
			}}},
		},
		{
			name:    "interaction ingress could not acknowledge is not published",
			event:   gateway.Event{Type: "INTERACTION_CREATE", Raw: interaction},
			restErr: discordapi.ErrForbidden,
			want:    []any{deferred},
		},
		{
			name:  "undecodable interaction is dropped without a callback",
			event: gateway.Event{Type: "INTERACTION_CREATE", Raw: []byte(`not json`)},
		},
		{
			name:  "unknown event types are dropped",
			event: gateway.Event{Type: "PRESENCE_UPDATE", Raw: []byte(`{}`)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, dispatch(t, &recorder{restErr: tc.restErr}, tc.event))
		})
	}
}

func TestDispatchPublishesConfirmedWithTheSessionSequenceID(t *testing.T) {
	rec := &recorder{}
	r := &relay.Relay{REST: rec, Pub: rec}

	require.NoError(t, r.Dispatch(context.Background(), gateway.Event{
		Type: "MESSAGE_CREATE", Raw: []byte(`{"guild_id":"g1"}`), SessionID: "sid", Seq: 42,
	}))

	assert.Equal(t, []string{"sid-42"}, rec.ids)
}

func TestDispatchWithoutASessionPublishesWithoutAnID(t *testing.T) {
	rec := &recorder{}
	r := &relay.Relay{REST: rec, Pub: rec}

	require.NoError(t, r.Dispatch(context.Background(), gateway.Event{
		Type: "MESSAGE_CREATE", Raw: []byte(`{"guild_id":"g1"}`), Seq: 3,
	}))

	assert.Empty(t, rec.ids, "an empty session must not mint a colliding ID")
	assert.Len(t, rec.calls, 1)
}
