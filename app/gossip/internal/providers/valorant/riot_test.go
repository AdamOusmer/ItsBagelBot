// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valorant

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const riotContentBody = `{"acts":[
  {"id":"old-act","type":"act","isActive":false},
  {"id":"episode-9","type":"episode","isActive":true},
  {"id":"act-now","type":"act","isActive":true}
]}`

const riotBoardBody = `{"actId":"act-now","players":[
  {"leaderboardRank":2,"gameName":"","tagLine":"","rankedRating":880,"numberOfWins":40,"competitiveTier":27},
  {"leaderboardRank":1,"gameName":"One","tagLine":"1111","rankedRating":910,"numberOfWins":42,"competitiveTier":27}
]}`

func riotMatchBody(started time.Time, queue string, tier int) string {
	return fmt.Sprintf(`{
  "matchInfo":{"mapId":"/Game/Maps/Ascent/Ascent","gameStartMillis":%d,"isCompleted":true,"queueId":%q},
  "players":[
    {"puuid":"enc-self","gameName":"Frosty","tagLine":"EUW1","teamId":"Red","characterId":"ADD6443A-41BD-E414-F6AD-E58D267F4E95","competitiveTier":%d,
     "stats":{"score":4563,"roundsPlayed":24,"kills":24,"deaths":15,"assists":7}},
    {"puuid":"enc-other","gameName":"Rival","tagLine":"2222","teamId":"Blue","characterId":"9f0d8ba9-4140-b941-57d3-a7ad57c6b417","competitiveTier":12,
     "stats":{"score":2100,"roundsPlayed":24,"kills":10,"deaths":20,"assists":3}}
  ],
  "teams":[
    {"teamId":"Red","won":true,"roundsPlayed":24,"roundsWon":14},
    {"teamId":"Blue","won":false,"roundsPlayed":24,"roundsWon":10}
  ]
}`, started.UnixMilli(), queue, tier)
}

func riotUpstream(t testing.TB, started time.Time) http.HandlerFunc {
	routes := map[string]string{
		"/riot/account/v1/accounts/me":                                 `{"puuid":"enc-self","gameName":"Frosty","tagLine":"EUW1"}`,
		"/riot/account/v1/active-shards/by-game/val/by-puuid/enc-self": `{"puuid":"enc-self","game":"val","activeShard":"EU"}`,
		"/val/match/v1/matchlists/by-puuid/enc-self": fmt.Sprintf(`{"puuid":"enc-self","history":[
  {"matchId":"m-old","gameStartTimeMillis":%d,"queueId":"competitive"},
  {"matchId":"m-dm","gameStartTimeMillis":%d,"queueId":"deathmatch"},
  {"matchId":"m-new","gameStartTimeMillis":%d,"queueId":"competitive"}
]}`, started.Add(-time.Hour).UnixMilli(), started.Add(time.Minute).UnixMilli(), started.UnixMilli()),
		"/val/match/v1/matches/m-new":                riotMatchBody(started, "competitive", 17),
		"/val/match/v1/matches/m-old":                riotMatchBody(started.Add(-time.Hour), "competitive", 16),
		"/val/content/v1/contents":                   riotContentBody,
		"/val/ranked/v1/leaderboards/by-act/act-now": riotBoardBody,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Riot-Token") != "riot-key" {
			t.Errorf("missing riot token on %s", r.URL.Path)
		}
		if body, ok := routes[r.URL.Path]; ok {
			fmt.Fprint(w, body)
			return
		}
		t.Errorf("unexpected riot path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func valorantAssets(t testing.TB) http.HandlerFunc {
	return henrik(t, map[string]string{
		"/v1/maps":             `{"data":[{"uuid":"7eaecc1b","displayName":"Ascent","mapUrl":"/Game/Maps/Ascent/Ascent"}]}`,
		"/v1/agents":           `{"data":[{"uuid":"add6443a-41bd-e414-f6ad-e58d267f4e95","displayName":"Jett"}]}`,
		"/v1/competitivetiers": `{"data":[{"uuid":"old","tiers":[{"tier":17,"tierName":"OLD NAME"}]},{"uuid":"now","tiers":[{"tier":17,"tierName":"DIAMOND 2"}]}]}`,
	})
}

func newRiotProvider(t testing.TB, henrikKey string, riot http.Handler) provider.Provider {
	return newSignOnProvider(t, henrikKey, riot, providertest.Forbid(t))
}

func newSignOnProvider(t testing.TB, henrikKey string, riot, auth http.Handler) provider.Provider {
	d := providertest.Deps(providertest.NewMemStore())
	d.RiotLinks = fakeLinks{linkedChannel: {Puuid: "enc-self", GameName: "Frosty", TagLine: "EUW1", Shard: "eu"}}
	return New(Config{
		BaseURL:        providertest.Upstream(t, providertest.Forbid(t)),
		ContentBaseURL: providertest.Upstream(t, valorantAssets(t)),
		APIKey:         henrikKey,
		Riot: RiotConfig{
			BaseURL: providertest.Upstream(t, riot), APIKey: "riot-key",
			SignOn: SignOnConfig{AuthBaseURL: providertest.Upstream(t, auth), ClientID: "rso-id", ClientSecret: "rso-secret"},
		},
	}, d)
}

func TestOfficialRiotAnswersWhereItHasTheData(t *testing.T) {
	started := time.Now().Add(-10 * time.Minute)
	linked := frosty("", "")
	game := matchEntry{Map: "Ascent", Agent: "Jett", Result: "win", Kills: 24, Deaths: 15, Assists: 7, ACS: 190.1}
	for _, tc := range []struct {
		name      string
		henrikKey string
		endpoint  string
		req       gossiprpc.Request
		want      any
	}{
		{"matches skip other queues, newest first, even with a stats key", "val-key", "matches", linked,
			matchesReply{Player: "Frosty#EUW1", Region: "eu", Matches: []matchEntry{game, game}}},
		{"account reports the encrypted puuid and active shard", "", "account", linked,
			accountReply{Player: "Frosty#EUW1", Puuid: "enc-self", Region: "eu"}},
		{"rank without a stats key reads the tier of the latest ranked match", "", "rank", linked,
			rankReply{Player: "Frosty#EUW1", Region: "eu", Tier: "Diamond 2"}},
		{"leaderboard uses the active act and names hidden players", "", "leaderboard", gossiprpc.Request{Region: "eu"},
			leaderboardReply{Board: "eu/pc", Entries: []lbEntry{
				{Rank: 1, Player: "One#1111", Tier: 27, RR: 910, Wins: 42},
				{Rank: 2, Player: "Secret Agent", Tier: 27, RR: 880, Wins: 40},
			}}},
		{"record has no official source", "", "record", linked,
			recordReply{Error: "valorant record is not available right now"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newRiotProvider(t, tc.henrikKey, riotUpstream(t, started))

			res := providertest.Endpoint(t, p, tc.endpoint)(t.Context(), tc.req)

			assert.Equal(t, tc.want, decodeAs(t, res, tc.want), "ago is checked apart")
		})
	}
}

func decodeAs(t testing.TB, res, like any) any {
	switch like.(type) {
	case matchesReply:
		reply := providertest.Decode[matchesReply](t, res)
		for i := range reply.Matches {
			reply.Matches[i].AgoSeconds = 0
		}
		return reply
	case accountReply:
		return providertest.Decode[accountReply](t, res)
	case rankReply:
		return providertest.Decode[rankReply](t, res)
	case leaderboardReply:
		return providertest.Decode[leaderboardReply](t, res)
	default:
		return providertest.Decode[recordReply](t, res)
	}
}

func TestSignInExchangeReturnsTheSignedInAccount(t *testing.T) {
	auth := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		user, pass, _ := r.BasicAuth()
		assert.Equal(t, []string{"/token", "rso-id", "rso-secret", "authorization_code", "code-1", "https://console.test/riot/callback"},
			[]string{r.URL.Path, user, pass, r.PostForm.Get("grant_type"), r.PostForm.Get("code"), r.PostForm.Get("redirect_uri")})
		fmt.Fprint(w, `{"access_token":"player-token","token_type":"Bearer"}`)
	})
	riot := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/riot/account/v1/accounts/me" {
			assert.Equal(t, "Bearer player-token", r.Header.Get("Authorization"))
		}
		riotUpstream(t, time.Now())(w, r)
	})
	p := newSignOnProvider(t, "", riot, auth)

	res := providertest.Endpoint(t, p, "link")(t.Context(), gossiprpc.Request{
		ChannelID: linkedChannel, Code: "code-1", RedirectURI: "https://console.test/riot/callback",
	})

	assert.Equal(t, gossiprpc.ValorantLinkReply{Puuid: "enc-self", GameName: "Frosty", TagLine: "EUW1", Shard: "eu"},
		providertest.Decode[gossiprpc.ValorantLinkReply](t, res))
}
