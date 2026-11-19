// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
)

type matcherCase struct {
	name  string
	input chatSearchCase
}

type matcherName string
type matcherQuery string
type matcherID string
type matcherCredit string

func pickIn(name matcherName, query matcherQuery, id matcherID, tracks ...gossiprpc.SpotifyTrack) matcherCase {
	return matcherCase{string(name), chatSearchCase{query: string(query), window: tracks, wantID: string(id)}}
}

func rejectTrack(name matcherName, query matcherQuery, title queryField, credits ...string) matcherCase {
	return pickIn(name, query, "", searchTrack("wrong", string(title), credits...))
}

func namedTrack(name matcherName, query matcherQuery, title queryField, credits matcherCredit) matcherCase {
	return matcherCase{string(name), chatTrack(string(query), string(title), string(credits))}
}

func retryTrack(name matcherName, query matcherQuery, lookup queryField, first gossiprpc.SpotifyTrack) matcherRetry {
	return matcherRetry{test: pickIn(name, query, "", first), lookup: string(lookup)}
}

type matcherRetry struct {
	test   matcherCase
	lookup string
}

func (r matcherRetry) finds(track gossiprpc.SpotifyTrack) matcherCase {
	r.test.input.byQuery = map[string][]gossiprpc.SpotifyTrack{r.lookup: {track}}
	r.test.input.wantQueries = []string{r.test.input.query, r.lookup}
	r.test.input.wantID = track.ID
	return r.test
}

func filteredTrack(song, artist queryField) queryField {
	return `track:"` + song + `" artist:"` + artist + `"`
}

func titleEditRetry(query matcherQuery, song, artist queryField, first gossiprpc.SpotifyTrack) matcherCase {
	return retryTrack("searchable retry", query, filteredTrack(song, artist), first).
		finds(searchTrack("plain", strings.Split(first.Name, " - ")[0], first.Artists...))
}

func partialArtist(query matcherQuery, title queryField, credit matcherCredit) matcherCase {
	track := searchTrack("right", string(title), string(credit))
	return retryTrack("partial credit", query, filteredTrack(queryField(strings.ToLower(string(title))), queryField(strings.ToLower(string(credit)))), track).finds(track)
}

func TestSearchMatcherRegressions(t *testing.T) {
	groups := [][]matcherCase{
		versionSafetyRegressions(), rankingRegressions(), subtitleRegressions(),
		artistRegressions(), artistConfirmationRegressions(), retryRegressions(), queryRegressions(), catalogRegressions(),
		arrangerRegressions(), countedTitleRegressions(), recoveryRoundTwoRegressions(), cleanupRoundTwoRegressions(),
	}
	for _, group := range groups {
		runMatcherCases(t, group)
	}
}

func runMatcherCases(t *testing.T, tests []matcherCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.name+"/"+tc.input.query, func(t *testing.T) {
			want := chatSearchOutcome{queries: tc.input.wantQueries}
			if tc.input.wantID != "" {
				want.ids = []string{tc.input.wantID}
			}
			assert.Equal(t, want, runChatSearch(t, tc.input))
		})
	}
}

func versionSafetyRegressions() []matcherCase {
	return []matcherCase{
		rejectTrack("short marker is exact", "baby love justin bieber", "Baby - Live", "Justin Bieber"),
		rejectTrack("short marker is exact", "sweet love anita baker", "Sweet - Live", "Anita Baker"),
		rejectTrack("short marker is exact", "crazy love", "Crazy - Live", "Gnarls Barkley"),
		rejectTrack("short marker is exact", "jurassic park", "Jurassic - Part 1", "Other"),
		rejectTrack("changed start is not a version typo", "river flowed", "River - Slowed", "Other"),
		pickIn("no live typo", "hello love adele", "", helloLive, searchTrack("lionel", "Hello", "Lionel Richie")),
		pickIn("no live typo", "hello lie", "", helloLive, searchTrack("lionel", "Hello", "Lionel Richie")),
		namedTrack("position in title", "piano man billy joel", "Piano Man", "Billy Joel"),
		namedTrack("position in title", "live forever oasis", "Live Forever", "Oasis"),
		namedTrack("position in title", "acoustic soul india arie", "Acoustic Soul", "India.Arie"),
		namedTrack("position in title", "cover me bruce springsteen", "Cover Me", "Bruce Springsteen"),
		namedTrack("literal named ensemble", "love rollercoaster ohio players", "Love Rollercoaster", "Ohio Players"),
		namedTrack("requested derivative recording", "hello karaoke", "Hello (Karaoke)", "Karaoke Kings"),
		rejectTrack("short cover typo", "hello coverr", "Hello (Cover)", "Other"),
		namedTrack("position in title", "part of me", "Part of Me", "Katy Perry"),
		namedTrack("position in title", "mix tape", "Mix Tape", "Brand New"),
		namedTrack("position in title", "remix of life", "Remix of Life", "Other"),
		namedTrack("long version typo", "hello acustic adele", "Hello (Acoustic)", "Adele"),
		namedTrack("trailing version typo", "hello adele acustic", "Hello (Acoustic)", "Adele"),
		pickIn("exact title beats version typo", "hello acustic", "literal", searchTrack("version", "Hello (Acoustic)", "Adele"), searchTrack("literal", "Hello Acustic", "Other")),
		pickIn("long version typo", "despacito remixx luis fonsi", "remix", despacitoRemix),
		namedTrack("speed effect qualifier", "love story sped up taylor swift", "Love Story - Sped Up + Reverb", "Taylor Swift"),
		rejectTrack("no extra effect", "love story live taylor swift", "Love Story - Live + Reverb", "Taylor Swift"),
		rejectTrack("different version", "hello intro adele", "Hello - Outro", "Adele"),
	}
}

func rankingRegressions() []matcherCase {
	prince := searchTrack("prince", "Purple Rain", "Prince")
	rain := searchTrack("rain", "Purple", "Rain")
	original := searchTrack("original", "Love Story", "Taylor Swift")
	rerecord := searchTrack("rerecord", "Love Story (Taylor's Version)", "Taylor Swift")
	subtitle := searchTrack("subtitle", "Blue (Da Ba Dee)", "Eiffel 65")
	plain := searchTrack("plain", "Blue", "Eiffel 65")
	wembley := searchTrack("wembley", "Hello - Live at Wembley", "Adele")
	royal := searchTrack("royal", "Hello - Live at Royal Albert Hall", "Adele")
	return []matcherCase{
		pickIn("literal first", "purple rain", "prince", prince, rain),
		pickIn("split first", "purple rain", "rain", rain, prince),
		pickIn("literal first", "paint it black", "stones", searchTrack("stones", "Paint It Black", "The Rolling Stones"), searchTrack("split", "Paint It", "Black")),
		pickIn("first exact original", "hello", "lionel", searchTrack("lionel", "Hello", "Lionel Richie"), helloOriginal),
		pickIn("natural re-record first", "love story taylor swift", "rerecord", rerecord, original),
		pickIn("original first", "love story taylor swift", "original", original, rerecord),
		pickIn("subtitle first", "blue eiffel 65", "subtitle", subtitle, plain),
		pickIn("plain first", "blue eiffel 65", "plain", plain, subtitle),
		pickIn("live first", "hello live adele", "wembley", wembley, royal),
		pickIn("venue specified", "hello live at royal albert hall adele", "royal", wembley, royal),
		pickIn("arrangement credit loses", "hello", "hello", searchTrack("piano", "Hello", "The Piano Guys"), helloOriginal),
		pickIn("literal guest credit", "hello live", "duet", helloLive, searchTrack("duet", "Hello", "Adele", "Live")),
	}
}

func subtitleRegressions() []matcherCase {
	return []matcherCase{
		namedTrack("unknown parenthetical", "rocket man elton john", "Rocket Man (I Think It's Going to Be a Long, Long Time)", "Elton John"),
		retryTrack("plain beats unknown dash edition", "sunflower post malone", filteredTrack("sunflower", "post malone"),
			searchTrack("sun", "Sunflower - Spider-Man: Into the Spider-Verse", "Post Malone", "Swae Lee")).
			finds(searchTrack("plain", "Sunflower", "Post Malone", "Swae Lee")),
		pickIn("title-only unknown dash edition", "sunflower", "sun", searchTrack("sun", "Sunflower - Spider-Man: Into the Spider-Verse", "Post Malone", "Swae Lee")),
		namedTrack("unknown parenthetical", "single ladies beyonce", "Single Ladies (Put a Ring on It)", "Beyoncé"),
		namedTrack("unknown parenthetical", "i wanna dance with somebody whitney houston", "I Wanna Dance with Somebody (Who Loves Me)", "Whitney Houston"),
		namedTrack("unknown parenthetical", "drops of jupiter train", "Drops of Jupiter (Tell Me)", "Train"),
		namedTrack("unknown parenthetical", "blue eiffel 65", "Blue (Da Ba Dee)", "Eiffel 65"),
		namedTrack("unknown parenthetical", "everybody backstreet boys", "Everybody (Backstreet's Back)", "Backstreet Boys"),
		namedTrack("unknown parenthetical", "last friday night katy perry", "Last Friday Night (T.G.I.F.)", "Katy Perry"),
		namedTrack("unknown parenthetical", "good riddance green day", "Good Riddance (Time of Your Life)", "Green Day"),
		namedTrack("verified alternate title", "time of your life green day", "Good Riddance (Time of Your Life)", "Green Day"),
		namedTrack("title-only base", "drops of jupiter", "Drops of Jupiter (Tell Me)", "Train"),
		namedTrack("release year", "hotel california eagles", "Hotel California - 2013 Remaster", "Eagles"),
		namedTrack("Spotify bilingual title", "spring day bts", "봄날 (Spring Day)", "BTS"),
		namedTrack("Spotify romanized title", "bomnal bts", "봄날 (Bomnal)", "BTS"),
		namedTrack("Spotify original script", "봄날 bts", "봄날 (Spring Day)", "BTS"),
	}
}

func artistRegressions() []matcherCase {
	return []matcherCase{
		rejectTrack("different artist", "uprising muse", "Uprising", "Mose"),
		rejectTrack("different artist", "crazy seal", "Crazy", "Sean"),
		rejectTrack("different artist", "tik tok kesha", "Tik Tok", "Keisha"),
		rejectTrack("different artist", "bohemian rhapsody queen", "Bohemian Rhapsody", "Queens"),
		rejectTrack("different artist", "halo beyonce", "Halo", "Beyonca"),
		retryTrack("unique closest credit on confirmation", "hello adel", filteredTrack("hello", "adele"), helloOriginal).
			finds(helloOriginal).withFirstWindow(searchTrack("abel", "Hello", "Abel"), helloOriginal),
		retryTrack("closest title and credit on retry", "hello adel", filteredTrack("hello", "adel"),
			searchTrack("other", "Skina", "Adel")).finds(helloOriginal).withFirstWindow(helloOriginal, searchTrack("other", "Skina", "Adel")),
		pickIn("tied closest credits", "hello ade", "", searchTrack("aden", "Hello", "Aden"), searchTrack("adel", "Hello", "Adel")),
		partialArtist("humble kendrick", "Humble", "Kendrick Lamar"),
		partialArtist("7 rings ariana", "7 Rings", "Ariana Grande"),
		partialArtist("levitating dua", "Levitating", "Dua Lipa"),
		pickIn("exact artist beats partial", "home drake", "drake", searchTrack("nick", "Home", "Nick Drake"), searchTrack("drake", "Home", "Drake")),
		pickIn("multiple partial credits", "home drake", "", searchTrack("nick", "Home", "Nick Drake"), searchTrack("other", "Home", "Drake Bell")),
		partialConfirmationConflict(),
		pickIn("known credit blocks a fuzzy collaborator", "stay rihana and mikky ekko", "", searchTrack("near", "Stay", "Rihanna", "Mikky Ekko"), searchTrack("known", "Other Song", "Rihana")),
		rejectTrack("partial credit cannot be derivative", "humble kendrick", "Humble", "Tribute to Kendrick Lamar"),
		pickIn("three implicit credits", "despacito remix daddy yankee justin bieber luis fonsi", "remix", despacitoOriginal, despacitoRemix),
		pickIn("explicit feature in credits", "despacito luis fonsi feat justin bieber", "remix", despacitoOriginal, despacitoRemix),
		pickIn("feature in title metadata", "despacito luis fonsi feat justin bieber", "feat", searchTrack("feat", "Despacito (feat. Justin Bieber)", "Luis Fonsi")),
	}
}

func artistConfirmationRegressions() []matcherCase {
	tribute := searchTrack("tribute", "Love Story", "The Taylor Swift Experience")
	known := searchTrack("known", "Other Song", "Taylor Swift")
	lookup := filteredTrack("love story", "the taylor swift experience")
	return []matcherCase{
		pickIn("empty partial confirmation", "love story taylor swift", "", tribute).
			withQueries("love story taylor swift", string(lookup), string(filteredTrack("love story", "taylor swift"))),
		pickIn("complete artist credit on another title", "love story taylor swift", "", tribute, known),
		retryTrack("partial confirmation returns another title", "love story taylor swift", lookup, tribute).
			finds(searchTrack("wrong", "Other Song", "The Taylor Swift Experience")).withoutPick().
			withQueries("love story taylor swift", string(lookup), string(filteredTrack("love story", "taylor swift"))),
		partialArtist("bad romance gaga", "Bad Romance", "Lady Gaga"),
		partialArtist("espresso sabrina", "Espresso", "Sabrina Carpenter"),
		pickIn("empty fuzzy artist confirmation", "hello adel", "", helloOriginal),
		retryTrack("fuzzy confirmation returns competing credits", "hello adel", filteredTrack("hello", "adele"), helloOriginal).
			finds(searchTrack("wrong", "Hello", "Adele", "Adelh")).withoutPick().
			withQueries("hello adel", string(filteredTrack("hello", "adele")), string(filteredTrack("hello", "adel")), "hello adele"),
	}
}

func partialConfirmationConflict() matcherCase {
	first := searchTrack("first", "Bad Romance", "Lady Gaga")
	tc := pickIn("confirmation finds ambiguous credits", "bad romance gaga", "", first)
	tc.input.byQuery = map[string][]gossiprpc.SpotifyTrack{
		`track:"bad romance" artist:"lady gaga"`: {first, searchTrack("other", "Bad Romance", "Gaga Duo")},
	}
	return tc
}

func retryRegressions() []matcherCase {
	green := searchTrack("green", "Good Riddance (Time of Your Life)", "Green Day")
	taylor := retryTrack("original title first", "good riddance green day", filteredTrack("good riddance", "green day"),
		searchTrack("fuzzy", "Good Riddancer", "Green Day")).finds(green)
	return []matcherCase{
		titleEditRetry("99 problems jay z", "99 problems", "jay z", searchTrack("radio", "99 Problems - Radio Edit", "JAY-Z")),
		titleEditRetry("7/11 beyonce", "7 11", "beyonce", searchTrack("radio", "7/11 - Radio Edit", "Beyoncé")),
		titleEditRetry("simon & garfunkel the boxer", "the boxer", "simon garfunkel", searchTrack("radio", "The Boxer - Radio Edit", "Simon & Garfunkel")),
		titleEditRetry("blue eiffel 65", "blue", "eiffel 65", searchTrack("radio", "Blue - Radio Edit", "Eiffel 65")),
		titleEditRetry("faded alan walker", "faded", "alan walker", searchTrack("radio", "Faded - Radio Edit", "Alan Walker")),
		titleEditRetry("lucid dreams juice wrld", "lucid dreams", "juice wrld", searchTrack("clean", "Lucid Dreams - Clean", "Juice WRLD")),
		retryTrack("exact credit first", "stay future rihanna", filteredTrack("stay", "future rihanna"), searchTrack("islands", "Stay", "Future Islands", "Rihanna")).
			finds(searchTrack("future", "Stay", "Future", "Rihanna")),
		taylor,
		retryTrack("original beats mono", "satisfaction rolling stones", filteredTrack("satisfaction", "rolling stones"), searchTrack("mono", "(I Can't Get No) Satisfaction - Mono Version", "The Rolling Stones")).
			finds(searchTrack("plain", "(I Can't Get No) Satisfaction", "The Rolling Stones")),
		retryTrack("untried raw split", "pink venom blackpink", filteredTrack("pink venom", "blackpink"), searchTrack("unrelated", "Other", "Nobody")).
			finds(searchTrack("right", "Pink Venom", "BLACKPINK")),
		typoConfirmation("helo adele", titleTypo{"helo", "hello", "adele"}, helloOriginal),
		typoConfirmation("fadedd alan walker", titleTypo{"fadedd", "faded", "alan walker"}, searchTrack("right", "Faded", "Alan Walker")),
		typoConfirmation("shalow lady gaga", titleTypo{"shalow", "shallow", "lady gaga"}, searchTrack("right", "Shallow", "Lady Gaga")),
		typoConfirmation("cloer the chainsmokers", titleTypo{"cloer", "closer", "the chainsmokers"}, searchTrack("right", "Closer", "The Chainsmokers")),
	}
}

type titleTypo struct{ input, corrected, artist queryField }

func typoConfirmation(query matcherQuery, typo titleTypo, track gossiprpc.SpotifyTrack) matcherCase {
	corrected := filteredTrack(typo.corrected, typo.artist)
	tc := retryTrack("typo verification", query, corrected, track).finds(track)
	tc.input.wantQueries = []string{string(query), string(filteredTrack(typo.input, typo.artist)), string(corrected)}
	return tc
}

func queryRegressions() []matcherCase {
	var tests []matcherCase
	for _, query := range []string{
		"@streamer hello adele", "hello adele @mod", "hello adele https://youtu.be/x", "https://youtu.be/x hello adele",
		"hello adele <3", "!request hello adele", "hello adele LUL", "hello adele CheerEmote",
		"Adele – Hello", "Adele — Hello", "please play Adele - Hello",
	} {
		tests = append(tests, pickIn("chat input", matcherQuery(query), "hello", helloOriginal))
	}
	return append(tests,
		pickIn("year filter retains title", "Halo year:2008", "halo", searchTrack("wrong", "Ave Maria", "Beyoncé"), searchTrack("halo", "Halo", "Beyoncé")),
		pickIn("artist filter retains title", `artist:"Adele" Hello`, "hello", searchTrack("wrong", "Easy on Me", "Adele"), helloOriginal),
		pickIn("explicit track filter retains residual title", `track:stand by me`, "ben", searchTrack("wrong", "Stand", "Other"), searchTrack("ben", "Stand by Me", "Ben E. King")),
	)
}

func catalogRegressions() []matcherCase {
	album := searchTrack("wrong", "Hello", "Other")
	album.Album = "Songs made famous by Adele"
	window := make([]gossiprpc.SpotifyTrack, maxSearchLimit)
	for i := range window {
		window[i] = searchTrack(fmt.Sprint(i), "Hello - Live", "Adele")
	}
	secondPage := retryTrack("second page", "hello adele", "hello adele offset:10", window[0]).finds(helloOriginal)
	secondPage.input.window = window
	return []matcherCase{
		secondPage,
		pickIn("derivative album", "hello", "hello", album, helloOriginal),
		rejectTrack("Japanese tail substring", "hello adele", "Hello - 東京ライブ公演", "Adele"),
		rejectTrack("two Japanese version substrings", "hello ライブ adele", "Hello (東京ライブリミックス)", "Adele"),
		namedTrack("both Japanese versions requested", "hello ライブリミックス adele", "Hello (東京ライブリミックス)", "Adele"),
		rejectTrack("Korean tail substring", "hello adele", "Hello [서울라이브공연]", "Adele"),
		rejectTrack("Portuguese live", "hello adele", "Hello – Ao Vivo", "Adele"),
		rejectTrack("Russian remix", "hello adele", "Hello — ремикс", "Adele"),
		rejectTrack("French acoustic", "hello adele", "Hello (Acoustique)", "Adele"),
	}
}

func TestSearchISRCReleasePreference(t *testing.T) {
	window := []trackItem{
		{ID: "compilation", Name: "Hello"},
		{ID: "different", Name: "Hello"},
		{ID: "album", Name: "Hello"},
	}
	window[0].ExternalIDs.ISRC, window[2].ExternalIDs.ISRC = "same", "same"
	window[0].Album.Type, window[2].Album.Type = "compilation", "album"
	p := newPlayerProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		assert.NoError(t, codec.NewEncoder(w).Encode(map[string]any{"tracks": map[string]any{"items": window}}))
	}))
	got := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: "hello", Limit: 3})
	assert.Equal(t, []string{"album", "different"}, searchIDs(got))
}

func TestSearchCacheDistinguishesMatcherUnicode(t *testing.T) {
	window := []gossiprpc.SpotifyTrack{searchTrack("upper", "Ϲ", "Adele"), searchTrack("lower", "ϲ", "Adele")}
	var calls int
	p := newPlayerProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		writeSearchTracks(t, w, window...)
	}))
	type outcome struct {
		ids   []string
		calls int
	}
	for _, tc := range []struct {
		query string
		want  outcome
	}{{"Ϲ Adele", outcome{[]string{"upper"}, 1}}, {"ϲ Adele", outcome{[]string{"lower"}, 2}}} {
		got := providertest.Call[gossiprpc.SpotifySearchReply](t, p, "search", gossiprpc.Request{ChannelID: "2", Query: tc.query, Limit: 1})
		assert.Equal(t, tc.want, outcome{searchIDs(got), calls})
	}
}

func arrangerRegressions() []matcherCase {
	elo := searchTrack("elo", "Mr. Blue Sky", "Electric Light Orchestra")
	ohio := searchTrack("ohio", "Love Rollercoaster", "Ohio Players")
	hendrix := searchTrack("hendrix", "Purple Haze", "The Jimi Hendrix Experience")
	return []matcherCase{
		pickIn("only eligible original", "mr blue sky", "elo", elo, searchTrack("wrong", "Mr. Blue Sky", "Karaoke Hits Band")),
		retryTrack("unique partial with arranger credit", "mr blue sky electric light", filteredTrack("mr blue sky", "electric light orchestra"), elo).finds(elo),
		retryTrack("artist initials with arranger credit", "mr blue sky elo", filteredTrack("mr blue sky", "electric light orchestra"), elo).finds(elo),
		namedTrack("orchestral in credit", "enola gay", "Enola Gay - Remastered 2003", "Orchestral Manoeuvres In The Dark"),
		namedTrack("orchestra in credit", "wizards in winter trans siberian orchestra", "Wizards in Winter", "Trans-Siberian Orchestra"),
		pickIn("players in credit", "love rollercoaster", "ohio", ohio, searchTrack("cover", "Love Rollercoaster", "Red Hot Chili Peppers")),
		partialArtist("love rollercoaster ohio", "Love Rollercoaster", "Ohio Players"),
		namedTrack("strings in credit", "dust in a baggie", "Dust in a Baggie", "Billy Strings"),
		namedTrack("cover in credit", "show me the cover girls", "Show Me", "The Cover Girls"),
		retryTrack("experience in partial credit", "purple haze jimi hendrix", filteredTrack("purple haze", "the jimi hendrix experience"), hendrix).finds(hendrix),
		pickIn("named arranger credit", "hello piano guys", "piano", searchTrack("piano", "Hello", "The Piano Guys"), helloOriginal),
	}
}

func countedTitleRegressions() []matcherCase {
	one := searchTrack("part1", "Another Brick in the Wall, Pt. 1", "Pink Floyd")
	two := searchTrack("part2", "Another Brick in the Wall, Pt. 2", "Pink Floyd")
	dash := searchTrack("part2", "Another Brick in the Wall - Pt. 2", "Pink Floyd")
	john := searchTrack("original", "Take Me Home, Country Roads - Original Version", "John Denver")
	return []matcherCase{
		pickIn("requested part counts are distinct", "another brick in the wall part 2 pink floyd", "part2", one, two),
		pickIn("sole numbered song with comma", "another brick in the wall pink floyd", "part2", two),
		pickIn("sole numbered song with dash", "another brick in the wall pink floyd", "part2", dash),
		pickIn("two numbered songs need a part", "another brick in the wall pink floyd", "", one, dash),
		retryTrack("another part on retry blocks the first part", "another brick in the wall pink floyd",
			filteredTrack("another brick in the wall", "pink floyd"), one).finds(two).withoutPick(),
		rejectTrack("large part count is required", "i love you part adele", "I Love You - Part 100", "Adele"),
		namedTrack("part counts compare numerically", "i love you pt 00100 adele", "I Love You, Part 100", "Adele"),
		pickIn("original version is the original", "take me home country roads john denver", "original", john,
			searchTrack("cover", "Take Me Home, Country Roads", "Toots & The Maytals")),
	}
}

func recoveryRoundTwoRegressions() []matcherCase {
	live := searchTrack("live", "Honky Tonk Women - From Get Yer Ya-Ya's Out!", "The Rolling Stones")
	live.Album = "Get Yer Ya-Ya's Out! The Rolling Stones In Concert"
	studio := searchTrack("studio", "Honky Tonk Women", "The Rolling Stones")
	radio := searchTrack("radio", "Faded - Radio Edit", "Alan Walker")
	return []matcherCase{
		retryTrack("source album indicates live audio", "honky tonk women rolling stones",
			filteredTrack("honky tonk women", "rolling stones"), live).finds(studio),
		pickIn("ambiguous credits on one track", "espresso sabrina", "", searchTrack("wrong", "Espresso", "Sabrina Carpenter", "Sabrina Claudio")),
		pickIn("ambiguous credits reversed", "espresso sabrina", "", searchTrack("wrong", "Espresso", "Sabrina Claudio", "Sabrina Carpenter")),
		retryTrack("confirm fuzzy tie using narrowed window", "helo adele", filteredTrack("helo", "adele"),
			helloOriginal).finds(helloOriginal).withFirstWindow(helloOriginal, searchTrack("help", "Help", "Adele")),
		retryTrack("title-only radio edit checks for the original", "faded", `track:"faded"`, radio).finds(radio),
		pickIn("explicit radio alias", "faded radio alan walker", "radio", searchTrack("album", "Faded - Album Version", "Alan Walker"), radio),
		pickIn("first-letter version typo", "hello bcoustic adele", "acoustic", searchTrack("acoustic", "Hello - Acoustic", "Adele")),
	}
}

func cleanupRoundTwoRegressions() []matcherCase {
	bridge := searchTrack("bridge", "Under the Bridge", "Red Hot Chili Peppers")
	bridge.Album = "Under the Covers: Essential Red Hot Chili Peppers"
	karaoke := searchTrack("karaoke", "Hello (Karaoke Version)", "Sing King")
	karaoke.Album = "Hello (Karaoke Version)"
	return []matcherCase{
		pickIn("cover word in an otherwise ordinary album", "under the bridge red hot chili peppers", "bridge", bridge),
		pickIn("explicit karaoke and exact credit", "hello karaoke sing king", "karaoke", karaoke),
		retryTrack("single derivative prompts an original lookup", "hello", `track:"hello"`, karaoke).finds(helloOriginal),
		pickIn("trailing emote-shaped tokens", "hello KEKW PogChamp", "hello", helloOriginal),
		pickIn("one trailing caps-shaped token", "hello KEKW", "hello", helloOriginal),
		namedTrack("date in the exact title", "christmas eve sarajevo 12/24", "Christmas Eve/Sarajevo 12/24", "Trans-Siberian Orchestra"),
		pickIn("mixed chat text and Spotify URL", "hello adele https://open.spotify.com/track/3n3Ppam7vgaVa1aRUc9Lp", "hello", helloOriginal),
		namedTrack("acronym credit survives emote fallback", "dancing queen ABBA", "Dancing Queen", "ABBA"),
		namedTrack("acronym credit survives emote fallback", "kids MGMT", "Kids", "MGMT"),
		namedTrack("acronym credit survives emote fallback", "dynamite BTS", "Dynamite", "BTS"),
		namedTrack("capitalized title with punctuation", "HUMBLE.", "HUMBLE.", "Kendrick Lamar"),
		namedTrack("capitalized multiword title", "SICKO MODE", "SICKO MODE", "Travis Scott"),
	}
}

func (tc matcherCase) withFirstWindow(tracks ...gossiprpc.SpotifyTrack) matcherCase {
	tc.input.window = tracks
	return tc
}

func (tc matcherCase) withoutPick() matcherCase {
	tc.input.wantID = ""
	return tc
}

func (tc matcherCase) withQueries(queries ...string) matcherCase {
	tc.input.wantQueries = queries
	return tc
}

type searchBudgetCase struct {
	name, query, pick string
	calls             int
}

func TestSearchLongRequestsStayWithinBudget(t *testing.T) {
	credits := []string{"Christopher", "Christophar", "Christophel", "Christofher", "Christopder", "Christoptor", "Christopher Jr", "Christophes"}
	query := "Recording " + strings.Join(credits, " and ")
	query += strings.Repeat(".", 200-len(query))
	dense := "Recording " + strings.Repeat("u ", 94) + "uu"
	checkSearchBudget(t, recordingWindow(credits), []searchBudgetCase{
		{"confusable collaborators", query, "0", 1},
		{"dense unrelated artist", dense, "", 3},
	})

	var overlapping []string
	for i := range 8 {
		overlapping = append(overlapping, strings.Repeat("x ", 10+i)+"x")
	}
	checkSearchBudget(t, recordingWindow(overlapping), []searchBudgetCase{
		{"dense overlapping credits", "Recording nope " + strings.Repeat("x ", 92) + "x", "", 4},
	})
}

func recordingWindow(credits []string) []gossiprpc.SpotifyTrack {
	window := make([]gossiprpc.SpotifyTrack, maxSearchLimit)
	for i := range window {
		window[i] = searchTrack(fmt.Sprint(i), "Recording", credits...)
	}
	return window
}

func checkSearchBudget(t *testing.T, window []gossiprpc.SpotifyTrack, cases []searchBudgetCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := chatSearchCase{query: tc.query, window: window, wantQueries: []string{}}
			got := runChatSearch(t, input)
			want := searchBudgetOutcome{pick: tc.pick, calls: tc.calls}
			assert.Equal(t, want, searchBudgetOutcome{pick: firstSearchID(got.ids), calls: len(got.queries), err: got.err})
		})
	}
}

type searchBudgetOutcome struct {
	pick  string
	calls int
	err   string
}

func firstSearchID(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func searchIDs(reply gossiprpc.SpotifySearchReply) []string {
	var ids []string
	for _, track := range reply.Tracks {
		ids = append(ids, track.ID)
	}
	return ids
}
