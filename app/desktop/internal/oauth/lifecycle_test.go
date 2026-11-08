// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package oauth

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/pkg/dpop"
)

func TestCallbackIsOneShotWhileExchangeIsPending(t *testing.T) {
	opened := make(chan string, 1)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	m, _ := nativeManager(t, func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		writeReply(w, 200, validReply())
	}, func(c *Config) { c.OpenBrowser = func(value string) error { opened <- value; return nil } })
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	if err := m.StartPairing(); err != nil {
		t.Fatal(err)
	}
	q := consentParams(t, <-opened)
	callback := q.Get("redirect_uri") + "?state=" + q.Get("state") + "&code=first"
	if status := getCallback(t, callback); status != 200 {
		t.Fatal("valid callback rejected")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("code exchange did not begin")
	}
	if status := getCallback(t, callback); status != 409 {
		t.Fatal("callback replay accepted after first code was consumed")
	}
	request, _ := http.NewRequest(http.MethodGet, callback, nil)
	request.Host = "foreign.example"
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("foreign callback host accepted")
	}
	once.Do(func() { close(release) })
	await(t, func() bool { return m.Status().SignedIn })
}

func replacementReply() tokenReply {
	r := validReply()
	r.Access = "v2.new-account"
	r.Refresh = "bbr.new-account"
	r.Grant = "new-grant"
	r.Device = "new-device"
	return r
}

func TestAccountReplacementRetainsOldCredentialsUntilJoined(t *testing.T) {
	joining := make(chan string, 1)
	release := make(chan struct{})
	ready := make(chan struct{}, 1)
	var once sync.Once
	m, _ := nativeManager(t, func(w http.ResponseWriter, r *http.Request) { writeReply(w, 200, replacementReply()) }, func(c *Config) {
		c.ProofTargets = []ProofTarget{{Method: "GET", URL: "https://events.example/bridge/v1/events"}}
		c.Ready = func(context.Context, *Manager) error { ready <- struct{}{}; return nil }
	})
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	seed(t, m)
	m.config.SignedOut = func() error { token, err := m.AccessToken(t.Context()); joining <- token; <-release; return err }
	p := replacementPair(t, m)
	done := make(chan error, 1)
	go func() { done <- m.finishPairing(t.Context(), p, "code") }()
	assertOldReplacementToken(t, m, <-joining)
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("replacement did not become ready")
	}
	if token, err := m.AccessToken(t.Context()); err != nil || token != replacementReply().Access {
		t.Fatal("replacement token missing after old workers joined")
	}
}

func assertOldReplacementToken(t *testing.T, m *Manager, joinedToken string) {
	t.Helper()
	if joinedToken != validReply().Access {
		t.Fatal("old account join observed replacement credential")
	}
	access, err := m.AccessToken(t.Context())
	if err != nil || access != validReply().Access {
		t.Fatal("old account received new credentials before joining")
	}
	proof, err := m.Proof("GET", "https://events.example/bridge/v1/events", access, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dpop.Verify(proof, dpop.Expected{Method: "GET", URL: "https://events.example/bridge/v1/events", Token: access, Now: time.Now(), Window: time.Minute}); err != nil {
		t.Fatal("old proof key changed before join")
	}
}

func replacementPair(t *testing.T, m *Manager) *pairing {
	t.Helper()
	p, err := newPairing()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.close)
	m.mu.Lock()
	m.generation++
	p.generation = m.generation
	m.mu.Unlock()
	return p
}

func TestFailedJoinPreventsAccountCredentialReplacement(t *testing.T) {
	m, _ := nativeManager(t, func(w http.ResponseWriter, r *http.Request) { writeReply(w, 200, replacementReply()) }, func(c *Config) { c.SignedOut = func() error { return ErrUnavailable } })
	seed(t, m)
	if err := m.finishPairing(t.Context(), replacementPair(t, m), "code"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("failed old-account join accepted new credentials")
	}
	if token, err := m.AccessToken(t.Context()); err != nil || token != validReply().Access {
		t.Fatal("join failure replaced old credentials")
	}
}

func TestStaleStopCannotCloseNewerSession(t *testing.T) {
	var stops atomic.Int32
	m, _ := nativeManager(t, func(w http.ResponseWriter, r *http.Request) { writeReply(w, 200, validReply()) }, func(c *Config) { c.SignedOut = func() error { stops.Add(1); return nil } })
	seed(t, m)
	m.mu.Lock()
	m.generation = 2
	m.mu.Unlock()
	ack := make(chan struct{})
	m.notify(lifecycleNotice{generation: 1, ack: ack})
	<-ack
	if stops.Load() != 0 {
		t.Fatal("stale sign-out callback closed a newer account")
	}
}

func TestRestoreRefreshesThroughOverlayWithoutDashboard(t *testing.T) {
	var requests atomic.Int32
	m, store := nativeManager(t, func(w http.ResponseWriter, r *http.Request) { requests.Add(1); writeReply(w, 200, validReply()) }, nil)
	seed(t, m)
	_ = m.Close()
	config := m.config
	config.Store = store
	config.OpenBrowser = func(string) error { t.Error("restore opened dashboard consent"); return ErrUnavailable }
	ready := make(chan struct{}, 1)
	config.Ready = func(context.Context, *Manager) error { ready <- struct{}{}; return nil }
	restored, err := New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	if err = restored.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("protected restore did not refresh")
	}
	if requests.Load() != 1 || !restored.Status().SignedIn {
		t.Fatal("restore did not use one independent token request")
	}
}
