// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package feed

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"ItsBagelBot/internal/domain/channelevents"
)

type Counts struct {
	Follows     int64            `json:"follows"`
	Subs        int64            `json:"subs"`
	Resubs      int64            `json:"resubs"`
	Gifts       int64            `json:"gifts"`
	Bits        int64            `json:"bits"`
	Raids       int64            `json:"raids"`
	Redemptions int64            `json:"redemptions"`
	SubPoints   int64            `json:"sub_points"`
	Tiers       map[string]int64 `json:"tiers,omitempty"`
}

type Leader struct {
	User
	Amount int64 `json:"amount"`
}

// Stats is the stats frame. Latest carries each kind's newest event; its At is the version for guarded writes.
type Stats struct {
	Session           Counts            `json:"session"`
	Week              Counts            `json:"week"`
	Month             Counts            `json:"month"`
	AllTime           Counts            `json:"all_time"`
	Latest            map[Kind]Event    `json:"latest,omitempty"`
	TopCheerer        *Leader           `json:"top_cheerer,omitempty"`
	TopGifter         *Leader           `json:"top_gifter,omitempty"`
	AllTimeTopCheerer *Leader           `json:"all_time_top_cheerer,omitempty"`
	AllTimeTopGifter  *Leader           `json:"all_time_top_gifter,omitempty"`
	Credits           map[Kind][]Leader `json:"credits,omitempty"`
}

// Add applies one count step; tier:<tier> fields land in Tiers.
func (c *Counts) Add(d CountDelta) {
	if tier, ok := strings.CutPrefix(d.Field, FieldTierPrefix); ok {
		if c.Tiers == nil {
			c.Tiers = map[string]int64{}
		}
		c.Tiers[tier] += d.Amount
		return
	}
	if field, ok := countFields[d.Field]; ok {
		*field(c) += d.Amount
	}
}

var countFields = map[string]func(*Counts) *int64{
	FieldFollows:     func(c *Counts) *int64 { return &c.Follows },
	FieldSubs:        func(c *Counts) *int64 { return &c.Subs },
	FieldResubs:      func(c *Counts) *int64 { return &c.Resubs },
	FieldGifts:       func(c *Counts) *int64 { return &c.Gifts },
	FieldBits:        func(c *Counts) *int64 { return &c.Bits },
	FieldRaids:       func(c *Counts) *int64 { return &c.Raids },
	FieldRedemptions: func(c *Counts) *int64 { return &c.Redemptions },
	FieldSubPoints:   func(c *Counts) *int64 { return &c.SubPoints },
}

// Tally is one window's counts and per-viewer leaderboards, keyed by Twitch user id.
type Tally struct {
	Counts
	Cheerers map[string]Leader
	Gifters  map[string]Leader
}

func (t *Tally) Apply(d Delta) {
	for _, count := range d.Counts {
		t.Counts.Add(count)
	}
	if d.Leader != nil {
		t.addLeader(*d.Leader)
	}
}

func (t *Tally) addLeader(d LeaderDelta) {
	board := &t.Cheerers
	if d.Board == BoardGift {
		board = &t.Gifters
	}
	if *board == nil {
		*board = map[string]Leader{}
	}
	entry := (*board)[d.User.ID]
	entry.User = d.User
	entry.Amount += d.Amount
	(*board)[d.User.ID] = entry
}

func (t Tally) TopCheerer() *Leader { return topOf(t.Cheerers) }

func (t Tally) TopGifter() *Leader { return topOf(t.Gifters) }

func topOf(board map[string]Leader) *Leader {
	var top *Leader
	for _, entry := range board {
		if outranks(entry, top) {
			top = &entry
		}
	}
	return top
}

func outranks(a Leader, b *Leader) bool {
	if b == nil || a.Amount != b.Amount {
		return b == nil || a.Amount > b.Amount
	}
	return a.ID < b.ID
}

func (c Counts) clone() Counts {
	c.Tiers = maps.Clone(c.Tiers)
	return c
}

// State folds channel events into stats. Counts, leaderboards and credits are sums and latest keeps the newest event,
// so folding the same events in any order gives the same Stats.
type State struct {
	Session, Week, Month, AllTime Tally
	Latest                        map[Kind]Event
	Credits                       map[Kind]map[string]Leader
}

// Apply folds e and reports whether it changed the stats.
func (s *State) Apply(e channelevents.Event) bool {
	delta := DeltaOf(e)
	event, framed := FromChannel(e)
	if len(delta.Counts) == 0 && !framed {
		return false
	}
	for _, tally := range []*Tally{&s.Session, &s.Week, &s.Month, &s.AllTime} {
		tally.Apply(delta)
	}
	for _, credit := range delta.Credits {
		s.addCredit(credit)
	}
	if framed && event.Kind != Chat {
		s.setLatest(event)
	}
	return true
}

func (s *State) setLatest(e Event) {
	if s.Latest == nil {
		s.Latest = map[Kind]Event{}
	}
	current, ok := s.Latest[e.Kind]
	if !ok || newer(e, current) {
		s.Latest[e.Kind] = e
	}
}

func newer(a, b Event) bool {
	if a.At.Equal(b.At) {
		return a.ID > b.ID
	}
	return a.At.After(b.At)
}

func (s *State) addCredit(d CreditDelta) {
	if s.Credits == nil {
		s.Credits = map[Kind]map[string]Leader{}
	}
	if s.Credits[d.List] == nil {
		s.Credits[d.List] = map[string]Leader{}
	}
	entry := s.Credits[d.List][d.User.ID]
	entry.User = d.User
	entry.Amount += d.Amount
	s.Credits[d.List][d.User.ID] = entry
}

// ResetSession starts a new session: its counts and credits clear, week, month, all-time and latest carry on.
func (s *State) ResetSession() {
	s.Session = Tally{}
	s.Credits = nil
}

func (s State) Stats() Stats {
	stats := Stats{
		Session: s.Session.clone(), Week: s.Week.clone(), Month: s.Month.clone(), AllTime: s.AllTime.clone(),
		Latest: maps.Clone(s.Latest), TopCheerer: s.Session.TopCheerer(), TopGifter: s.Session.TopGifter(),
		AllTimeTopCheerer: s.AllTime.TopCheerer(), AllTimeTopGifter: s.AllTime.TopGifter(),
	}
	if len(s.Credits) > 0 {
		stats.Credits = make(map[Kind][]Leader, len(s.Credits))
	}
	for list, board := range s.Credits {
		stats.Credits[list] = RankedLeaders(board)
	}
	return stats
}

// RankedLeaders orders a board by amount, highest first, then by user id.
func RankedLeaders(board map[string]Leader) []Leader {
	out := slices.Collect(maps.Values(board))
	slices.SortFunc(out, func(a, b Leader) int {
		return cmp.Or(cmp.Compare(b.Amount, a.Amount), cmp.Compare(a.ID, b.ID))
	})
	return out
}
