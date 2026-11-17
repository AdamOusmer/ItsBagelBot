// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"
	"slices"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
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
	budget   textSearchBudget
	raw      string
	plain    gossiprpc.SpotifySearchReply
	observed []gossiprpc.SpotifyTrack
	sent     map[string]bool
}

func (p *api) searchText(ctx context.Context, tok accessToken, request textSearchRequest) (gossiprpc.SpotifySearchReply, error) {
	search := textSearch{
		budget: textSearchBudget{p: p, tok: tok, remaining: maxTextSearchRequests, admit: p.rateAdmit(request.broadcaster)},
		raw:    request.raw,
		plain:  gossiprpc.SpotifySearchReply{ResolvedAs: viaText},
		sent:   make(map[string]bool),
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
	return s.recover(ctx, plan)
}

func (s *textSearch) runPlan(ctx context.Context, plan []searchCandidate) (gossiprpc.SpotifySearchReply, bool, error) {
	fields := plannedFields(plan)
	for _, candidate := range plan {
		if s.budget.remaining == 0 || s.sent[candidate.q] {
			continue
		}
		reply, done, err := s.attempt(ctx, candidate, fields)
		if err != nil || done {
			return reply, done, err
		}
	}
	return s.plain, false, nil
}

func (s *textSearch) attempt(ctx context.Context, candidate searchCandidate, planFields []searchFields) (gossiprpc.SpotifySearchReply, bool, error) {
	s.sent[candidate.q] = true
	reply, err := s.budget.search(ctx, candidate)
	if err != nil {
		return reply, false, err
	}
	s.observed = append(s.observed, reply.Tracks...)
	reply, done := s.evaluate(reply, candidate, planFields)
	return reply, done, nil
}

func (s *textSearch) evaluate(reply gossiprpc.SpotifySearchReply, candidate searchCandidate, planFields []searchFields) (gossiprpc.SpotifySearchReply, bool) {
	if candidate.hasFields() {
		ranked := rankWindow(reply.Tracks, []searchFields{candidate.fields()}).passing()
		reply.Tracks = ranked.tracks
		return reply, ranked.hasMatch()
	}
	if candidate.operators {
		return reply, true
	}
	ranked := rankPlain(reply.Tracks, s.raw)
	if !ranked.hasMatch() && len(planFields) > 0 {
		ranked = rankWindow(reply.Tracks, planFields).passing()
	}
	reply.Tracks = ranked.tracks
	s.plain = reply
	return reply, ranked.artist != artistNone || ranked.ambiguous
}

func plannedFields(plan []searchCandidate) []searchFields {
	var fields []searchFields
	for _, candidate := range plan {
		if candidate.hasFields() {
			fields = append(fields, candidate.fields())
		}
	}
	return fields
}

func (s *textSearch) recover(ctx context.Context, plan []searchCandidate) (gossiprpc.SpotifySearchReply, error) {
	if s.budget.remaining == 0 {
		return s.plain, nil
	}
	candidates := s.recoveryPlan(plan)
	reply, done, err := s.runPlan(ctx, candidates)
	if done || err != nil {
		return reply, err
	}
	return s.plain, nil
}

func (s *textSearch) recoveryPlan(plan []searchCandidate) []searchCandidate {
	if len(plannedFields(plan)) > 0 {
		return s.explicitRecovery(plan)
	}
	candidates := recoveryCandidates(s.raw, s.observed)
	return withPlainFallback(candidates, s.sent)
}

func (s *textSearch) explicitRecovery(plan []searchCandidate) []searchCandidate {
	window := catalogWindow(s.observed)
	var candidates []searchCandidate
	for _, candidate := range plan {
		corrected, ok := correctFields(candidate, window)
		if ok && s.recoverableAfterPlan(corrected) {
			candidates = append(candidates, corrected)
		}
	}
	return withPlainFallback(candidates, s.sent)
}

func (s *textSearch) recoverableAfterPlan(candidate searchCandidate) bool {
	return !s.sent[candidate.q] || !s.sent[candidate.plainFallback().q]
}

func withPlainFallback(candidates []searchCandidate, sent map[string]bool) []searchCandidate {
	var fallback searchCandidate
	for i, candidate := range candidates {
		plain := candidate.plainFallback()
		if sent[plain.q] {
			continue
		}
		if candidate.corrected {
			return slices.Insert(candidates, i+1, plain)
		}
		if fallback.q == "" {
			fallback = plain
		}
	}
	if fallback.q != "" {
		return append(candidates, fallback)
	}
	return candidates
}
