// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"
	"slices"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

const maxTextSearchRequests = 4

type textSearchRequest struct {
	raw         matchText
	limit       int
	broadcaster string
}

type textSearch struct {
	api       *api
	token     accessToken
	admit     func(context.Context) error
	remaining int
	paginated bool
	sent      map[searchLookup]bool
	query     textQuery
	observed  []catalogTrack
	best      rankedWindow
	scorer    windowScorer
	name      string
}

type searchLookup struct {
	text   string
	offset int
}

type textQuery struct {
	raw      matchText
	sources  []sourceWords
	fields   [][]searchFields
	explicit []searchCandidate
}

func prepareTextQuery(raw matchText, plan []searchCandidate) textQuery {
	q := textQuery{raw: raw}
	source := raw.sourceWords()
	q.sources = source.variants()
	for i, words := range q.sources {
		v := queryVariant{words: words.words, titleOnly: source.allowsTitleOnly(i)}
		q.fields = append(q.fields, v.interpretations())
	}
	if alternate := raw.withoutEmotes(); alternate != raw {
		words := alternate.sourceWords().variants()
		v := queryVariant{words: words[len(words)-1].words, titleOnly: raw.emoteTitleOnly(alternate)}
		q.fields = append(q.fields, v.interpretations())
		q.sources = append(q.sources, words...)
	}
	for _, candidate := range plan {
		if candidate.hasFields() {
			q.explicit = append(q.explicit, candidate)
		}
	}
	return q
}

func (s sourceWords) allowsTitleOnly(variant int) bool {
	if variant == 0 {
		return true
	}
	content := s.words.contentRange()
	return fillerAllowsTitle(s.words[:content.start], s.words[content.end:])
}

func (p *api) searchText(ctx context.Context, tok accessToken, request textSearchRequest) (gossiprpc.SpotifySearchReply, error) {
	plan := planTextSearch(request.raw)
	s := textSearch{
		api: p, token: tok, admit: p.rateAdmit(request.broadcaster), remaining: maxTextSearchRequests,
		sent: make(map[searchLookup]bool), name: viaText,
		scorer: windowScorer{credits: &creditMemo{matches: make(map[creditIdentity]artistMatch), queries: make(map[phraseSlice]artistQuery)}},
	}
	if len(plan) == 0 {
		return s.reply(request.limit), nil
	}
	s.query = prepareTextQuery(request.raw, plan)
	err := s.resolve(ctx, plan)
	return s.reply(request.limit), err
}

func (s *textSearch) resolve(ctx context.Context, plan []searchCandidate) error {
	done, err := s.runPlan(ctx, plan)
	if done || err != nil {
		return err
	}
	_, err = s.runPlan(ctx, s.recoveryPlan())
	return err
}

func (s *textSearch) runPlan(ctx context.Context, plan []searchCandidate) (bool, error) {
	for _, candidate := range plan {
		if candidate.q == "" || s.used(candidate) {
			continue
		}
		done, err := s.attempt(ctx, candidate)
		if shouldStop(done, err) || s.remaining == 0 {
			return done, err
		}
	}
	return false, nil
}

func shouldStop(done bool, err error) bool { return done || err != nil }

func (s *textSearch) used(candidate searchCandidate) bool {
	return s.sent[searchLookup{candidate.q, candidate.offset}]
}

func (s *textSearch) attempt(ctx context.Context, candidate searchCandidate) (bool, error) {
	window, err := s.fetch(ctx, candidate)
	if err != nil {
		return false, err
	}
	ranked := s.rank(window, candidate)
	s.confirmArtists(ranked, candidate, window)
	if s.canPaginate(candidate, window) && ranked.unaskedVersion() {
		return s.nextPage(ctx, candidate, ranked)
	}
	s.remember(ranked, candidate)
	return !candidate.checkPlain && ranked.top().score.quality == exactMatch, nil
}

func (s *textSearch) canPaginate(candidate searchCandidate, window []catalogTrack) bool {
	if s.paginated || s.remaining == 0 {
		return false
	}
	return len(window) == maxSearchLimit && candidate.startsPageSearch()
}

func (c searchCandidate) startsPageSearch() bool {
	return c.offset == 0 && !c.checkPlain
}

func (w rankedWindow) unaskedVersion() bool {
	if w.top().score.quality == exactMatch {
		return false
	}
	return slices.ContainsFunc(w.matches, func(m windowMatch) bool {
		return m.score.unaskedVersion()
	})
}

func (s matchScore) unaskedVersion() bool {
	if s.quality != blockedRecording && s.quality != fallbackMatch {
		return false
	}
	return s.artist.level == artistNone || s.artist.exact()
}

func (s *textSearch) nextPage(ctx context.Context, candidate searchCandidate, first rankedWindow) (bool, error) {
	s.remember(first, candidate)
	s.paginated = true
	candidate.offset = maxSearchLimit
	return s.attempt(ctx, candidate)
}

func (s *textSearch) fetch(ctx context.Context, candidate searchCandidate) ([]catalogTrack, error) {
	s.remaining--
	s.sent[searchLookup{candidate.q, candidate.offset}] = true
	if err := s.admit(ctx); err != nil {
		return nil, err
	}
	window, err := s.api.runSearch(ctx, s.token, candidate)
	s.observed = append(s.observed, window...)
	s.scorer.parts = s.observed
	return window, err
}

func (s *textSearch) rank(window []catalogTrack, candidate searchCandidate) rankedWindow {
	if candidate.hasFields() || candidate.operators {
		return s.scorer.rank(window, []searchFields{candidate.fields()})
	}
	return s.rankPlainWindow(window)
}

func (s *textSearch) rankPlainWindow(window []catalogTrack) rankedWindow {
	best := rankedWindow{}
	for _, fields := range s.query.fields {
		ranked := s.scorer.rank(window, fields)
		if ranked.top().score.quality == exactMatch {
			return ranked
		}
		best = best.prefer(ranked)
	}
	for _, explicit := range s.query.explicit {
		best = best.prefer(s.scorer.rank(window, []searchFields{explicit.fields()}))
	}
	return best
}

func (w rankedWindow) prefer(other rankedWindow) rankedWindow {
	if other.top().score.compare(w.top().score) > 0 {
		return other
	}
	return w
}

func (s *textSearch) confirmArtists(ranked rankedWindow, candidate searchCandidate, window []catalogTrack) {
	if ranked.ambiguous || !candidate.hasFields() {
		return
	}
	if ranked.top().score.quality == exactMatch {
		return
	}
	prior := s.observed[:len(s.observed)-len(window)]
	for i := range ranked.matches {
		match := &ranked.matches[i]
		if s.confirmsPartial(*match, candidate, window) || match.confirmsFuzzyArtist(candidate.confirmCredit, window, prior) {
			match.score.quality = exactMatch
		}
	}
	slices.SortStableFunc(ranked.matches, compareWindowMatches)
}

func (s *textSearch) confirmsPartial(match windowMatch, candidate searchCandidate, window []catalogTrack) bool {
	if match.score.quality != partialMatch || len(candidate.confirmCredit) == 0 {
		return false
	}
	top := s.best.top()
	if top.score.quality != partialMatch || !sameCompact(match.credit(), top.credit()) {
		return false
	}
	if !sameCompact(match.credit(), candidate.confirmCredit) || !match.sameTitleAndCorrections(top.track) {
		return false
	}
	return !match.competingPartialCredit(s.observed) && !slices.ContainsFunc(window, match.matchesOtherFuzzyCredit)
}

func (m windowMatch) confirmsFuzzyArtist(credit matchPhrase, window, prior []catalogTrack) bool {
	if m.score.quality != fuzzyMatch || m.score.artist.edits == 0 {
		return false
	}
	if slices.ContainsFunc(window, m.matchesOtherFuzzyCredit) {
		return false
	}
	if len(credit) == 0 {
		return slices.ContainsFunc(prior, m.sameTitleAndCorrections)
	}
	return m.score.title.edits == 0 && sameCompact(m.credit(), credit)
}

func (s *textSearch) remember(ranked rankedWindow, candidate searchCandidate) {
	old, next := s.best.top(), ranked.top()
	comparison := next.score.compare(old.score)
	if s.confirmedFuzzy(ranked, candidate, comparison) {
		s.best, s.name = ranked, candidate.name
		return
	}
	conflict := old.conflicts(next)
	if comparison > 0 || next.score.quality == exactMatch {
		s.best, s.name = ranked, candidate.name
	}
	s.best.ambiguous = s.best.ambiguous || conflict
}

func (s *textSearch) confirmedFuzzy(ranked rankedWindow, candidate searchCandidate, comparison int) bool {
	if !candidate.hasFields() || !s.best.ambiguous {
		return false
	}
	return ranked.top().score.quality == fuzzyMatch && comparison >= 0 && !ranked.ambiguous
}

func (s *textSearch) reply(limit int) gossiprpc.SpotifySearchReply {
	reply := gossiprpc.SpotifySearchReply{ResolvedAs: viaText}
	if !s.canReply() {
		return reply
	}
	reply.ResolvedAs = s.name
	reply.Tracks = s.best.tracks()
	return truncateTracks(reply, limit)
}

func (s *textSearch) canReply() bool {
	if s.best.ambiguous {
		return false
	}
	top := s.best.top()
	switch top.score.quality {
	case fallbackMatch:
		return !s.scorer.competingPart(top.track, top.score.title) && s.used(top.originalCandidate(s.query))
	case fuzzyMatch:
		return top.score.artist.edits == 0
	default:
		return top.score.quality == exactMatch
	}
}

func (w rankedWindow) tracks() []gossiprpc.SpotifyTrack {
	quality := w.top().score.quality
	var tracks []gossiprpc.SpotifyTrack
	for _, m := range w.matches {
		if m.score.quality != quality {
			break
		}
		if quality != fuzzyMatch || m.score.artist.edits == 0 {
			tracks = append(tracks, m.track.track)
		}
	}
	return tracks
}
