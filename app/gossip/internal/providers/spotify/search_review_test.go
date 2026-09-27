// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"
	"net/http"
	"strings"
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchRejectsWeakAndConflictingMetadata(t *testing.T) {
	tests := []struct {
		name, query string
		track       gossiprpc.SpotifyTrack
	}{
		{"unrelated first result", "a nonexistent purple spaceship song", searchTrack("wrong", "Flowers", "Miley Cyrus")},
		{"tribute artist", "all i wanted by paramore", searchTrack("wrong", "All I Wanted", "Paramore Tribute")},
		{"longer unrelated title", "all i wanted by paramore", searchTrack("wrong", "All I Wanted Was You", "Paramore")},
		{"unsolicited live", "all i wanted paramore", searchTrack("wrong", "All I Wanted - Live", "Paramore")},
		{"missing requested acoustic", "all i wanted acoustic paramore", searchTrack("wrong", "All I Wanted", "Paramore")},
		{"operator artist constraint", "track:\"all i wanted\" artist:\"paramore\"", searchTrack("wrong", "All I Wanted", "Paramore Tribute")},
		{"operator title constraint", "track:\"all i wanted\" artist:\"paramore\"", searchTrack("wrong", "All I Wanted Was You", "Paramore")},
		{"meaningful clean title words", "i want your clean hands by artist", searchTrack("wrong", "I Want Your", "Artist")},
		{"meaningful stereo title words", "all i wanted stereo nonsense", searchTrack("wrong", "All I Wanted", "Paramore")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mint, _ := newMintServer(t, "tok-1")
			calls := 0
			p := newTestProvider(t, fakeKeys{key: "rt-1"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				writeSearchTracks(t, w, tt.track)
			}), mint)
			reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{ChannelID: "2", Query: tt.query, Limit: 1}))
			assert.Empty(t, reply.Error)
			assert.Empty(t, reply.Tracks)
			assert.LessOrEqual(t, calls, maxTextSearchRequests)
		})
	}
}

func TestSearchRecoversFromLiteralCoverTitle(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	var queries []string
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		queries = append(queries, query)
		if len(queries) == 1 {
			writeSearchTracks(t, w, searchTrack("cover", "All I Wanted Paramore", "ARTxJ"))
			return
		}
		assert.Equal(t, `track:"all i wanted" artist:"paramore"`, query)
		writeSearchTracks(t, w, searchTrack("right", "All I Wanted", "Paramore"))
	}), mint)
	reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{ChannelID: "2", Query: "all i wanted paramore", Limit: 1}))
	require.Len(t, reply.Tracks, 1)
	assert.Equal(t, "right", reply.Tracks[0].ID)
	assert.Len(t, queries, 2)
}

func TestSearchByAndDashConventionsUseVerifiedMetadata(t *testing.T) {
	tests := []struct{ query, title, artist string }{
		{"stand by me ben e king", "Stand by Me", "Ben E. King"},
		{"death by a thousand cuts taylor swift", "Death by a Thousand Cuts", "Taylor Swift"},
		{"death by a thousand cuts by taylor swift", "Death by a Thousand Cuts", "Taylor Swift"},
		{"all i wanted - paramore", "All I Wanted", "Paramore"},
		{"paramore - all i wanted", "All I Wanted", "Paramore"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			mint, _ := newMintServer(t, "tok-1")
			calls := 0
			p := newTestProvider(t, fakeKeys{key: "rt-1"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				writeSearchTracks(t, w, searchTrack("right", tt.title, tt.artist))
			}), mint)
			reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{ChannelID: "2", Query: tt.query, Limit: 1}))
			require.Empty(t, reply.Error)
			require.Len(t, reply.Tracks, 1)
			assert.Equal(t, "right", reply.Tracks[0].ID)
			assert.LessOrEqual(t, calls, maxTextSearchRequests)
		})
	}
}

func TestSearchOneWordReleaseMetadataRecoversArtist(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	calls := 0
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			writeSearchTracks(t, w, searchTrack("wrong", "Hello - Remastered", "Other Artist"))
			return
		}
		assert.Equal(t, `track:"hello" artist:"adele"`, r.URL.Query().Get("q"))
		writeSearchTracks(t, w, searchTrack("right", "Hello", "Adele"))
	}), mint)
	reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{ChannelID: "2", Query: "hello adele", Limit: 1}))
	require.Len(t, reply.Tracks, 1)
	assert.Equal(t, "right", reply.Tracks[0].ID)
}

func TestSearchLiteralTitleAndTypoRanking(t *testing.T) {
	tests := []struct {
		query  string
		tracks []gossiprpc.SpotifyTrack
		want   string
	}{
		{"all i wanted", []gossiprpc.SpotifyTrack{searchTrack("right", "All I Wanted", "Paramore")}, "right"},
		{"angel", []gossiprpc.SpotifyTrack{searchTrack("plural", "Angels", "Artist"), searchTrack("right", "Angel", "Artist")}, "right"},
		{"love: let me go", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Let Go", "Other Artist"), searchTrack("right", "Love: Let Me Go", "Artist")}, "right"},
		{"bohemian rapsody queen", []gossiprpc.SpotifyTrack{searchTrack("right", "Bohemian Rhapsody", "Queen")}, "right"},
		{"all i wanted parmore", []gossiprpc.SpotifyTrack{searchTrack("right", "All I Wanted", "Paramore")}, "right"},
		{"human kilers", []gossiprpc.SpotifyTrack{searchTrack("right", "Human", "The Killers")}, "right"},
		{"deja vu beyonce", []gossiprpc.SpotifyTrack{searchTrack("right", "Déjà Vu", "Beyoncé")}, "right"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			mint, _ := newMintServer(t, "tok-1")
			p := newTestProvider(t, fakeKeys{key: "rt-1"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Query().Get("q"), `artist:`) {
					writeSearchTracks(t, w)
					return
				}
				writeSearchTracks(t, w, tt.tracks...)
			}), mint)
			reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{ChannelID: "2", Query: tt.query, Limit: 1}))
			require.Len(t, reply.Tracks, 1)
			assert.Equal(t, tt.want, reply.Tracks[0].ID)
		})
	}
}

func TestMetadataAnnotationsPreserveMeaningfulWords(t *testing.T) {
	for _, tt := range []struct {
		query, name string
		want        bool
	}{
		{"hello", "Hello - 2015 Remaster", true},
		{"hello", "Hello (Remastered 2015)", true},
		{"hello", "Hello - Remastered 2015 - Live", false},
		{"hello", "Hello Stereo Nonsense", false},
		{"hello", "Hello - Clean Nonsense", false},
		{"i want your clean hands", "I Want Your", false},
		{"hello", "Hello (feat. Artist)", true},
		{"hello live", "Hello - Live", true},
	} {
		t.Run(tt.query+"/"+tt.name, func(t *testing.T) { assert.Equal(t, tt.want, titleMatch(matchWords(tt.query), tt.name)) })
	}
}

func TestSpotifyURLSchemeHandlingPreservesIDs(t *testing.T) {
	const id = "3n3Ppam7vgaVa1iaRUc9Lp"
	for _, query := range []string{"//open.spotify.com/track/" + id, "HTTPS://OPEN.SPOTIFY.COM/track/" + id} {
		target := classify(query)
		assert.Equal(t, resolveTrackID, target.kind)
		assert.Equal(t, id, target.id)
	}
	for _, query := range []string{"https:/open.spotify.com/track/" + id, "ftp://open.spotify.com/track/" + id} {
		assert.Equal(t, resolveInvalidLink, classify(query).kind)
	}
}

func TestLiteralCoverTitleRecoversMultipleWordArtist(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	calls := 0
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("q") == `track:"all i wanted" artist:"fall out boy"` {
			writeSearchTracks(t, w, searchTrack("right", "All I Wanted", "Fall Out Boy"))
			return
		}
		writeSearchTracks(t, w, searchTrack("cover", "All I Wanted Fall Out Boy", "Cover Artist"))
	}), mint)
	reply := asReply[gossiprpc.SpotifySearchReply](t, endpoint(t, p, "search")(context.Background(), gossiprpc.Request{ChannelID: "2", Query: "all i wanted fall out boy", Limit: 1}))
	require.Len(t, reply.Tracks, 1)
	assert.Equal(t, "right", reply.Tracks[0].ID)
	assert.LessOrEqual(t, calls, maxTextSearchRequests)
}
