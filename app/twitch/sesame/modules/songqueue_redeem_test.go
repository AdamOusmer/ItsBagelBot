// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func songRedeemCtx(config, input string) *module.Context {
	payload := fmt.Sprintf(`{"id":"redeem-1","broadcaster_user_id":"100","user_id":"42","user_name":"Alice","user_login":"alice","user_input":%q,"reward":{"id":"rw-sr","title":"Song request","cost":500}}`, input)
	c := eventCtx(eventInput{redemptionAddType, payload, config})
	c.BroadcasterID = 100
	return c
}

func songRedeemConfig(rewardID, extra string) string {
	return `{"redeem":{"enabled":true,"rewardId":"` + rewardID + `","onRedeem":"fulfill"` + extra + `}}`
}

type redeemOutcome int

const (
	redeemIgnored redeemOutcome = iota
	redeemQueued
	redeemRefunded
)

type redeemCase struct {
	name    string
	config  string
	input   string
	live    liveState
	preload []engine.SongEntry
	want    redeemOutcome
	reply   string
}

func TestSongRedeem(t *testing.T) {
	two := []engine.SongEntry{entry("a", "A", "1", "x"), entry("b", "B", "2", "y")}
	template := func(text string) string { return songRedeemConfig("rw-sr", `,"replyMessage":`+quote(text)) }
	cases := []redeemCase{
		{name: "offline refunds", config: songRedeemConfig("rw-sr", ""), live: liveOffline, want: redeemRefunded},
		{name: "allowOffline queues", config: songRedeemConfig("rw-sr", `,"allowOffline":true`), live: liveOffline, want: redeemQueued},
		{name: "a disabled path refunds", config: `{"redeem":{"enabled":false,"rewardId":"rw-sr","onRedeem":"fulfill"}}`, live: liveOnline, want: redeemRefunded},
		{name: "another reward is ignored", config: songRedeemConfig("rw-other", ""), live: liveOnline, want: redeemIgnored},
		{name: "the default reply names the track and position", config: songRedeemConfig("rw-sr", ""), live: liveOnline, preload: two, want: redeemQueued,
			reply: "@Alice queued Mr. Brightside, position #3."},
		{name: "a choice token resolves to its only option", config: template("{choice:only} pick, @{user}"), live: liveOnline, want: redeemQueued, reply: "only pick, @Alice"},
		{name: "an empty choice resolves to nothing", config: template("[{choice:}]"), live: liveOnline, want: redeemQueued, reply: "[]"},
		{name: "a bare choice stays literal", config: template("{choice}"), live: liveOnline, want: redeemQueued, reply: "{choice}"},
		{name: "a degenerate random range resolves to its bound", config: template("{random:7-7}"), live: liveOnline, want: redeemQueued, reply: "7"},
		{name: "an unknown token stays literal", config: template("{unknown}"), live: liveOnline, want: redeemQueued, reply: "{unknown}"},
		{name: "leading slashes and spaces never reach the reply", config: template("[{input}]"), input: " /announce pwned", live: liveOnline, want: redeemQueued,
			reply: "[announce pwned]"},
		{name: "an empty request refunds", config: songRedeemConfig("rw-sr", ""), input: "  ", live: liveOnline, want: redeemRefunded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeSongQueue{up: append([]engine.SongEntry(nil), tc.preload...)}
			d := songDeps(store, srSearchGossip(srTrack("t1", "Mr. Brightside", "The Killers")))
			d.Live = tc.live.store()
			input := tc.input
			if input == "" {
				input = "brightside"
			}
			out := runEvent(t, SongQueue(d), songRedeemCtx(tc.config, input))
			assertRedeemOutcome(t, tc, store, out)
		})
	}
}

func assertRedeemOutcome(t *testing.T, tc redeemCase, store *fakeSongQueue, out []module.Output) {
	t.Helper()
	preloaded := len(tc.preload)
	switch tc.want {
	case redeemQueued:
		assert.Len(t, store.up, preloaded+1)
		require.Len(t, out, 2)
		assert.Equal(t, outgress.RedemptionFulfilled, out[1].Status)
		if tc.reply != "" {
			assert.Equal(t, tc.reply, out[0].Text)
		}
	case redeemRefunded:
		assert.Len(t, store.up, preloaded)
		assertRefund(t, out)
	default:
		assert.Empty(t, out)
		assert.Empty(t, store.up)
	}
}

func TestSongRedeemRefundMentionsViewerOnce(t *testing.T) {
	config := songRedeemConfig("rw-sr", `,"allowOffline":true`)
	failing := func(endpoint string, failure error) func() engine.GossipCaller {
		return func() engine.GossipCaller {
			return songQueueFailureGossip(songQueueFailureCase{endpoint: endpoint}, failure)
		}
	}
	gossips := map[string]func() engine.GossipCaller{
		"search transport": failing("search", errors.New("connection reset")),
		"queue transport":  failing("queue", errors.New("connection reset")),
		"no match":         func() engine.GossipCaller { return srSearchGossip() },
		"provider reason":  failing("search", bus.RPCReplyError{Message: "no Spotify connection on file"}),
	}
	for _, locale := range []string{"en", "fr"} {
		for name, gossip := range gossips {
			t.Run(locale+"/"+name, func(t *testing.T) {
				c := songRedeemCtx(config, "brightside")
				c.Locale = locale
				out := runEvent(t, SongQueue(songDeps(&fakeSongQueue{}, gossip())), c)
				text := chatText(t, out)
				assert.Equal(t, 1, strings.Count(text, "@"), text)
				assert.NotContains(t, text, "{")
				assertRefund(t, out)
			})
		}
	}
}
