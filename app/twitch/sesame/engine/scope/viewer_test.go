// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSpans struct {
	values map[string]string
	asked  []string
}

func (f *fakeSpans) Span(_ context.Context, login string) string {
	f.asked = append(f.asked, login)
	return f.values[login]
}

type fakeBalances struct {
	values   map[string]Balance
	currency string
	asked    []string
	names    int
}

func (f *fakeBalances) Balance(_ context.Context, login string) Balance {
	f.asked = append(f.asked, login)
	return f.values[login]
}

func (f *fakeBalances) CurrencyName(context.Context) string {
	f.names++
	return f.currency
}

func TestViewerLeavesTokensLiteralWhenNoFamilyIsMounted(t *testing.T) {
	chain := Chain{Viewer{Sender: "alice"}}
	const template = "{followage} {accountage} {points} {watchtime} {pointsname}"
	assert.Equal(t, template, render(t, template, chain, nil),
		"an unmounted family owns nothing, so its spans stay literal like a typo")
}

func TestViewerMountsEachFamilyOnItsOwn(t *testing.T) {
	follow := &fakeSpans{values: map[string]string{"alice": "3 months"}}
	chain := Chain{Viewer{Sender: "alice", Follow: follow}}
	assert.Equal(t, "3 months / {points}", render(t, "{followage} / {points}", chain, nil),
		"followage resolves while loyalty stays literal")
}

func TestViewerResolvesSenderAndNamedViewer(t *testing.T) {
	follow := &fakeSpans{values: map[string]string{"alice": "3 months", "bob": "2 years"}}
	account := &fakeSpans{values: map[string]string{"alice": "5 years"}}
	chain := Chain{Viewer{Sender: "alice", Follow: follow, Account: account}}

	assert.Equal(t, "3 months, 2 years, 5 years",
		render(t, "{followage}, {followage:@Bob}, {accountage}", chain, nil))
	assert.Equal(t, []string{"alice", "bob"}, follow.asked, "'@Bob' folds to the bare login")
}

func TestViewerLooksEachViewerUpOnce(t *testing.T) {
	follow := &fakeSpans{values: map[string]string{"alice": "3 months", "bob": "2 years"}}
	chain := Chain{Viewer{Sender: "alice", Follow: follow}}

	assert.Equal(t, "2 years 2 years 3 months",
		render(t, "{followage:bob} {followage:@BOB} {followage:alice}", chain, nil))
	assert.Equal(t, []string{"bob", "alice"}, follow.asked)
}

func TestViewerFoldsTheBareSpanOntoTheSenderLogin(t *testing.T) {
	follow := &fakeSpans{values: map[string]string{"alice": "3 months"}}
	chain := Chain{Viewer{Sender: "alice", Follow: follow}}

	assert.Equal(t, "3 months 3 months", render(t, "{followage} {followage:alice}", chain, nil))
	assert.Equal(t, []string{"alice"}, follow.asked)
}

func TestViewerRendersFallbackWhenTheLookupFoundNothing(t *testing.T) {
	follow := &fakeSpans{values: map[string]string{}}
	chain := Chain{Viewer{Sender: "alice", Follow: follow}}

	assert.Equal(t, "", render(t, "{followage}", chain, nil))
	require.Equal(t, []string{"alice"}, follow.asked, "the lookup still ran")
	assert.Equal(t, "not yet!", render(t, "{followage|not yet!}", chain, nil))
}

func TestViewerReadsOneBalanceForPointsAndWatchTime(t *testing.T) {
	balances := &fakeBalances{
		currency: "bagels",
		values: map[string]Balance{
			"alice": {Points: 1280, WatchSeconds: 9000, Found: true},
			"bob":   {Points: 4, Found: true},
		},
	}
	chain := Chain{Viewer{Sender: "alice", Balances: balances}}

	assert.Equal(t, "1280 bagels, 2 hours, 30 minutes watched; bob has 4",
		render(t, "{points} {points.name}, {watchtime} watched; bob has {points:bob}", chain, nil))
	assert.Equal(t, []string{"alice", "bob"}, balances.asked,
		"{points} and {watchtime} are two fields of one read")
	assert.Equal(t, 1, balances.names)
}

func TestViewerPointsNameLegacyAlias(t *testing.T) {
	balances := &fakeBalances{currency: "bagels"}
	chain := Chain{Viewer{Sender: "alice", Balances: balances}}
	assert.Equal(t, render(t, "{points.name}", chain, nil), render(t, "{pointsname}", chain, nil))
}

func TestViewerRendersUnknownViewerAsEmpty(t *testing.T) {
	balances := &fakeBalances{currency: "points", values: map[string]Balance{}}
	chain := Chain{Viewer{Sender: "alice", Balances: balances}}

	assert.Equal(t, "|", render(t, "{points}|{watchtime}", chain, nil))
	assert.Equal(t, "who?", render(t, "{points:stranger|who?}", chain, nil))
}

func TestViewerLeavesUnusableSpansLiteral(t *testing.T) {
	follow := &fakeSpans{values: map[string]string{"alice": "3 months"}}
	balances := &fakeBalances{currency: "points"}
	chain := Chain{Viewer{Sender: "alice", Follow: follow, Balances: balances}}

	for _, span := range []string{"{followage:}", "{points:}", "{points: @}", "{pointsname:bob}", "{followage:a\nb}"} {
		assert.Equal(t, span, render(t, span, chain, nil), span)
	}
	assert.Empty(t, follow.asked)
	assert.Empty(t, balances.asked)
	assert.Zero(t, balances.names)
}

func TestViewerLeavesBareSpansLiteralWithoutASender(t *testing.T) {
	follow := &fakeSpans{}
	chain := Chain{Viewer{Follow: follow}}
	assert.Equal(t, "{followage}", render(t, "{followage}", chain, nil))
	assert.Empty(t, follow.asked)
}
