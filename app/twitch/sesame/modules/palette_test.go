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

// TestReplyPureFamilyWorksAcrossSurfaces is the Phase 3 headline claim: the
// pure family ({math:…}, {countdown:…}, and by extension {countup:…},
// {repeat:…}, {queryescape:…}, {pathescape:…}, {random}, {choice:…}) now
// resolves in every module reply, not just custom commands — because every
// module reply now expands through module.Palette (or module.KV, its
// closure-free adapter), and Palette.Resolve's miss path is
// engine/scope/pure.go's Pure, the exact scope a custom command's own
// template resolves against.
//
// Two surfaces stand in for the whole tree: alerts (a onAlert/module.KV
// reply, migrated off a hand-rolled tmpl.Dynamic-only switch) and queue (a
// chatReplier.reply kv caller, the 146-caller shape reply.go's rewrite had to
// keep working). Both used to answer {math:…} and {countdown:…} with the
// literal braces; both must not now.
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

// TestReplyLocaleReachesPureFamily pins that the channel's console locale —
// not just the pure family itself — reaches every Palette-based reply.
// alerts.go's onAlert used to build its Palette from module.KV alone, which
// starts locale-less; without WithLocale(c.Locale) a FR channel's
// {countdown} would render in the catalog's English fallback regardless of
// the broadcaster's own language, same as every other non-Common KV
// callsite this migration threads c.Locale through (shoutout, govee,
// songqueue_redeem, channelpoints, emoteplay, fortnite, triggers,
// module_vars.quoteLine, raffle_mechanics.expandTokens, timeofday's
// Spec.Bind).
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

// TestReplyPalettePayloadOnDeclaredNameStaysLiteral pins Palette.Resolve's
// half-a-token rule (module/vars.go): a payload on a name the palette itself
// declares is not "the token with extra data", it is a different, unknown
// span — {user:x} is not {user}, the same way {random:x} is not {random}
// unless x is a well-formed range. The queue module declares {user} through
// module.Common, so {user:x} in a customized template must render literally
// rather than silently dropping the payload and printing the chatter's name.
func TestReplyPalettePayloadOnDeclaredNameStaysLiteral(t *testing.T) {
	q := &fakeQueue{open: true}
	m := Queue(queueDeps(q))
	c := queueCtx("alice", "")
	c.Config = []byte(`{"joinMessage":"hi {user:x}, spot {pos}"}`)
	out := runQueue(t, m, "join", c, "")
	require.Len(t, out, 1)
	assert.Equal(t, "hi {user:x}, spot 1", out[0].Text)
}

// TestRewardTrioSanitizesInput pins the shared {input} entry
// (sanitizeRewardInput, channelpoints.go): channelpoints, songqueue_redeem
// and govee are the three reward-redemption surfaces that expose {input},
// and all three must now strip the same leading slash/space run so a
// redeemer cannot mint a slash-verb as the bot regardless of which reward
// the template is pasted into.
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
