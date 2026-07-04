package automod

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// emoteServer serves the three provider shapes on distinct paths.
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
	codes, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	sort.Strings(codes)
	// KEKW appears in both BTTV and FFZ but is deduped.
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

	// Point FFZ at a 404 path; BTTV and 7TV still resolve.
	eps := endpointsFor(srv.URL)
	eps.FFZ = srv.URL + "/missing"

	f := NewEmoteFetcher(srv.Client(), eps)
	codes, err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected a per-source error from the 404 provider")
	}
	// BTTV (KEKW, OMEGALUL) + 7TV (PagMan, Clap) survive the FFZ failure.
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
	srv.Close() // every source now fails

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
