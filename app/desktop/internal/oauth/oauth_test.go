// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package oauth

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/dpop"
)

type memoryStore struct {
	mu         sync.Mutex
	raw        []byte
	failSave   bool
	failDelete bool
	loaded     chan struct{}
	once       sync.Once
}

func (s *memoryStore) Load() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded != nil {
		s.once.Do(func() { close(s.loaded) })
	}
	if len(s.raw) == 0 {
		return nil, ErrNotFound
	}
	return append([]byte(nil), s.raw...), nil
}
func (s *memoryStore) Save(raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failSave {
		return ErrStorage
	}
	s.raw = append(s.raw[:0], raw...)
	return nil
}
func (s *memoryStore) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failDelete {
		return ErrStorage
	}
	s.raw = nil
	return nil
}

func validReply() tokenReply {
	return tokenReply{Access: "v2.access", Type: "DPoP", Refresh: "bbr.refresh", Expires: 86400, Grant: "grant", Device: "device"}
}

func writeReply(w http.ResponseWriter, status int, reply tokenReply) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	raw, _ := codec.Marshal(reply)
	_, _ = w.Write(raw)
}

func nativeManager(t *testing.T, handler http.HandlerFunc, customize func(*Config)) (*Manager, *memoryStore) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	store := &memoryStore{loaded: make(chan struct{})}
	network, err := NewWorkerNetwork(WorkerNetworkConfig{Endpoint: server.URL, TLSConfig: server.Client().Transport.(*http.Transport).TLSClientConfig, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = network.Close() })
	config := Config{AuthorizeEndpoint: "https://dashboard.example", TokenEndpoint: server.URL, Platform: "macos", Store: store, OpenBrowser: func(string) error { return nil }, Network: network}
	if customize != nil {
		customize(&config)
	}
	m, err := New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	select {
	case <-store.loaded:
	case <-time.After(time.Second):
		t.Fatal("restore did not finish")
	}
	return m, store
}

func await(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatal("native authorization did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func seed(t *testing.T, m *Manager) {
	t.Helper()
	key, err := dpop.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err = m.lock(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer m.unlock()
	if err = m.commit(key, validReply()); err != nil {
		t.Fatal(err)
	}
}

func forceRenew(m *Manager, expired bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record.RenewAt = time.Now().Add(-time.Minute).Unix()
	if expired {
		m.record.Expires = time.Now().Add(-time.Minute).Unix()
	}
}

func proofClaims(t *testing.T, r *http.Request, token string) dpop.Claims {
	t.Helper()
	claims, err := dpop.Verify(r.Header.Get("DPoP"), dpop.Expected{Method: r.Method, URL: "https://" + r.Host + r.URL.Path, Token: token, Now: time.Now(), Window: time.Minute})
	if err != nil {
		t.Error("invalid request-bound DPoP proof")
	}
	if r.Header.Get("Cookie") != "" {
		t.Error("native OAuth sent browser cookies")
	}
	return claims
}

func TestNativePairingUsesConsentAndIndependentTokenEndpoint(t *testing.T) {
	var calls atomic.Int32
	opened := make(chan string, 1)
	ready := make(chan string, 1)
	m, store := nativeManager(t, pairingHandler(t, &calls), func(c *Config) {
		c.OpenBrowser = func(value string) error { opened <- value; return nil }
		c.Ready = func(ctx context.Context, m *Manager) error {
			token, err := m.AccessToken(ctx)
			ready <- token
			return err
		}
	})
	if err := m.StartPairing(); err != nil {
		t.Fatal(err)
	}
	q := consentParams(t, <-opened)
	callback := q.Get("redirect_uri")
	if status := getCallback(t, callback+"?state=wrong&code=code"); status != 400 {
		t.Fatal("wrong callback state accepted")
	}
	if status := getCallback(t, callback+"?state="+q.Get("state")+"&code=code"); status != 200 {
		t.Fatal("valid callback rejected")
	}
	select {
	case token := <-ready:
		if token != validReply().Access {
			t.Fatal("ready token mismatch")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ready callback deadlocked")
	}
	if calls.Load() != 2 {
		t.Fatal("nonce retry or exchange count mismatch")
	}
	assertProtectedRecord(t, store)
	await(t, func() bool { return !m.Status().Pending })
}

func getCallback(t *testing.T, endpoint string) int {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	r, err := client.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	return r.StatusCode
}

func TestConcurrentRefreshHasOneRotation(t *testing.T) {
	var calls atomic.Int32
	m, _ := nativeManager(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		proofClaims(t, r, "")
		reply := validReply()
		reply.Access = "v2.rotated"
		reply.Refresh = "bbr.rotated"
		writeReply(w, 200, reply)
	}, nil)
	seed(t, m)
	forceRenew(m, false)
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			token, err := m.AccessToken(t.Context())
			if err != nil || token != "v2.rotated" {
				t.Error("refresh did not share valid token")
			}
		}()
	}
	group.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent callers rotated the same refresh family")
	}
}

func TestFailedPersistenceRetainsRotatedFamilyWithoutPublishing(t *testing.T) {
	var calls atomic.Int32
	m, store := nativeManager(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		reply := validReply()
		reply.Access = "v2.rotated"
		reply.Refresh = "bbr.rotated"
		writeReply(w, 200, reply)
	}, nil)
	seed(t, m)
	forceRenew(m, true)
	store.mu.Lock()
	store.failSave = true
	store.mu.Unlock()
	if token, err := m.AccessToken(t.Context()); token != "" || !errors.Is(err, ErrStorage) {
		t.Fatal("unpersisted rotated token published")
	}
	store.mu.Lock()
	store.failSave = false
	store.mu.Unlock()
	token, err := m.AccessToken(t.Context())
	if err != nil || token != "v2.rotated" || calls.Load() != 1 {
		t.Fatal("rotated family was lost or rotated twice")
	}
}

func TestInvalidGrantReturnsBeforeLifecycleJoin(t *testing.T) {
	returned := make(chan struct{})
	stopped := make(chan struct{})
	m, _ := nativeManager(t, func(w http.ResponseWriter, r *http.Request) { writeReply(w, 400, tokenReply{Error: "invalid_grant"}) }, func(c *Config) { c.SignedOut = func() error { <-returned; close(stopped); return nil } })
	seed(t, m)
	forceRenew(m, true)
	go func() {
		_, err := m.AccessToken(t.Context())
		if !errors.Is(err, ErrAuthentication) {
			t.Error("invalid grant did not require authorization")
		}
		close(returned)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("token provider joined its own event worker")
	}
	if m.Status().SignedIn {
		t.Fatal("invalid grant retained signed-in state")
	}
}

func TestTransientRefreshNeverReturnsExpiredToken(t *testing.T) {
	m, _ := nativeManager(t, func(w http.ResponseWriter, r *http.Request) { writeReply(w, 503, tokenReply{Error: "server_error"}) }, nil)
	seed(t, m)
	forceRenew(m, false)
	if token, err := m.AccessToken(t.Context()); err != nil || token != validReply().Access {
		t.Fatal("early refresh discarded valid access")
	}
	forceRenew(m, true)
	if token, err := m.AccessToken(t.Context()); token != "" || err == nil {
		t.Fatal("expired access returned during outage")
	}
	if !m.Status().SignedIn {
		t.Fatal("temporary outage discarded device grant")
	}
}

func TestRefreshCannotSwitchDeviceGrant(t *testing.T) {
	m, _ := nativeManager(t, func(w http.ResponseWriter, r *http.Request) {
		reply := validReply()
		reply.Grant = "other-grant"
		writeReply(w, 200, reply)
	}, nil)
	seed(t, m)
	forceRenew(m, true)
	if token, err := m.AccessToken(t.Context()); token != "" || !errors.Is(err, ErrProtocol) {
		t.Fatal("refresh switched account family")
	}
}

func TestSignOutFencesLateReadyAndPersistsTombstone(t *testing.T) {
	var installed atomic.Int32
	m, store := nativeManager(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }, func(c *Config) { c.Ready = func(context.Context, *Manager) error { installed.Add(1); return nil } })
	seed(t, m)
	store.mu.Lock()
	store.failDelete = true
	store.mu.Unlock()
	if err := m.SignOut(); err != nil {
		t.Fatal(err)
	}
	ack := make(chan struct{})
	m.notify(lifecycleNotice{ready: true, generation: 0, ack: ack})
	<-ack
	if installed.Load() != 0 || m.Status().SignedIn {
		t.Fatal("late approval restored a signed-out session")
	}
	raw, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var record credentials
	if codec.Unmarshal(raw, &record) != nil || record.Version != -1 {
		t.Fatal("failed deletion left a renewable credential")
	}
}

func TestCloseStopsPendingCallbackAndRejectsForeignProof(t *testing.T) {
	opened := make(chan string, 1)
	m, _ := nativeManager(t, func(w http.ResponseWriter, r *http.Request) { writeReply(w, 200, validReply()) }, func(c *Config) { c.OpenBrowser = func(value string) error { opened <- value; return nil } })
	seed(t, m)
	if _, err := m.Proof("GET", "https://foreign.example/bridge/v1/events", validReply().Access, ""); !errors.Is(err, ErrConfiguration) {
		t.Fatal("foreign proof target accepted")
	}
	if err := m.StartPairing(); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(<-opened)
	callback := u.Query().Get("redirect_uri")
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second}
	if response, err := client.Get(callback); err == nil {
		response.Body.Close()
		t.Fatal("callback listener remained open after shutdown")
	}
}

func pairingHandler(t *testing.T, calls *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		claims := proofClaims(t, r, "")
		if claims.Nonce != "server-nonce" {
			w.Header().Set("DPoP-Nonce", "server-nonce")
			writeReply(w, 400, tokenReply{Error: "use_dpop_nonce"})
			return
		}
		if r.ParseForm() != nil || r.Form.Get("grant_type") != "authorization_code" {
			t.Error("code exchange missing")
		}
		writeReply(w, 200, validReply())
	}
}
func consentParams(t *testing.T, consent string) url.Values {
	t.Helper()
	u, err := url.Parse(consent)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "dashboard.example" || u.Path != AuthorizePath || len(q.Get("dpop_jkt")) != 43 || q.Get("code_challenge_method") != "S256" {
		t.Fatal("invalid native consent request")
	}
	return q
}

func assertProtectedRecord(t *testing.T, store *memoryStore) {
	t.Helper()
	raw, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 2048 || strings.Contains(string(raw), validReply().Access) {
		t.Fatal("persistent record contains access token or exceeds protected item bound")
	}
}
