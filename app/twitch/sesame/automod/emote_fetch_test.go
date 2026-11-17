// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	unmatchedCapsLine = "SHOUT SHOUT SHOUT SHOUT KEKW"
	ffzOnlyCapsLine   = "LUL LUL LUL SHOUT"
	newBTTVCapsLine   = "POGGERS POGGERS POGGERS SHOUT"
)

var healthyProviderBodies = map[string]string{
	"/bttv": `[{"id":"1","code":"KEKW"},{"id":"2","code":"OMEGALUL"}]`,
	"/ffz":  `{"sets":{"3":{"emoticons":[{"name":"LUL"},{"name":"KEKW"}]}}}`,
	"/7tv":  `{"emotes":[{"name":"PagMan"},{"name":"Clap"}]}`,
}

var healthyCatalog = EmoteCatalog{
	SevenTV: []string{"Clap", "PagMan"},
	BTTV:    []string{"KEKW", "OMEGALUL"},
	FFZ:     []string{"KEKW", "LUL"},
}

type emoteProviders struct {
	mu     sync.Mutex
	bodies map[string]string
}

func (p *emoteProviders) set(bodies map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	maps.Copy(p.bodies, bodies)
}

func (p *emoteProviders) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	body, ok := p.bodies[r.URL.Path]
	p.mu.Unlock()
	if !ok || body == "" {
		http.Error(w, "stalled", http.StatusGatewayTimeout)
		return
	}
	_, _ = w.Write([]byte(body))
}

func emoteServer(t *testing.T) (*httptest.Server, *emoteProviders) {
	t.Helper()
	providers := &emoteProviders{bodies: maps.Clone(healthyProviderBodies)}
	srv := httptest.NewServer(providers)
	t.Cleanup(srv.Close)
	return srv, providers
}

func endpointsFor(base string) EmoteEndpoints {
	return EmoteEndpoints{BTTV: base + "/bttv", FFZ: base + "/ffz", SVTV: base + "/7tv"}
}

func TestFetchCatalogMergesAndKeepsProvidersApart(t *testing.T) {
	srv, _ := emoteServer(t)

	cat, err := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL)).FetchCatalog(context.Background())
	require.NoError(t, err)

	assert.Equal(t, healthyCatalog, cat)
	assert.Equal(t, []string{"Clap", "KEKW", "LUL", "OMEGALUL", "PagMan"}, cat.Codes())
}

func TestFetchCatalogKeepsTheCodesOfWorkingProviders(t *testing.T) {
	srv, providers := emoteServer(t)
	providers.set(map[string]string{"/ffz": ""})

	cat, err := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL)).FetchCatalog(context.Background())

	require.Error(t, err, "the failing provider reports an error")
	assert.Len(t, cat.Codes(), 4)
}

type refreshOutcome struct {
	failed    bool
	allFailed bool
	result    RefreshResult
	catalog   EmoteCatalog
	verdict   [2]Verdict
}

func refreshed(codes, failed int) RefreshResult {
	return RefreshResult{Codes: codes, Failed: failed, Providers: 3}
}

func observeRefresh(g *Gate, f *EmoteFetcher) refreshOutcome {
	res, err := f.Refresh(context.Background(), g)
	return refreshOutcome{
		failed:    err != nil,
		allFailed: res.AllFailed(),
		result:    res,
		catalog:   f.Catalog(),
		verdict: [2]Verdict{
			g.Inspect(module.RoleEveryone, ffzOnlyCapsLine),
			g.Inspect(module.RoleEveryone, newBTTVCapsLine),
		},
	}
}

func TestRefreshInstallsTheSetAndPublishesTheCatalog(t *testing.T) {
	srv, _ := emoteServer(t)
	g := New()
	f := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL))

	assert.Empty(t, f.Catalog().SevenTV, "the catalog is empty before the first refresh")
	assert.Empty(t, NewEmoteFetcher(nil, DefaultEmoteEndpoints).Catalog().SevenTV, "a nil client falls back to the default")
	assert.Equal(t, Verdict{}, g.Inspect(module.RoleEveryone, unmatchedCapsLine), "no set is loaded yet")

	got := observeRefresh(g, f)

	assert.Equal(t, refreshOutcome{
		result: refreshed(5, 0), catalog: healthyCatalog, verdict: [2]Verdict{{}, verdictHeuristic},
	}, got)
	assert.Equal(t, verdictHeuristic, g.Inspect(module.RoleEveryone, unmatchedCapsLine), "the installed set no longer rescues everything")
}

func TestRefreshReplacesOnlyTheProvidersThatAnswered(t *testing.T) {
	updatedBTTV := `[{"id":"1","code":"KEKW"},{"id":"9","code":"POGGERS"}]`
	tests := []struct {
		name   string
		second map[string]string
		want   refreshOutcome
	}{
		{
			name:   "a failed provider keeps its codes while a healthy one updates",
			second: map[string]string{"/bttv": updatedBTTV, "/ffz": ""},
			want: refreshOutcome{failed: true, result: refreshed(5, 1), verdict: [2]Verdict{{}, {}}, catalog: EmoteCatalog{
				SevenTV: healthyCatalog.SevenTV, BTTV: []string{"KEKW", "POGGERS"}, FFZ: healthyCatalog.FFZ,
			}},
		},
		{
			name:   "every provider failing keeps the whole set",
			second: map[string]string{"/bttv": "", "/ffz": "", "/7tv": ""},
			want: refreshOutcome{
				failed: true, allFailed: true, result: refreshed(5, 3), catalog: healthyCatalog, verdict: [2]Verdict{{}, verdictHeuristic},
			},
		},
		{
			name:   "a successful empty response clears that provider",
			second: map[string]string{"/ffz": `{"sets":{}}`},
			want: refreshOutcome{result: refreshed(4, 0), verdict: [2]Verdict{verdictHeuristic, verdictHeuristic}, catalog: EmoteCatalog{
				SevenTV: healthyCatalog.SevenTV, BTTV: healthyCatalog.BTTV, FFZ: []string{},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, providers := emoteServer(t)
			g := New()
			f := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL))
			_, err := f.Refresh(context.Background(), g)
			require.NoError(t, err)
			providers.set(tt.second)

			assert.Equal(t, tt.want, observeRefresh(g, f))
		})
	}
}
