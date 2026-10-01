// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// Match GossipRPC's production contract: provider errors return through err
// without populating the typed reply. Other endpoints still succeed normally.
type songQueueErrorGossip struct {
	*fakeGossip
	endpoint string
	err      error
}

func (g songQueueErrorGossip) Call(ctx context.Context, route engine.GossipRoute, req gossiprpc.Request, out any) error {
	if route.Endpoint == g.endpoint {
		return g.err
	}
	return g.fakeGossip.Call(ctx, route, req, out)
}

type songQueueFailureCase struct {
	endpoint string
	command  string
	reason   string
}

var songQueueFailureCases = []songQueueFailureCase{
	{"search", "sr", "no Spotify connection on file"},
	{"queue", "sr", "no active Spotify device, start playing something first"},
	{"next", "skip", "Spotify Premium is required for queue control"},
	{"nowplaying", "song", "your Spotify connection needs to be set up again"},
}

func songQueueFailureReply(t *testing.T, tc songQueueFailureCase, g engine.GossipCaller) string {
	t.Helper()
	return songQueueFailureReplyLogged(t, tc, songQueueFailureRun{gossip: g, log: zap.NewNop()})
}

type songQueueFailureRun struct {
	gossip engine.GossipCaller
	log    *zap.Logger
	locale string
}

func songQueueFailureReplyLogged(t *testing.T, tc songQueueFailureCase, run songQueueFailureRun) string {
	t.Helper()
	pending := []engine.SongEntry{{TrackID: "t0", Title: "Existing request", RequesterID: "7"}}
	store := &fakeSongQueue{up: append([]engine.SongEntry(nil), pending...)}
	deps := songDeps(store, run.gossip)
	deps.Log = run.log
	m := SongQueue(deps)
	c := songCtx("42", "Cardistry", "moderator")
	c.Locale = run.locale
	var text string
	if tc.command == "sr" {
		text = chatText(t, runSR(t, m, c, "Human"))
	} else {
		text = chatText(t, runSongCmd(t, m, tc.command, c))
	}
	assert.Equal(t, pending, store.up, "a refusal must preserve the pending queue")
	assert.Nil(t, store.current, "a failed skip must not advance playback")
	return text
}

func songQueueFailureGossip(tc songQueueFailureCase, failure error) songQueueErrorGossip {
	return songQueueErrorGossip{
		fakeGossip: srSearchGossip(srTrack("t1", "Human", "The Killers")),
		endpoint:   tc.endpoint,
		err:        failure,
	}
}

func TestSongQueueRPCFailuresReplyWithProviderReason(t *testing.T) {
	for _, tc := range songQueueFailureCases {
		t.Run(tc.endpoint, func(t *testing.T) {
			failure := fmt.Errorf("gossip call: %w", bus.RPCReplyError{
				Subject: "bagel.rpc.gossip.spotify." + tc.endpoint,
				Message: tc.reason,
			})
			text := songQueueFailureReply(t, tc, songQueueFailureGossip(tc, failure))
			assert.Equal(t, tc.reason, text)
			assert.NotContains(t, text, "bagel.rpc")
		})
	}
}

func TestSongQueueTransportErrorsStayGeneric(t *testing.T) {
	for _, tc := range songQueueFailureCases {
		t.Run(tc.endpoint, func(t *testing.T) {
			failure := errors.New("connection reset with internal connection details")
			text := songQueueFailureReply(t, tc, songQueueFailureGossip(tc, failure))
			assert.Contains(t, text, "music lookup is down")
			assert.NotContains(t, text, "internal connection details")
		})
	}
}

func checkSongQueueSensitiveError(t *testing.T, tc songQueueFailureCase, sensitive string) {
	t.Helper()
	failure := bus.RPCReplyError{Subject: "bagel.rpc.gossip.spotify." + tc.endpoint, Message: sensitive}
	base := srSearchGossip(srTrack("t1", "Human", "The Killers"))
	base.replies["spotify."+tc.endpoint] = map[string]string{"error": sensitive}
	for _, g := range []engine.GossipCaller{songQueueFailureGossip(tc, failure), base} {
		text := songQueueFailureReply(t, tc, g)
		assert.Contains(t, text, "music lookup is down")
		assert.NotContains(t, text, sensitive)
		assert.NotContains(t, text, "private-")
		assert.NotContains(t, text, "bagel.rpc")
		assert.NotContains(t, text, "internal.example")
	}
}

func TestSongQueueErrorsDoNotExposeInternalDetails(t *testing.T) {
	for _, tc := range songQueueFailureCases {
		t.Run(tc.endpoint, func(t *testing.T) {
			for _, sensitive := range []string{
				"client_secret=private-secret refresh_token=private-token",
				"request to https://internal.example/token?code=private-code failed: Authorization: Bearer private-token",
				"no active Spotify device, start playing something first; client_secret=private-secret",
			} {
				checkSongQueueSensitiveError(t, tc, sensitive)
			}
		})
	}
}

func TestSongQueueHiddenSpotifyFailuresAreLogged(t *testing.T) {
	const sensitive = "client_secret=private-secret"
	for _, tc := range songQueueFailureCases {
		t.Run(tc.endpoint, func(t *testing.T) {
			failure := bus.RPCReplyError{Subject: "bagel.rpc.gossip.spotify." + tc.endpoint, Message: sensitive}
			base := srSearchGossip(srTrack("t1", "Human", "The Killers"))
			base.replies["spotify."+tc.endpoint] = map[string]string{"error": sensitive}
			for _, g := range []engine.GossipCaller{songQueueFailureGossip(tc, failure), base} {
				core, logs := observer.New(zap.WarnLevel)
				text := songQueueFailureReplyLogged(t, tc, songQueueFailureRun{gossip: g, log: zap.New(core)})
				assert.Contains(t, text, "music lookup is down")
				assert.True(t, loggedRefusal(logs, sensitive), "a hidden failure must be logged with the channel")
			}
		})
	}
}

func loggedRefusal(logs *observer.ObservedLogs, reason string) bool {
	for _, entry := range logs.FilterField(zap.String("reason", reason)).All() {
		if entry.ContextMap()["broadcaster_id"] == uint64(100) {
			return true
		}
	}
	return false
}

func TestSongQueueSpotifyRefusalWarnIsThrottledPerMinute(t *testing.T) {
	const reason = "mystery refusal"
	gossip := nowPlayingGossip(gossiprpc.SpotifyNowPlayingReply{Error: reason})
	core, logs := observer.New(zap.WarnLevel)
	deps := songDeps(&fakeSongQueue{}, gossip)
	deps.Log = zap.New(core)
	m := SongQueue(deps)
	c := songCtx("42", "alice")
	for i := 0; i < 3; i++ {
		runSong(t, m, c)
	}
	assert.Len(t, logs.FilterField(zap.String("reason", reason)).All(), 1,
		"a refused reason must warn at most once per broadcaster per minute")
}

func TestSongQueueGenericRepliesExpandPlaceholders(t *testing.T) {
	transport := errors.New("connection reset")
	for _, locale := range []string{"en", "fr"} {
		for _, tc := range songQueueFailureCases {
			t.Run(locale+"/"+tc.endpoint, func(t *testing.T) {
				text := songQueueFailureReplyLogged(t, tc, songQueueFailureRun{gossip: songQueueFailureGossip(tc, transport), log: zap.NewNop(), locale: locale})
				assert.NotContains(t, text, "{")
				assert.Contains(t, text, "@Cardistry")
			})
		}
		t.Run(locale+"/no match", func(t *testing.T) {
			c := songCtx("42", "Cardistry")
			c.Locale = locale
			text := chatText(t, runSR(t, SongQueue(songDeps(&fakeSongQueue{}, srSearchGossip())), c, "zzzz"))
			assert.NotContains(t, text, "{")
			assert.Contains(t, text, "@Cardistry")
		})
	}
}

func TestSongRedeemRefundMentionsViewerOnce(t *testing.T) {
	const config = `{"redeem":{"enabled":true,"rewardId":"rw-sr","onRedeem":"fulfill","allowOffline":true}}`
	gossips := map[string]func() engine.GossipCaller{
		"search transport": func() engine.GossipCaller {
			return songQueueFailureGossip(songQueueFailureCase{endpoint: "search"}, errors.New("connection reset"))
		},
		"queue transport": func() engine.GossipCaller {
			return songQueueFailureGossip(songQueueFailureCase{endpoint: "queue"}, errors.New("connection reset"))
		},
		"no match": func() engine.GossipCaller { return srSearchGossip() },
		"provider reason": func() engine.GossipCaller {
			return songQueueFailureGossip(songQueueFailureCase{endpoint: "search"}, bus.RPCReplyError{Message: "no Spotify connection on file"})
		},
	}
	for _, locale := range []string{"en", "fr"} {
		for name, gossip := range gossips {
			t.Run(locale+"/"+name, func(t *testing.T) {
				c := songRedeemCtx(config)
				c.Locale = locale
				var col collector
				require.NoError(t, SongQueue(songDeps(&fakeSongQueue{}, gossip())).Events[redemptionAddType](context.Background(), c, col.emit))
				text := chatText(t, col.out)
				assert.Equal(t, 1, strings.Count(text, "@"), text)
				assert.NotContains(t, text, "{")
				assertRefund(t, col.out)
			})
		}
	}
}
