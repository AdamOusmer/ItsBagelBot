// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package relay

import (
	"context"
	"testing"

	"ItsBagelBot/app/discord/ingress/internal/gateway"
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
)

type fakeREST struct {
	calls []discordapi.Callback
	err   error
}

func (f *fakeREST) InteractionCallback(_ context.Context, cb discordapi.Callback) error {
	f.calls = append(f.calls, cb)
	return f.err
}

type fakePublisher struct {
	subjects []string
	payloads [][]byte
}

func (f *fakePublisher) PublishOwned(_ context.Context, subject string, payload []byte) error {
	f.subjects = append(f.subjects, subject)
	f.payloads = append(f.payloads, payload)
	return nil
}
func (f *fakePublisher) PublishOwnedWithID(ctx context.Context, subject, _ string, payload []byte) error {
	return f.PublishOwned(ctx, subject, payload)
}
func (f *fakePublisher) Flush(context.Context) error { return nil }
func (f *fakePublisher) Close() error                { return nil }

func TestDispatchRoutesEventsToTheirSubject(t *testing.T) {
	cases := []struct {
		eventType string
		want      string
	}{
		{"GUILD_MEMBER_ADD", ddiscord.SubjectEventMember},
		{"GUILD_MEMBER_REMOVE", ddiscord.SubjectEventMember},
		{"VOICE_STATE_UPDATE", ddiscord.SubjectEventVoice},
		{"MESSAGE_CREATE", ddiscord.SubjectEventMessage},
		{"MESSAGE_UPDATE", ddiscord.SubjectEventMessage},
		{"MESSAGE_DELETE", ddiscord.SubjectEventMessage},
		{"GUILD_CREATE", ddiscord.SubjectEventGuild},
		{"GUILD_AUDIT_LOG_ENTRY_CREATE", ddiscord.SubjectEventAudit},
	}
	for _, tc := range cases {
		t.Run(tc.eventType, func(t *testing.T) {
			pub := &fakePublisher{}
			r := &Relay{Pub: pub}
			raw, _ := codec.Marshal(map[string]string{"guild_id": "g1"})
			if err := r.Dispatch(context.Background(), gateway.Event{Type: tc.eventType, Raw: raw}); err != nil {
				t.Fatalf("dispatch: %v", err)
			}
			if len(pub.subjects) != 1 || pub.subjects[0] != tc.want {
				t.Fatalf("subjects = %v, want [%s]", pub.subjects, tc.want)
			}
		})
	}
}

func TestDispatchDropsUnknownEventTypes(t *testing.T) {
	pub := &fakePublisher{}
	r := &Relay{Pub: pub}
	if err := r.Dispatch(context.Background(), gateway.Event{Type: "PRESENCE_UPDATE", Raw: []byte(`{}`)}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(pub.subjects) != 0 {
		t.Fatalf("unexpected publish for unhandled type: %v", pub.subjects)
	}
}

func TestDispatchDefersInteractionBeforePublishing(t *testing.T) {
	rest := &fakeREST{}
	pub := &fakePublisher{}
	r := &Relay{REST: rest, Pub: pub}
	raw, _ := codec.Marshal(map[string]string{"id": "int-1", "token": "tok-1", "guild_id": "g1"})

	if err := r.Dispatch(context.Background(), gateway.Event{Type: "INTERACTION_CREATE", Raw: raw}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(rest.calls) != 1 {
		t.Fatalf("interaction callback calls = %d, want 1", len(rest.calls))
	}
	if rest.calls[0].Type != deferredResponseType {
		t.Fatalf("callback type = %d, want %d", rest.calls[0].Type, deferredResponseType)
	}
	if rest.calls[0].Interaction.ID != "int-1" || rest.calls[0].Interaction.Token != "tok-1" {
		t.Fatalf("callback interaction = %+v", rest.calls[0].Interaction)
	}
	if len(pub.subjects) != 1 || pub.subjects[0] != ddiscord.SubjectEventInteraction {
		t.Fatalf("subjects = %v", pub.subjects)
	}
}

func TestDispatchDropsInteractionWhenDeferFails(t *testing.T) {
	rest := &fakeREST{err: discordapi.ErrForbidden}
	pub := &fakePublisher{}
	r := &Relay{REST: rest, Pub: pub}
	raw, _ := codec.Marshal(map[string]string{"id": "int-1", "token": "tok-1"})

	if err := r.Dispatch(context.Background(), gateway.Event{Type: "INTERACTION_CREATE", Raw: raw}); err != nil {
		t.Fatalf("dispatch should swallow a defer failure, got %v", err)
	}
	if len(pub.subjects) != 0 {
		t.Fatal("must not publish work for an interaction ingress could not acknowledge")
	}
}

func TestRouteFieldsLiftsIDsByEventShape(t *testing.T) {
	guildCreate, _ := codec.Marshal(map[string]string{"id": "g1"})
	assertRouteFields(t, "GUILD_CREATE", guildCreate, routeIDs{Guild: "g1"})

	member, _ := codec.Marshal(map[string]any{
		"guild_id": "g1", "user": map[string]string{"id": "u1"},
	})
	assertRouteFields(t, "GUILD_MEMBER_ADD", member, routeIDs{Guild: "g1", User: "u1"})

	msg, _ := codec.Marshal(map[string]any{
		"guild_id": "g1", "channel_id": "c1", "author": map[string]string{"id": "u1"},
	})
	assertRouteFields(t, "MESSAGE_CREATE", msg, routeIDs{Guild: "g1", Channel: "c1", User: "u1"})
}

// routeIDs is routeFields' (guild, channel, user) result, named so
// assertRouteFields can compare "got" against "want" with a single struct
// comparison instead of a three-clause g != wantGuild || c != wantChannel ||
// u != wantUser -- CodeScene's Complex Conditional flags any single
// expression combining more than one && / ||, and this comparison is
// naturally one equality check per field, not one "are these different in
// any way" expression.
type routeIDs struct {
	Guild   string
	Channel string
	User    string
}

// assertRouteFields fails the test unless routeFields lifts exactly the
// given guild/channel/user ids for one event shape, so each shape above is a
// single call instead of its own chain of ||.
func assertRouteFields(t *testing.T, eventType string, raw []byte, want routeIDs) {
	t.Helper()
	g, c, u := routeFields(eventType, raw)
	got := routeIDs{Guild: g, Channel: c, User: u}
	if got != want {
		t.Fatalf("%s fields = %+v, want %+v", eventType, got, want)
	}
}

func TestReadyIsANoOp(t *testing.T) {
	r := &Relay{}
	if err := r.Ready(context.Background(), gateway.Identity{ApplicationID: "app-1"}); err != nil {
		t.Fatalf("ready: %v", err)
	}
}
