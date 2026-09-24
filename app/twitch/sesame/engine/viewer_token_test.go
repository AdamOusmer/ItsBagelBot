// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type stubFollowage struct {
	result FollowageResult
	err    error
	calls  []string
}

func (s *stubFollowage) Lookup(_ context.Context, _, targetID, targetLogin string) (FollowageResult, error) {
	s.calls = append(s.calls, targetID+"/"+targetLogin)
	return s.result, s.err
}

type stubAccountAge struct {
	result AccountAgeResult
	err    error
	calls  []string
}

func (s *stubAccountAge) Lookup(_ context.Context, targetID, targetLogin string) (AccountAgeResult, error) {
	s.calls = append(s.calls, targetID+"/"+targetLogin)
	return s.result, s.err
}

type balanceLoyalty struct {
	LoyaltyStore
	balances map[uint64]loyaltyrpc.Balance
	err      error
	calls    []uint64
}

func (f *balanceLoyalty) BalanceGet(_ context.Context, _, viewerID uint64) (loyaltyrpc.Balance, error) {
	f.calls = append(f.calls, viewerID)
	return f.balances[viewerID], f.err
}

type viewerFixture struct {
	response   string
	modules    map[string]projection.ModuleView
	followage  FollowageLookup
	accountAge AccountAgeLookup
	loyalty    LoyaltyStore
}

func on() projection.ModuleView  { return projection.ModuleView{IsEnabled: true} }
func off() projection.ModuleView { return projection.ModuleView{IsEnabled: false} }

func loyaltyOn(name string) projection.ModuleView {
	return projection.ModuleView{IsEnabled: true, Configs: []byte(`{"pointsName":"` + name + `"}`)}
}

func viewerPipeline(t *testing.T, f viewerFixture) *Pipeline {
	t.Helper()
	d := Deps{
		Proj: fakeReader{
			cmd:      projection.Command{Name: "brag", Response: f.response, IsActive: true, Perm: "everyone"},
			cmdFound: true,
			modules:  f.modules,
		},
		Live:       liveAlways{},
		Cooldown:   NoopCooldown{},
		Pub:        &fakePublisher{},
		Followage:  f.followage,
		AccountAge: f.accountAge,
		Loyalty:    f.loyalty,
		Log:        zap.NewNop(),
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

func expandViewer(t *testing.T, p *Pipeline, line string) string {
	t.Helper()
	got := collectDispatch(p, chatCtx(line, ""))
	require.Len(t, got, 1)
	return got[0].Text
}

type viewerCase struct {
	name    string
	fixture viewerFixture
	want    string
}

func runViewerCases(t *testing.T, cases []viewerCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := viewerPipeline(t, tc.fixture)
			assert.Equal(t, tc.want, expandViewer(t, p, "!brag"))
		})
	}
}

func TestSpanTokensExpandThroughTheModuleGates(t *testing.T) {
	followedAt := time.Now().Add(-90 * 24 * time.Hour)
	createdAt := time.Now().Add(-3 * 365 * 24 * time.Hour)

	runViewerCases(t, []viewerCase{
		{
			name: "followage and account age render the humanized span",
			fixture: viewerFixture{
				response:   "{user} has followed for {followage} on an account {accountage} old",
				modules:    map[string]projection.ModuleView{},
				followage:  &stubFollowage{result: FollowageResult{UserFound: true, Following: true, FollowedAt: followedAt}},
				accountAge: &stubAccountAge{result: AccountAgeResult{UserFound: true, CreatedAt: createdAt}},
			},
			want: "alice has followed for 3 months on an account 3 years old",
		},
		{
			name: "an explicit module-off leaves the span literal",
			fixture: viewerFixture{
				response:  "followed for {followage|a while}",
				modules:   map[string]projection.ModuleView{FollowageModuleName: off()},
				followage: &stubFollowage{result: FollowageResult{UserFound: true, Following: true, FollowedAt: followedAt}},
			},
			want: "followed for {followage|a while}",
		},
		{
			name: "a viewer who does not follow renders the fallback",
			fixture: viewerFixture{
				response:  "followed for {followage|not yet}",
				modules:   map[string]projection.ModuleView{},
				followage: &stubFollowage{result: FollowageResult{UserFound: true}},
			},
			want: "followed for not yet",
		},
		{
			name: "a failed lookup degrades to the fallback, never to a literal",
			fixture: viewerFixture{
				response:  "followed for {followage|a while}",
				modules:   map[string]projection.ModuleView{},
				followage: &stubFollowage{err: errors.New("outgress down")},
			},
			want: "followed for a while",
		},
	})
}

func TestLoyaltyTokensExpandThroughTheModuleGate(t *testing.T) {
	runViewerCases(t, []viewerCase{
		{
			name: "loyalty tokens render the balance and the currency name",
			fixture: viewerFixture{
				response: "{user}: {points} {pointsname}, {watchtime} watched",
				modules:  map[string]projection.ModuleView{LoyaltyModuleName: loyaltyOn("bagels")},
				loyalty:  &balanceLoyalty{balances: map[uint64]loyaltyrpc.Balance{999: {Points: 1280, WatchSeconds: 9000}}},
			},
			want: "alice: 1280 bagels, 2 hours, 30 minutes watched",
		},
		{
			name: "an enabled module with no configured name uses the default",
			fixture: viewerFixture{
				response: "{points} {pointsname}",
				modules:  map[string]projection.ModuleView{LoyaltyModuleName: on()},
				loyalty:  &balanceLoyalty{balances: map[uint64]loyaltyrpc.Balance{999: {Points: 7}}},
			},
			want: "7 points",
		},
		{
			name: "loyalty off leaves every loyalty token literal",
			fixture: viewerFixture{
				response: "{points} {pointsname} {watchtime}",
				modules:  map[string]projection.ModuleView{},
				loyalty:  &balanceLoyalty{},
			},
			want: "{points} {pointsname} {watchtime}",
		},
		{
			name: "an unresolvable named viewer renders the fallback",
			fixture: viewerFixture{
				response: "ferret_king has {points:ferret_king|no} points",
				modules:  map[string]projection.ModuleView{LoyaltyModuleName: on()},
				loyalty:  &balanceLoyalty{},
			},
			want: "ferret_king has no points",
		},
		{
			name: "a wired reader with no token in the template changes nothing",
			fixture: viewerFixture{
				response:  "just {user}",
				modules:   map[string]projection.ModuleView{},
				followage: &stubFollowage{},
			},
			want: "just alice",
		},
	})
}

func TestViewerTokensLookUpEachViewerOnce(t *testing.T) {
	follow := &stubFollowage{result: FollowageResult{UserFound: true, Following: true, FollowedAt: time.Now().Add(-time.Hour)}}
	loyalty := &balanceLoyalty{balances: map[uint64]loyaltyrpc.Balance{999: {Points: 3, WatchSeconds: 60}}}
	p := viewerPipeline(t, viewerFixture{
		response:  "{followage} {followage:alice} {followage:@ALICE} {points} {watchtime}",
		modules:   map[string]projection.ModuleView{LoyaltyModuleName: on()},
		followage: follow,
		loyalty:   loyalty,
	})

	assert.Equal(t, "1 hour 1 hour 1 hour 3 1 minute", expandViewer(t, p, "!brag"))
	assert.Equal(t, []string{"999/alice"}, follow.calls,
		"one lookup, and the chatter's own id rides it so the RPC skips a login resolution")
	assert.Equal(t, []uint64{999}, loyalty.calls, "{points} and {watchtime} share one balance read")
}

func TestViewerTokensResolveANamedViewer(t *testing.T) {
	follow := &stubFollowage{result: FollowageResult{UserFound: true, Following: true, FollowedAt: time.Now().Add(-48 * time.Hour)}}
	loyalty := &balanceLoyalty{balances: map[uint64]loyaltyrpc.Balance{7: {Points: 55}}}
	p := viewerPipeline(t, viewerFixture{
		response:  "{followage:@bob} / {points:BOB}",
		modules:   map[string]projection.ModuleView{LoyaltyModuleName: on()},
		followage: follow,
		loyalty:   loyalty,
	})
	p.roster.Observe(123, chatterIdentity{login: "bob", id: "7", name: "Bob"})

	assert.Equal(t, "2 days / 55", expandViewer(t, p, "!brag"))
	assert.Equal(t, []string{"/bob"}, follow.calls)
	assert.Equal(t, []uint64{7}, loyalty.calls)
}

func TestViewerTokensNeverMutate(t *testing.T) {
	loyalty := &balanceLoyalty{balances: map[uint64]loyaltyrpc.Balance{999: {Points: 10}}}
	p := viewerPipeline(t, viewerFixture{
		response: "{points} {points} {points}",
		modules:  map[string]projection.ModuleView{LoyaltyModuleName: on()},
		loyalty:  loyalty,
	})

	assert.Equal(t, "10 10 10", expandViewer(t, p, "!brag"))
	assert.Equal(t, []uint64{999}, loyalty.calls, "read once, rendered three times")
}

func TestViewerTokensLeaveUnknownSpansLiteral(t *testing.T) {
	p := viewerPipeline(t, viewerFixture{
		response:  "{followage} {followage_years} {pointsrank}",
		modules:   map[string]projection.ModuleView{},
		followage: &stubFollowage{result: FollowageResult{UserFound: true, Following: true, FollowedAt: time.Now().Add(-time.Hour)}},
	})

	assert.Equal(t, "1 hour {followage_years} {pointsrank}", expandViewer(t, p, "!brag"))
}
