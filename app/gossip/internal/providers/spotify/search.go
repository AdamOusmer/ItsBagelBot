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

func (p *api) searchText(ctx context.Context, tok accessToken, raw string, limit int, broadcaster string) (gossiprpc.SpotifySearchReply, error) {
	budget := textSearchBudget{p: p, tok: tok, remaining: maxTextSearchRequests, admit: p.rateAdmit(broadcaster)}
	plan := planTextSearch(raw)
	plain := gossiprpc.SpotifySearchReply{ResolvedAs: viaText}
	for _, candidate := range plan {
		if budget.remaining == 0 {
			break
		}
		reply, err := budget.search(ctx, candidate)
		if err != nil {
			return gossiprpc.SpotifySearchReply{}, err
		}
		if candidate.song != "" || candidate.artist != "" {
			reply.Tracks = matchingFields(reply.Tracks, candidate)
			if len(reply.Tracks) > 0 {
				return truncateTracks(reply, limit), nil
			}
			if candidate.operators {
				return reply, nil
			}
			continue
		}
		if candidate.operators {
			// A genre/year/etc query deliberately asks Spotify for discovery.
			return truncateTracks(reply, limit), nil
		}
		plain = reply
		rankPlain(plain.Tracks, candidate.q)
		if len(plain.Tracks) > 0 && completeArtistMatch(candidate.q, plain.Tracks[0]) {
			return truncateTracks(confidentPlain(plain, raw), limit), nil
		}
	}
	confident := confidentPlain(plain, raw)
	// A title containing "by" can be literal, despite the first convention
	// attempt. A complete metadata match also overrides a false convention.
	if len(plan) > 1 {
		return truncateTracks(confident, limit), nil
	}
	for _, candidate := range recoveryCandidates(raw, plain.Tracks) {
		if budget.remaining == 0 {
			break
		}
		reply, err := budget.search(ctx, candidate)
		if err != nil {
			return gossiprpc.SpotifySearchReply{}, err
		}
		reply.Tracks = matchingFields(reply.Tracks, candidate)
		if len(reply.Tracks) > 0 {
			return truncateTracks(reply, limit), nil
		}
	}
	return truncateTracks(confident, limit), nil
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

func plainScore(query []string, track gossiprpc.SpotifyTrack) float64 {
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

func fieldsMatch(track gossiprpc.SpotifyTrack, song, artist []string) bool {
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

func fieldScore(track gossiprpc.SpotifyTrack, song, artist []string) float64 {
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
func titleMatch(query []string, name string) bool {
	metadata := matchWords(name)
	if conflictingRecording(query, metadata) {
		return false
	}
	return sameIdentity(strings.Join(query, " "), strings.Join(metadata, " ")) ||
		sameIdentity(strings.Join(query, " "), strings.Join(matchWords(cleanTrackTitle(name)), " "))
}

func artistIdentity(query, metadata []string) bool {
	clean := func(words []string) string {
		if len(words) > 1 && words[0] == "the" {
			words = words[1:]
		}
		return strings.Join(words, " ")
	}
	return sameIdentity(clean(query), clean(metadata))
}

func conflictingRecording(query, metadata []string) bool {
	for _, word := range []string{"live", "acoustic", "remix", "cover", "instrumental", "karaoke", "demo", "session"} {
		if slices.Contains(query, word) != slices.Contains(metadata, word) {
			return true
		}
	}
	return false
}

// Permit one typo in a sufficiently long name, but never partial artist or
// title containment. A transposition counts as one typo.
func sameIdentity(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	left, right := []rune(a), []rune(b)
	if min(len(left), len(right)) < 5 || len(left)-len(right) > 1 || len(right)-len(left) > 1 {
		return false
	}
	i := 0
	for i < min(len(left), len(right)) && left[i] == right[i] {
		i++
	}
	if len(left) == len(right) {
		if i+1 < len(left) && left[i] == right[i+1] && left[i+1] == right[i] && slices.Equal(left[i+2:], right[i+2:]) {
			return true
		}
		return slices.Equal(left[i+1:], right[i+1:])
	}
	if len(left) > len(right) {
		return slices.Equal(left[i+1:], right[i:])
	}
	return slices.Equal(left[i:], right[i+1:])
}

// Recovery requires a title prefix supported by observed metadata, not an
// arbitrary split. Prefer the longest title prefix, then artist-last input.
func recoveryCandidates(raw string, tracks []gossiprpc.SpotifyTrack) []searchCandidate {
	type scoredCandidate struct {
		candidate searchCandidate
		score     float64
	}
	var candidates []scoredCandidate
	for _, split := range wordSplits(matchWords(raw)) {
		if slices.Contains([]string{"a", "an", "the", "i", "it", "by", "of", "to", "in", "and"}, strings.Join(split[1], " ")) {
			continue
		}
		score := 0.0
		for _, track := range tracks {
			title := matchWords(track.Name)
			// Harmless release suffixes still support a one-word title.
			if titleMatch(split[0], track.Name) {
				score = max(score, 1)
				continue
			}
			if len(split[0]) < 2 || len(split[0]) >= len(title) || !slices.Equal(split[0], title[:len(split[0])]) {
				continue
			}
			score = max(score, similarity(split[0], title))
		}
		if score == 0 {
			continue
		}
		candidate, ok := (textSplit{left: strings.Join(split[0], " "), right: strings.Join(split[1], " "), ok: true}).candidate()
		if ok {
			candidates = append(candidates, scoredCandidate{candidate, score})
		}
	}
	slices.SortStableFunc(candidates, func(a, b scoredCandidate) int { return compareScore(b.score, a.score) })
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

func wordSplits(words []string) [][2][]string {
	var out [][2][]string
	for i := len(words) - 1; i >= 1; i-- {
		out = append(out, [2][]string{words[:i], words[i:]}, [2][]string{words[i:], words[:i]})
	}
	return out
}

func matchWords(s string) []string {
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

func coverage(query, metadata []string) float64 {
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

func similarity(a, b []string) float64 {
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
