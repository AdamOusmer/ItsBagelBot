// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
)

const (
	appToken  = "ghs_installation"
	codescene = "CodeScene Code Health Review (main)"
	tokenPath = "POST /app/installations/2/access_tokens"
)

// appKey is generated once: a 2048-bit key costs ~100 ms and every test
// builds a client.
var appKey = sync.OnceValue(func() []byte {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
})

// routes maps "METHOD /path" to a handler; the query is not part of the key.
type routes map[string]http.HandlerFunc

// fakeGitHub serves routes and records every API call other than the token
// exchange, as "METHOD /path?query".
type fakeGitHub struct {
	t      *testing.T
	routes routes
	mu     sync.Mutex
	calls  []string
	bodies map[string]string
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	h, ok := f.routes[key]
	if !ok {
		f.t.Errorf("unexpected request %s", r.URL)
		http.Error(w, "no route", http.StatusTeapot)
		return
	}
	if key != tokenPath {
		f.record(key, r)
	}
	h(w, r)
}

func (f *fakeGitHub) record(key string, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	call := key
	if r.URL.RawQuery != "" {
		call += "?" + r.URL.RawQuery
	}
	f.calls = append(f.calls, call)
	if len(body) > 0 {
		f.bodies[key] = string(body)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
}

// writes are the recorded calls that change something.
func (f *fakeGitHub) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "GET ") {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeGitHub) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// clock is the test's c.now, moved by hand to cross cacheTTL.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func newClient(t *testing.T, rt routes) (*Client, *fakeGitHub, *clock) {
	t.Helper()
	fake := &fakeGitHub{t: t, routes: rt, bodies: map[string]string{}}
	rt[tokenPath] = reply(http.StatusCreated, `{"token":"`+appToken+`","expires_at":"2099-01-01T00:00:00Z"}`)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	c, err := New(Config{
		AppID: 1, InstallationID: 2, PrivateKey: appKey(), BaseURL: srv.URL + "/",
		Deploy: ports.Config{Owner: "o", Repo: "r", MainBranch: "main", CodeSceneCheck: codescene},
	})
	require.NoError(t, err)
	clk := &clock{at: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)}
	c.now = clk.now
	return c, fake, clk
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// kindOf names the ports sentinel err wraps, so a case compares it in one
// struct with the rest of its outcome.
func kindOf(err error) error {
	for _, kind := range []error{ports.ErrNotFound, ports.ErrConflict, ports.ErrInvalid} {
		if errors.Is(err, kind) {
			return kind
		}
	}
	if err != nil {
		return errOther
	}
	return nil
}

var errOther = errors.New("other error")

func TestNewRejectsBadKey(t *testing.T) {
	_, err := New(Config{AppID: 1, InstallationID: 2, PrivateKey: []byte("not a key")})
	assert.Error(t, err)
}

func TestAPIRequestsCarryInstallationToken(t *testing.T) {
	var auth string
	c, _, _ := newClient(t, routes{
		"GET /repos/o/r/git/ref/heads/main": func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("Authorization")
			reply(http.StatusOK, `{"ref":"refs/heads/main","object":{"sha":"m1","type":"commit"}}`)(w, r)
		},
	})
	sha, err := c.BranchHead(t.Context(), "main")
	require.NoError(t, err)
	assert.Equal(t, [2]string{"m1", "token " + appToken}, [2]string{string(sha), auth})
}

func TestCacheExpires(t *testing.T) {
	cache := newCache[string, int](cacheTTL)
	start := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	fetches := 0
	fetch := func() (int, error) { fetches++; return fetches, nil }
	var got []int
	for _, at := range []time.Duration{0, cacheTTL - time.Second, cacheTTL, cacheTTL + time.Second} {
		v, _ := cache.load("k", start.Add(at), fetch)
		got = append(got, v)
	}
	assert.Equal(t, []int{1, 1, 2, 2}, got)
}

func secs(n int) time.Duration { return time.Duration(n) * time.Second }
