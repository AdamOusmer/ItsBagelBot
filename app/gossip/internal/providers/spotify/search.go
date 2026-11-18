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
	if !candidate.hasFields() && candidate.operators {
		return reply, true, nil
	}
	ranked := s.rankReply(reply.Tracks, candidate, planFields)
	reply.Tracks = ranked.tracks
	if ranked.retry != nil {
		confirmed, err := s.confirmRecording(ctx, reply, *ranked.retry)
		return confirmed, true, err
	}
	return reply, s.acceptRanked(reply, candidate, ranked), nil
}

func (s *textSearch) rankReply(tracks []gossiprpc.SpotifyTrack, candidate searchCandidate, planFields []searchFields) rankedWindow {
	if candidate.hasFields() {
		return rankWindow(tracks, []searchFields{candidate.fields()}).passing()
	}
	ranked := rankPlain(tracks, s.raw)
	if !ranked.hasMatch() && len(planFields) > 0 {
		return rankWindow(tracks, planFields).passing()
	}
	return ranked
}

func (s *textSearch) acceptRanked(reply gossiprpc.SpotifySearchReply, candidate searchCandidate, ranked rankedWindow) bool {
	if candidate.hasFields() {
		return ranked.hasMatch()
	}
	s.plain = reply
	return ranked.artist != artistNone || ranked.ambiguous
}

func (s *textSearch) confirmRecording(ctx context.Context, fallback gossiprpc.SpotifySearchReply, fields searchFields) (gossiprpc.SpotifySearchReply, error) {
	retry, _ := recoveryCandidate(textSplit{left: fields.song.text(), right: fields.artist.text()}, fields)
	if s.sent[retry.q] {
		return selectOriginal(fallback, gossiprpc.SpotifySearchReply{Tracks: s.observed, ResolvedAs: viaFiltered}, fields), nil
	}
	if s.budget.remaining == 0 {
		fallback.Tracks = nil
		return fallback, nil
	}
	s.sent[retry.q] = true
	reply, err := s.budget.search(ctx, retry)
	if err != nil {
		return reply, err
	}
	return selectOriginal(fallback, reply, fields), nil
}

func selectOriginal(fallback, reply gossiprpc.SpotifySearchReply, fields searchFields) gossiprpc.SpotifySearchReply {
	ranked := rankWindow(reply.Tracks, []searchFields{fields}).passing()
	if ranked.ambiguous || ranked.hasVerifiedArtist() {
		reply.Tracks = ranked.tracks
		return reply
	}
	return fallback
}

func (w rankedWindow) hasVerifiedArtist() bool {
	return w.artist == artistFull && w.retry == nil
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
