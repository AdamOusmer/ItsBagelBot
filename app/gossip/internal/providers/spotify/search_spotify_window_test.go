// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"net/http"
	"strconv"
	"testing"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
)

type spotifyWindowCase struct {
	query    string
	window   []gossiprpc.SpotifyTrack
	filtered map[string][]gossiprpc.SpotifyTrack
	want     spotifyWindowOutcome
}

type spotifyWindowOutcome struct {
	pick    string
	queries []string
	err     string
}

func TestSearchSpotifyWindows(t *testing.T) {
	for _, tc := range spotifyWindowCases {
		t.Run(tc.query, func(t *testing.T) {
			assert.Equal(t, tc.want, replaySpotifyWindow(t, tc))
		})
	}
}

func replaySpotifyWindow(t *testing.T, tc spotifyWindowCase) spotifyWindowOutcome {
	t.Helper()
	var queries []string
	p := newPlayerProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		assert.Equal(t, "10", values.Get("limit"))
		q := values.Get("q")
		if offset := values.Get("offset"); offset != "" {
			q += " offset:" + offset
		}
		queries = append(queries, q)
		tracks := tc.filtered[q]
		if q == tc.query {
			tracks = tc.window
		}
		writeSearchTracks(t, w, tracks...)
	}))
	reply := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: tc.query, Limit: 1})
	return spotifyWindowOutcome{pick: firstSearchID(searchIDs(reply)), queries: queries, err: reply.Error}
}

func tributeTitleWindow() []gossiprpc.SpotifyTrack {
	var window []gossiprpc.SpotifyTrack
	for i := range maxSearchLimit {
		track := searchTrack("tribute"+strconv.Itoa(i), "Hello", "Adele Tribute Band")
		track.Album = "Tribute to Adele"
		window = append(window, track)
	}
	return window
}

var spotifyWindowCases = []spotifyWindowCase{
	{query: "hello", window: tributeTitleWindow(),
		filtered: map[string][]gossiprpc.SpotifyTrack{"hello offset:10": {helloOriginal}},
		want:     spotifyWindowOutcome{pick: "hello", queries: []string{"hello", "hello offset:10"}}},
	{query: "faded alan walker",
		filtered: map[string][]gossiprpc.SpotifyTrack{`track:"faded" artist:"alan walker"`: {searchTrack("right", "Faded", "Alan Walker")}},
		want:     spotifyWindowOutcome{pick: "right", queries: []string{"faded alan walker", `track:"faded alan" artist:"walker"`, `track:"faded" artist:"alan walker"`}}},
	{query: "closer nine inch nails",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("long", "Closer Nine Inch", "Studio One"),
			searchTrack("medium", "Closer Nine", "Studio Two"),
			searchTrack("short", "Closer", "The Chainsmokers", "Halsey"),
		},
		filtered: map[string][]gossiprpc.SpotifyTrack{`track:"closer" artist:"nine inch nails"`: {searchTrack("right", "Closer", "Nine Inch Nails")}},
		want: spotifyWindowOutcome{pick: "right", queries: []string{
			"closer nine inch nails", `track:"closer nine inch" artist:"nails"`,
			`track:"closer nine" artist:"inch nails"`, `track:"closer" artist:"nine inch nails"`,
		}}},

	{query: "stand by me",
		window: []gossiprpc.SpotifyTrack{
			{ID: "0", Name: "Stand By Me", Artists: []string{"Ben E. King"}, Album: "Don't Play That Song (Mono)"},
			searchTrack("1", "Stand By Me - Live at The Print Shop", "Stephen Wilson Jr."),
			searchTrack("2", "Stand By Me", "Ben E. King"),
			searchTrack("3", "Stand By Me (feat. Morgan Wallen)", "Lil Durk", "Morgan Wallen"),
			searchTrack("4", "Stand By Me", "Ben E. King"),
			searchTrack("5", "Stand By Me", "Florence + The Machine"),
			searchTrack("6", "Stand By Me", "Ben E. King"),
			searchTrack("7", "Stand By Me - Remastered", "Oasis"),
			searchTrack("8", "Stand By Me", "Billy Fury"),
			searchTrack("9", "Fall On My Knees", "Abigail Washburn"),
		},
		filtered: map[string][]gossiprpc.SpotifyTrack{`track:"stand" artist:"me"`: {searchTrack("wrong", "Stand By Me", "Sons of Legion")}},
		want:     spotifyWindowOutcome{pick: "0", queries: []string{"stand by me"}}},
	{query: "stand by me ben e king",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("0", "Stand By Me", "Ben E. King"),
			searchTrack("1", "Stand By Me", "Ben E. King"),
			searchTrack("2", "Stand By Me", "Ben E. King"),
			searchTrack("3", "Stand By Me", "Ben E. King"),
			searchTrack("4", "Stand by Me", "Karizma Duo"),
			searchTrack("5", "Destiny", "Ben"),
			searchTrack("6", "C’est La Vie (with bbno$ & Rich Brian)", "Yung Gravy", "bbno$", "Rich Brian"),
			searchTrack("7", "M.E.N III", "Bugzy Malone"),
			searchTrack("8", "PAPER THIN", "ScoobE", "Nvm"),
			searchTrack("9", "Day Off", "Rob Niece"),
		},
		filtered: map[string][]gossiprpc.SpotifyTrack{`track:"stand" artist:"me ben e king"`: {searchTrack("wrong", "Stand By Me", "Ben E. King", "Polski Chór Męski", "Piotr Jańczak")}},
		want:     spotifyWindowOutcome{pick: "0", queries: []string{"stand by me ben e king"}}},
	{query: "satisfaction rolling stones",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("0", "(I Can't Get No) Satisfaction - Mono", "The Rolling Stones"),
			searchTrack("1", "(I Can't Get No) Satisfaction - Saturday Club / 1965", "The Rolling Stones"),
			searchTrack("2", "Satisfaction", "David Guetta", "Benny Benassi"),
			searchTrack("3", "Satisfaction - Isak original", "Benny Benassi"),
			searchTrack("4", "(I Can't Get No) Satisfaction - Mono", "The Rolling Stones"),
			searchTrack("5", "Satisfaction Skank", "Fatboy Slim", "The Rolling Stones"),
			searchTrack("6", "(I Can't Get No) Satisfaction - Mono", "The Rolling Stones"),
			searchTrack("7", "Don't Rock the Jukebox", "Alan Jackson"),
			searchTrack("8", "MANON B", "Tiakola"),
			searchTrack("9", "Rolling Stone", "Hamza"),
		},
		want: spotifyWindowOutcome{pick: "0", queries: []string{"satisfaction rolling stones", "satisfaction rolling stones offset:10", "track:\"satisfaction\" artist:\"rolling stones\""}}},
	{query: "numb linkn park",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("0", "Numb", "Linkin Park"),
			searchTrack("1", "Numb", "Linkin Park"),
			searchTrack("2", "Numb / Encore", "JAY-Z", "Linkin Park"),
			searchTrack("3", "Numb", "Linkin Park"),
			searchTrack("4", "Good Goodbye (feat. Pusha T and Stormzy)", "Linkin Park", "Pusha T", "Stormzy"),
			searchTrack("5", "Moana Lisa", "Only Fire"),
			searchTrack("6", "I Know I'm Funny haha", "Faye Webster"),
			searchTrack("7", "Midnight Crusade", "Dance Gavin Dance"),
			searchTrack("8", "2017", "Cold Hart", "GothBoiClique"),
			searchTrack("9", "Pain and Sorrow", "Evado"),
		},
		filtered: map[string][]gossiprpc.SpotifyTrack{`track:"numb" artist:"linkin park"`: {searchTrack("0", "Numb", "Linkin Park")}},
		want:     spotifyWindowOutcome{pick: "0", queries: []string{"numb linkn park", "track:\"numb\" artist:\"linkin park\""}}},
	{query: "everybody backstreet boys",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("0", "Everybody (Backstreet's Back) - Radio Edit", "Backstreet Boys"),
			searchTrack("1", "Everybody (Backstreet's Back) - Extended Version", "Backstreet Boys"),
			searchTrack("2", "Everybody (Backstreet's Back) - Extended Version", "Backstreet Boys"),
			searchTrack("3", "Everybody (Backstreet's Back) - Radio Edit", "Backstreet Boys"),
			searchTrack("4", "Everybody (Backstreet's Back)", "MICAH", "Perfect Pitch", "Rocco", "Toni Arcelinni"),
			searchTrack("5", "Get Down (You're the One for Me)", "Backstreet Boys"),
			searchTrack("6", "Get Down (You're the One for Me)", "Backstreet Boys"),
			searchTrack("7", "Boys (Summertime Love)", "Sabrina"),
			searchTrack("8", "Boys (Summertime Love)", "Sabrina"),
			searchTrack("9", "Boys (Summertime Love)", "Sabrina"),
		},
		want: spotifyWindowOutcome{pick: "0", queries: []string{"everybody backstreet boys", "everybody backstreet boys offset:10", "track:\"everybody\" artist:\"backstreet boys\""}}},
	{query: "blue eiffel 65",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("0", "Blue (Da Ba Dee) - Gabry Ponte Video Edit", "Eiffel 65", "Gabry Ponte"),
			searchTrack("1", "Blue (Da Ba Dee)", "I'm Blue"),
			searchTrack("2", "Blue (Da Ba Dee) - Gabry Ponte Ice Pop Radio", "Eiffel 65", "Gabry Ponte"),
			searchTrack("3", "Blue - Flume Remix", "Eiffel 65", "Flume"),
			searchTrack("4", "Blue (Da Ba Dee) - Gabry Ponte Ice Pop Radio", "Eiffel 65", "Gabry Ponte"),
			searchTrack("5", "Santa Baby", "Michael Bublé"),
			searchTrack("6", "Blue", "MDMC"),
			searchTrack("7", "Bettin' Man", "Riley Green"),
			searchTrack("8", "Country Boyz", "Nappy Roots"),
			searchTrack("9", "Stoned On Ocean", "Curren$y"),
		},
		want: spotifyWindowOutcome{pick: "0", queries: []string{"blue eiffel 65", "blue eiffel 65 offset:10", "track:\"blue\" artist:\"eiffel 65\""}}},
	{query: "hello live adele",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("0", "Hello", "Adele"),
			searchTrack("1", "Hello", "Adele"),
			searchTrack("2", "Hello - Live", "Hello Adele Tribute"),
			searchTrack("3", "Hello (Acoustic) - Live", "Hello Adele Tribute"),
			searchTrack("4", "Rolling in the Deep - Live", "Hello Adele Tribute"),
			searchTrack("5", "hello¿", "TopOppGen"),
			searchTrack("6", "Hello - Live in Theatre", "The Daydreamers"),
			searchTrack("7", "hello¿", "TopOppGen"),
			searchTrack("8", "Don't Go", "Marnz Malone"),
			searchTrack("9", "Barz Simpson", "Sonnyjim", "The Purist", "MF DOOM", "J A Y E L E C T R O N I C A"),
		},
		want: spotifyWindowOutcome{pick: "", queries: []string{"hello live adele", "track:\"hello live\" artist:\"adele\"", "track:\"hello\" artist:\"live adele\"", "track:\"live adele\" artist:\"hello\""}}},
	{query: "mr brightside acoustic the killers",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("0", "Mr. Brightside", "The Killers"),
			searchTrack("1", "Mr. Brightside - Live At Connect / 2004", "The Killers"),
			searchTrack("2", "Mr Brightside - Acoustic", "Matt Johnson"),
			searchTrack("3", "Mr. Brightside (Acoustic Cover)", "Cloud Illusions"),
			searchTrack("4", "Mr. Brightside - Live From The Royal Albert Hall / 2009", "The Killers"),
			searchTrack("5", "Mr Brightside", "Acoustic Sessions"),
			searchTrack("6", "Mr. Brightside (Originally Performed by The Killers) - Acoustic Guitar Karaoke", "Sing2Guitar"),
			searchTrack("7", "So Far...", "Eminem"),
			searchTrack("8", "2 Minutes To Midnight - Live '01", "Iron Maiden"),
			searchTrack("9", "California", "Colonel Loud", "T.I.", "Young Dolph", "Ricco Barrino"),
		},
		want: spotifyWindowOutcome{pick: "", queries: []string{"mr brightside acoustic the killers", "track:\"mr brightside acoustic the\" artist:\"the killers\"", "mr brightside acoustic the the killers", "track:\"mr brightside - acoustic\" artist:\"the killers\""}}},
	{query: "sunflower post malone swae lee",
		window: []gossiprpc.SpotifyTrack{
			searchTrack("0", "Sunflower - Spider-Man: Into the Spider-Verse", "Post Malone", "Swae Lee"),
			searchTrack("1", "Sunflower (Remix) [with Swae Lee, Nicky Jam, and Prince Royce]", "Post Malone", "Swae Lee", "Nicky Jam", "Prince Royce", "G.O.K.B."),
			searchTrack("2", "Sunflower - Spider-Man: Into the Spider-Verse", "Post Malone", "Swae Lee"),
			searchTrack("3", "Sunflower - Spider-Man: Into the Spider-Verse", "Post Malone", "Swae Lee"),
			searchTrack("4", "Sunflower - Spider-Man: Into the Spider-Verse", "Post Malone", "Swae Lee"),
			searchTrack("5", "Sunflower", "Sunflower"),
			searchTrack("6", "Sunflower", "Sunflower"),
			searchTrack("7", "Buckets (feat. Future) - From SR3MM", "Rae Sremmurd", "Swae Lee", "Slim Jxmmi", "Future"),
			searchTrack("8", "Por Nosotros", "Delaossa", "KIDDO", "Gese Da O"),
			searchTrack("9", "Dogecoin - Freestyle", "Ishmael", "Sonnyb"),
		},
		want: spotifyWindowOutcome{pick: "0", queries: []string{"sunflower post malone swae lee", "sunflower post malone swae lee offset:10", "track:\"sunflower\" artist:\"post malone swae lee\""}}},
}
