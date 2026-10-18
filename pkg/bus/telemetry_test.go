// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/testnats"

	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRPCTraceContextCrossesTheWire(t *testing.T) {
	app := newLocalApplication(t)
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	require.NoError(t, QueueSubscribeJSON(nc, subject, "svc", time.Second, app, zap.NewNop(),
		func(ctx context.Context, _ map[string]string) map[string]string {
			return map[string]string{"trace": newrelic.FromContext(ctx).GetTraceMetadata().TraceID}
		}))
	caller := app.StartTransaction("caller")
	defer caller.End()

	reply, err := RequestJSON[map[string]string](newrelic.NewContext(context.Background(), caller), nc, subject, map[string]string{})

	require.NoError(t, err)
	assert.NotEmpty(t, reply["trace"])
	assert.Equal(t, caller.GetTraceMetadata().TraceID, reply["trace"], "the handler must continue the caller's distributed trace")
}

func TestNormalizedDestinationNamesLanes(t *testing.T) {
	cases := map[string]string{
		"discord.ingress.event.message":     "discord.ingress.event.message",
		"discord.ingress.event.member":      "discord.ingress.event.member",
		"discord.ingress.event.voice":       "discord.ingress.event.voice",
		"discord.ingress.event.interaction": "discord.ingress.event.interaction",
		"discord.ingress.event.audit":       "discord.ingress.event.audit",
		"discord.ingress.event.guild":       "discord.ingress.event.guild",
		"discord.ingress.event.unknown":     "discord.ingress.event.other",
		"discord.outgress.mod":              "discord.outgress.mod",
		"discord.outgress.default":          "discord.outgress.default",
		"discord.outgress.surprise":         "discord.outgress.other",
		"data.twitch.clip.created":          "data.twitch.clip.created",
		"data.twitch.clip.deleted":          "data.twitch.clip.other",
		"data.users.changed":                "data.users.changed",
		"data.users.removed":                "data.users.other",
		"twitch.ingress.event.premium":      "twitch.ingress.event.premium",
		"twitch.ingress.event.nope":         "twitch.ingress.event.other",
		"twitch.outgress.system":            "twitch.outgress.system",
		"twitch.outgress.nope":              "twitch.outgress.other",
		"bagel.rpc.users.get":               "bagel.rpc.users",
		"bagel.rpc.discord-data.guild.get":  "bagel.rpc.discord-data",
		"bagel.rpc.discord-outgress.x.y":    "bagel.rpc.discord-outgress",
		"bagel.rpc.bogus.x":                 "bagel.rpc.other",
		"bagel.cache.invalidate.users":      "bagel.cache.invalidate",
		"something.else":                    "other",
	}
	for subject, want := range cases {
		assert.Equal(t, want, normalizedDestination(subject), subject)
	}
}
