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

// stubFollowage answers the followage RPC from a fixture and counts calls, so
// a test can pin both the rendered span and the fan-out.
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

// balanceLoyalty answers BalanceGet per viewer id; every other verb is
// unreachable from the custom-command path, so the embedded interface stays
// nil.
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

// viewerFixture is one channel's wiring for the viewer tokens: which modules
// are on, and which readers answer them.
type viewerFixture struct {
	response   string
	modules    map[string]projection.ModuleView
	followage  FollowageLookup
	accountAge AccountAgeLookup
	loyalty    LoyaltyStore
}

// on is the module row a broadcaster gets by enabling a module in the
// dashboard; loyaltyOn carries the currency name blob !points reads.
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

// expandViewer runs "!brag" through the real dispatch path and returns the one
// chat line it produced.
func expandViewer(t *testing.T, p *Pipeline, line string) string {
	t.Helper()
	got := collectDispatch(p, chatCtx(line, ""))
	require.Len(t, got, 1)
	return got[0].Text
}

// viewerCase is one row of the tables below: a template, the module rows the
// channel has, and the line chat sees.
type viewerCase struct {
	name    string
	fixture viewerFixture
	want    string
}

// runViewerCases expands each row's template through a pipeline built from its
// fixture, so the two tables assert on the one path a real !brag takes.
func runViewerCases(t *testing.T, cases []viewerCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := viewerPipeline(t, tc.fixture)
			assert.Equal(t, tc.want, expandViewer(t, p, "!brag"))
		})
	}
}

// TestSpanTokensExpandThroughTheModuleGates covers the two duration families,
// which share a gate (the built-in's own module row, which ships enabled) and
// share the rule that every unresolvable outcome renders the fallback.
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
			// A built-in module ships enabled, so a channel with no row at all
			// still expands the token — the same polarity !followage runs on.
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

// TestLoyaltyTokensExpandThroughTheModuleGate covers the loyalty family, whose
// gate is the opposite polarity: the module is opt-in, so no row means the
// tokens stay literal rather than printing a zero.
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
			// The currency name falls back exactly the way !points does.
			name: "an enabled module with no configured name uses the default",
			fixture: viewerFixture{
				response: "{points} {pointsname}",
				modules:  map[string]projection.ModuleView{LoyaltyModuleName: on()},
				loyalty:  &balanceLoyalty{balances: map[uint64]loyaltyrpc.Balance{999: {Points: 7}}},
			},
			want: "7 points",
		},
		{
			// Loyalty is opt-in: no row means the broadcaster never turned it
			// on, so the tokens stay literal rather than printing a zero.
			name: "loyalty off leaves every loyalty token literal",
			fixture: viewerFixture{
				response: "{points} {pointsname} {watchtime}",
				modules:  map[string]projection.ModuleView{},
				loyalty:  &balanceLoyalty{},
			},
			want: "{points} {pointsname} {watchtime}",
		},
		{
			// A named viewer nobody has spoken where this replica could see is
			// not resolvable to an id, so the span renders its fallback rather
			// than the sender's own balance under someone else's name.
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

// A template naming one viewer several ways costs one lookup per family: the
// scope plans before it renders, and folds the spellings first.
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

// A named viewer the roster has seen speak resolves through the same path a
// target-addressed counter uses, and carries no id into the followage RPC —
// the shape parseLookupTarget hands !followage for an explicit "@name".
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

// Nothing on this path grants, spends or bumps: the family is read-only, so a
// response spamming {points} moves no balance.
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

// An unknown token stays literal beside a resolved one, the rule every token
// family in this palette shares.
func TestViewerTokensLeaveUnknownSpansLiteral(t *testing.T) {
	p := viewerPipeline(t, viewerFixture{
		response:  "{followage} {followage_years} {pointsrank}",
		modules:   map[string]projection.ModuleView{},
		followage: &stubFollowage{result: FollowageResult{UserFound: true, Following: true, FollowedAt: time.Now().Add(-time.Hour)}},
	})

	assert.Equal(t, "1 hour {followage_years} {pointsrank}", expandViewer(t, p, "!brag"))
}
