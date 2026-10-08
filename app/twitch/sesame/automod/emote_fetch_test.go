// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const unmatchedCapsLine = "SHOUT SHOUT SHOUT SHOUT KEKW"

func emoteServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/bttv", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"1","code":"KEKW"},{"id":"2","code":"OMEGALUL"}]`))
	})
	mux.HandleFunc("/ffz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sets":{"3":{"emoticons":[{"name":"LUL"},{"name":"KEKW"}]}}}`))
	})
	mux.HandleFunc("/7tv", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"emotes":[{"name":"PagMan"},{"name":"Clap"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func endpointsFor(base string) EmoteEndpoints {
	return EmoteEndpoints{BTTV: base + "/bttv", FFZ: base + "/ffz", SVTV: base + "/7tv"}
}

func TestFetchCatalogMergesAndKeepsProvidersApart(t *testing.T) {
	srv := emoteServer(t)

	cat, err := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL)).FetchCatalog(context.Background())
	require.NoError(t, err)

	codes := cat.Codes()
	sort.Strings(codes)
	assert.Equal(t, []string{"Clap", "KEKW", "LUL", "OMEGALUL", "PagMan"}, codes)
	assert.Equal(t, []string{"KEKW", "OMEGALUL"}, cat.BTTV)
	assert.Equal(t, []string{"KEKW", "LUL"}, cat.FFZ)
	assert.Equal(t, []string{"Clap", "PagMan"}, cat.SevenTV)
}

func TestFetchCatalogKeepsTheCodesOfWorkingProviders(t *testing.T) {
	srv := emoteServer(t)
	endpoints := endpointsFor(srv.URL)
	endpoints.FFZ = srv.URL + "/missing"

	cat, err := NewEmoteFetcher(srv.Client(), endpoints).FetchCatalog(context.Background())

	require.Error(t, err, "the 404 provider reports an error")
	assert.Len(t, cat.Codes(), 4)
}

func TestRefreshInstallsTheSetAndPublishesTheCatalog(t *testing.T) {
	srv := emoteServer(t)
	g := New()
	f := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL))

	assert.Empty(t, f.Catalog().SevenTV, "the catalog is empty before the first refresh")
	assert.Empty(t, NewEmoteFetcher(nil, DefaultEmoteEndpoints).Catalog().SevenTV, "a nil client falls back to the default")
	assert.Equal(t, Verdict{}, g.Inspect(module.RoleEveryone, unmatchedCapsLine), "no set is loaded yet")

	n, err := f.Refresh(context.Background(), g)

	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, []string{"Clap", "PagMan"}, f.Catalog().SevenTV)
	assert.Equal(t, verdictHeuristic, g.Inspect(module.RoleEveryone, unmatchedCapsLine), "the installed set no longer rescues everything")
}

func TestRefreshKeepsThePreviousSetOnTotalFailure(t *testing.T) {
	srv := emoteServer(t)
	g := New()
	f := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL))
	_, err := f.Refresh(context.Background(), g)
	require.NoError(t, err)
	srv.Close()

	n, err := f.Refresh(context.Background(), g)

	require.Error(t, err)
	assert.Zero(t, n)
	assert.Equal(t, verdictHeuristic, g.Inspect(module.RoleEveryone, unmatchedCapsLine), "the previous set stays installed")
}
