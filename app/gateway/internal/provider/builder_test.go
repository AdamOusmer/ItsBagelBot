package provider

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memStore is an in-memory core.Store for tests.
type memStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemStore() *memStore { return &memStore{m: map[string][]byte{}} }

func (s *memStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[key]
	return b, ok, nil
}
func (s *memStore) Set(_ context.Context, key string, val []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Copy: the Store contract says val may come from a pooled buffer the
	// caller recycles as soon as Set returns.
	s.m[key] = append([]byte(nil), val...)
	return nil
}
func (s *memStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func testDeps() Deps {
	return Deps{Cache: core.NewCache(newMemStore())}
}

// testReply is a minimal typed reply for flow tests.
type testReply struct {
	Player string `json:"player"`
	Value  int    `json:"value"`
	Error  string `json:"error,omitempty"`
}

func testErrReply(id, msg string) any { return testReply{Player: id, Error: msg} }

func decode(t *testing.T, res any) testReply {
	t.Helper()
	if v, ok := res.(testReply); ok {
		return v
	}
	raw, ok := res.(json.RawMessage)
	require.True(t, ok, "unexpected handler result type %T", res)
	var v testReply
	require.NoError(t, json.Unmarshal(raw, &v))
	return v
}

func TestBuildIndexesEndpointsInOrder(t *testing.T) {
	b := NewProvider("demo", testDeps())
	b.Endpoint("one").Timeout(3 * time.Second).Handle(func(context.Context, gatewayrpc.Request) any { return "1" })
	b.Endpoint("two").Handle(func(context.Context, gatewayrpc.Request) any { return "2" })
	p := b.Build()

	assert.Equal(t, "demo", p.Name())
	require.Len(t, p.Endpoints(), 2)
	assert.Equal(t, "one", p.Endpoints()[0].Name)
	assert.Equal(t, 3*time.Second, p.Endpoints()[0].Timeout)
	assert.Equal(t, "two", p.Endpoints()[1].Name)
	assert.Equal(t, "1", p.Endpoints()[0].Handle(context.Background(), gatewayrpc.Request{}))
}

func TestValidateRejectsMisassembly(t *testing.T) {
	handler := func(context.Context, gatewayrpc.Request) any { return nil }

	t.Run("empty provider name", func(t *testing.T) {
		b := NewProvider("", testDeps())
		b.Endpoint("x").Handle(handler)
		assert.ErrorContains(t, b.Validate(), "non-empty name")
	})
	t.Run("no endpoints", func(t *testing.T) {
		assert.ErrorContains(t, NewProvider("demo", testDeps()).Validate(), "no endpoints")
	})
	t.Run("empty endpoint name", func(t *testing.T) {
		b := NewProvider("demo", testDeps())
		b.Endpoint("").Handle(handler)
		assert.ErrorContains(t, b.Validate(), "empty name")
	})
	t.Run("duplicate endpoint", func(t *testing.T) {
		b := NewProvider("demo", testDeps())
		b.Endpoint("x").Handle(handler)
		b.Endpoint("x").Handle(handler)
		assert.ErrorContains(t, b.Validate(), "twice")
	})
	t.Run("no terminal", func(t *testing.T) {
		b := NewProvider("demo", testDeps())
		b.Endpoint("x")
		assert.ErrorContains(t, b.Validate(), "no terminal")
	})
	t.Run("flow without Fetch", func(t *testing.T) {
		b := NewProvider("demo", testDeps())
		b.Endpoint("x").Cached(time.Minute, time.Minute).Reply(testErrReply)
		assert.ErrorContains(t, b.Validate(), "no Fetch")
	})
	t.Run("flow without Reply", func(t *testing.T) {
		b := NewProvider("demo", testDeps())
		b.Endpoint("x").Cached(time.Minute, time.Minute).
			Fetch(func(context.Context, gatewayrpc.Request, ID) (any, error) { return nil, nil })
		assert.ErrorContains(t, b.Validate(), "no Reply")
	})
	t.Run("flow without cache", func(t *testing.T) {
		b := NewProvider("demo", Deps{})
		b.Endpoint("x").Cached(time.Minute, time.Minute).Reply(testErrReply).
			Fetch(func(context.Context, gatewayrpc.Request, ID) (any, error) { return nil, nil })
		assert.ErrorContains(t, b.Validate(), "Deps.Cache is nil")
	})
}

func TestBuildPanicsOnProgrammerError(t *testing.T) {
	b := NewProvider("demo", testDeps())
	b.Endpoint("x")
	assert.Panics(t, func() { b.Build() })
}

// A flow endpoint runs the full skeleton: identity validation, cached fetch,
// and raw-bytes hits.
func TestFlowServesAndCaches(t *testing.T) {
	fetches := 0
	b := NewProvider("demo", testDeps())
	b.Endpoint("stats").
		Cached(time.Minute, time.Minute).
		Reply(testErrReply).
		Fallback("stats lookup failed").
		Fetch(func(_ context.Context, _ gatewayrpc.Request, id ID) (any, error) {
			fetches++
			return testReply{Player: id.Display, Value: 7}, nil
		})
	h := b.Build().Endpoints()[0].Handle

	first := decode(t, h(context.Background(), gatewayrpc.Request{Account: "Techno"}))
	require.Empty(t, first.Error)
	assert.Equal(t, "Techno", first.Player)
	assert.Equal(t, 7, first.Value)

	// Case-insensitive hit: served from the cache as raw wire bytes.
	res := h(context.Background(), gatewayrpc.Request{Account: "techno"})
	_, isRaw := res.(json.RawMessage)
	assert.True(t, isRaw, "cache hit must answer stored wire bytes")
	assert.Equal(t, 1, fetches)
}

func TestFlowRejectsMissingIdentity(t *testing.T) {
	b := NewProvider("demo", testDeps())
	b.Endpoint("stats").
		Cached(time.Minute, time.Minute).
		Reply(testErrReply).
		Fetch(func(context.Context, gatewayrpc.Request, ID) (any, error) {
			t.Error("no fetch expected")
			return nil, nil
		})
	h := b.Build().Endpoints()[0].Handle

	reply := decode(t, h(context.Background(), gatewayrpc.Request{}))
	assert.Equal(t, "missing account", reply.Error)
}

// A friendly upstream failure (404) answers through the Reply shaper and
// negative-caches; an infrastructure failure answers the Fallback message.
func TestFlowErrorShaping(t *testing.T) {
	t.Run("friendly upstream", func(t *testing.T) {
		fetches := 0
		b := NewProvider("demo", testDeps())
		b.Endpoint("stats").
			Cached(time.Minute, time.Minute).
			Reply(testErrReply).
			Fetch(func(context.Context, gatewayrpc.Request, ID) (any, error) {
				fetches++
				return nil, &core.UpstreamError{Status: 404, Message: "player not found"}
			})
		h := b.Build().Endpoints()[0].Handle

		reply := decode(t, h(context.Background(), gatewayrpc.Request{Account: "ghost"}))
		assert.Equal(t, "player not found", reply.Error)
		assert.Equal(t, "ghost", reply.Player)

		reply = decode(t, h(context.Background(), gatewayrpc.Request{Account: "ghost"}))
		assert.Equal(t, "player not found", reply.Error)
		assert.Equal(t, 1, fetches, "the miss must be served from the negative cache")
	})
	t.Run("infrastructure fallback", func(t *testing.T) {
		b := NewProvider("demo", testDeps())
		b.Endpoint("stats").
			Cached(time.Minute, time.Minute).
			Reply(testErrReply).
			Fallback("stats lookup failed").
			Fetch(func(context.Context, gatewayrpc.Request, ID) (any, error) {
				return nil, errors.New("upstream unreachable")
			})
		h := b.Build().Endpoints()[0].Handle

		reply := decode(t, h(context.Background(), gatewayrpc.Request{Account: "Techno"}))
		assert.Equal(t, "stats lookup failed", reply.Error)
		assert.Equal(t, "Techno", reply.Player)
	})
}

func TestIDExtractors(t *testing.T) {
	t.Run("Account", func(t *testing.T) {
		id, reject := Account(gatewayrpc.Request{Account: "  Techno "})
		require.Empty(t, reject)
		assert.Equal(t, ID{Display: "Techno", Key: "techno"}, id)

		_, reject = Account(gatewayrpc.Request{})
		assert.Equal(t, "missing account", reject)
	})
	t.Run("Channel", func(t *testing.T) {
		id, reject := Channel(gatewayrpc.Request{ChannelID: " 42 "})
		require.Empty(t, reject)
		assert.Equal(t, ID{Display: "42", Key: "42"}, id)

		_, reject = Channel(gatewayrpc.Request{})
		assert.Equal(t, "missing channel", reject)
	})
	t.Run("StaticID", func(t *testing.T) {
		id, reject := StaticID("current")(gatewayrpc.Request{Account: "ignored"})
		require.Empty(t, reject)
		assert.Equal(t, ID{Key: "current"}, id)
	})
}
