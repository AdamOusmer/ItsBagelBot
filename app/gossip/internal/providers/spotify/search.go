// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"unicode"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"golang.org/x/text/unicode/norm"
)

const (
	maxRecoverySearches   = 3
	maxTextSearchRequests = 1 + maxRecoverySearches
)

type textSearchBudget struct {
	p         *api
	tok       accessToken
	remaining int
	admit     func(context.Context) error
}

func (b *textSearchBudget) search(ctx context.Context, candidate searchCandidate) (gossiprpc.SpotifySearchReply, error) {
	b.remaining--
	if err := b.admit(ctx); err != nil {
		return gossiprpc.SpotifySearchReply{}, err
	}
	return b.p.runSearch(ctx, b.tok, candidate, maxSearchLimit)
}

type textSearchRequest struct {
	raw         string
	limit       int
	broadcaster string
}

type textSearch struct {
	budget textSearchBudget
	raw    string
	plain  gossiprpc.SpotifySearchReply
}

func (p *api) searchText(ctx context.Context, tok accessToken, request textSearchRequest) (gossiprpc.SpotifySearchReply, error) {
	search := textSearch{
		budget: textSearchBudget{p: p, tok: tok, remaining: maxTextSearchRequests, admit: p.rateAdmit(request.broadcaster)},
		raw:    request.raw,
		plain:  gossiprpc.SpotifySearchReply{ResolvedAs: viaText},
	}
	reply, err := search.resolve(ctx)
	return truncateTracks(reply, request.limit), err
}

func (s *textSearch) resolve(ctx context.Context) (gossiprpc.SpotifySearchReply, error) {
	plan := planTextSearch(s.raw)
	reply, done, err := s.runPlan(ctx, plan)
	if err != nil || done {
		return reply, err
	}
	confident := confidentPlain(s.plain, s.raw)
	// A title containing "by" can be literal despite the first convention.
	if len(plan) > 1 {
		return confident, nil
	}
	reply, done, err = s.recover(ctx)
	if err != nil || done {
		return reply, err
	}
	return confident, nil
}

func (s *textSearch) runPlan(ctx context.Context, plan []searchCandidate) (gossiprpc.SpotifySearchReply, bool, error) {
	for _, candidate := range plan {
		if s.budget.remaining == 0 {
			break
		}
		reply, err := s.budget.search(ctx, candidate)
		if err != nil {
			return reply, false, err
		}
		reply, done := s.evaluate(reply, candidate)
		if done {
			return reply, true, nil
		}
	}
	return s.plain, false, nil
}

func (s *textSearch) evaluate(reply gossiprpc.SpotifySearchReply, candidate searchCandidate) (gossiprpc.SpotifySearchReply, bool) {
	if candidate.song != "" || candidate.artist != "" {
		reply.Tracks = matchingFields(reply.Tracks, candidate)
		return reply, len(reply.Tracks) > 0 || candidate.operators
	}
	if candidate.operators {
		// A genre/year/etc query deliberately asks Spotify for discovery.
		return reply, true
	}
	s.plain = reply
	rankPlain(s.plain.Tracks, candidate.q)
	if len(s.plain.Tracks) == 0 {
		return reply, false
	}
	return confidentPlain(s.plain, s.raw), completeArtistMatch(candidate.q, s.plain.Tracks[0])
}

func (s *textSearch) recover(ctx context.Context) (gossiprpc.SpotifySearchReply, bool, error) {
	for _, candidate := range recoveryCandidates(s.raw, s.plain.Tracks) {
		if s.budget.remaining == 0 {
			break
		}
		reply, err := s.budget.search(ctx, candidate)
		if err != nil {
			return reply, false, err
		}
		reply.Tracks = matchingFields(reply.Tracks, candidate)
		if len(reply.Tracks) > 0 {
			return reply, true, nil
		}
	}
	return gossiprpc.SpotifySearchReply{}, false, nil
}

func confidentPlain(reply gossiprpc.SpotifySearchReply, query string) gossiprpc.SpotifySearchReply {
	words := matchWords(query)
	reply.Tracks = slices.DeleteFunc(slices.Clone(reply.Tracks), func(track gossiprpc.SpotifyTrack) bool {
		return !titleMatch(words, track.Name) && !completeArtistMatch(query, track)
	})
	return reply
}

func rankPlain(tracks []gossiprpc.SpotifyTrack, query string) {
	words := matchWords(query)
	type scoredTrack struct {
		track gossiprpc.SpotifyTrack
		score float64
	}
	ranked := make([]scoredTrack, len(tracks))
	for i, track := range tracks {
		ranked[i] = scoredTrack{track, plainScore(words, track)}
	}
	slices.SortStableFunc(ranked, func(a, b scoredTrack) int { return compareScore(b.score, a.score) })
	for i, result := range ranked {
		tracks[i] = result.track
	}
}

func plainScore(query matchPhrase, track gossiprpc.SpotifyTrack) float64 {
	best := 0.8 * similarity(query, matchWords(track.Name))
	if titleMatch(query, track.Name) {
		best = 0.86
	}
	if slices.Equal(query, matchWords(track.Name)) {
		best = 0.9
	}
	for _, split := range wordSplits(query) {
		if fieldsMatch(track, split[0], split[1]) {
			best = max(best, 0.95+0.05*fieldScore(track, split[0], split[1]))
		}
	}
	return best
}

func completeArtistMatch(query string, track gossiprpc.SpotifyTrack) bool {
	for _, split := range wordSplits(matchWords(query)) {
		if fieldsMatch(track, split[0], split[1]) {
			return true
		}
	}
	return false
}

func matchingFields(tracks []gossiprpc.SpotifyTrack, candidate searchCandidate) []gossiprpc.SpotifyTrack {
	song, artist := matchWords(candidate.song), matchWords(candidate.artist)
	matched := slices.DeleteFunc(tracks, func(track gossiprpc.SpotifyTrack) bool { return !fieldsMatch(track, song, artist) })
	slices.SortStableFunc(matched, func(a, b gossiprpc.SpotifyTrack) int {
		return compareScore(fieldScore(b, song, artist), fieldScore(a, song, artist))
	})
	return matched
}

func fieldsMatch(track gossiprpc.SpotifyTrack, song, artist matchPhrase) bool {
	if len(song) == 0 && len(artist) == 0 {
		return false
	}
	if len(song) > 0 && !titleMatch(song, track.Name) {
		return false
	}
	if len(artist) == 0 {
		return true
	}
	for _, name := range track.Artists {
		if artistIdentity(artist, matchWords(name)) {
			return true
		}
	}
	return false
}

func fieldScore(track gossiprpc.SpotifyTrack, song, artist matchPhrase) float64 {
	artistScore := 0.0
	for _, name := range track.Artists {
		artistScore = max(artistScore, similarity(artist, matchWords(name)))
	}
	return 0.65*similarity(song, matchWords(track.Name)) + 0.35*artistScore
}

var releaseSuffix = regexp.MustCompile(`(?i)(?:\s+[-–—]\s*|\s*\()(?:\d{4}\s+)?(?:re-?master(?:ed)?(?:\s+\d{4})?|mono|stereo|(?:radio|single|album)\s+(?:edit|version)|(?:explicit|clean)(?:\s+version)?)(?:\s*\))?$`)
var featuredSuffix = regexp.MustCompile(`(?i)(?:\s+[-–—]\s*|\s*\()(?:feat\.?|ft\.?|featuring)\s+[^()]+\)?$`)

func cleanTrackTitle(name string) string {
	name = releaseSuffix.ReplaceAllString(name, "")
	return featuredSuffix.ReplaceAllString(name, "")
}

// Only delimited release/featured-credit metadata is stripped. User query
// words and recording markers always remain meaningful.
func titleMatch(query matchPhrase, name string) bool {
	metadata := matchWords(name)
	if conflictingRecording(query, metadata) {
		return false
	}
	return sameIdentity(strings.Join(query, " "), strings.Join(metadata, " ")) ||
		sameIdentity(strings.Join(query, " "), strings.Join(matchWords(cleanTrackTitle(name)), " "))
}

func artistIdentity(query, metadata matchPhrase) bool {
	clean := func(words matchPhrase) string {
		if len(words) > 1 && words[0] == "the" {
			words = words[1:]
		}
		return strings.Join(words, " ")
	}
	return sameIdentity(clean(query), clean(metadata))
}

func conflictingRecording(query, metadata matchPhrase) bool {
	for _, word := range []string{"live", "acoustic", "remix", "cover", "instrumental", "karaoke", "demo", "session"} {
		if slices.Contains(query, word) != slices.Contains(metadata, word) {
			return true
		}
	}
	return false
}

// Permit one typo in a sufficiently long name, never partial containment.
// A transposition counts as one typo.
func sameIdentity(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	left, right := []rune(a), []rune(b)
	if !canCorrectTypo(left, right) {
		return false
	}
	return matchesOneTypo(left, right, firstMismatch(left, right))
}

func canCorrectTypo(left, right []rune) bool {
	if min(len(left), len(right)) < 5 {
		return false
	}
	return absDifference(len(left), len(right)) <= 1
}

func absDifference(a, b int) int {
	return max(a-b, b-a)
}

func firstMismatch(left, right []rune) int {
	i := 0
	for i < min(len(left), len(right)) {
		if left[i] != right[i] {
			break
		}
		i++
	}
	return i
}

func matchesOneTypo(left, right []rune, i int) bool {
	if len(left) > len(right) {
		return slices.Equal(left[i+1:], right[i:])
	}
	if len(left) < len(right) {
		return slices.Equal(left[i:], right[i+1:])
	}
	return transposedAt(left, right, i) || slices.Equal(left[i+1:], right[i+1:])
}

func transposedAt(left, right []rune, i int) bool {
	if i+1 >= len(left) {
		return false
	}
	if left[i] != right[i+1] || left[i+1] != right[i] {
		return false
	}
	return slices.Equal(left[i+2:], right[i+2:])
}

// Recovery requires a title prefix supported by observed metadata, not an
// arbitrary split. Prefer the longest title prefix, then artist-last input.
type scoredCandidate struct {
	candidate searchCandidate
	score     float64
}

func recoveryCandidates(raw string, tracks []gossiprpc.SpotifyTrack) []searchCandidate {
	var candidates []scoredCandidate
	for _, split := range wordSplits(matchWords(raw)) {
		if candidate, ok := recoverySplit(split, tracks); ok {
			candidates = append(candidates, candidate)
		}
	}
	slices.SortStableFunc(candidates, func(a, b scoredCandidate) int { return compareScore(b.score, a.score) })
	return uniqueRecoveryCandidates(candidates)
}

func recoverySplit(split [2]matchPhrase, tracks []gossiprpc.SpotifyTrack) (scoredCandidate, bool) {
	if slices.Contains([]string{"a", "an", "the", "i", "it", "by", "of", "to", "in", "and"}, strings.Join(split[1], " ")) {
		return scoredCandidate{}, false
	}
	score := recoveryScore(split[0], tracks)
	if score == 0 {
		return scoredCandidate{}, false
	}
	candidate, ok := (textSplit{left: strings.Join(split[0], " "), right: strings.Join(split[1], " "), ok: true}).candidate()
	return scoredCandidate{candidate, score}, ok
}

func recoveryScore(song matchPhrase, tracks []gossiprpc.SpotifyTrack) float64 {
	score := 0.0
	for _, track := range tracks {
		score = max(score, titleEvidence(song, track.Name))
	}
	return score
}

func titleEvidence(song matchPhrase, name string) float64 {
	// Harmless release suffixes still support a one-word title.
	if titleMatch(song, name) {
		return 1
	}
	title := matchWords(name)
	if len(song) < 2 || len(song) >= len(title) {
		return 0
	}
	if !slices.Equal(song, title[:len(song)]) {
		return 0
	}
	return similarity(song, title)
}

func uniqueRecoveryCandidates(candidates []scoredCandidate) []searchCandidate {
	out := make([]searchCandidate, 0, maxRecoverySearches)
	for _, scored := range candidates {
		if slices.ContainsFunc(out, func(c searchCandidate) bool { return c.q == scored.candidate.q }) {
			continue
		}
		out = append(out, scored.candidate)
		if len(out) == maxRecoverySearches {
			break
		}
	}
	return out
}

func wordSplits(words matchPhrase) [][2]matchPhrase {
	var out [][2]matchPhrase
	for i := len(words) - 1; i >= 1; i-- {
		out = append(out, [2]matchPhrase{words[:i], words[i:]}, [2]matchPhrase{words[i:], words[:i]})
	}
	return out
}

type matchPhrase []string

func matchWords(s string) matchPhrase {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Mn, r), r == '\'', r == '’':
			return -1
		case unicode.IsLetter(r), unicode.IsNumber(r):
			return unicode.ToLower(r)
		default:
			return ' '
		}
	}, norm.NFD.String(s))
	return strings.Fields(s)
}

func coverage(query, metadata matchPhrase) float64 {
	if len(query) == 0 {
		return 0
	}
	matched := 0
	for _, word := range query {
		if slices.Contains(metadata, word) {
			matched++
		}
	}
	return float64(matched) / float64(len(query))
}

func similarity(a, b matchPhrase) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	return (coverage(a, b)*float64(len(a)) + coverage(b, a)*float64(len(b))) / float64(len(a)+len(b))
}

func compareScore(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
