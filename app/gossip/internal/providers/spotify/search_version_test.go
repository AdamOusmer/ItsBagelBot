// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
)

var (
	helloOriginal     = searchTrack("hello", "Hello", "Adele")
	levelsEdit        = searchTrack("radio", "Levels - Radio Edit", "Avicii")
	helloLive         = searchTrack("live", "Hello (Live at the BBC)", "Adele")
	helloLiveDash     = searchTrack("live", "Hello - Live at the BBC", "Adele")
	helloRemix        = searchTrack("remix", "Hello (Remix)", "Adele")
	despacitoOriginal = searchTrack("despacito", "Despacito", "Luis Fonsi", "Daddy Yankee")
	despacitoRemix    = searchTrack("remix", "Despacito - Remix", "Luis Fonsi", "Daddy Yankee", "Justin Bieber")
)

const (
	helloRetry       = `track:"hello" artist:"adele"`
	bieberRetry      = `track:"despacito" artist:"justin bieber"`
	daddyYankeeRetry = `track:"despacito" artist:"daddy yankee"`
	levelsRetry      = `track:"levels" artist:"avicii"`
	despacitoRetry   = `track:"despacito" artist:"luis fonsi"`
)

var recordingVersionCases = []struct {
	name, query string
	window      []gossiprpc.SpotifyTrack
	byQuery     map[string][]gossiprpc.SpotifyTrack
	want        chatSearchOutcome
}{
	{"live before artist", "hello live adele", []gossiprpc.SpotifyTrack{helloLive}, nil,
		chatSearchOutcome{ids: []string{"live"}, queries: []string{"hello live adele"}}},
	{"live after artist", "hello adele live", []gossiprpc.SpotifyTrack{helloLive}, nil,
		chatSearchOutcome{ids: []string{"live"}, queries: []string{"hello adele live"}}},
	{"explicit remix", "despacito remix luis fonsi", []gossiprpc.SpotifyTrack{despacitoRemix}, nil,
		chatSearchOutcome{ids: []string{"remix"}, queries: []string{"despacito remix luis fonsi"}}},
	{"sped up", "love story sped up taylor swift", []gossiprpc.SpotifyTrack{
		searchTrack("sped-up", "Love Story - Sped Up", "Taylor Swift"),
	}, nil, chatSearchOutcome{ids: []string{"sped-up"}, queries: []string{"love story sped up taylor swift"}}},
	{"duration", "all too well 10 minute version taylor swift", []gossiprpc.SpotifyTrack{
		searchTrack("ten-minute", "All Too Well (10 Minute Version) (Taylor's Version) (From The Vault)", "Taylor Swift"),
	}, nil, chatSearchOutcome{ids: []string{"ten-minute"}, queries: []string{"all too well 10 minute version taylor swift"}}},
	{"unrequested extra version", "hello live adele", []gossiprpc.SpotifyTrack{
		searchTrack("wrong", "Hello (Live) - Remix", "Adele"), helloLive,
	}, nil, chatSearchOutcome{ids: []string{"live"}, queries: []string{"hello live adele"}}},
	{"exact title-only version stops immediately", "hello live", []gossiprpc.SpotifyTrack{
		searchTrack("live", "Hello - Live", "Adele"),
	}, nil, chatSearchOutcome{ids: []string{"live"}, queries: []string{"hello live"}}},
	{"artist recovery beats title-only version", "hello live adele", []gossiprpc.SpotifyTrack{
		searchTrack("fallback", "Hello Adele - Live", "Other"), helloOriginal,
	}, map[string][]gossiprpc.SpotifyTrack{`track:"hello live" artist:"adele"`: {helloLive}},
		chatSearchOutcome{ids: []string{"live"}, queries: []string{"hello live adele", `track:"hello live" artist:"adele"`}}},
	{"remix guest", "despacito justin bieber", []gossiprpc.SpotifyTrack{despacitoRemix},
		map[string][]gossiprpc.SpotifyTrack{bieberRetry: {despacitoRemix}},
		chatSearchOutcome{ids: []string{"remix"}, queries: []string{"despacito justin bieber", bieberRetry}}},
	{"guest credited on original", "despacito justin bieber", []gossiprpc.SpotifyTrack{
		despacitoRemix, searchTrack("original", "Despacito", "Justin Bieber"),
	}, nil, chatSearchOutcome{ids: []string{"original"}, queries: []string{"despacito justin bieber"}}},
	{"no live guest exception", "hello guest", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello (Live)", "Adele", "Guest")}, nil,
		chatSearchOutcome{queries: []string{"hello guest", `track:"hello" artist:"guest"`}}},
	{"hello original retry", "hello adele", []gossiprpc.SpotifyTrack{helloLiveDash, helloRemix},
		map[string][]gossiprpc.SpotifyTrack{helloRetry: {helloOriginal}},
		chatSearchOutcome{ids: []string{"hello"}, queries: []string{"hello adele", helloRetry}}},
	{"original retry without an artist", "hello", []gossiprpc.SpotifyTrack{helloLiveDash, helloRemix},
		map[string][]gossiprpc.SpotifyTrack{`track:"hello"`: {helloOriginal}},
		chatSearchOutcome{ids: []string{"hello"}, queries: []string{"hello", `track:"hello"`}}},
	{"despacito original retry", "despacito luis fonsi", []gossiprpc.SpotifyTrack{despacitoRemix},
		map[string][]gossiprpc.SpotifyTrack{despacitoRetry: {despacitoOriginal}},
		chatSearchOutcome{ids: []string{"despacito"}, queries: []string{"despacito luis fonsi", despacitoRetry}}},
	{"original after live", "hello adele", []gossiprpc.SpotifyTrack{helloLiveDash, helloOriginal}, nil,
		chatSearchOutcome{ids: []string{"hello"}, queries: []string{"hello adele"}}},
	{"original after remix", "despacito luis fonsi", []gossiprpc.SpotifyTrack{despacitoRemix, despacitoOriginal}, nil,
		chatSearchOutcome{ids: []string{"despacito"}, queries: []string{"despacito luis fonsi"}}},
	{"live after original", "hello live adele", []gossiprpc.SpotifyTrack{helloOriginal, helloLive}, nil,
		chatSearchOutcome{ids: []string{"live"}, queries: []string{"hello live adele"}}},
	{"requested live retry", "hello adele live", []gossiprpc.SpotifyTrack{helloOriginal},
		map[string][]gossiprpc.SpotifyTrack{`track:"hello live" artist:"adele"`: {helloLive}},
		chatSearchOutcome{ids: []string{"live"}, queries: []string{"hello adele live", `track:"hello live" artist:"adele"`}}},
	{"remix only after retry", "despacito luis fonsi", []gossiprpc.SpotifyTrack{despacitoRemix},
		map[string][]gossiprpc.SpotifyTrack{despacitoRetry: {despacitoRemix}},
		chatSearchOutcome{queries: []string{"despacito luis fonsi", despacitoRetry}}},
	{"live only after retry", "hello adele", []gossiprpc.SpotifyTrack{helloLiveDash},
		map[string][]gossiprpc.SpotifyTrack{helloRetry: {helloLive}},
		chatSearchOutcome{queries: []string{"hello adele", helloRetry}}},
	{"retry finds the original with the remix guest", "despacito daddy yankee", []gossiprpc.SpotifyTrack{despacitoRemix},
		map[string][]gossiprpc.SpotifyTrack{daddyYankeeRetry: {despacitoOriginal}},
		chatSearchOutcome{ids: []string{"despacito"}, queries: []string{"despacito daddy yankee", daddyYankeeRetry}}},
	{"acoustic version qualifier", "hello acoustic adele", []gossiprpc.SpotifyTrack{
		searchTrack("acoustic", "Hello - Acoustic Version", "Adele"),
	}, nil, chatSearchOutcome{ids: []string{"acoustic"}}},
	{"live version qualifier", "hello live adele", []gossiprpc.SpotifyTrack{
		searchTrack("live", "Hello (Live Version)", "Adele"),
	}, nil, chatSearchOutcome{ids: []string{"live"}}},
	{"instrumental version qualifier", "hello instrumental adele", []gossiprpc.SpotifyTrack{
		searchTrack("instrumental", "Hello (Instrumental Version)", "Adele"),
	}, nil, chatSearchOutcome{ids: []string{"instrumental"}}},
	{"remix typo", "despacito remixx luis fonsi", []gossiprpc.SpotifyTrack{despacitoRemix}, nil, chatSearchOutcome{ids: []string{"remix"}}},
	{"acoustic typo", "hello acustic adele", []gossiprpc.SpotifyTrack{
		searchTrack("acoustic", "Hello - Acoustic", "Adele"),
	}, nil, chatSearchOutcome{ids: []string{"acoustic"}}},
	{"short version word typo is rejected", "love story sped uo taylor swift", []gossiprpc.SpotifyTrack{
		searchTrack("sped-up", "Love Story - Sped Up", "Taylor Swift"),
	}, nil, chatSearchOutcome{}},
	{"reverb qualifies slowed", "love story slowed taylor swift", []gossiprpc.SpotifyTrack{
		searchTrack("slowed", "Love Story - Slowed + Reverb", "Taylor Swift"),
	}, nil, chatSearchOutcome{ids: []string{"slowed"}}},
	{"original beats sole nightcore credit", "love story", []gossiprpc.SpotifyTrack{
		searchTrack("nightcore", "Love Story", "Nightcore"), searchTrack("original", "Love Story", "Taylor Swift"),
	}, nil, chatSearchOutcome{ids: []string{"original"}}},
	{"band named Live", "lightning crashes live", []gossiprpc.SpotifyTrack{
		searchTrack("band", "Lightning Crashes", "Live"),
	}, nil, chatSearchOutcome{ids: []string{"band"}}},
	{"artist Live beats title-only live version", "hello live", []gossiprpc.SpotifyTrack{
		helloLive, searchTrack("band", "Hello", "Live"),
	}, nil, chatSearchOutcome{ids: []string{"band"}}},
	{"original beats radio edit", "hello adele", []gossiprpc.SpotifyTrack{
		searchTrack("radio", "Hello - Radio Edit", "Adele"), helloOriginal,
	}, nil, chatSearchOutcome{ids: []string{"hello"}, queries: []string{"hello adele"}}},
	{"radio edit after original search", "levels avicii", []gossiprpc.SpotifyTrack{levelsEdit},
		map[string][]gossiprpc.SpotifyTrack{levelsRetry: {levelsEdit}},
		chatSearchOutcome{ids: []string{"radio"}, queries: []string{"levels avicii", levelsRetry}}},
	{"single version after original search", "hello adele", []gossiprpc.SpotifyTrack{
		searchTrack("single", "Hello - Single Version", "Adele"),
	}, nil, chatSearchOutcome{ids: []string{"single"}, queries: []string{"hello adele", helloRetry}}},
	{"original retry beats album version", "hello adele", []gossiprpc.SpotifyTrack{
		searchTrack("album", "Hello - Album Version", "Adele"),
	}, map[string][]gossiprpc.SpotifyTrack{helloRetry: {helloOriginal}},
		chatSearchOutcome{ids: []string{"hello"}, queries: []string{"hello adele", helloRetry}}},
	{"exact subtitle follows first window", "last friday night katy perry", []gossiprpc.SpotifyTrack{
		searchTrack("subtitle", "Last Friday Night (T.G.I.F.)", "Katy Perry"),
	}, map[string][]gossiprpc.SpotifyTrack{
		`track:"last friday night" artist:"katy perry"`: {searchTrack("plain", "Last Friday Night", "Katy Perry")},
	}, chatSearchOutcome{ids: []string{"subtitle"}, queries: []string{"last friday night katy perry"}}},
	{"remix in an artist name", "hello adele", []gossiprpc.SpotifyTrack{
		searchTrack("duet", "Hello", "Adele", "The Remix Boys"),
	}, nil, chatSearchOutcome{ids: []string{"duet"}, queries: []string{"hello adele"}}},
	{"asked version in the credits", "love story sped up taylor swift", []gossiprpc.SpotifyTrack{
		searchTrack("sped-up", "Love Story", "Taylor Swift", "Sped Up"),
	}, nil, chatSearchOutcome{ids: []string{"sped-up"}}},
	{"requested version with joined base words", "lovestory slowed taylor swift", []gossiprpc.SpotifyTrack{
		searchTrack("slowed", "Love Story - Slowed", "Taylor Swift"),
	}, nil, chatSearchOutcome{ids: []string{"slowed"}}},
}

func TestSearchRecordingVersions(t *testing.T) {
	for _, tc := range recordingVersionCases {
		t.Run(tc.name, func(t *testing.T) {
			result := runChatSearch(t, chatSearchCase{
				query: tc.query, window: tc.window, byQuery: tc.byQuery, wantQueries: tc.want.queries,
			})
			assert.Equal(t, tc.want, result)
		})
	}
}
