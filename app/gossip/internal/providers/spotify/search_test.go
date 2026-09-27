// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"net/http"
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func searchTrack(id, title string, artists ...string) gossiprpc.SpotifyTrack {
	return gossiprpc.SpotifyTrack{ID: id, Name: title, Artists: artists}
}

func writeSearchTracks(t *testing.T, w http.ResponseWriter, tracks ...gossiprpc.SpotifyTrack) {
	t.Helper()
	items := make([]trackItem, 0, len(tracks))
	for _, track := range tracks {
		it := trackItem{ID: track.ID, Name: track.Name}
		for _, artist := range track.Artists {
			it.Artists = append(it.Artists, struct {
				Name string `json:"name"`
			}{Name: artist})
		}
		items = append(items, it)
	}
	require.NoError(t, codec.NewEncoder(w).Encode(map[string]any{"tracks": map[string]any{"items": items}}))
}

func TestSearchRanksTitleAndArtistBeforeOutputLimit(t *testing.T) {
	for _, query := range []string{"all I wanted paramore", "Paramore all I wanted"} {
		t.Run(query, func(t *testing.T) {
			mint, _ := newMintServer(t, "tok-1")
			calls := 0
			api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, "10", r.URL.Query().Get("limit"), "matching window is independent of !sr's limit")
				writeSearchTracks(t, w,
					searchTrack("wrong", "ALL I WANTED WAS YOU", "ily", "EVO"),
					searchTrack("cover", "All I Wanted Paramore", "ARTxJ"),
					searchTrack("right", "All I Wanted", "Paramore"))
			})
			p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)
			reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
				ChannelID: "2", Query: query, Limit: 1,
			}))
			require.Empty(t, reply.Error)
			require.Len(t, reply.Tracks, 1)
			assert.Equal(t, "right", reply.Tracks[0].ID)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestSearchRecoversArtistWhenBroadResultsOmitCorrectSong(t *testing.T) {
	for _, query := range []string{"all I wanted paramore", "paramore all I wanted", "all i wanted fall out boy"} {
		t.Run(query, func(t *testing.T) {
			artist := "paramore"
			if query == "all i wanted fall out boy" {
				artist = "fall out boy"
			}
			mint, _ := newMintServer(t, "tok-1")
			var queries []string
			api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				queries = append(queries, r.URL.Query().Get("q"))
				if len(queries) == 1 {
					writeSearchTracks(t, w, searchTrack("wrong", "ALL I WANTED WAS YOU", "ily", "EVO"))
					return
				}
				assert.Equal(t, `track:"all i wanted" artist:"`+artist+`"`, queries[len(queries)-1])
				writeSearchTracks(t, w,
					searchTrack("wrong-artist", "All I Wanted", "ily"),
					searchTrack("right", "All I Wanted", artist))
			})
			p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)
			req := gossiprpc.Request{ChannelID: "2", Query: query, Limit: 1}
			for range 2 {
				reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), req))
				require.Empty(t, reply.Error)
				require.Len(t, reply.Tracks, 1)
				assert.Equal(t, "right", reply.Tracks[0].ID)
				assert.Equal(t, viaFiltered, reply.ResolvedAs)
			}
			assert.Len(t, queries, 2, "repeat requests reuse the final ranked reply")
		})
	}
}

func TestSearchRecoveryRejectsUnverifiedArtistAndBoundsCalls(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	calls := 0
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeSearchTracks(t, w, searchTrack("wrong", "ALL I WANTED WAS YOU", "ily", "EVO"))
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)
	reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
		ChannelID: "2", Query: "all i wanted paramore", Limit: 1,
	}))
	require.Empty(t, reply.Error)
	assert.Empty(t, reply.Tracks, "a title match cannot account for the missing artist")
	assert.LessOrEqual(t, calls, maxTextSearchRequests)
	assert.Greater(t, calls, 1)
}

func TestSearchRecoveryDoesNotHideSpotifyThrottling(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	calls := 0
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			writeSearchTracks(t, w, searchTrack("wrong", "ALL I WANTED WAS YOU", "ily", "EVO"))
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)
	reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
		ChannelID: "2", Query: "all i wanted paramore", Limit: 1,
	}))
	assert.NotEmpty(t, reply.Error)
	assert.Empty(t, reply.Tracks)
	assert.Equal(t, 2, calls, "an upstream failure stops the recovery plan")
}

func TestSearchExplicitArtistDoesNotFallBackToAnotherArtist(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	calls := 0
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeSearchTracks(t, w, searchTrack("wrong", "All I Wanted", "ily"))
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)
	reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
		ChannelID: "2", Query: "all i wanted by paramore", Limit: 1,
	}))
	assert.Empty(t, reply.Error)
	assert.Empty(t, reply.Tracks)
	assert.Equal(t, 2, calls)
}

func TestSearchLiteralTitleContainingBySurvivesWrongFilteredResults(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	calls := 0
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			writeSearchTracks(t, w, searchTrack("wrong", "Stand", "Other Artist"))
			return
		}
		writeSearchTracks(t, w, searchTrack("right", "Stand by Me", "Ben E. King"))
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)
	reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
		ChannelID: "2", Query: "stand by me", Limit: 1,
	}))
	require.Empty(t, reply.Error)
	require.Len(t, reply.Tracks, 1)
	assert.Equal(t, "right", reply.Tracks[0].ID)
	assert.Equal(t, 2, calls)
}

func TestSearchLiteralTitleSurvivesFailedInterpretations(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	calls := 0
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeSearchTracks(t, w, searchTrack("right", "I Want It That Way", "Backstreet Boys"))
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)
	reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{
		ChannelID: "2", Query: "i want it that way", Limit: 1,
	}))
	require.Len(t, reply.Tracks, 1)
	assert.Equal(t, "right", reply.Tracks[0].ID)
	assert.LessOrEqual(t, calls, maxTextSearchRequests)
}

func TestRankPlainPreservesVersionsArtistsAndTies(t *testing.T) {
	tests := []struct {
		name, query string
		tracks      []gossiprpc.SpotifyTrack
		want        string
	}{
		{"secondary credited artist", "under pressure david bowie", []gossiprpc.SpotifyTrack{
			searchTrack("cover", "Under Pressure", "Queen Tribute"),
			searchTrack("right", "Under Pressure", "Queen", "David Bowie"),
		}, "right"},
		{"requested live recording", "all i wanted live paramore", []gossiprpc.SpotifyTrack{
			searchTrack("studio", "All I Wanted", "Paramore"),
			searchTrack("right", "All I Wanted - Live", "Paramore"),
		}, "right"},
		{"studio before unsolicited live recording", "all i wanted paramore", []gossiprpc.SpotifyTrack{
			searchTrack("live", "All I Wanted - Live", "Paramore"),
			searchTrack("right", "All I Wanted", "Paramore"),
		}, "right"},
		{"punctuation and accents", "deja vu beyonce", []gossiprpc.SpotifyTrack{
			searchTrack("wrong", "Deja Vu", "Other Artist"),
			searchTrack("right", "Déjà Vu", "Beyoncé"),
		}, "right"},
		{"equal match preserves Spotify order", "human killers", []gossiprpc.SpotifyTrack{
			searchTrack("first", "Human", "The Killers"),
			searchTrack("second", "Human", "The Killers"),
		}, "first"},
		{"abbreviated artist outranks literal cover title", "human killers", []gossiprpc.SpotifyTrack{
			searchTrack("cover", "Human Killers", "Other Artist"),
			searchTrack("right", "Human", "The Killers"),
		}, "right"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rankPlain(tt.tracks, tt.query)
			assert.Equal(t, tt.want, tt.tracks[0].ID)
		})
	}
}
