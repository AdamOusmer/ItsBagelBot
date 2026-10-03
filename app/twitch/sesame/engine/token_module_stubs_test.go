// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/projection"
)

type stubQuotes struct {
	QuotesStore
	random []modulesrpc.Quote
	byNum  map[uint64]modulesrpc.Quote
	err    error
	draws  int
	gets   []uint64
}

func (s *stubQuotes) QuoteRandom(context.Context, uint64) (modulesrpc.Quote, bool, error) {
	s.draws++
	if s.err != nil || s.draws > len(s.random) {
		return modulesrpc.Quote{}, false, s.err
	}
	return s.random[s.draws-1], true, nil
}

func (s *stubQuotes) QuoteGet(_ context.Context, _, number uint64) (modulesrpc.Quote, bool, error) {
	s.gets = append(s.gets, number)
	quote, found := s.byNum[number]
	return quote, found, s.err
}

type stubGossip struct {
	reply gossiprpc.SpotifyNowPlayingReply
	calls []GossipRoute
}

func (s *stubGossip) Call(_ context.Context, route GossipRoute, _ gossiprpc.Request, out any) error {
	s.calls = append(s.calls, route)
	if reply, ok := out.(*gossiprpc.SpotifyNowPlayingReply); ok {
		*reply = s.reply
	}
	return nil
}

type stubViewerLookup struct {
	entries []chattersSnapshotEntry
	state   viewerSnapshotState
	calls   int
}

func (s *stubViewerLookup) Snapshot(context.Context, uint64) ([]chattersSnapshotEntry, viewerSnapshotState) {
	s.calls++
	return s.entries, s.state
}

type fakeEmotes struct {
	sets scope.EmoteSets
}

func (f fakeEmotes) Emotes() scope.EmoteSets { return f.sets }

func loyaltyOn(name string) projection.ModuleView {
	return projection.ModuleView{IsEnabled: true, Configs: []byte(`{"pointsName":"` + name + `"}`)}
}

func timeOn(zone, format string) projection.ModuleView {
	return projection.ModuleView{IsEnabled: true, Configs: []byte(`{"timezone":"` + zone + `","format":"` + format + `"}`)}
}

func quoteAt(number uint64, text string) modulesrpc.Quote {
	return modulesrpc.Quote{Number: number, Text: text, CreatedAt: "2026-01-31T12:00:00Z"}
}

func playing(title string, artists ...string) gossiprpc.SpotifyNowPlayingReply {
	return gossiprpc.SpotifyNowPlayingReply{
		IsPlaying: true,
		Track:     &gossiprpc.SpotifyTrack{Name: title, Artists: artists},
	}
}

type stubLoyalty struct {
	LoyaltyStore
	counters     map[string]int64
	balances     map[uint64]loyaltyrpc.Balance
	bumps        []CounterBump
	peeks        []string
	balanceReads []uint64
}

func (s *stubLoyalty) CounterBump(_ context.Context, b CounterBump) (int64, error) {
	s.bumps = append(s.bumps, b)
	return 42, nil
}

func (s *stubLoyalty) CounterPeek(_ context.Context, target CounterTarget) (loyaltyrpc.Counter, bool, error) {
	s.peeks = append(s.peeks, target.Name)
	value, found := s.counters[target.Name]
	return loyaltyrpc.Counter{Name: target.Name, Value: value}, found, nil
}

func (s *stubLoyalty) BalanceGet(_ context.Context, _, viewerID uint64) (loyaltyrpc.Balance, error) {
	s.balanceReads = append(s.balanceReads, viewerID)
	return s.balances[viewerID], nil
}
