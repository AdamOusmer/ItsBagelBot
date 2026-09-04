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
	if g, _, _ := routeFields("GUILD_CREATE", guildCreate); g != "g1" {
		t.Fatalf("guild create id = %q", g)
	}

	member, _ := codec.Marshal(map[string]any{
		"guild_id": "g1", "user": map[string]string{"id": "u1"},
	})
	if g, _, u := routeFields("GUILD_MEMBER_ADD", member); g != "g1" || u != "u1" {
		t.Fatalf("member fields = %q/%q", g, u)
	}

	msg, _ := codec.Marshal(map[string]any{
		"guild_id": "g1", "channel_id": "c1", "author": map[string]string{"id": "u1"},
	})
	if g, c, u := routeFields("MESSAGE_CREATE", msg); g != "g1" || c != "c1" || u != "u1" {
		t.Fatalf("message fields = %q/%q/%q", g, c, u)
	}
}

func TestReadyIsANoOp(t *testing.T) {
	r := &Relay{}
	if err := r.Ready(context.Background(), gateway.Identity{ApplicationID: "app-1"}); err != nil {
		t.Fatalf("ready: %v", err)
	}
}
