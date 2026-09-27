// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
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

func TestSongQueueRPCFailuresReplyWithProviderReason(t *testing.T) {
	for _, tc := range []struct {
		endpoint string
		command  string
		reason   string
	}{
		{"search", "sr", "no Spotify connection on file"},
		{"queue", "sr", "no active Spotify device, start playing something first"},
		{"next", "skip", "Spotify Premium is required for queue control"},
		{"nowplaying", "song", "your Spotify connection needs to be set up again"},
	} {
		for _, transport := range []bool{false, true} {
			name := tc.endpoint + "/provider"
			var failure error = fmt.Errorf("gossip call: %w", bus.RPCReplyError{
				Subject: "bagel.rpc.gossip.spotify." + tc.endpoint,
				Message: tc.reason,
			})
			if transport {
				name = tc.endpoint + "/transport"
				failure = errors.New("connection reset with internal connection details")
			}
			t.Run(name, func(t *testing.T) {
				store := &fakeSongQueue{}
				if tc.endpoint == "next" {
					store.up = []engine.SongEntry{{TrackID: "t1", Title: "Human"}}
				}
				pending := append([]engine.SongEntry(nil), store.up...)
				g := songQueueErrorGossip{
					fakeGossip: srSearchGossip(srTrack("t1", "Human", "The Killers")),
					endpoint:   tc.endpoint,
					err:        failure,
				}
				m := SongQueue(songDeps(store, g))
				c := songCtx("42", "Cardistry", "moderator")
				var text string
				if tc.command == "sr" {
					text = chatText(t, runSR(t, m, c, "Human"))
				} else {
					text = chatText(t, runSongCmd(t, m, tc.command, c))
				}
				if transport {
					assert.Contains(t, text, "music lookup is down")
					assert.NotContains(t, text, "internal connection details")
				} else {
					assert.Equal(t, tc.reason, text)
					assert.NotContains(t, text, "bagel.rpc")
				}
				assert.Len(t, store.up, len(pending), "a refusal must preserve the pending queue")
				if len(pending) > 0 {
					assert.Equal(t, pending, store.up)
				}
				assert.Nil(t, store.current, "a failed skip must not advance playback")
			})
		}
	}
}

func TestSongQueueErrorsDoNotExposeInternalDetails(t *testing.T) {
	for _, tc := range []struct {
		endpoint string
		command  string
	}{
		{"search", "sr"},
		{"queue", "sr"},
		{"next", "skip"},
		{"nowplaying", "song"},
	} {
		for _, shape := range []string{"rpc", "reply"} {
			for _, sensitive := range []string{
				"client_secret=private-secret refresh_token=private-token",
				"request to https://internal.example/token?code=private-code failed: Authorization: Bearer private-token",
				"no active Spotify device, start playing something first; client_secret=private-secret",
			} {
				t.Run(tc.endpoint+"/"+shape+"/"+sensitive, func(t *testing.T) {
					base := srSearchGossip(srTrack("t1", "Human", "The Killers"))
					var g engine.GossipCaller
					if shape == "rpc" {
						g = songQueueErrorGossip{fakeGossip: base, endpoint: tc.endpoint, err: bus.RPCReplyError{
							Subject: "bagel.rpc.gossip.spotify." + tc.endpoint, Message: sensitive,
						}}
					} else {
						base.replies["spotify."+tc.endpoint] = map[string]string{"error": sensitive}
						g = base
					}
					store := &fakeSongQueue{}
					m := SongQueue(songDeps(store, g))
					c := songCtx("42", "Cardistry", "moderator")
					var text string
					if tc.command == "sr" {
						text = chatText(t, runSR(t, m, c, "Human"))
					} else {
						text = chatText(t, runSongCmd(t, m, tc.command, c))
					}
					assert.Contains(t, text, "music lookup is down")
					assert.NotContains(t, text, sensitive)
					assert.NotContains(t, text, "private-")
					assert.NotContains(t, text, "bagel.rpc")
					assert.NotContains(t, text, "internal.example")
					assert.Empty(t, store.up)
				})
			}
		}
	}
}
