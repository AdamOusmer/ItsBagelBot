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

var rejectedMetadataCases = []struct {
	name, query string
	tracks      []gossiprpc.SpotifyTrack
}{
	{"unrelated first result", "a nonexistent purple spaceship song", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Flowers", "Miley Cyrus")}},
	{"tribute artist", "all i wanted by paramore", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All I Wanted", "Paramore Tribute")}},
	{"longer unrelated title", "all i wanted by paramore", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All I Wanted Was You", "Paramore")}},
	{"unsolicited live", "all i wanted paramore", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All I Wanted - Live", "Paramore")}},
	{"missing requested acoustic", "all i wanted acoustic paramore", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All I Wanted", "Paramore")}},
	{"operator artist constraint", "track:\"all i wanted\" artist:\"paramore\"", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All I Wanted", "Paramore Tribute")}},
	{"operator title constraint", "track:\"all i wanted\" artist:\"paramore\"", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All I Wanted Was You", "Paramore")}},
	{"meaningful clean title words", "i want your clean hands by artist", []gossiprpc.SpotifyTrack{searchTrack("wrong", "I Want Your", "Artist")}},
	{"meaningful stereo title words", "all i wanted stereo nonsense", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All I Wanted", "Paramore")}},
	{"meaningful word ending", "all i want paramore", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All I Wanted", "Paramore")}},
	{"explicit live conflict", "all i wanted by paramore", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All I Wanted (Live)", "Paramore")}},
	{"sped up conflict", "love story by taylor swift", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Love Story - Sped Up", "Taylor Swift")}},
	{"part conflict", "love story by taylor swift", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Love Story - Part II", "Taylor Swift")}},
	{"unverified base title", "hello", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello - Clean Nonsense", "Other")}},
	{"partial tribute credit", "paramore all i wanted", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All I Wanted", "Paramore Tribute Band")}},
	{"partial karaoke credit", "espresso sabrina", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Espresso", "Sabrina Carpenter Karaoke")}},
	{"short word transposition", "god is a woman", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Dog Is a Woman", "Other")}},
	{"different two-digit title", "22 taylor swift", []gossiprpc.SpotifyTrack{searchTrack("wrong", "23", "Taylor Swift")}},
	{"different year", "1999 prince", []gossiprpc.SpotifyTrack{searchTrack("wrong", "1998", "Prince")}},
	{"extra title word", "hold on", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hold On Tight", "Other")}},
	{"marker is not a typo", "all i wanted life paramore", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All I Wanted - Live", "Paramore")}},
	{"fuzzy title alone", "angels", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Angel", "Sarah McLachlan")}},
	{"two missing letters exceed one edit", "mr brightside the kllrs", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Mr. Brightside", "The Killers")}},
	{"ambiguous fuzzy titles alone", "lovers", []gossiprpc.SpotifyTrack{searchTrack("one", "Lover", "Taylor Swift"), searchTrack("two", "Loners", "Other")}},
	{"repeated leading title word", "please please me", []gossiprpc.SpotifyTrack{searchTrack("wrong", "ME!", "Taylor Swift")}},
	{"meaningful next", "thank u next", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Thank You", "Dido")}},
	{"meaningful song", "love song", []gossiprpc.SpotifyTrack{searchTrack("wrong", "LOVE.", "Kendrick Lamar")}},
	{"meaningful play and song", "play that song", []gossiprpc.SpotifyTrack{searchTrack("wrong", "That", "Other")}},
	{"meaningful play alone", "play that", []gossiprpc.SpotifyTrack{searchTrack("wrong", "That", "Other")}},
	{"meaningful add", "add it up", []gossiprpc.SpotifyTrack{searchTrack("wrong", "It Up", "Other")}},
	{"trailing song without artist", "mr brightside song", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Mr. Brightside", "The Killers")}},
	{"a cappella", "hello adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello (A Cappella)", "Adele")}},
	{"stripped", "hello adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello (Stripped)", "Adele")}},
	{"remixed", "hello adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello (Remixed)", "Adele")}},
	{"spanish version", "hello adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello (Spanish Version)", "Adele")}},
	{"8d audio", "hello adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello (8D Audio)", "Adele")}},
	{"re-recorded", "hello adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello - Re-Recorded", "Adele")}},
	{"tiktok version", "hello adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello (TikTok Version)", "Adele")}},
	{"slowed down", "hello adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello - Slowed Down", "Adele")}},
	{"live at the bbc", "hello adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello - Live at the BBC", "Adele")}},
	{"unspecified live recording", "hello live adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello (Live at the BBC)", "Adele")}},
	{"sped up parentheses", "love story", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Love Story (Sped Up)", "Taylor Swift")}},
	{"extended mix", "levels avicii", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Levels - Extended Mix", "Avicii")}},
	{"radio mix", "levels avicii", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Levels - Radio Mix", "Avicii")}},
	{"implicit remix", "despacito justin bieber", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Despacito - Remix", "Luis Fonsi", "Daddy Yankee", "Justin Bieber")}},
	{"different duration version", "all too well taylor swfit", []gossiprpc.SpotifyTrack{searchTrack("wrong", "All Too Well (10 Minute Version) (Taylor's Version) (From The Vault)", "Taylor Swift")}},
	{"from a tribute album", "hello", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello (From The Tribute Album)", "Tribute Kings")}},
	{"nightcore title", "love story taylor swift", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Love Story - Nightcore", "Taylor Swift")}},
	{"nightcore credit", "love story taylor swift", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Love Story", "Taylor Swift", "Nightcore")}},
	{"partial queen suffix", "bohemian rhapsody queen", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Bohemian Rhapsody", "Killer Queen")}},
	{"partial queen prefix", "under pressure queen", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Under Pressure", "Queen Extravaganza")}},
	{"partial adele credit", "hello adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello", "Adele Hits Band")}},
	{"partial taylor swift credit", "love story taylor swift", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Love Story", "The Taylor Swift Experience")}},
	{"partial sabrina credit", "espresso sabrina", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Espresso", "Sabrina Carpenter")}},
	{"partial kendrick credit", "humble kendrick", []gossiprpc.SpotifyTrack{searchTrack("wrong", "HUMBLE.", "Kendrick Lamar")}},
	{"partial ariana credit", "7 rings ariana", []gossiprpc.SpotifyTrack{searchTrack("wrong", "7 rings", "Ariana Grande")}},
	{"short metadata artist typo", "hello adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello", "Adel")}},
	{"eight versus eighty", "8 billie eilish", []gossiprpc.SpotifyTrack{searchTrack("wrong", "80", "Billie Eilish")}},
	{"for versus four", "for taylor swift", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Four", "Taylor Swift")}},
	{"twenty one versus twenty two", "21 adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "22", "Adele")}},
	{"twenty two versus twenty one", "22 adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "21", "Adele")}},
	{"meaningful extra title word", "hold on adele", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hold", "Adele")}},
	{"kana voicing", "ガラス", []gossiprpc.SpotifyTrack{searchTrack("wrong", "カラス", "Other")}},
	{"shorter CJK title", "紅蓮華", []gossiprpc.SpotifyTrack{searchTrack("wrong", "紅蓮", "LiSA")}},
	{"longer CJK title", "紅蓮", []gossiprpc.SpotifyTrack{searchTrack("wrong", "紅蓮華", "LiSA")}},
	{"short fuzzy title with exact artist", "angel the weeknd", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Angels", "The Weeknd")}},
	{"ambiguous fuzzy titles with same artist", "superstiton stevie wonder", []gossiprpc.SpotifyTrack{
		searchTrack("one", "Superstition", "Stevie Wonder"), searchTrack("two", "Superstitions", "Stevie Wonder"),
	}},
	{"ambiguous short titles with different credits", "shiner andrew", []gossiprpc.SpotifyTrack{
		searchTrack("one", "Shined", "Andrews"), searchTrack("two", "Shines", "Andrew"),
	}},
	{"remaster label before live marker", "hello", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello - Remastered 2015 - Live", "Adele")}},
	{"meaningful stereo text", "hello", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello Stereo Nonsense", "Adele")}},
	{"meaningful clean text without artist", "i want your clean hands", []gossiprpc.SpotifyTrack{searchTrack("wrong", "I Want Your", "Artist")}},
	{"emoji only", "🎶🎶", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello", "Adele")}},
	{"keycap only", "2️⃣", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Two", "Adele")}},
	{"punctuation only", "!!! $$ @", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Hello", "Adele")}},
	{"ampersand only", "&", []gossiprpc.SpotifyTrack{searchTrack("wrong", "And", "Adele")}},
	{"unrelated plain window", "one two three four five six seven", []gossiprpc.SpotifyTrack{searchTrack("wrong", "Unrelated", "Nobody")}},
	{"empty dash windows", "a - b - c", nil},
	{"recovery budget without a verified track", "bohemian rapsody by quen", []gossiprpc.SpotifyTrack{
		searchTrack("wrong-title-credit", "Bohemian Rhapsody", "Cover Band"), searchTrack("wrong-title", "Killer Queen", "Queen"),
	}},
	{"long unrelated query", strings.Repeat("love and hate x war with peace ", 40), []gossiprpc.SpotifyTrack{
		searchTrack("wrong", "Love", "War", "Peace", "Hate", "And", "X", "With", "Love and Hate"),
	}},
}

func TestSearchRejectsWeakAndConflictingMetadata(t *testing.T) {
	type outcome struct {
		err        string
		trackCount int
		overBudget bool
	}
	for _, tt := range rejectedMetadataCases {
		t.Run(tt.name, func(t *testing.T) {
			mint, _ := newMintServer(t, "tok-1")
			calls := 0
			p := newTestProvider(t, fakeKeys{key: "rt-1"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				writeSearchTracks(t, w, tt.tracks...)
			}), mint)
			reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: tt.query, Limit: 1})
			assert.Equal(t, outcome{}, outcome{
				err: reply.Error, trackCount: len(reply.Tracks), overBudget: calls > maxTextSearchRequests,
			})
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
