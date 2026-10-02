// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"testing"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

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

func TestConfirmedPublisherSendsOnTheLaneWithAnIdentity(t *testing.T) {
	pub := &recordingPublisher{}
	cmd := ddiscord.Command{Type: ddiscord.TypeDeleteMessage, GuildID: "g1", ChannelID: "c1"}

	require.NoError(t, confirmedPublisher(pub)(context.Background(), cmd))

	require.Equal(t, ddiscord.Lane(cmd.Type), pub.subject)
	require.NotEmpty(t, pub.id, "bus.PublishConfirmed refuses an empty message id outright")
	var got ddiscord.Command
	require.NoError(t, codec.Unmarshal(pub.payload, &got))
	require.Equal(t, cmd, got)
}

func TestConfirmedPublisherReturnsTheBrokerVerdict(t *testing.T) {
	want := errors.New("bus: asynchronous publish cohort PubAck timeout")
	pub := &recordingPublisher{fail: want}

	err := confirmedPublisher(pub)(context.Background(), ddiscord.Command{Type: "post"})

	require.ErrorIs(t, err, want)
}
