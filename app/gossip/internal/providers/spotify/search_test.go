// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

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
			reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{
				ChannelID: "2", Query: query, Limit: 1,
			})
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
				reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", req)
				require.Empty(t, reply.Error)
				require.Len(t, reply.Tracks, 1)
				assert.Equal(t, "right", reply.Tracks[0].ID)
				assert.Equal(t, viaFiltered, reply.ResolvedAs)
			}
			assert.Len(t, queries, 2, "repeat requests reuse the final ranked reply")
		})
	}
}

type searchStep struct {
	status int
	tracks []gossiprpc.SpotifyTrack
}

type searchOutcome struct {
	TrackIDs   []string
	Failed     bool
	ResolvedAs string
	Queries    []string
}

func TestSearchRecoveryPlans(t *testing.T) {
	wrongTitle := searchStep{tracks: []gossiprpc.SpotifyTrack{searchTrack("wrong", "ALL I WANTED WAS YOU", "ily", "EVO")}}
	step := func(id, title string, artists ...string) searchStep {
		return searchStep{tracks: []gossiprpc.SpotifyTrack{searchTrack(id, title, artists...)}}
	}
	quoted := func(song, artist string) string { return `track:"` + song + `" artist:"` + artist + `"` }

	for _, tc := range []struct {
		name    string
		query   string
		steps   []searchStep
		byQuery map[string]searchStep
		want    searchOutcome
	}{
		{"TestSearchRecoveryRejectsUnverifiedArtistAndBoundsCalls", "all i wanted paramore", []searchStep{wrongTitle}, nil,
			searchOutcome{ResolvedAs: "text", Queries: []string{"all i wanted paramore", quoted("all i wanted", "paramore"), quoted("all i", "wanted paramore")}}},
		{"TestSearchRecoveryDoesNotHideSpotifyThrottling", "all i wanted paramore", []searchStep{wrongTitle, {status: http.StatusTooManyRequests}}, nil,
			searchOutcome{Failed: true, Queries: []string{"all i wanted paramore", quoted("all i wanted", "paramore")}}},
		{"TestSearchExplicitArtistDoesNotFallBackToAnotherArtist", "all i wanted by paramore", []searchStep{step("wrong", "All I Wanted", "ily")}, nil,
			searchOutcome{ResolvedAs: "text", Queries: []string{quoted("all i wanted", "paramore"), "all i wanted by paramore"}}},
		{"TestSearchLiteralTitleContainingBySurvivesWrongFilteredResults", "stand by me",
			[]searchStep{step("wrong", "Stand", "Other Artist"), step("right", "Stand by Me", "Ben E. King")}, nil,
			searchOutcome{TrackIDs: []string{"right"}, ResolvedAs: "text", Queries: []string{quoted("stand", "me"), "stand by me"}}},
		{"TestSearchLiteralTitleSurvivesFailedInterpretations", "i want it that way", []searchStep{step("right", "I Want It That Way", "Backstreet Boys")}, nil,
			searchOutcome{TrackIDs: []string{"right"}, ResolvedAs: "text", Queries: []string{
				"i want it that way", quoted("i want it that", "way"), quoted("i want it", "that way"), quoted("i want", "it that way"),
			}}},
		{"TestSearchRecoversFromLiteralCoverTitle", "all i wanted paramore", []searchStep{step("cover", "All I Wanted Paramore", "ARTxJ")},
			map[string]searchStep{quoted("all i wanted", "paramore"): step("right", "All I Wanted", "Paramore")},
			searchOutcome{TrackIDs: []string{"right"}, ResolvedAs: "filtered", Queries: []string{"all i wanted paramore", quoted("all i wanted", "paramore")}}},
		{"TestSearchOneWordReleaseMetadataRecoversArtist", "hello adele", []searchStep{step("wrong", "Hello - Remastered", "Other Artist")},
			map[string]searchStep{quoted("hello", "adele"): step("right", "Hello", "Adele")},
			searchOutcome{TrackIDs: []string{"right"}, ResolvedAs: "filtered", Queries: []string{"hello adele", quoted("hello", "adele")}}},
		{"TestLiteralCoverTitleRecoversMultipleWordArtist", "all i wanted fall out boy", []searchStep{step("cover", "All I Wanted Fall Out Boy", "Cover Artist")},
			map[string]searchStep{quoted("all i wanted", "fall out boy"): step("right", "All I Wanted", "Fall Out Boy")},
			searchOutcome{TrackIDs: []string{"right"}, ResolvedAs: "filtered", Queries: []string{
				"all i wanted fall out boy", quoted("all i wanted fall out", "boy"), quoted("all i wanted fall", "out boy"), quoted("all i wanted", "fall out boy"),
			}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries []string
			p := newPlayerProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query().Get("q")
				chosen, scripted := tc.byQuery[query]
				if !scripted {
					chosen = tc.steps[min(len(queries), len(tc.steps)-1)]
				}
				queries = append(queries, query)
				if chosen.status != 0 {
					w.WriteHeader(chosen.status)
					return
				}
				writeSearchTracks(t, w, chosen.tracks...)
			}))

			reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: tc.query, Limit: 1})

			var ids []string
			for _, track := range reply.Tracks {
				ids = append(ids, track.ID)
			}
			assert.Equal(t, tc.want, searchOutcome{TrackIDs: ids, Failed: reply.Error != "", ResolvedAs: reply.ResolvedAs, Queries: queries})
		})
	}
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

func TestSearchFilteredFallsBackToPlainWhenEmpty(t *testing.T) {
	mint, _ := newMintServer(t, "tok-1")
	var searches []string
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/search", r.URL.Path)
		searches = append(searches, r.URL.Query().Get("q"))
		if len(searches) == 1 {
			_, _ = io.WriteString(w, `{"tracks":{"items":[]}}`)
			return
		}
		writeSearchTracks(t, w, searchTrack("stand-by-me", "Stand by Me", "Ben E. King"))
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{
		ChannelID: "2",
		Query:     "stand by me",
	})
	require.Len(t, searches, 2, "a missed filtered search must fall back to plain text")
	assert.Contains(t, searches[0], `track:"stand"`)
	assert.Equal(t, "stand by me", searches[1])
	assert.Equal(t, viaText, reply.ResolvedAs, "the fallback win reports itself as best-effort")
	require.Len(t, reply.Tracks, 1)
	assert.Equal(t, "stand-by-me", reply.Tracks[0].ID)
}

func TestSearchPlainTextStaysSingleShot(t *testing.T) {
	mint, mints := newMintServer(t, "tok-1")
	calls := 0
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"tracks":{"items":[`+brightsideBody+`]}}`)
	})
	p := newTestProvider(t, fakeKeys{key: "rt-1"}, api, mint)

	reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{
		ChannelID: "2",
		Query:     "mr   brightside",
	})
	assert.Equal(t, 1, calls)
	assert.EqualValues(t, 1, mints.Load())
	assert.Equal(t, viaText, reply.ResolvedAs)
	require.Len(t, reply.Tracks, 1)
}

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
			reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: tt.query, Limit: 1})
			assert.Empty(t, reply.Error)
			assert.Empty(t, reply.Tracks)
			assert.LessOrEqual(t, calls, maxTextSearchRequests)
		})
	}
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
			reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: tt.query, Limit: 1})
			require.Empty(t, reply.Error)
			require.Len(t, reply.Tracks, 1)
			assert.Equal(t, "right", reply.Tracks[0].ID)
			assert.LessOrEqual(t, calls, maxTextSearchRequests)
		})
	}
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
			reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: tt.query, Limit: 1})
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

func TestSearchTextSpellingsShareOneCacheEntry(t *testing.T) {
	calls := 0
	p := newPlayerProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"tracks":{"items":[`+brightsideBody+`]}}`)
	}))

	for _, query := range []string{"Mr.   Brightside ", "mr. brightside"} {
		reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: query, Limit: 1})
		require.Empty(t, reply.Error)
	}

	assert.Equal(t, 1, calls, "whitespace and case normalize into one cache entry")
}

func TestSearchPlansFilteredAndPlainQueriesInOrder(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{"a song by an artist tries the filtered form first", "mr brightside BY the killers",
			[]string{`track:"mr brightside" artist:"the killers"`, "mr brightside by the killers"}},
		{"the artist dash song convention tries both orders before plain text", "the killers - mr brightside",
			[]string{`track:"mr brightside" artist:"the killers"`, `track:"the killers" artist:"mr brightside"`, "the killers - mr brightside"}},
		{"a false by split still plans a plain fallback", "stand by me",
			[]string{`track:"stand" artist:"me"`, "stand by me"}},
		{"spotify operators containing by are passed through untouched", `track:"stand by me" artist:"ben e king"`,
			[]string{`track:"stand by me" artist:"ben e king"`}},
		{"inner quotes are stripped from qualifiers", `say "hi" by x`,
			[]string{`track:"say hi" artist:"x"`, `say "hi" by x`}},
		{"a dash needs content on both sides", "ac/dc - ", []string{"ac/dc -"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries []string
			p := newPlayerProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				queries = append(queries, r.URL.Query().Get("q"))
				_, _ = io.WriteString(w, `{"tracks":{"items":[]}}`)
			}))

			reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: tc.query, Limit: 1})

			assert.Empty(t, reply.Error)
			assert.Equal(t, tc.want, queries)
		})
	}
}
