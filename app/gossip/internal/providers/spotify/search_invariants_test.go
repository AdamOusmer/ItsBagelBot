// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode"
	"unicode/utf8"

	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"golang.org/x/text/unicode/norm"
)

type catalogSong struct {
	track        gossiprpc.SpotifyTrack
	request      bool
	requestTitle string
}

func catalogOriginal(id, title string, artists ...string) catalogSong {
	return catalogSong{track: searchTrack(id, title, artists...), request: true}
}

func catalogOriginalWithTail(id, title, base, artist string) catalogSong {
	entry := catalogOriginal(id, title, artist)
	entry.requestTitle = base
	return entry
}

func catalogRecording(id, title string, artists ...string) catalogSong {
	return catalogSong{track: searchTrack(id, title, artists...)}
}

var invariantCatalog = []catalogSong{
	catalogOriginal("hello-adele", "Hello", "Adele"),
	catalogOriginal("hello-lionel", "Hello", "Lionel Richie"),
	catalogOriginal("stay-rihanna", "Stay", "Rihanna", "Mikky Ekko"),
	catalogOriginal("stay-laroi", "Stay", "The Kid LAROI", "Justin Bieber"),
	catalogOriginal("closer-chainsmokers", "Closer", "The Chainsmokers", "Halsey"),
	catalogOriginal("closer-nin", "Closer", "Nine Inch Nails"),
	catalogOriginal("faded-walker", "Faded", "Alan Walker"),
	catalogOriginal("faded-zhu", "Faded", "ZHU"),
	catalogOriginal("believer-dragons", "Believer", "Imagine Dragons"),
	catalogOriginal("believer-ozzy", "Believer", "Ozzy Osbourne"),
	catalogOriginal("twentytwo", "22", "Taylor Swift"),
	catalogOriginal("seven-rings", "7 rings", "Ariana Grande"),
	catalogOriginal("idol", "アイドル", "YOASOBI"),
	catalogOriginal("yoru", "夜に駆ける", "YOASOBI"),
	catalogOriginal("gruppa", "Группа крови", "Кино"),
	catalogOriginal("despacito", "Despacito", "Luis Fonsi", "Daddy Yankee"),
	catalogOriginal("bohemian", "Bohemian Rhapsody", "Queen"),
	catalogOriginal("brightside", "Mr. Brightside", "The Killers"),
	catalogOriginal("blinding", "Blinding Lights", "The Weeknd"),
	catalogOriginal("shallow", "Shallow", "Lady Gaga", "Bradley Cooper"),
	catalogOriginal("halo", "Halo", "Beyoncé"),
	catalogOriginal("dakiti", "DÁKITI", "Bad Bunny", "Jhay Cortez"),
	catalogOriginal("levitating", "Levitating", "Dua Lipa", "DaBaby"),
	catalogOriginal("something", "Something Just Like This", "The Chainsmokers", "Coldplay"),
	catalogOriginal("rolling", "Rolling in the Deep", "Adele"),
	catalogOriginal("blue-sky", "Mr. Blue Sky", "Electric Light Orchestra"),
	catalogOriginalWithTail("brick-2", "Another Brick in the Wall, Pt. 2", "Another Brick in the Wall Part 2", "Pink Floyd"),
	catalogOriginalWithTail("country-roads", "Take Me Home, Country Roads - Original Version", "Take Me Home, Country Roads", "John Denver"),
	catalogRecording("brick-1", "Another Brick in the Wall, Pt. 1", "Pink Floyd"),
	catalogRecording("hello-adele-live", "Hello - Live at the Church Studios", "Adele"),
	catalogRecording("believer-remix", "Believer - Kaskade Remix", "Imagine Dragons"),
	catalogRecording("blinding-slowed", "Blinding Lights - Slowed + Reverb", "The Weeknd"),
	catalogRecording("faded-nightcore", "Faded", "Nightcore"),
	catalogRecording("halo-karaoke", "Halo (Karaoke Version)", "Sing King"),
}

type placedTrack struct {
	track gossiprpc.SpotifyTrack
	at    int
}

type searchWorld struct {
	catalog []gossiprpc.SpotifyTrack
	placed  []placedTrack
}

func baseWorld() searchWorld {
	world := searchWorld{}
	for _, entry := range invariantCatalog {
		world.catalog = append(world.catalog, entry.track)
	}
	return world
}

func (w searchWorld) sampled(seed uint64) searchWorld {
	rng := rand.New(rand.NewPCG(seed, seed>>1|1))
	w.catalog = slices.DeleteFunc(slices.Clone(w.catalog), func(gossiprpc.SpotifyTrack) bool { return rng.IntN(4) == 0 })
	rng.Shuffle(len(w.catalog), func(i, j int) {
		w.catalog[i], w.catalog[j] = w.catalog[j], w.catalog[i]
	})
	return w
}

func (w searchWorld) with(placed ...placedTrack) searchWorld {
	w.placed = append(slices.Clone(w.placed), placed...)
	return w
}

func (w searchWorld) without(id string) searchWorld {
	w.catalog = slices.DeleteFunc(slices.Clone(w.catalog), func(t gossiprpc.SpotifyTrack) bool { return t.ID == id })
	w.placed = slices.DeleteFunc(slices.Clone(w.placed), func(p placedTrack) bool { return p.track.ID == id })
	return w
}

func (w searchWorld) window(q string) []gossiprpc.SpotifyTrack {
	words := invariantFoldedWords(strings.NewReplacer("track:", " ", "artist:", " ").Replace(q))
	type hit struct {
		track gossiprpc.SpotifyTrack
		score int
	}
	var hits []hit
	for _, track := range w.catalog {
		if score := catalogOverlap(words, trackWords(track)); score > 0 {
			hits = append(hits, hit{track, score})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return b.score - a.score })
	window := make([]gossiprpc.SpotifyTrack, 0, maxSearchLimit)
	for _, h := range hits[:min(len(hits), maxSearchLimit)] {
		window = append(window, h.track)
	}
	for _, p := range w.placed {
		window = slices.Insert(window, min(p.at, len(window)), p.track)
	}
	return window[:min(len(window), maxSearchLimit)]
}

func catalogOverlap(query, track []string) int {
	score := 0
	for _, word := range query {
		if slices.ContainsFunc(track, func(t string) bool { return spellingClose(word, t) }) {
			score++
		}
	}
	return score
}

func spellingClose(a, b string) bool {
	if a == b {
		return true
	}
	return utf8.RuneCountInString(a) >= 4 && invariantEditDistance(a, b) <= 1
}

func invariantEditDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func invariantFoldedWords(s string) []string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(s) {
		switch {
		case unicode.IsMark(r), r == '\'', r == '’':
		case unicode.IsLetter(r), unicode.IsNumber(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Fields(b.String())
}

func trackWords(track gossiprpc.SpotifyTrack) []string {
	return invariantFoldedWords(track.Name + " " + strings.Join(track.Artists, " "))
}

type searchRun struct {
	query   string
	reply   gossiprpc.SpotifySearchReply
	queries []string
	served  map[string]bool
	other   bool
}

func (r searchRun) pick() string {
	if len(r.reply.Tracks) == 0 {
		return ""
	}
	return r.reply.Tracks[0].ID
}

func (r searchRun) String() string {
	return fmt.Sprintf("query=%q pick=%q error=%q spotify_q=%q", r.query, r.pick(), r.reply.Error, r.queries)
}

type searchHarness struct {
	p       provider.Provider
	mu      sync.Mutex
	t       *testing.T
	world   searchWorld
	run     *searchRun
	channel atomic.Int64
}

func newSearchHarness(tb testing.TB) *searchHarness {
	h := &searchHarness{}
	mint := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-1","token_type":"Bearer","expires_in":3600}`))
	})
	h.p = newTestProvider(tb, fakeKeys{key: "rt-1"}, http.HandlerFunc(h.serve), mint)
	return h
}

func (h *searchHarness) serve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.URL.Path != searchPath {
		h.run.other = true
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query().Get("q")
	window := h.world.window(q)
	h.run.queries = append(h.run.queries, q)
	for _, track := range window {
		h.run.served[track.ID] = true
	}
	writeSearchTracks(h.t, w, window...)
}

func (h *searchHarness) search(t *testing.T, world searchWorld, query string) searchRun {
	t.Helper()
	run := searchRun{query: query, served: map[string]bool{}}
	h.mu.Lock()
	h.t, h.world, h.run = t, world, &run
	h.mu.Unlock()
	channel := strconv.FormatInt(h.channel.Add(1), 10)
	reply := providertest.Call[gossiprpc.SpotifySearchReply](t, h.p, "search", gossiprpc.Request{ChannelID: channel, Query: query, Limit: 1})
	h.mu.Lock()
	defer h.mu.Unlock()
	run.reply = reply
	return run
}

var invariantVersionMarkers = map[string][]string{
	"live": {"live"}, "remix": {"remix", "remixed", "rmx"}, "remixed": {"remix", "remixed", "rmx"},
	"rmx": {"remix", "remixed", "rmx"}, "sped": {"sped", "speed"}, "slowed": {"slowed", "slow"},
	"reverb": {"reverb", "slowed"}, "nightcore": {"nightcore"}, "acoustic": {"acoustic"},
	"karaoke": {"karaoke"}, "instrumental": {"instrumental"}, "cover": {"cover", "covers"},
	"demo": {"demo"}, "extended": {"extended"}, "recorded": {"recorded", "rerecorded"},
	"rerecorded": {"recorded", "rerecorded"}, "cappella": {"cappella", "capella", "acapella"},
	"capella": {"cappella", "capella", "acapella"}, "acapella": {"cappella", "capella", "acapella"},
	"8d": {"8d"}, "daycore": {"daycore"},
}

var stylizedLetterForms = strings.NewReplacer("!", "i", "$", "s", "@", "a")

func markerRequested(marker string, query []string) bool {
	return slices.ContainsFunc(query, func(word string) bool {
		return slices.ContainsFunc(invariantVersionMarkers[marker], func(form string) bool { return markerSpelling(word, form) })
	})
}

func markerSpelling(word, form string) bool {
	if word == form {
		return true
	}
	return utf8.RuneCountInString(form) >= 4 && invariantEditDistance(word, form) <= 1
}

func unrequestedMarker(run searchRun) string {
	if len(run.reply.Tracks) == 0 {
		return ""
	}
	query := append(invariantFoldedWords(run.query), invariantFoldedWords(stylizedLetterForms.Replace(run.query))...)
	for _, word := range trackWords(run.reply.Tracks[0]) {
		if _, ok := invariantVersionMarkers[word]; ok && !markerRequested(word, query) {
			return word
		}
	}
	return ""
}

func (r searchRun) searchedOnly() bool   { return len(r.queries) > 0 && !r.other }
func (r searchRun) reachedSpotify() bool { return len(r.queries) > 0 || r.other }
func (r searchRun) failedOffline() bool  { return r.reply.Error != "" && !r.reachedSpotify() }
func (r searchRun) servedPick() bool     { return r.pick() == "" || r.served[r.pick()] }

func plainText(query string) bool {
	return strings.TrimSpace(query) != "" && !strings.ContainsAny(query, "/:")
}

var runInvariants = []struct {
	name  string
	holds func(searchRun) bool
}{
	{"reached spotify", func(r searchRun) bool { return !r.failedOffline() || !plainText(r.query) }},
	{"no error on 200", func(r searchRun) bool { return !r.searchedOnly() || r.reply.Error == "" }},
	{"request budget", func(r searchRun) bool { return len(r.queries) <= maxTextSearchRequests }},
	{"limit honored", func(r searchRun) bool { return len(r.reply.Tracks) <= 1 }},
	{"pick was served", func(r searchRun) bool { return !r.searchedOnly() || r.servedPick() }},
	{"version requested", func(r searchRun) bool { return !r.searchedOnly() || unrequestedMarker(r) == "" }},
}

func checkRun(t *testing.T, run searchRun) {
	t.Helper()
	for _, inv := range runInvariants {
		if !inv.holds(run) {
			t.Errorf("%s: %s", inv.name, run)
		}
	}
}

func FuzzSearchInvariants(f *testing.F) {
	for i, entry := range invariantCatalog[:6] {
		f.Add(entry.track.Name+" "+entry.track.Artists[0], uint64(i))
	}
	for _, query := range []string{"hello", "helo adele", "hello live", "faded nightcore", "halo karaoke", "!sr stay pls", "adele - hello", "closer by halsey", "blinding lights slowed", "believer remixx"} {
		f.Add(query, uint64(len(query)))
	}
	h := newSearchHarness(f)
	f.Fuzz(func(t *testing.T, query string, seed uint64) {
		world := baseWorld().sampled(seed)
		first := h.search(t, world, query)
		checkRun(t, first)
		again := h.search(t, world, query)
		if again.pick() != first.pick() || again.reply.Error != first.reply.Error {
			t.Errorf("nondeterministic: first %s, again %s", first, again)
		}
	})
}

type invariantRequest struct {
	want   gossiprpc.SpotifyTrack
	title  string
	artist string
}

func (r invariantRequest) query() string { return strings.TrimSpace(r.title + " " + r.artist) }

func (r invariantRequest) name() string {
	if r.artist == "" {
		return r.want.ID + "/title only"
	}
	return r.want.ID
}

func invariantRequests() []invariantRequest {
	var requests []invariantRequest
	for _, entry := range invariantCatalog {
		if !entry.request {
			continue
		}
		title := entry.requestTitle
		if title == "" {
			title = entry.track.Name
		}
		requests = append(requests, invariantRequest{want: entry.track, title: title, artist: entry.track.Artists[0]},
			invariantRequest{want: entry.track, title: title})
	}
	return requests
}

type invariantVariant struct {
	name  string
	query string
	world searchWorld
	fresh bool
}

func (v invariantVariant) harness(t *testing.T, shared *searchHarness) *searchHarness {
	if v.fresh {
		return newSearchHarness(t)
	}
	return shared
}

type invariantRelation struct {
	name     string
	variants func(invariantRequest, searchRun) []invariantVariant
	holds    func(base searchRun, got searchRun) bool
	oneSong  bool
	recall   bool
	scope    requestScope
}

type requestScope int

const (
	anyRequest requestScope = iota
	namedRequest
	titleRequest
)

func (s requestScope) covers(r invariantRequest) bool {
	return s == anyRequest || (s == namedRequest) == (r.artist != "")
}

func sameOrMiss(base, got searchRun) bool { return got.pick() == "" || got.pick() == base.pick() }
func samePick(base, got searchRun) bool   { return got.pick() == base.pick() }

func agreeOrMiss(base, got searchRun) bool { return base.pick() == "" || sameOrMiss(base, got) }

func invariantTitleWords(track gossiprpc.SpotifyTrack) []string {
	return invariantFoldedWords(track.Name)
}

func sameWordsOrMiss(words func(gossiprpc.SpotifyTrack) []string) func(base, got searchRun) bool {
	return func(base, got searchRun) bool {
		if got.pick() == "" || base.pick() == "" {
			return true
		}
		return slices.Equal(words(got.reply.Tracks[0]), words(base.reply.Tracks[0]))
	}
}

type queryEdit struct {
	name string
	edit func(invariantRequest) string
}

func rewrites(edits []queryEdit) func(invariantRequest, searchRun) []invariantVariant {
	return func(r invariantRequest, _ searchRun) []invariantVariant {
		var out []invariantVariant
		for _, e := range edits {
			out = append(out, invariantVariant{name: e.name, query: e.edit(r), world: baseWorld()})
		}
		return out
	}
}

var surfaceNoise = []queryEdit{
	{"upper", func(r invariantRequest) string { return strings.ToUpper(r.query()) }},
	{"lower", func(r invariantRequest) string { return strings.ToLower(r.query()) }},
	{"punctuation", func(r invariantRequest) string { return r.title + ", " + r.artist + "!!" }},
	{"spaces", func(r invariantRequest) string { return "  " + strings.ReplaceAll(r.query(), " ", "   ") + " " }},
	{"diacritics", func(r invariantRequest) string { return accentFirstVowel(r.query()) }},
	{"plain latin", func(r invariantRequest) string { return stripLatinAccents(r.query()) }},
	{"quotes", func(r invariantRequest) string { return `"` + r.title + `" ` + r.artist }},
	{"leaked command", func(r invariantRequest) string { return "!sr " + r.query() }},
	{"please", func(r invariantRequest) string { return r.query() + " pls" }},
}

func stripLatinAccents(s string) string {
	var b strings.Builder
	base := ' '
	for _, r := range norm.NFD.String(s) {
		if unicode.IsMark(r) && unicode.Is(unicode.Latin, base) {
			continue
		}
		base = r
		b.WriteRune(r)
	}
	return norm.NFC.String(b.String())
}

func accentFirstVowel(s string) string {
	accents := strings.NewReplacer("a", "á", "e", "é", "o", "ö", "u", "ü", "i", "í")
	for i, r := range s {
		if strings.ContainsRune("aeiou", r) {
			return s[:i] + accents.Replace(string(r)) + s[i+1:]
		}
	}
	return s
}

var orderForms = []queryEdit{
	{"title artist", func(r invariantRequest) string { return r.title + " " + r.artist }},
	{"artist title", func(r invariantRequest) string { return r.artist + " " + r.title }},
	{"title by artist", func(r invariantRequest) string { return r.title + " by " + r.artist }},
	{"artist - title", func(r invariantRequest) string { return r.artist + " - " + r.title }},
}

var windowSlots = []int{0, 1, maxSearchLimit - 1}

func versionTracks(r invariantRequest) []gossiprpc.SpotifyTrack {
	var tracks []gossiprpc.SpotifyTrack
	for i, tail := range []string{" (Live)", " - Sped Up", " (Remix)", " (Karaoke Version)"} {
		tracks = append(tracks, searchTrack("injected-"+strconv.Itoa(i), r.title+tail, r.want.Artists...))
	}
	return tracks
}

func decoyTracks(r invariantRequest) []gossiprpc.SpotifyTrack {
	return []gossiprpc.SpotifyTrack{searchTrack("decoy", r.title, "Northern Static")}
}

func placing(injected func(invariantRequest) []gossiprpc.SpotifyTrack) func(invariantRequest, searchRun) []invariantVariant {
	return func(r invariantRequest, _ searchRun) []invariantVariant {
		var out []invariantVariant
		for _, track := range injected(r) {
			for _, at := range windowSlots {
				out = append(out, invariantVariant{name: fmt.Sprintf("%s at %d", track.Name, at), query: r.query(), world: baseWorld().with(placedTrack{track, at})})
			}
		}
		return out
	}
}

func typoVariants(r invariantRequest, _ searchRun) []invariantVariant {
	var out []invariantVariant
	for _, word := range strings.Fields(r.query()) {
		runes := []rune(word)
		if len(runes) < 6 {
			continue
		}
		mid := len(runes) / 2
		typos := []string{
			string(slices.Delete(slices.Clone(runes), mid, mid+1)),
			string(slices.Insert(slices.Clone(runes), mid, runes[mid])),
			string(slices.Concat(runes[:mid], []rune{'x'}, runes[mid+1:])),
		}
		for _, typo := range typos {
			out = append(out, invariantVariant{name: typo, query: strings.Replace(r.query(), word, typo, 1), world: baseWorld()})
		}
	}
	return out
}

func removalVariants(r invariantRequest, base searchRun) []invariantVariant {
	if base.pick() == "" {
		return nil
	}
	return []invariantVariant{{name: "without " + base.pick(), query: r.query(), world: baseWorld().without(base.pick())}}
}

var searchRelations = []invariantRelation{
	{name: "determinism", variants: rerunVariant, holds: samePick},
	{name: "surface noise", variants: rewrites(surfaceNoise), holds: sameOrMiss, recall: true},
	{name: "order forms", variants: rewrites(orderForms), holds: agreeOrMiss, oneSong: true, recall: true, scope: namedRequest},
	{name: "one typo", variants: typoVariants, holds: sameOrMiss},
	{name: "version injection", variants: placing(versionTracks), holds: samePick},
	{name: "decoy artist", variants: placing(decoyTracks), holds: samePick, scope: namedRequest},
	{name: "removal", variants: removalVariants, holds: sameWordsOrMiss(trackWords), scope: namedRequest},
	{name: "removal", variants: removalVariants, holds: sameWordsOrMiss(invariantTitleWords), scope: titleRequest},
}

func rerunVariant(r invariantRequest, _ searchRun) []invariantVariant {
	return []invariantVariant{{name: "rerun on a fresh provider", query: r.query(), world: baseWorld(), fresh: true}}
}

func TestSearchMetamorphicRelations(t *testing.T) {
	h := newSearchHarness(t)
	for _, request := range invariantRequests() {
		t.Run(request.name(), func(t *testing.T) {
			base := h.search(t, baseWorld(), request.query())
			checkRun(t, base)
			checkNamedRecall(t, request, base)
			checkRelations(t, h, relationBase{request, base})
		})
	}
}

func checkNamedRecall(t *testing.T, request invariantRequest, run searchRun) {
	t.Helper()
	if request.artist == "" {
		return
	}
	if run.pick() != request.want.ID {
		t.Errorf("named request want %q, got %s", request.want.ID, run)
	}
}

func checkRelations(t *testing.T, h *searchHarness, base relationBase) {
	t.Helper()
	for _, rel := range searchRelations {
		if rel.scope.covers(base.request) {
			checkRelation(t, h, rel, base)
		}
	}
}

type relationBase struct {
	request invariantRequest
	run     searchRun
}

func checkRelation(t *testing.T, h *searchHarness, rel invariantRelation, base relationBase) {
	t.Helper()
	runs := []searchRun{base.run}
	for _, v := range rel.variants(base.request, base.run) {
		got := v.harness(t, h).search(t, v.world, v.query)
		checkRun(t, got)
		checkVariantRecall(t, base.request, got, rel.recall)
		runs = append(runs, got)
		if !rel.holds(base.run, got) {
			t.Errorf("%s [%s]: base %s, got %s", rel.name, v.name, base.run, got)
		}
	}
	if rel.oneSong {
		checkOneSong(t, runs)
	}
}

func checkVariantRecall(t *testing.T, request invariantRequest, run searchRun, required bool) {
	t.Helper()
	if required {
		checkNamedRecall(t, request, run)
	}
}

func checkOneSong(t *testing.T, runs []searchRun) {
	t.Helper()
	picks := map[string]string{}
	for _, run := range runs {
		if run.pick() != "" {
			picks[run.pick()] = run.query
		}
	}
	if len(picks) > 1 {
		t.Errorf("order forms picked different songs: %v", picks)
	}
}
