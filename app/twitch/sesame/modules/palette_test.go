// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplyPureFamilyWorksAcrossSurfaces(t *testing.T) {
	digits := regexp.MustCompile(`\d`)

	t.Run("alerts reply", func(t *testing.T) {
		cfg := `{"followMessage":"{user} joined! sum={math:1+1} in {countdown:2030-01-01}"}`
		out := runAlert(t, alertInput{event: "channel.follow", payload: followJSON, cfg: cfg})
		require.Len(t, out, 1)
		text := out[0].Text
		assert.NotContains(t, text, "{math:1+1}")
		assert.NotContains(t, text, "{countdown:2030-01-01}")
		assert.Contains(t, text, "sum=2")
		assert.True(t, digits.MatchString(text), "countdown span must render some duration, got %q", text)
	})

	t.Run("chatReplier reply (queue)", func(t *testing.T) {
		q := &fakeQueue{open: true}
		m := Queue(queueDeps(q))
		c := queueCtx("alice", "")
		c.Config = []byte(`{"joinMessage":"welcome {user}! sum={math:1+1} in {countdown:2030-01-01}"}`)
		out := runQueue(t, m, "join", c, "")
		require.Len(t, out, 1)
		text := out[0].Text
		assert.NotContains(t, text, "{math:1+1}")
		assert.NotContains(t, text, "{countdown:2030-01-01}")
		assert.Contains(t, text, "sum=2")
		assert.True(t, digits.MatchString(text), "countdown span must render some duration, got %q", text)
	})
}

func TestReplyLocaleReachesPureFamily(t *testing.T) {
	cfg := `{"followMessage":"in {countdown:9999-01-01}"}`

	en := alertsCtx("channel.follow", followJSON, cfg)
	var enOut collector
	require.NoError(t, alertsHandler(t, "channel.follow")(context.Background(), en, enOut.emit))
	require.Len(t, enOut.out, 1)

	fr := alertsCtx("channel.follow", followJSON, cfg)
	fr.Locale = "fr"
	var frOut collector
	require.NoError(t, alertsHandler(t, "channel.follow")(context.Background(), fr, frOut.emit))
	require.Len(t, frOut.out, 1)

	assert.NotEqual(t, enOut.out[0].Text, frOut.out[0].Text,
		"a FR channel's {countdown} must not render the same words as an EN one")
}

func TestReplyPalettePayloadOnDeclaredNameStaysLiteral(t *testing.T) {
	q := &fakeQueue{open: true}
	m := Queue(queueDeps(q))
	c := queueCtx("alice", "")
	c.Config = []byte(`{"joinMessage":"hi {user:x}, spot {pos}"}`)
	out := runQueue(t, m, "join", c, "")
	require.Len(t, out, 1)
	assert.Equal(t, "hi {user:x}, spot 1", out[0].Text)
}

func TestRewardTrioSanitizesInput(t *testing.T) {
	ev := redemptionEvent{
		UserName:             "Viewer",
		UserLogin:            "viewer",
		UserInput:            " /announce pwned",
		BroadcasterUserLogin: "streamer",
	}
	const wantInput = "announce pwned"

	t.Run("channelpoints", func(t *testing.T) {
		got := expandReward(rewardChatParams{event: ev, binding: rewardBinding{Message: "[{input}]"}})
		assert.Equal(t, "["+wantInput+"]", got)
	})

	t.Run("songqueue_redeem", func(t *testing.T) {
		got := renderSongqueueRedeemReply(songqueueRedeemReplyParams{event: ev, text: "[{input}]", track: "Track", pos: 1})
		assert.Equal(t, "["+wantInput+"]", got)
	})

	t.Run("govee", func(t *testing.T) {
		got := renderGoveeReply("", "[{input}]", ev, "red")
		assert.Equal(t, "["+wantInput+"]", got)
	})
}
