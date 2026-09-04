// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelUpdateOutgress(t *testing.T) {
	msg, err := buildOutgressMessage(&module.Output{
		Type:          outgress.TypeChannelUpdate,
		BroadcasterID: "100",
		Reason:        "title",
		Text:          "Ranked grind",
		Template:      "en",
		To:            "alice",
	})
	require.NoError(t, err)
	assert.Equal(t, outgress.TypeChannelUpdate, msg.Type)
	assert.Equal(t, "100", msg.BroadcasterID)
	var inner struct {
		Field  string `json:"field"`
		Value  string `json:"value"`
		Locale string `json:"locale"`
		User   string `json:"user"`
	}
	require.NoError(t, codec.Unmarshal(msg.Payload, &inner))
	assert.Equal(t, "title", inner.Field)
	assert.Equal(t, "Ranked grind", inner.Value)
	assert.Equal(t, "en", inner.Locale)
	assert.Equal(t, "alice", inner.User)
}

func TestCommercialOutgress(t *testing.T) {
	msg, err := buildOutgressMessage(&module.Output{
		Type:          outgress.TypeCommercial,
		BroadcasterID: "100",
		Duration:      60,
		Template:      "fr",
		To:            "alice",
	})
	require.NoError(t, err)
	var inner struct {
		Length int    `json:"length"`
		Locale string `json:"locale"`
		User   string `json:"user"`
	}
	require.NoError(t, codec.Unmarshal(msg.Payload, &inner))
	assert.Equal(t, 60, inner.Length)
	assert.Equal(t, "fr", inner.Locale)
}

func TestStreamMarkerOutgress(t *testing.T) {
	msg, err := buildOutgressMessage(&module.Output{
		Type:          outgress.TypeStreamMarker,
		BroadcasterID: "100",
		Text:          "boss",
		Template:      "en",
		To:            "alice",
	})
	require.NoError(t, err)
	var inner struct {
		Description string `json:"description"`
		Locale      string `json:"locale"`
		User        string `json:"user"`
	}
	require.NoError(t, codec.Unmarshal(msg.Payload, &inner))
	assert.Equal(t, "boss", inner.Description)
}
