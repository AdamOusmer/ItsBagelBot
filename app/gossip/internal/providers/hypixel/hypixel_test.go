// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package hypixel_test

import (
	"net/http"
	"testing"

	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers/hypixel"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	bareUUID = "deadbeefdeadbeefdeadbeefdeadbeef"
	mojangID = `{"id":"` + bareUUID + `","name":"Techno"}`
)

func newProvider(t testing.TB, mojang, upstream http.Handler) provider.Provider {
	return hypixel.New(hypixel.Config{
		BaseURL:       providertest.Upstream(t, upstream),
		MojangBaseURL: providertest.Upstream(t, mojang),
		APIKey:        "hypixel-key",
	}, providertest.Deps(providertest.NewMemStore()))
}

const playerBody = `{
	"success": true,
	"player": {
		"displayname": "Techno",
		"achievements": {"bedwars_level": 402},
		"stats": {"Bedwars": {
			"wins_bedwars": 1000, "losses_bedwars": 100,
			"final_kills_bedwars": 5000, "final_deaths_bedwars": 500,
			"beds_broken_bedwars": 2000
		}}
	}
}`

func TestStatsResolvesViaMojangThenHypixel(t *testing.T) {
	var gotKey, gotUUID string
	p := newProvider(t,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/users/profiles/minecraft/Techno", r.URL.Path)
			_, _ = w.Write([]byte(mojangID))
		}),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/v2/player", r.URL.Path)
			gotUUID = r.URL.Query().Get("uuid")
			gotKey = r.Header.Get("API-Key")
			_, _ = w.Write([]byte(playerBody))
		}))

	reply := providertest.Call[gossiprpc.HypixelStatsReply](t, p, "stats", gossiprpc.Request{Account: "Techno"})
	require.Empty(t, reply.Error)
	assert.Equal(t, "hypixel-key", gotKey)
	assert.Equal(t, bareUUID, gotUUID)
	assert.Equal(t, "Techno", reply.Player)
	assert.Equal(t, int64(402), reply.Stars)
	assert.Equal(t, int64(1000), reply.Wins)
	assert.Equal(t, int64(500), reply.FinalDeaths)
}

func TestStatsSkipsMojangForUUIDAccounts(t *testing.T) {
	for _, tc := range []struct {
		name            string
		account         string
		mojang          []providertest.Reply
		wantMojangCalls int
	}{
		{"a dashed uuid is used as is", "deadbeef-dead-beef-dead-beefdeadbeef", nil, 0},
		{"an undashed uuid is used as is", bareUUID, nil, 0},
		{"a short hex string is still a name", "deadbeef", []providertest.Reply{{Body: mojangID}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mojang := providertest.NewSequence(t, tc.mojang...)
			p := newProvider(t, mojang, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, bareUUID, r.URL.Query().Get("uuid"))
				_, _ = w.Write([]byte(playerBody))
			}))

			reply := providertest.Call[gossiprpc.HypixelStatsReply](t, p, "stats", gossiprpc.Request{Account: tc.account})
			assert.Equal(t, int64(402), reply.Stars)
			assert.Equal(t, tc.wantMojangCalls, mojang.Hits())
		})
	}
}

func TestStatsFailuresAreFriendlyAndCachedOnlyWhenTheyAreFacts(t *testing.T) {
	missingProfile := providertest.Reply{Status: http.StatusNotFound, Body: `{"errorMessage":"Couldn't find any profile"}`}
	unknownPlayer := providertest.Reply{Body: `{"success": true, "player": null}`}
	badKey := providertest.Reply{Status: http.StatusForbidden, Body: `{"success":false,"cause":"Invalid API key"}`}
	throttled := providertest.Reply{Status: http.StatusTooManyRequests, Body: `{"error":"TooManyRequestsException"}`}
	resolved := providertest.Reply{Body: mojangID}
	played := providertest.Reply{Body: playerBody}
	const busy = "stats provider is rate limiting us, try again in a minute"

	for _, tc := range []struct {
		name            string
		account         string
		mojang          []providertest.Reply
		hypixel         []providertest.Reply
		wantErrors      [2]string
		wantMojangHits  int
		wantHypixelHits int
	}{
		{"rejects a missing account before any upstream call", "", nil, nil,
			[2]string{"missing account", "missing account"}, 0, 0},
		{"stops at Mojang and caches a name that does not resolve", "NoSuchName123", []providertest.Reply{missingProfile}, nil,
			[2]string{"player not found", "player not found"}, 1, 0},
		{"serves an unknown player from the negative cache", "Ghosty", []providertest.Reply{resolved}, []providertest.Reply{unknownPlayer},
			[2]string{"player not found", "player not found"}, 1, 1},
		{"retries after a refusal instead of caching it", "Techno", []providertest.Reply{resolved}, []providertest.Reply{badKey, played},
			[2]string{"stats lookup not permitted right now", ""}, 1, 2},
		{"TestStatsMojangRateLimitedPinsBriefly", "Techno", []providertest.Reply{throttled}, nil,
			[2]string{busy, busy}, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mojang, upstream := providertest.NewSequence(t, tc.mojang...), providertest.NewSequence(t, tc.hypixel...)
			p := newProvider(t, mojang, upstream)

			for i, want := range tc.wantErrors {
				reply := providertest.Call[gossiprpc.HypixelStatsReply](t, p, "stats", gossiprpc.Request{Account: tc.account})
				assert.Equal(t, want, reply.Error, "call %d", i+1)
			}
			assert.Equal(t, tc.wantMojangHits, mojang.Hits(), "mojang hits")
			assert.Equal(t, tc.wantHypixelHits, upstream.Hits(), "hypixel hits")
		})
	}
}

func TestUUIDEndpointResolvesOnlyThroughMojang(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account string
		mojang  []providertest.Reply
		want    gossiprpc.HypixelUUIDReply
	}{
		{"resolves a name through Mojang", "Techno", []providertest.Reply{{Body: mojangID}},
			gossiprpc.HypixelUUIDReply{UUID: bareUUID, Player: "Techno"}},
		{"returns a uuid account without any upstream call", "deadbeef-dead-beef-dead-beefdeadbeef", nil,
			gossiprpc.HypixelUUIDReply{UUID: bareUUID, Player: "deadbeef-dead-beef-dead-beefdeadbeef"}},
		{"reports a name Mojang does not know", "ghost", []providertest.Reply{{Status: http.StatusNotFound, Body: `{"errorMessage":"Couldn't find any profile"}`}},
			gossiprpc.HypixelUUIDReply{Player: "ghost", Error: "player not found"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProvider(t, providertest.NewSequence(t, tc.mojang...), providertest.Forbid(t))

			assert.Equal(t, tc.want, providertest.Call[gossiprpc.HypixelUUIDReply](t, p, "uuid", gossiprpc.Request{Account: tc.account}))
		})
	}
}

func TestProviderWithoutAPIKeyOnlyResolvesUUIDs(t *testing.T) {
	p := hypixel.New(hypixel.Config{}, providertest.Deps(providertest.NewMemStore()))

	var names []string
	for _, ep := range p.Endpoints() {
		names = append(names, ep.Name)
	}
	assert.Contains(t, names, "uuid")
	assert.NotContains(t, names, "stats")
}
