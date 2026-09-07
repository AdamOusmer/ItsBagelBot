// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
)

// recordingPublisher is a bus.Publisher that answers only through the
// confirmed entry point, so a caller that slipped back onto PublishOwned
// fails here rather than silently losing its error classification.
type recordingPublisher struct {
	subject string
	id      string
	payload []byte
	fail    error
}

func (p *recordingPublisher) PublishOwned(context.Context, string, []byte) error {
	return errors.New("engine must publish commands on the confirmed path")
}

func (p *recordingPublisher) PublishOwnedWithID(_ context.Context, subject, id string, payload []byte) error {
	p.subject, p.id, p.payload = subject, id, payload
	return p.fail
}

func (p *recordingPublisher) Flush(context.Context) error { return nil }
func (p *recordingPublisher) Close() error                { return nil }

// The engine's publisher must go through PublishOwnedWithID with a non-empty
// identity: bus.PublishConfirmed rejects an empty one outright, and the
// confirmed path is the only one that hands dispatch the broker's real
// verdict to classify.
func TestConfirmedPublisherSendsOnTheLaneWithAnIdentity(t *testing.T) {
	pub := &recordingPublisher{}
	cmd := ddiscord.Command{Type: ddiscord.TypeDeleteMessage, GuildID: "g1", ChannelID: "c1"}

	if err := confirmedPublisher(pub)(context.Background(), cmd); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if want := ddiscord.Lane(cmd.Type); pub.subject != want {
		t.Fatalf("subject = %q, want %q", pub.subject, want)
	}
	if pub.id == "" {
		t.Fatal("message id is empty; bus.PublishConfirmed refuses that publish outright")
	}
	var got ddiscord.Command
	if err := codec.Unmarshal(pub.payload, &got); err != nil {
		t.Fatalf("payload does not decode as a Command: %v", err)
	}
	if !reflect.DeepEqual(got, cmd) {
		t.Fatalf("payload = %+v, want %+v", got, cmd)
	}
}

// The broker's verdict has to reach the caller unchanged; dispatch
// classifies on it.
func TestConfirmedPublisherReturnsTheBrokerVerdict(t *testing.T) {
	want := errors.New("bus: asynchronous publish cohort PubAck timeout")
	pub := &recordingPublisher{fail: want}

	err := confirmedPublisher(pub)(context.Background(), ddiscord.Command{Type: "post"})

	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want the publisher's own error", err)
	}
}
