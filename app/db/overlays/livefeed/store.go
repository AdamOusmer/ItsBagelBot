// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed

import (
	"context"
	"maps"
	"slices"
	"time"

	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/internal/domain/overlays/feed"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/valkey"

	valkey_go "github.com/valkey-io/valkey-go"
)

type store struct{ client valkey_go.Client }

func (s store) state(c channel) valkey.VersionedHash {
	return valkey.VersionedHash{Key: c.state(), TTL: stateTTL}
}

func (s store) session(ctx context.Context, c channel) (sessionID, error) {
	fields, err := s.state(c).ReadFields(ctx, s.client, fieldSession)
	if current, found := fields[fieldSession]; found {
		return sessionID(current.Value), err
	}
	return defaultSession, err
}

// roll makes the event's id the channel's session unless a newer session already holds the slot.
func (s store) roll(ctx context.Context, c channel, e channelevents.Event) (bool, error) {
	return s.state(c).Set(ctx, s.client, fieldSession, valkey.VersionedValue{Value: e.ID, Version: e.Version()})
}

type folding struct {
	c       channel
	session sessionID
	event   channelevents.Event
	delta   feed.Delta
}

// fold adds the event's delta to every window once per event id and reports whether it did.
func (s store) fold(ctx context.Context, f folding) (bool, error) {
	fields := make([]valkey.FieldDelta, len(f.delta.Counts))
	for i, count := range f.delta.Counts {
		fields[i] = valkey.FieldDelta{Field: count.Field, Delta: count.Amount}
	}
	hashes := make([]valkey.HashTally, len(tallyPeriods))
	for i, period := range tallyPeriods {
		hashes[i] = valkey.HashTally{Key: f.c.counts(period, f.event.OccurredAt, f.session), TTL: period.TTL(), Fields: fields}
	}
	receipt := valkey.ReceiptTally{Receipt: valkey.Key{Name: f.c.receipt(f.event.ID), TTL: receiptTTL}}
	return receipt.Add(ctx, s.client, hashes, f.rankings())
}

func (f folding) rankings() []valkey.RankedDelta {
	var ranked []valkey.RankedDelta
	if leader := f.delta.Leader; leader != nil {
		for _, period := range tallyPeriods {
			ranked = append(ranked, valkey.RankedDelta{
				Key: f.c.board(leader.Board, period, f.event.OccurredAt, f.session), TTL: period.TTL(), Member: leader.User.ID, Delta: leader.Amount,
			})
		}
	}
	for _, credit := range f.delta.Credits {
		ranked = append(ranked, valkey.RankedDelta{
			Key: f.c.credits(credit.List, f.session), TTL: valkey.PeriodSession.TTL(), Member: credit.User.ID, Delta: credit.Amount,
		})
	}
	return ranked
}

// remember keeps what is not a sum: each kind's newest event, the display names the leaderboards need, and the session's event log.
func (s store) remember(ctx context.Context, f folding, frame feed.Event) error {
	encoded, err := codec.Marshal(frame)
	if err != nil {
		return err
	}
	latest := valkey.VersionedHash{Key: f.c.latest(), TTL: stateTTL}
	if _, err := latest.Set(ctx, s.client, string(frame.Kind), valkey.VersionedValue{Value: string(encoded), Version: frame.Version()}); err != nil {
		return err
	}
	log := valkey.ScoredLog{Key: f.c.events(f.session), TTL: valkey.PeriodSession.TTL()}
	if err := log.Add(ctx, s.client, float64(frame.Version()), string(encoded)); err != nil {
		return err
	}
	return s.rememberNames(ctx, f)
}

func (s store) rememberNames(ctx context.Context, f folding) error {
	names := valkey.VersionedHash{Key: f.c.names(), TTL: stateTTL}
	for _, user := range usersOf(f.delta) {
		encoded, err := codec.Marshal(user)
		if err != nil {
			return err
		}
		if _, err := names.Set(ctx, s.client, user.ID, valkey.VersionedValue{Value: string(encoded), Version: f.event.Version()}); err != nil {
			return err
		}
	}
	return nil
}

func usersOf(delta feed.Delta) []feed.User {
	var users []feed.User
	if delta.Leader != nil {
		users = append(users, delta.Leader.User)
	}
	for _, credit := range delta.Credits {
		users = append(users, credit.User)
	}
	return users
}

func (s store) counts(ctx context.Context, key string) (feed.Counts, error) {
	values, err := valkey.HashAllInts(ctx, s.client, key)
	var counts feed.Counts
	for field, amount := range values {
		counts.Add(feed.CountDelta{Field: field, Amount: amount})
	}
	return counts, err
}

func (s store) ranked(ctx context.Context, key string, top int64) ([]valkey.RankedEntry, error) {
	board := valkey.RankedTally{Key: key}
	if top > 0 {
		return board.Top(ctx, s.client, top)
	}
	return board.All(ctx, s.client)
}

func (s store) latest(ctx context.Context, c channel) (map[feed.Kind]feed.Event, error) {
	raw, err := valkey.VersionedHash{Key: c.latest(), TTL: stateTTL}.Read(ctx, s.client)
	if err != nil {
		return nil, err
	}
	latest := make(map[feed.Kind]feed.Event, len(raw))
	for kind, value := range raw {
		var event feed.Event
		if err := codec.Unmarshal([]byte(value.Value), &event); err != nil {
			return nil, err
		}
		latest[feed.Kind(kind)] = event
	}
	return latest, nil
}

func (s store) who(ctx context.Context, c channel, ids []string) (map[string]feed.User, error) {
	raw, err := valkey.VersionedHash{Key: c.names(), TTL: stateTTL}.ReadFields(ctx, s.client, ids...)
	if err != nil {
		return nil, err
	}
	users := make(map[string]feed.User, len(ids))
	for _, id := range ids {
		user := feed.User{ID: id}
		if value, found := raw[id]; found {
			if err := codec.Unmarshal([]byte(value.Value), &user); err != nil {
				return nil, err
			}
		}
		users[id] = user
	}
	return users, nil
}

func (s store) recent(ctx context.Context, c channel, session sessionID, limit int64) ([]feed.Event, error) {
	log := valkey.ScoredLog{Key: c.events(session), TTL: valkey.PeriodSession.TTL()}
	read := log.All
	if limit > 0 {
		read = func(ctx context.Context, client valkey_go.Client) ([]string, error) {
			return log.Latest(ctx, client, limit)
		}
	}
	members, err := read(ctx, s.client)
	if err != nil {
		return nil, err
	}
	events := make([]feed.Event, len(members))
	for i, member := range members {
		if err := codec.Unmarshal([]byte(member), &events[len(members)-1-i]); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func (s store) clearRecent(ctx context.Context, c channel, session sessionID) error {
	return valkey.NewKV(s.client).Del(ctx, c.events(session))
}

func (s store) stats(ctx context.Context, c channel, session sessionID, totals repository.FeedTotals) (feed.Stats, error) {
	now := time.Now()
	stats := feed.Stats{AllTime: totals.Counts, AllTimeTopCheerer: totals.TopCheerer, AllTimeTopGifter: totals.TopGifter}
	for _, window := range []struct {
		period valkey.Period
		into   *feed.Counts
	}{{valkey.PeriodSession, &stats.Session}, {valkey.PeriodWeek, &stats.Week}, {valkey.PeriodMonth, &stats.Month}} {
		counts, err := s.counts(ctx, c.counts(window.period, now, session))
		if err != nil {
			return feed.Stats{}, err
		}
		*window.into = counts
	}
	var err error
	if stats.Latest, err = s.latest(ctx, c); err != nil {
		return feed.Stats{}, err
	}
	return s.withLeaders(ctx, c, session, stats)
}

func (s store) withLeaders(ctx context.Context, c channel, session sessionID, stats feed.Stats) (feed.Stats, error) {
	cheer, err := s.ranked(ctx, c.board(feed.BoardCheer, valkey.PeriodSession, time.Now(), session), 1)
	if err != nil {
		return feed.Stats{}, err
	}
	gift, err := s.ranked(ctx, c.board(feed.BoardGift, valkey.PeriodSession, time.Now(), session), 1)
	if err != nil {
		return feed.Stats{}, err
	}
	users, err := s.who(ctx, c, memberIDs(nil, cheer, gift))
	if err != nil {
		return feed.Stats{}, err
	}
	stats.TopCheerer, stats.TopGifter = leaderOf(cheer, users), leaderOf(gift, users)
	return stats, nil
}

func (s store) credits(ctx context.Context, c channel, session sessionID) (map[feed.Kind][]feed.Leader, error) {
	lists, err := s.creditBoards(ctx, c, session)
	if err != nil {
		return nil, err
	}
	users, err := s.who(ctx, c, memberIDs(lists))
	if err != nil {
		return nil, err
	}
	return creditsOf(lists, users), nil
}

func (s store) touchedCredits(ctx context.Context, f folding) (map[feed.Kind][]feed.Leader, error) {
	touched := make(map[feed.Kind][]feed.Leader, len(f.delta.Credits))
	for _, credit := range f.delta.Credits {
		totals, err := s.rankedTotals(ctx, f.c.credits(credit.List, f.session), credit.User.ID)
		if err != nil {
			return nil, err
		}
		touched[credit.List] = append(touched[credit.List], feed.Leader{User: credit.User, Amount: totals})
	}
	return touched, nil
}

func (s store) rankedTotals(ctx context.Context, key, member string) (int64, error) {
	totals, err := valkey.RankedTally{Key: key}.Totals(ctx, s.client, []string{member})
	if err != nil {
		return 0, err
	}
	return totals[0], nil
}

func (s store) creditBoards(ctx context.Context, c channel, session sessionID) (map[feed.Kind][]valkey.RankedEntry, error) {
	lists := make(map[feed.Kind][]valkey.RankedEntry, len(creditLists))
	for _, list := range creditLists {
		entries, err := s.ranked(ctx, c.credits(list, session), 0)
		if err != nil {
			return nil, err
		}
		if len(entries) > 0 {
			lists[list] = entries
		}
	}
	return lists, nil
}

func creditsOf(lists map[feed.Kind][]valkey.RankedEntry, users map[string]feed.User) map[feed.Kind][]feed.Leader {
	if len(lists) == 0 {
		return nil
	}
	credits := make(map[feed.Kind][]feed.Leader, len(lists))
	for list, entries := range lists {
		credits[list] = leadersOf(entries, users)
	}
	return credits
}

func memberIDs(lists map[feed.Kind][]valkey.RankedEntry, boards ...[]valkey.RankedEntry) []string {
	seen := map[string]struct{}{}
	var ids []string
	for _, entries := range append(slices.Collect(maps.Values(lists)), boards...) {
		for _, entry := range entries {
			if _, dup := seen[entry.Member]; !dup {
				seen[entry.Member] = struct{}{}
				ids = append(ids, entry.Member)
			}
		}
	}
	return ids
}

func leadersOf(entries []valkey.RankedEntry, users map[string]feed.User) []feed.Leader {
	leaders := make([]feed.Leader, len(entries))
	for i, entry := range entries {
		leaders[i] = feed.Leader{User: users[entry.Member], Amount: entry.Total}
	}
	return leaders
}

func leaderOf(entries []valkey.RankedEntry, users map[string]feed.User) *feed.Leader {
	if len(entries) == 0 {
		return nil
	}
	return &leadersOf(entries[:1], users)[0]
}
