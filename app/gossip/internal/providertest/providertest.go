// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package providertest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type MemStore struct {
	mu   sync.Mutex
	m    map[string][]byte
	ttls map[string]time.Duration
}

func NewMemStore() *MemStore {
	return &MemStore{m: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (s *MemStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[key]
	return append([]byte(nil), b...), ok, nil
}

func (s *MemStore) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), val...)
	s.ttls[key] = ttl
	return nil
}

func (s *MemStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *MemStore) SetNX(_ context.Context, key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; ok {
		return false, nil
	}
	s.m[key] = []byte("1")
	s.ttls[key] = ttl
	return true, nil
}

func (s *MemStore) Retention(key string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ttls[key]
}

func (s *MemStore) Snapshot() map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte, len(s.m))
	for k, v := range s.m {
		out[k] = append([]byte(nil), v...)
	}
	return out
}

func (s *MemStore) Keys() []string {
	keys := make([]string, 0, len(s.m))
	for k := range s.Snapshot() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func Deps(store core.Store) provider.Deps {
	return provider.Deps{Cache: core.NewCache(store), Log: zap.NewNop()}
}

// Upstream lifts the SSRF gate process-wide so providers can dial the loopback server.
func Upstream(t testing.TB, handler http.Handler) string {
	t.Helper()
	core.SetSSRFCheckForTests(false)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

func Endpoint(t testing.TB, p provider.Provider, name string) provider.HandlerFunc {
	t.Helper()
	for _, ep := range p.Endpoints() {
		if ep.Name == name {
			return ep.Handle
		}
	}
	t.Fatalf("endpoint %q not declared", name)
	return nil
}

func Decode[T any](t testing.TB, res any) T {
	t.Helper()
	if v, ok := res.(T); ok {
		return v
	}
	raw, ok := res.(codec.RawMessage)
	require.True(t, ok, "unexpected handler result type %T", res)
	var v T
	require.NoError(t, codec.Unmarshal(raw, &v))
	return v
}

func ErrorOf(t testing.TB, res any) string {
	t.Helper()
	raw, err := codec.Marshal(res)
	require.NoError(t, err)
	var out struct {
		Error string `json:"error"`
	}
	require.NoError(t, codec.Unmarshal(raw, &out))
	return out.Error
}

func Call[T any](t testing.TB, p provider.Provider, endpoint string, req gossiprpc.Request) T {
	t.Helper()
	return Decode[T](t, Endpoint(t, p, endpoint)(context.Background(), req))
}

type Case[R any] struct {
	Name     string
	Req      gossiprpc.Request
	Upstream http.HandlerFunc
	Want     R
}

func RunCases[R any](t *testing.T, newProvider func(testing.TB, http.Handler) provider.Provider, endpoint string, cases []Case[R], normalize ...func(R) R) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := Call[R](t, newProvider(t, tc.Upstream), endpoint, tc.Req)
			for _, fn := range normalize {
				got = fn(got)
			}
			assert.Equal(t, tc.Want, got)
		})
	}
}

func Respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func Forbid(t testing.TB) http.HandlerFunc {
	return func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected upstream call: %s %s", r.Method, r.URL.Path)
	}
}

type Reply struct {
	Status int
	Body   string
}

type Sequence struct {
	t       testing.TB
	replies []Reply
	hits    atomic.Int32
}

func NewSequence(t testing.TB, replies ...Reply) *Sequence {
	return &Sequence{t: t, replies: replies}
}

func (s *Sequence) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hit := int(s.hits.Add(1))
	if len(s.replies) == 0 {
		s.t.Errorf("unexpected upstream call: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
		return
	}
	reply := s.replies[min(hit, len(s.replies))-1]
	if reply.Status != 0 {
		w.WriteHeader(reply.Status)
	}
	_, _ = w.Write([]byte(reply.Body))
}

func (s *Sequence) Hits() int { return int(s.hits.Load()) }
