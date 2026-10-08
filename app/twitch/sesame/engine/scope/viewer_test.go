// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestViewerMountsOnlyWhatIsWired(t *testing.T) {
	follow := &fakeSpans{values: map[string]string{"alice": "3 months"}}
	tests := []struct {
		name     string
		viewer   Viewer
		template string
		want     string
	}{
		{
			name:     "leaves every token literal when no family is mounted",
			viewer:   Viewer{Sender: "alice"},
			template: "{followage} {accountage} {points} {watchtime} {pointsname}",
			want:     "{followage} {accountage} {points} {watchtime} {pointsname}",
		},
		{
			name:     "mounts each family on its own",
			viewer:   Viewer{Sender: "alice", Follow: follow},
			template: "{followage} / {points}",
			want:     "3 months / {points}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, render(t, tt.template, Chain{tt.viewer}, nil))
		})
	}
}

func TestViewerSpanLookups(t *testing.T) {
	follow := map[string]string{"alice": "3 months", "bob": "2 years"}
	tests := []struct {
		name      string
		sender    string
		follow    map[string]string
		template  string
		want      string
		wantAsked []string
	}{
		{
			name: "resolves the sender and a named viewer", sender: "alice", follow: follow,
			template: "{followage}, {followage:@Bob}, {accountage}", want: "3 months, 2 years, 5 years",
			wantAsked: []string{"alice", "bob"},
		},
		{
			name: "looks each viewer up once", sender: "alice", follow: follow,
			template: "{followage:bob} {followage:@BOB} {followage:alice}", want: "2 years 2 years 3 months",
			wantAsked: []string{"bob", "alice"},
		},
		{
			name: "folds the bare span onto the sender login", sender: "alice", follow: follow,
			template: "{followage} {followage:alice}", want: "3 months 3 months",
			wantAsked: []string{"alice"},
		},
		{
			name: "renders empty when the lookup found nothing", sender: "alice", follow: map[string]string{},
			template: "{followage}", want: "", wantAsked: []string{"alice"},
		},
		{
			name: "renders the fallback when the lookup found nothing", sender: "alice", follow: map[string]string{},
			template: "{followage|not yet!}", want: "not yet!", wantAsked: []string{"alice"},
		},
		{
			name: "leaves the bare span literal without a sender", follow: follow,
			template: "{followage}", want: "{followage}",
		},
		{
			name: "leaves an empty named span literal", sender: "alice", follow: follow,
			template: "{followage:}", want: "{followage:}",
		},
		{
			name: "leaves a multiline name literal", sender: "alice", follow: follow,
			template: "{followage:a\nb}", want: "{followage:a\nb}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := &fakeSpans{values: tt.follow}
			viewer := Viewer{Sender: tt.sender, Follow: spans, Account: &fakeSpans{values: map[string]string{"alice": "5 years"}}}

			assert.Equal(t, tt.want, render(t, tt.template, Chain{viewer}, nil))
			assert.Equal(t, tt.wantAsked, spans.asked)
		})
	}
}

func TestViewerBalanceSpans(t *testing.T) {
	known := map[string]Balance{
		"alice": {Points: 1280, WatchSeconds: 9000, Found: true},
		"bob":   {Points: 4, Found: true},
	}
	tests := []struct {
		name      string
		values    map[string]Balance
		template  string
		want      string
		wantAsked []string
		wantNames int
	}{
		{
			name: "reads one balance for points and watch time", values: known,
			template:  "{points} {points.name}, {watchtime} watched; bob has {points:bob}",
			want:      "1280 bagels, 2 hours, 30 minutes watched; bob has 4",
			wantAsked: []string{"alice", "bob"}, wantNames: 1,
		},
		{
			name: "keeps the legacy points name alias", values: known,
			template: "{pointsname}", want: "bagels", wantNames: 1,
		},
		{
			name: "renders an unknown viewer as empty", values: map[string]Balance{},
			template: "{points}|{watchtime}", want: "|", wantAsked: []string{"alice"},
		},
		{
			name: "renders the fallback for an unknown named viewer", values: map[string]Balance{},
			template: "{points:stranger|who?}", want: "who?", wantAsked: []string{"stranger"},
		},
		{name: "leaves an empty points span literal", values: known, template: "{points:}", want: "{points:}"},
		{name: "leaves a blank at-sign points span literal", values: known, template: "{points: @}", want: "{points: @}"},
		{name: "leaves a payloaded points name literal", values: known, template: "{pointsname:bob}", want: "{pointsname:bob}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			balances := &fakeBalances{values: tt.values, currency: "bagels"}

			assert.Equal(t, tt.want, render(t, tt.template, Chain{Viewer{Sender: "alice", Balances: balances}}, nil))
			assert.Equal(t, tt.wantAsked, balances.asked)
			assert.Equal(t, tt.wantNames, balances.names)
		})
	}
}
