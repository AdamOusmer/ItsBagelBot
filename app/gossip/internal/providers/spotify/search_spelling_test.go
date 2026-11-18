// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"net/http"
	"testing"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
)

type chatSearchCase struct {
	query       string
	window      []gossiprpc.SpotifyTrack
	byQuery     map[string][]gossiprpc.SpotifyTrack
	wantID      string
	wantQueries []string
}

type chatSearchOutcome struct {
	ids        []string
	err        string
	overBudget bool
	queries    []string
}

var chatSpellingCases = []chatSearchCase{
	chatTrack("love story taylor swift", "Love Story (Taylor's Version)", "Taylor Swift"),
	chatTrack("love story", "Love Story (Taylor's Version)", "Taylor Swift"),
	chatTrack("what was i made for billie eilish", `What Was I Made For? [From The Motion Picture "Barbie"]`, "Billie Eilish"),
	chatTrack("bohemian rapsody quen", "Bohemian Rhapsody - Remastered 2011", "Queen"),
	chatTrack("bohemain rapsody queen", "Bohemian Rhapsody", "Queen"),
	chatTrack("suppersttion stevie wonder", "Superstition", "Stevie Wonder"),
	chatTrack("hello adel", "Hello", "Adele"),
	{
		query:       "play mr brightside pls",
		window:      []gossiprpc.SpotifyTrack{searchTrack("right", "Mr. Brightside", "The Killers")},
		wantID:      "right",
		wantQueries: []string{"play mr brightside pls"},
	},
	chatTrack("mr brightside killers song", "Mr. Brightside", "The Killers"),
	chatTrack("play mr brightside killers", "Mr. Brightside", "The Killers"),
	scriptedFiller(chatTrack("please please me", "Please Please Me", "The Beatles"), "me", searchTrack("wrong", "ME!", "Taylor Swift")),
	scriptedFiller(chatTrack("thank u next", "thank u, next", "Ariana Grande"), "thank u", searchTrack("wrong", "Thank You", "Dido")),
	scriptedFiller(chatTrack("love song", "Love Song", "Sara Bareilles"), "love", searchTrack("wrong", "LOVE.", "Kendrick Lamar")),
	scriptedFiller(chatTrack("play that song", "Play That Song", "Train"), "that", searchTrack("wrong", "That", "Other")),
	chatTrack("the sound of silence simon and garfunkel", "The Sound of Silence", "Simon & Garfunkel"),
	chatTrack("rockstar post malone ft 21 savage", "rockstar (feat. 21 Savage)", "Post Malone", "21 Savage"),
	chatTrack("under pressure queen x david bowie", "Under Pressure - Remastered 2011", "Queen", "David Bowie"),
	chatTrack("die with a smile lady gaga & bruno mars", "Die With A Smile", "Lady Gaga", "Bruno Mars"),
	chatTrack("die with a smile gaga bruno mars", "Die With A Smile", "Lady Gaga", "Bruno Mars"),
	chatTrack("thunderstruck acdc", "Thunderstruck", "AC/DC"),
	chatTrack("take on me aha", "Take on Me", "a-ha"),
	chatTrack("so what pink", "So What", "P!nk"),
	chatTrack("stressed out 21 pilots", "Stressed Out", "Twenty One Pilots"),
	chatTrack("party in the usa", "Party In The U.S.A.", "Miley Cyrus"),
	chatTrack("never gonna give u up", "Never Gonna Give You Up", "Rick Astley"),
	chatTrack("lean on major lazer mo", "Lean On", "Major Lazer", "MØ", "DJ Snake"),
	chatTrack("loyalty kendrick lamar", "LOYALTY. FEAT. RIHANNA.", "Kendrick Lamar", "Rihanna"),
	chatTrack("time of your life green day", "Good Riddance (Time of Your Life)", "Green Day"),
	chatTrack("good riddance green day", "Good Riddance (Time of Your Life)", "Green Day"),
	{
		query: "play that funky music",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("trimmed", "That Funky Music", "Other"),
			searchTrack("literal", "Play That Funky Music", "Wild Cherry"),
		},
		wantID: "literal",
	},
	chatTrack("can you play 2️⃣ so what pink 🎶 pls", "So What", "P!nk"),
	chatTrack("hello", "Hello - 2015 Remaster", "Adele"),
	chatTrack("hello", "Hello (Remastered 2015)", "Adele"),
	chatTrack("hello", "Hello (feat. Artist)", "Adele", "Artist"),
	chatTrack("hello live", "Hello - Live", "Adele"),
	chatTrack("levels avicii", "Levels - Original Mix", "Avicii"),
	chatTrack("blinding lights the weekend", "Blinding Lights", "The Weeknd"),
	chatTrack("shape of you ed sheran", "Shape of You", "Ed Sheeran"),
	chatTrack("uptown funk bruno mars", "Uptown Funk (feat. Bruno Mars)", "Mark Ronson", "Bruno Mars"),
	chatTrack("bad guy billie elish", "bad guy", "Billie Eilish"),
	chatTrack("hey jude beatles", "Hey Jude - Remastered 2015", "The Beatles"),
	chatTrack("dont stop me now queen", "Don't Stop Me Now - Remastered 2011", "Queen"),
	chatTrack("sweet child o mine guns n roses", "Sweet Child O' Mine", "Guns N' Roses"),
	chatTrack("september earth wind and fire", "September", "Earth, Wind & Fire"),
	chatTrack("gods plan drake", "God's Plan", "Drake"),
	chatTrack("x gon give it to ya dmx", "X Gon' Give It To Ya", "DMX"),
	chatTrack("a-ha take on me", "Take on Me", "a-ha"),
	chatTrack("tik tok ke$ha", "TiK ToK", "Kesha"),
	chatTrack("sicko mode travis scot", "SICKO MODE", "Travis Scott"),
	chatTrack("umbrella rihana", "Umbrella", "Rihanna", "JAY-Z"),
	chatTrack("7 rings", "7 rings", "Ariana Grande"),
	chatTrack("7 rings ariana grande", "7 rings", "Ariana Grande"),
	chatTrack("99 problems jay z", "99 Problems", "JAY-Z"),
	chatTrack("queen - bohemain rhapsody", "Bohemian Rhapsody", "Queen"),
	chatTrack("lean on major lazer feat mo", "Lean On (feat. MØ & DJ Snake)", "Major Lazer", "MØ", "DJ Snake"),
	chatTrack("halo beyonce", "Halo", "Beyoncé"),
	chatTrack("nothing compares 2 u sinead oconnor", "Nothing Compares 2 U", "Sinéad O'Connor"),
	chatTrack("mr brightside killers", "Mr. Brightside", "The Killers"),
	chatTrack("love story sped up taylor swift", "Love Story - Sped Up", "Taylor Swift"),
	chatTrack("old town road lil nas x", "Old Town Road", "Lil Nas X"),
	chatTrack("live forever oasis", "Live Forever", "Oasis"),
	chatTrack("a b c", "A B C", "Other"),
	chatTrack("x", "X", "Other"),
	chatTrack("紅蓮華 lisa", "紅蓮華", "LiSA"),
	chatTrack("ガラス", "ガラス", "Other"),
	chatTrack("song a and b and c and d and e and f and g and h", "Song", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"),
	chatTrack("all the small things blink182", "All the Small Things", "blink-182"),
	{
		query:  "7 rings ariana grande",
		window: []gossiprpc.SpotifyTrack{searchTrack("tribute", "7 Rings Tribute", "Cover Band")},
		byQuery: map[string][]gossiprpc.SpotifyTrack{
			`track:"7 rings" artist:"ariana grande"`: {searchTrack("right", "7 rings", "Ariana Grande")},
		},
		wantID:      "right",
		wantQueries: []string{"7 rings ariana grande", `track:"7 rings" artist:"ariana grande"`},
	},
	{
		query: "stressed out 21 pilots",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("cover", "Stressed Out", "Cover Band"),
			searchTrack("artist", "Ride", "Twenty One Pilots"),
		},
		byQuery: map[string][]gossiprpc.SpotifyTrack{
			"stressed out twenty one pilots": {searchTrack("right", "Stressed Out", "Twenty One Pilots")},
		},
		wantID: "right",
		wantQueries: []string{
			"stressed out 21 pilots", `track:"stressed out" artist:"twenty one pilots"`, "stressed out twenty one pilots",
		},
	},
	{
		query: "so what p!nk",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("cover", "So What", "Cover Band"),
			searchTrack("artist", "Just Like a Pill", `"P!nk"`),
		},
		byQuery: map[string][]gossiprpc.SpotifyTrack{
			`track:"so what" artist:"p!nk"`: {searchTrack("right", "So What", "P!nk")},
		},
		wantID:      "right",
		wantQueries: []string{"so what p!nk", `track:"so what" artist:"p!nk"`},
	},
	{
		query:  "pls the 21-rings ariána grande",
		window: []gossiprpc.SpotifyTrack{searchTrack("tribute", "21 Rings Tribute", "Cover Band")},
		byQuery: map[string][]gossiprpc.SpotifyTrack{
			`track:"the 21 rings" artist:"ariana grande"`: {searchTrack("right", "The 21 Rings", "Ariana Grande")},
		},
		wantID:      "right",
		wantQueries: []string{"pls the 21-rings ariána grande", `track:"the 21 rings" artist:"ariana grande"`},
	},
	{
		query: "dnot stop me now queen",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("cover", `"Don't Stop Me Now" (feat. "Band") - Remastered 2011`, "Cover Band"),
			searchTrack("artist", "Killer Queen", "Queen"),
		},
		byQuery: map[string][]gossiprpc.SpotifyTrack{
			`track:"don't stop me now" artist:"queen"`: {searchTrack("right", "Don't Stop Me Now", "Queen")},
		},
		wantID:      "right",
		wantQueries: []string{"dnot stop me now queen", `track:"don't stop me now" artist:"queen"`},
	},
	fastCarPlainFallback(),
	bohemianCorrectedRecovery(false),
	bohemianCorrectedRecovery(true),
	bohemianByRecovery(),
	{
		query: "espresso sabrina",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("partial", "Espresso", "Sabrina Carpenter"),
			searchTrack("full", "Espresso", "Sabrina"),
		},
		wantID: "full",
	},
	{
		query: "angel the weeknd",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("plural", "Angels", "The Weeknd"),
			searchTrack("right", "Angel", "The Weeknd"),
		},
		wantID: "right",
	},
}

func TestSearchToleratesChatSpelling(t *testing.T) {
	for _, tc := range chatSpellingCases {
		t.Run(tc.query, func(t *testing.T) {
			outcome := runChatSearch(t, tc)
			want := chatSearchOutcome{queries: tc.wantQueries}
			if tc.wantID != "" {
				want.ids = []string{tc.wantID}
			}
			assert.Equal(t, want, outcome)
		})
	}
}

func chatTrack(query, title string, artists ...string) chatSearchCase {
	return chatSearchCase{query: query, window: []gossiprpc.SpotifyTrack{searchTrack("right", title, artists...)}, wantID: "right"}
}

func scriptedFiller(tc chatSearchCase, trimmed string, wrong gossiprpc.SpotifyTrack) chatSearchCase {
	tc.byQuery = map[string][]gossiprpc.SpotifyTrack{tc.query: tc.window, trimmed: {wrong}}
	tc.window = nil
	return tc
}

func fastCarPlainFallback() chatSearchCase {
	const query = "fast car by tracy chapmen"
	return chatSearchCase{
		query: query,
		byQuery: map[string][]gossiprpc.SpotifyTrack{
			query: {searchTrack("right", "Fast Car", "Tracy Chapman")},
		},
		wantID:      "right",
		wantQueries: []string{`track:"fast car" artist:"tracy chapmen"`, query},
	}
}

func bohemianCorrectedRecovery(plainFallback bool) chatSearchCase {
	const query = "bohemian rapsody quen"
	const corrected = `track:"bohemian rhapsody" artist:"queen"`
	tc := chatSearchCase{
		query: query,
		window: []gossiprpc.SpotifyTrack{
			searchTrack("cover", "Bohemian Rhapsody", "Cover Band"),
			searchTrack("artist", "Killer Queen", "Queen"),
		},
		wantID:      "right",
		wantQueries: []string{query, corrected},
	}
	target := corrected
	if plainFallback {
		target = "bohemian rhapsody queen"
		tc.wantQueries = append(tc.wantQueries, target)
	}
	tc.byQuery = map[string][]gossiprpc.SpotifyTrack{target: {searchTrack("right", "Bohemian Rhapsody", "Queen")}}
	return tc
}

func bohemianByRecovery() chatSearchCase {
	tc := bohemianCorrectedRecovery(false)
	tc.query = "bohemian rapsody by quen"
	tc.wantQueries = append([]string{`track:"bohemian rapsody" artist:"quen"`, tc.query}, tc.wantQueries[1:]...)
	return tc
}

func runChatSearch(t *testing.T, tc chatSearchCase) chatSearchOutcome {
	t.Helper()
	var queries []string
	p := newPlayerProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		tracks, scripted := tc.byQuery[q]
		if !scripted && len(queries) == 0 {
			tracks = tc.window
		}
		queries = append(queries, q)
		writeSearchTracks(t, w, tracks...)
	}))
	reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: tc.query, Limit: 1})
	outcome := chatSearchOutcome{err: reply.Error, overBudget: len(queries) > maxTextSearchRequests}
	for _, track := range reply.Tracks {
		outcome.ids = append(outcome.ids, track.ID)
	}
	if tc.wantQueries != nil {
		outcome.queries = queries
	}
	return outcome
}
