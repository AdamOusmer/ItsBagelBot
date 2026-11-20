// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package eligibility_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/app/twitch/sesame/module"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/codec"

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
	lookups []eligibility.StandingLookup
}

func (d *eligibilityData) Snapshot(_ context.Context, lookup eligibility.StandingLookup) (loyaltyrpc.BalanceSnapshot, error) {
	d.calls = append(d.calls, "standings")
	d.lookups = append(d.lookups, lookup)
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
	frozen := loyaltyrpc.BalanceSnapshot{Found: true, Points: 100, WatchSeconds: 1800}
	cutoff := eligibility.Cutoff{ID: "snapshot", OpenedAt: frozenAt}
	cases := []struct {
		name  string
		rules eligibility.Rules
		who   eligibility.Joiner
		data  eligibilityData
		want  eligibility.Verdict
		reads int
	}{
		{name: "giveaway excludes its broadcaster", rules: eligibility.Rules{MinPoints: 1},
			who: eligibility.Joiner{Channel: 2, Viewer: 2, Kind: draw.KindGiveaway}, want: eligibility.Verdict{Reason: eligibility.ReasonBroadcaster}},
		{name: "mods excluded before standings", rules: eligibility.Rules{ExcludeMods: true, MinPoints: 1},
			who: eligibility.Joiner{Channel: 2, Viewer: 7, Role: module.RoleLeadModerator}, want: eligibility.Verdict{Reason: eligibility.ReasonMod}},
		{name: "VIP rule", rules: eligibility.Rules{Role: eligibility.VIP, MinPoints: 1}, who: everyone,
			want: eligibility.Verdict{Reason: eligibility.ReasonRole}},
		{name: "points before watch, missing row counts as zero", rules: eligibility.Rules{MinPoints: 10, MinWatchSeconds: 1}, who: everyone,
			want: eligibility.Verdict{Reason: eligibility.ReasonPoints, Need: 10, AsOf: checkTime}, reads: 1},
		{name: "watch short on frozen standings", rules: eligibility.Rules{MinPoints: 100, MinWatchSeconds: 3600}, who: eligibility.Joiner{Channel: 2, Viewer: 7, Cutoff: cutoff},
			data: eligibilityData{balance: frozen}, want: eligibility.Verdict{Reason: eligibility.ReasonWatch, Need: 3600, Had: 1800, AsOf: frozenAt}, reads: 1},
		{name: "subscriber luck", rules: eligibility.Rules{Role: eligibility.Sub, SubLuckPercent: 200, MinPoints: 100, MinWatchSeconds: 1800},
			who: eligibility.Joiner{Channel: 2, Viewer: 7, Role: module.RoleSubscriber, Subscriber: true, Cutoff: cutoff}, data: eligibilityData{balance: frozen},
			want: eligibility.Verdict{Pass: true, Weight: 200, AsOf: frozenAt}, reads: 1},
		{name: "draw-time rules do not read anything at join", rules: eligibility.Rules{FollowersOnly: true, MinAccountDays: 1, SkipWinnersDays: 1}, who: everyone,
			want: eligibility.Verdict{Pass: true, Weight: draw.Pins.EntryWeight}},
		{name: "snapshot failed", rules: eligibility.Rules{MinPoints: 1}, who: everyone,
			data: eligibilityData{err: errors.New("no reply")}, reads: 1},
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

func TestJoinSnapshotCache(t *testing.T) {
	for _, tc := range []struct {
		name, firstID, secondID string
		left, advance           time.Duration
		secondPass              bool
	}{
		{name: "live data is read again", secondPass: true},
		{name: "same private cutoff is reused", firstID: "round-a", secondID: "round-a", left: time.Minute},
		{name: "another round has a separate cutoff", firstID: "round-a", secondID: "round-b", left: time.Minute, secondPass: true},
		{name: "cached cutoff expires at the policy window", firstID: "round-a", secondID: "round-a", left: time.Second, advance: time.Second, secondPass: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &eligibilityData{}
			now := checkTime
			c := eligibility.NewChecker(eligibility.Ports{Standings: data, Now: func() time.Time { return now }})
			t.Cleanup(c.Close)
			rules := eligibility.Rules{MinPoints: 1}
			openedAt := checkTime.Add(tc.left - draw.Pins.GiveawayMaxEntryWindow)
			who := eligibility.Joiner{Channel: 2, Viewer: 7, Cutoff: eligibility.Cutoff{ID: tc.firstID, OpenedAt: openedAt}}
			first, err := c.Join(t.Context(), rules, who)
			require.NoError(t, err)
			data.balance = loyaltyrpc.BalanceSnapshot{Points: 1, Found: true}
			now = now.Add(tc.advance)
			who.Cutoff.ID = tc.secondID
			second, err := c.Join(t.Context(), rules, who)
			require.NoError(t, err)
			require.Equal(t, []bool{false, tc.secondPass}, []bool{first.Pass, second.Pass})
			require.Equal(t, tc.firstID, data.lookups[0].SnapshotID)
		})
	}
}

func TestVerifyDrawRules(t *testing.T) {
	const day = 24 * time.Hour
	old := checkTime.Add(-7 * day)
	retry := &engine.RetryAfterError{After: 3 * time.Second}
	rules := eligibility.Rules{MinFollowDays: 7, MinAccountDays: 7, SkipWinnersDays: 7}
	raw, err := codec.Marshal(rules)
	require.NoError(t, err)
	passed := draw.Verdict{Pass: true}
	for _, tc := range []struct {
		name  string
		kind  draw.Kind
		data  eligibilityData
		want  draw.Verdict
		calls []string
		err   error
	}{
		{"passes at cutoff", draw.KindGiveaway, eligibilityData{follow: old, account: old}, passed, []string{"follow", "account", "winners"}, nil},
		{"raffle avoids all lookups", draw.KindRaffle, eligibilityData{}, passed, nil, nil},
		{"not following", draw.KindGiveaway, eligibilityData{}, draw.Verdict{Reason: "follow"}, []string{"follow"}, nil},
		{"missing followers scope", draw.KindGiveaway, eligibilityData{err: eligibility.ErrFollowScope}, draw.Verdict{Reason: "scope"}, []string{"follow"}, nil},
		{"follow too recent", draw.KindGiveaway, eligibilityData{follow: old.Add(time.Second)}, rejectedAge(eligibility.ReasonFollowAge), []string{"follow"}, nil},
		{"account missing", draw.KindGiveaway, eligibilityData{follow: old}, rejectedAge(eligibility.ReasonAccountAge), []string{"follow", "account"}, nil},
		{"account too new", draw.KindGiveaway, eligibilityData{follow: old, account: old.Add(time.Second)}, rejectedAge(eligibility.ReasonAccountAge), []string{"follow", "account"}, nil},
		{"recent winner", draw.KindGiveaway, eligibilityData{follow: old, account: old, winners: []uint64{7}}, rejectedAge(eligibility.ReasonRecentWinner), []string{"follow", "account", "winners"}, nil},
		{"throttled", draw.KindGiveaway, eligibilityData{err: retry}, draw.Verdict{}, []string{"follow"}, retry},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChecker(t, &tc.data)
			r := draw.Round{Channel: 2, Terms: draw.Terms{Kind: tc.kind, Rules: raw}}
			got, err := c.Verify(t.Context(), draw.Check{Round: r, Viewer: "7"})
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.calls, tc.data.calls)
			if len(tc.calls) == 3 {
				require.Equal(t, old, tc.data.since)
			}
		})
	}
}

func rejectedAge(reason eligibility.Reason) draw.Verdict {
	return draw.Verdict{Reason: string(reason), Detail: map[string]string{"need": "7"}}
}

func TestEligibilityPolicy(t *testing.T) {
	bounds := eligibility.RuleLimits{MaxPoints: 1_000_000_000, MaxWatchHours: 10_000, MaxFollowDays: 3650,
		MaxAccountDays: 3650, MaxSkipDays: 90, LuckMin: 100, LuckMax: 300, CheckCooldown: 10 * time.Second, RefusalEnabled: true}
	require.Equal(t, bounds, eligibility.Limits)
	raw := codec.RawMessage(`{"role":"other","min_points":1000000001,"min_watch_seconds":36000001,"min_follow_days":3651,"min_account_days":3651,"skip_winners_days":91,"sub_luck_percent":301}`)
	rules, err := eligibility.Parse(raw)
	require.NoError(t, err)
	require.Equal(t, eligibility.Rules{MinPoints: bounds.MaxPoints, MinWatchSeconds: bounds.MaxWatchHours * 3600,
		MinFollowDays: bounds.MaxFollowDays, MinAccountDays: bounds.MaxAccountDays, SkipWinnersDays: bounds.MaxSkipDays, SubLuckPercent: bounds.LuckMax}, rules)
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

func TestMalformedRulesFailVerification(t *testing.T) {
	for _, raw := range []codec.RawMessage{codec.RawMessage(`{`), codec.RawMessage(`{"min_points":"invalid"}`)} {
		t.Run(string(raw), func(t *testing.T) {
			data := &eligibilityData{}
			checker := newChecker(t, data)
			_, parseErr := eligibility.Parse(raw)
			require.Error(t, parseErr)
			round := draw.Round{Terms: draw.Terms{Kind: draw.KindGiveaway, Rules: raw}}
			verdict, err := checker.Verify(t.Context(), draw.Check{Round: round, Viewer: "7"})
			require.EqualError(t, err, parseErr.Error())
			require.Equal(t, draw.Verdict{}, verdict)
			require.Empty(t, data.calls)
		})
	}
}

func TestDescribe(t *testing.T) {
	for _, tc := range []struct {
		rules eligibility.Rules
		want  eligibility.Requirement
	}{
		{eligibility.Rules{Role: eligibility.Sub}, eligibility.Requirement{Key: "req.sub"}},
		{eligibility.Rules{Role: eligibility.VIP}, eligibility.Requirement{Key: "req.vip"}},
		{eligibility.Rules{ExcludeMods: true}, eligibility.Requirement{Key: "req.exclude_mods"}},
		{eligibility.Rules{MinPoints: 50}, eligibility.Requirement{Key: "req.points", Value: 50}},
		{eligibility.Rules{MinWatchSeconds: 3600}, eligibility.Requirement{Key: "req.watch_hours", Value: 3600}},
		{eligibility.Rules{FollowersOnly: true}, eligibility.Requirement{Key: "req.followers"}},
		{eligibility.Rules{FollowersOnly: true, MinFollowDays: 7}, eligibility.Requirement{Key: "req.follow_days", Value: 7}},
		{eligibility.Rules{MinAccountDays: 14}, eligibility.Requirement{Key: "req.account_days", Value: 14}},
		{eligibility.Rules{SkipWinnersDays: 30}, eligibility.Requirement{Key: "req.skip_winners_days", Value: 30}},
		{eligibility.Rules{SubLuckPercent: 200}, eligibility.Requirement{Key: "req.sub_luck", Value: 200}},
	} {
		t.Run(tc.want.Key, func(t *testing.T) {
			require.Equal(t, []eligibility.Requirement{tc.want}, eligibility.Describe(tc.rules))
		})
	}
}
