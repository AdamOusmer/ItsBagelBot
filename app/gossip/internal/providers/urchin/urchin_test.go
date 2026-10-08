// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package urchin_test

import (
	"net/http"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers/urchin"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProvider(t testing.TB, handler http.Handler) provider.Provider {
	return urchin.New(
		urchin.Config{BaseURL: providertest.Upstream(t, handler), APIKey: "test-key", BatchWindow: 15 * time.Millisecond},
		providertest.Deps(providertest.NewMemStore()))
}

const sessionBody = `{
	"uuid": "abc",
	"displayname": "§7Techno",
	"from": 1720000000000,
	"from_readable": "today",
	"delta": {
		"stats": {"Bedwars": {
			"wins_bedwars": 5,
			"losses_bedwars": 2,
			"final_kills_bedwars": 21,
			"final_deaths_bedwars": 3,
			"beds_broken_bedwars": 9,
			"games_played_bedwars": 8,
			"Experience": 4870.5
		}},
		"achievements": {"bedwars_level": 1}
	}
}`

func TestDailySessionIsParsedFromTheAuthenticatedRequest(t *testing.T) {
	var gotKey, gotPlayer string
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/player/sessions/daily", r.URL.Path)
		gotKey = r.Header.Get("X-API-Key")
		gotPlayer = r.URL.Query().Get("player")
		_, _ = w.Write([]byte(sessionBody))
	}))

	reply := providertest.Call[gossiprpc.UrchinSessionReply](t, p, "daily", gossiprpc.Request{Account: "Techno"})

	assert.Equal(t, "test-key", gotKey)
	assert.Equal(t, "Techno", gotPlayer)
	assert.Equal(t, gossiprpc.UrchinSessionReply{
		Player: "Techno", SinceUnix: 1720000000, Wins: 5, Losses: 2, FinalKills: 21,
		FinalDeaths: 3, BedsBroken: 9, GamesPlayed: 8, Levels: 1,
	}, reply)
}

func TestSessionDeltasThatAreObjectsAreSkipped(t *testing.T) {
	p := newProvider(t, providertest.Respond(http.StatusOK,
		`{"uuid":"abc","from":0,"from_readable":"x","delta":{"stats":{"Bedwars":{"wins_bedwars":{"old":null,"new":5000}}}}}`))

	reply := providertest.Call[gossiprpc.UrchinSessionReply](t, p, "weekly", gossiprpc.Request{Account: "x"})

	assert.Equal(t, gossiprpc.UrchinSessionReply{Player: "x"}, reply)
}

func TestLookupsAreCachedPerPlayerAndFailuresAreAnsweredInChat(t *testing.T) {
	for _, tc := range []struct {
		name       string
		account    string
		upstream   []providertest.Reply
		wantErrors [2]string
		wantHits   int
	}{
		{"rejects a missing account before any upstream call", "", nil,
			[2]string{"missing account", "missing account"}, 0},
		{"serves a repeat lookup from the cache whatever the spelling", "Techno", []providertest.Reply{{Body: sessionBody}},
			[2]string{"", ""}, 1},
		{"serves an unknown player from the negative cache", "ghost", []providertest.Reply{{Status: http.StatusNotFound, Body: `{"error":"player not found"}`}},
			[2]string{"player not found", "player not found"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := providertest.NewSequence(t, tc.upstream...)
			p := newProvider(t, upstream)

			for i, want := range tc.wantErrors {
				account := tc.account
				if i == 1 && account != "" {
					account = "  " + account + "  "
				}
				reply := providertest.Call[gossiprpc.UrchinSessionReply](t, p, "daily", gossiprpc.Request{Account: account})
				assert.Equal(t, want, reply.Error, "call %d", i+1)
			}
			assert.Equal(t, tc.wantHits, upstream.Hits())
		})
	}
}

func TestSniperAndTagsRepliesAreShapedFromTheUpstreamPayload(t *testing.T) {
	cubelify := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/player/tags":
			_, _ = w.Write([]byte(`{"uuid":"deadbeef","displayname":"Aim","tags":[]}`))
		case "/v3/cubelify":
			assert.Equal(t, "deadbeef", r.URL.Query().Get("uuid"))
			assert.Equal(t, "test-key", r.URL.Query().Get("key"))
			_, _ = w.Write([]byte(`{"score":{"value":7.5,"mode":"warn"},"tags":[{"icon":"x","color":1,"tooltip":"t"}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}
	tagsBody := `{"uuid":"abc","displayname":"Sus","tags":[
		{"tag_type":"cheater","reason":"bhop","added_by":1,"added_on":0,"hide_username":false},
		{"tag_type":"sniper","reason":"","added_by":1,"added_on":0,"hide_username":false}
	]}`

	providertest.RunCases(t, func(t testing.TB, h http.Handler) provider.Provider { return newProvider(t, h) }, "sniper",
		[]providertest.Case[gossiprpc.UrchinSniperReply]{
			{Name: "resolves the uuid then scores the player", Req: gossiprpc.Request{Account: "Aim"}, Upstream: cubelify,
				Want: gossiprpc.UrchinSniperReply{Player: "Aim", Score: 7.5, Mode: "warn", TagCount: 1}},
			{Name: "reports a name that resolves to no uuid as not found", Req: gossiprpc.Request{Account: "ghost"},
				Upstream: providertest.Respond(http.StatusOK, `{"uuid":"","displayname":null,"tags":[]}`),
				Want:     gossiprpc.UrchinSniperReply{Player: "ghost", Error: "player not found"}},
		})
	providertest.RunCases(t, func(t testing.TB, h http.Handler) provider.Provider { return newProvider(t, h) }, "tags",
		[]providertest.Case[gossiprpc.UrchinTagsReply]{
			{Name: "lists the tags a player carries", Req: gossiprpc.Request{Account: "Sus"},
				Upstream: providertest.Respond(http.StatusOK, tagsBody),
				Want: gossiprpc.UrchinTagsReply{Player: "Sus", Tags: []gossiprpc.UrchinTag{
					{Type: "cheater", Reason: "bhop"}, {Type: "sniper"},
				}}},
		})
}

func TestTagsAndSniperShareOneUpstreamFetch(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"tags then sniper", "tags", "sniper"},
		{"sniper then tags", "sniper", "tags"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tagsHits, cubelifyHits int
			p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v3/player/tags":
					tagsHits++
					_, _ = w.Write([]byte(`{"uuid":"deadbeef","displayname":"Aim","tags":[]}`))
				case "/v3/cubelify":
					cubelifyHits++
					_, _ = w.Write([]byte(`{"score":{"value":3,"mode":"ok"},"tags":[]}`))
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			for _, endpoint := range []string{tc.first, tc.second} {
				_ = providertest.Endpoint(t, p, endpoint)(t.Context(), gossiprpc.Request{Account: "Aim"})
			}

			assert.Equal(t, 1, tagsHits, "the /v3/player/tags fetch must be shared, not repeated")
			assert.Equal(t, 1, cubelifyHits)
		})
	}
}

type playerNamed struct {
	Player string `json:"player"`
}

func TestPlayerNamePrefersCallerSpellingOverStaleAPIName(t *testing.T) {
	const uuid = "3bf23977c78843cbb55d96b87902d822"
	cases := []struct{ name, ep, account, body, want string }{
		{
			name:    "username keeps the caller's spelling",
			ep:      "tags",
			account: "Ofxs",
			body:    `{"uuid":"` + uuid + `","displayname":"Sho__YiYuan","tags":[]}`,
			want:    "Ofxs",
		},
		{
			name:    "uuid falls back to the API display name",
			ep:      "daily",
			account: uuid,
			body:    `{"uuid":"` + uuid + `","displayname":"Sho__YiYuan","from":0,"delta":{}}`,
			want:    "Sho__YiYuan",
		},
		{
			name:    "uuid display names lose their minecraft colour codes",
			ep:      "daily",
			account: uuid,
			body:    `{"uuid":"` + uuid + `","displayname":"§7§lSho__YiYuan","from":0,"delta":{}}`,
			want:    "Sho__YiYuan",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newProvider(t, providertest.Respond(http.StatusOK, tc.body))
			reply := providertest.Call[playerNamed](t, p, tc.ep, gossiprpc.Request{Account: tc.account})
			assert.Equal(t, tc.want, reply.Player)
		})
	}
}
