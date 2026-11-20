// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package eligibility_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/invalidate"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/require"
)

var checkTime = time.Date(2026, 11, 19, 20, 14, 0, 0, time.UTC)

type eligibilityData struct {
	balance loyaltyrpc.BalanceSnapshot
	follow  time.Time
	account time.Time
	winners []uint64
	err     error
	calls   []string
	since   time.Time
}

func (d *eligibilityData) Snapshot(context.Context, uint64, uint64) (loyaltyrpc.BalanceSnapshot, error) {
	d.calls = append(d.calls, "standings")
	return d.balance, d.err
}

func (d *eligibilityData) FollowedAt(context.Context, uint64, uint64) (time.Time, bool, error) {
	d.calls = append(d.calls, "follow")
	return d.follow, !d.follow.IsZero(), d.err
}

func (d *eligibilityData) CreatedAt(context.Context, uint64) (time.Time, bool, error) {
	d.calls = append(d.calls, "account")
	return d.account, !d.account.IsZero(), d.err
}

func (d *eligibilityData) WonWithin(_ context.Context, _ uint64, _ []uint64, since time.Time) ([]uint64, error) {
	d.calls = append(d.calls, "winners")
	d.since = since
	return d.winners, d.err
}

func newChecker(t *testing.T, data *eligibilityData) *eligibility.Checker {
	t.Helper()
	c := eligibility.NewChecker(eligibility.Ports{Standings: data, Follows: data, Accounts: data,
		Winners: data, Now: func() time.Time { return checkTime }})
	t.Cleanup(c.Close)
	return c
}

func TestJoinRules(t *testing.T) {
	everyone := eligibility.Joiner{Channel: 2, Viewer: 7}
	frozenAt := checkTime.Add(-time.Minute)
	frozen := loyaltyrpc.BalanceSnapshot{Found: true, Points: 100, WatchSeconds: 1800,
		FreezeStamp: loyaltyrpc.FreezeStamp{FreezeID: "freeze", FrozenAsOf: frozenAt.UnixMilli(), ExpiresAt: checkTime.Add(time.Minute).UnixMilli()}}
	cases := []struct {
		name  string
		rules eligibility.Rules
		who   eligibility.Joiner
		data  eligibilityData
		want  eligibility.Verdict
		reads int
	}{
		{name: "broadcaster before bot", rules: eligibility.Rules{MinPoints: 1},
			who: eligibility.Joiner{Channel: 2, Viewer: 2, IsBot: true}, want: eligibility.Verdict{Reason: eligibility.ReasonBroadcaster}},
		{name: "bot", who: eligibility.Joiner{Channel: 2, Viewer: 7, IsBot: true}, want: eligibility.Verdict{Reason: eligibility.ReasonBot}},
		{name: "mods excluded before standings", rules: eligibility.Rules{ExcludeMods: true, MinPoints: 1},
			who: eligibility.Joiner{Channel: 2, Viewer: 7, Role: module.RoleLeadModerator}, want: eligibility.Verdict{Reason: eligibility.ReasonMod}},
		{name: "VIP rule", rules: eligibility.Rules{Role: eligibility.VIP, MinPoints: 1}, who: everyone,
			want: eligibility.Verdict{Reason: eligibility.ReasonRole}},
		{name: "points before watch, missing row counts as zero", rules: eligibility.Rules{MinPoints: 10, MinWatchSeconds: 1}, who: everyone,
			want: eligibility.Verdict{Reason: eligibility.ReasonPoints, Need: 10, AsOf: checkTime}, reads: 1},
		{name: "watch short on frozen standings", rules: eligibility.Rules{MinPoints: 100, MinWatchSeconds: 3600}, who: everyone,
			data: eligibilityData{balance: frozen}, want: eligibility.Verdict{Reason: eligibility.ReasonWatch, Need: 3600, Had: 1800, AsOf: frozenAt}, reads: 1},
		{name: "subscriber luck", rules: eligibility.Rules{Role: eligibility.Sub, SubLuckPercent: 200, MinPoints: 100, MinWatchSeconds: 1800},
			who: eligibility.Joiner{Channel: 2, Viewer: 7, Role: module.RoleSubscriber}, data: eligibilityData{balance: frozen},
			want: eligibility.Verdict{Pass: true, Weight: 200, AsOf: frozenAt}, reads: 1},
		{name: "draw-time rules do not read anything at join", rules: eligibility.Rules{FollowersOnly: true, MinAccountDays: 1, SkipWinnersDays: 1}, who: everyone,
			want: eligibility.Verdict{Pass: true, Weight: draw.Pins.EntryWeight}},
		{name: "snapshot failed", rules: eligibility.Rules{MinPoints: 1}, who: everyone, data: eligibilityData{err: errors.New("no reply")},
			want: eligibility.Verdict{Reason: eligibility.ReasonLookup}, reads: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := newChecker(t, &tc.data).Join(t.Context(), tc.rules, tc.who)
			require.ErrorIs(t, err, tc.data.err)
			require.Equal(t, tc.want, result)
			require.Len(t, tc.data.calls, tc.reads)
		})
	}
}

type freezeFeed struct{ listener func(cache.Event) }

func (f *freezeFeed) Watch(_ string, fn func(cache.Event)) { f.listener = fn }
func (*freezeFeed) WatchGap(func())                        {}

func TestJoinSnapshotCache(t *testing.T) {
	for _, tc := range []struct {
		name, freezeID string
		offset         time.Duration
		invalidate     bool
		secondPass     bool
	}{
		{name: "live data is read again", secondPass: true},
		{name: "frozen data is reused", freezeID: "freeze", offset: time.Minute},
		{name: "expired response is not cached", freezeID: "freeze", offset: -time.Second, secondPass: true},
		{name: "early release invalidates cached data", freezeID: "freeze", offset: time.Minute, invalidate: true, secondPass: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &eligibilityData{balance: loyaltyrpc.BalanceSnapshot{FreezeStamp: loyaltyrpc.FreezeStamp{
				FreezeID: tc.freezeID, ExpiresAt: checkTime.Add(tc.offset).UnixMilli(),
			}}}
			c, feed := newChecker(t, data), &freezeFeed{}
			c.WatchFreezes(feed)
			rules, who := eligibility.Rules{MinPoints: 1}, eligibility.Joiner{Channel: 2, Viewer: 7}
			first, err := c.Join(t.Context(), rules, who)
			require.NoError(t, err)
			data.balance = loyaltyrpc.BalanceSnapshot{Points: 1, Found: true}
			if tc.invalidate {
				feed.listener(cache.Event{Scope: invalidate.ScopeLoyalty, ID: who.Channel, Keys: []string{invalidate.LoyaltyFreezeMarker}})
			}
			second, err := c.Join(t.Context(), rules, who)
			require.NoError(t, err)
			require.Equal(t, []bool{false, tc.secondPass}, []bool{first.Pass, second.Pass})
		})
	}
}

func TestVerifyDrawRules(t *testing.T) {
	const day = 24 * time.Hour
	old := checkTime.Add(-7 * day)
	retry := &engine.RetryAfterError{After: 3 * time.Second}
	rules := eligibility.Rules{MinFollowDays: 7, MinAccountDays: 7, SkipWinnersDays: 7}
	raw, err := json.Marshal(rules)
	require.NoError(t, err)
	passed := draw.Verdict{Pass: true}
	for _, tc := range []struct {
		name  string
		kind  draw.Kind
		data  eligibilityData
		want  draw.Verdict
		calls []string
	}{
		{"passes at cutoff", draw.KindGiveaway, eligibilityData{follow: old, account: old}, passed, []string{"follow", "account", "winners"}},
		{"raffle avoids all lookups", draw.KindRaffle, eligibilityData{}, passed, nil},
		{"not following", draw.KindGiveaway, eligibilityData{}, draw.Verdict{Reason: "follow"}, []string{"follow"}},
		{"follow too recent", draw.KindGiveaway, eligibilityData{follow: old.Add(time.Second)}, rejectedAge("follow_age"), []string{"follow"}},
		{"account missing", draw.KindGiveaway, eligibilityData{follow: old}, rejectedAge("account_age"), []string{"follow", "account"}},
		{"account too new", draw.KindGiveaway, eligibilityData{follow: old, account: old.Add(time.Second)}, rejectedAge("account_age"), []string{"follow", "account"}},
		{"recent winner", draw.KindGiveaway, eligibilityData{follow: old, account: old, winners: []uint64{7}}, rejectedAge("recent_winner"), []string{"follow", "account", "winners"}},
		{"throttled", draw.KindGiveaway, eligibilityData{err: retry}, draw.Verdict{}, []string{"follow"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChecker(t, &tc.data)
			r := draw.Round{Channel: 2, Terms: draw.Terms{Kind: tc.kind, Rules: raw}}
			got, err := c.Verify(t.Context(), draw.Check{Round: r, Viewer: "7"})
			require.ErrorIs(t, err, tc.data.err)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.calls, tc.data.calls)
			if len(tc.calls) == 3 {
				require.Equal(t, old, tc.data.since)
			}
		})
	}
}

func rejectedAge(reason string) draw.Verdict {
	return draw.Verdict{Reason: reason, Detail: map[string]string{"need": "7"}}
}

func TestEligibilityPolicy(t *testing.T) {
	bounds := eligibility.RuleLimits{MaxPoints: 1_000_000_000, MaxWatchHours: 10_000, MaxFollowDays: 3650,
		MaxAccountDays: 3650, MaxSkipDays: 90, LuckMin: 100, LuckMax: 300, CheckCooldown: 10 * time.Second, RefusalLineOn: true}
	require.Equal(t, bounds, eligibility.Limits)
	raw := json.RawMessage(`{"role":"other","min_points":1000000001,"min_watch_seconds":36000001,"min_follow_days":3651,"min_account_days":3651,"skip_winners_days":91,"sub_luck_percent":301}`)
	require.Equal(t, eligibility.Rules{MinPoints: bounds.MaxPoints, MinWatchSeconds: bounds.MaxWatchHours * 3600,
		MinFollowDays: bounds.MaxFollowDays, MinAccountDays: bounds.MaxAccountDays, SkipWinnersDays: bounds.MaxSkipDays, SubLuckPercent: bounds.LuckMax}, eligibility.Parse(raw))
}

func TestOpenGate(t *testing.T) {
	standing := eligibility.Rules{MinWatchSeconds: 1}
	for _, tc := range []struct {
		rules eligibility.Rules
		on    bool
		state string
		want  eligibility.Reason
	}{
		{eligibility.Rules{}, false, "stalled", ""},
		{standing, false, "offline", eligibility.ReasonLoyaltyOff},
		{standing, true, "stalled", eligibility.ReasonStalled},
		{standing, true, "starting", ""},
		{standing, true, "offline", ""},
		{standing, true, "healthy", ""},
	} {
		ok, reason := eligibility.OpenGate(tc.rules, tc.on, tc.state)
		require.Equal(t, tc.want, reason)
		require.Equal(t, tc.want == "", ok)
	}
}

func TestGiveawayRequirements(t *testing.T) {
	rules := eligibility.Rules{MinWatchSeconds: 7200, MinFollowDays: 7, FollowersOnly: true}
	require.Equal(t, "2h of watch time and 7 days of following", engine.GiveawayRequirements("en", rules))
	require.Empty(t, engine.GiveawayRequirements("en", eligibility.Rules{}))
}
