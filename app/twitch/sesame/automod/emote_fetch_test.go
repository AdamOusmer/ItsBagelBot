// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

func emoteServer() *httptest.Server {
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
	return httptest.NewServer(mux)
}

func endpointsFor(base string) EmoteEndpoints {
	return EmoteEndpoints{BTTV: base + "/bttv", FFZ: base + "/ffz", SVTV: base + "/7tv"}
}

func TestFetchMergesAndDedups(t *testing.T) {
	srv := emoteServer()
	defer srv.Close()

	f := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL))
	cat, err := f.FetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	codes := cat.Codes()

	sort.Strings(codes)
	want := []string{"Clap", "KEKW", "LUL", "OMEGALUL", "PagMan"}
	if len(codes) != len(want) {
		t.Fatalf("got %v, want %v", codes, want)
	}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("got %v, want %v", codes, want)
		}
	}
}

func TestRefreshInstallsOnGate(t *testing.T) {
	srv := emoteServer()
	defer srv.Close()

	g := New()
	f := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL))
	n, err := f.Refresh(context.Background(), g)
	if err != nil || n != 5 {
		t.Fatalf("refresh n=%d err=%v", n, err)
	}
	if got := g.emotes.Load(); !got.Has("KEKW") || !got.Has("PagMan") {
		t.Fatal("installed set missing expected codes")
	}
}

func TestFetchPartialFailureStillReturnsCodes(t *testing.T) {
	srv := emoteServer()
	defer srv.Close()

	eps := endpointsFor(srv.URL)
	eps.FFZ = srv.URL + "/missing"

	f := NewEmoteFetcher(srv.Client(), eps)
	cat, err := f.FetchCatalog(context.Background())
	if err == nil {
		t.Fatal("expected a per-source error from the 404 provider")
	}
	codes := cat.Codes()
	if len(codes) != 4 {
		t.Fatalf("partial fetch got %d codes, want 4: %v", len(codes), codes)
	}
}

func TestRefreshKeepsPreviousSetOnTotalFailure(t *testing.T) {
	srv := emoteServer()
	g := New()
	f := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL))
	if _, err := f.Refresh(context.Background(), g); err != nil {
		t.Fatalf("seed refresh: %v", err)
	}
	srv.Close()

	n, err := f.Refresh(context.Background(), g)
	if err == nil {
		t.Fatal("expected error when all sources fail")
	}
	if n != 0 {
		t.Fatalf("want 0 codes on total failure, got %d", n)
	}
	if got := g.emotes.Load(); !got.Has("KEKW") {
		t.Fatal("previous set must be kept when a refresh fully fails")
	}
}

func TestFetchCatalogKeepsProvidersApart(t *testing.T) {
	srv := emoteServer()
	defer srv.Close()

	f := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL))
	cat, err := f.FetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	assertCodes(t, "bttv", cat.BTTV, []string{"KEKW", "OMEGALUL"})
	assertCodes(t, "ffz", cat.FFZ, []string{"KEKW", "LUL"})
	assertCodes(t, "7tv", cat.SevenTV, []string{"Clap", "PagMan"})
}

func TestCatalogIsEmptyBeforeFirstRefresh(t *testing.T) {
	f := NewEmoteFetcher(nil, DefaultEmoteEndpoints)
	cat := f.Catalog()
	if len(cat.SevenTV)+len(cat.BTTV)+len(cat.FFZ) != 0 {
		t.Fatalf("want an empty catalog before the first refresh, got %+v", cat)
	}
}

func TestRefreshPublishesTheCatalog(t *testing.T) {
	srv := emoteServer()
	defer srv.Close()

	f := NewEmoteFetcher(srv.Client(), endpointsFor(srv.URL))
	if _, err := f.Refresh(context.Background(), New()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	assertCodes(t, "7tv", f.Catalog().SevenTV, []string{"Clap", "PagMan"})
}

func assertCodes(t *testing.T, provider string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", provider, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", provider, got, want)
		}
	}
}
