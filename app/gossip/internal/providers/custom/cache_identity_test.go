// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package custom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

// Cache assertions use the same compiled array-index representation that
// extraction addresses, rather than a raw stored authoring path.
func storedResultKey(channelID string, def gossiprpc.FetchDef) string {
	path, _ := effectivePath(&def, nil)
	return resultKey(channelID, def, path)
}

type tenantDefs map[string]map[string]gossiprpc.FetchDef

func (d tenantDefs) FetchDef(_ context.Context, channelID, name string) (gossiprpc.FetchDef, bool, error) {
	def, ok := d[channelID][name]
	return def, ok, nil
}

type tenantKeys struct{}

func (tenantKeys) FetchKey(_ context.Context, channelID, _ string) (string, error) {
	return "secret-for-" + channelID, nil
}

func TestFetchTenantRepliesAndFreshWritesStayIsolated(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/private", staged{status: http.StatusOK, ct: "application/json", body: `{"v":"tenant-one"}`})
	def := h.addDef("private", "/private", gossiprpc.FetchDef{IsActive: true, JSONPath: []string{"v"}})
	h.p.defs = tenantDefs{"one": {"private": def}, "two": {"private": def}}
	require.Equal(t, []string{"tenant-one"}, call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "private"}).Values)
	h.route(t, "/private", staged{status: http.StatusOK, ct: "application/json", body: `{"v":"tenant-two"}`})
	require.Equal(t, []string{"tenant-two"}, call(t, h, gossiprpc.Request{ChannelID: "two", DefID: "private"}).Values)
	h.route(t, "/private", staged{status: http.StatusOK, ct: "application/json", body: `{"v":"two-fresh"}`})
	require.Equal(t, []string{"two-fresh"}, call(t, h, gossiprpc.Request{ChannelID: "two", DefID: "private", Fresh: true}).Values)
	require.Equal(t, []string{"tenant-one"}, call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "private"}).Values)
	require.Equal(t, int32(3), h.hits.Load())
}

func TestFetchTenantNegativeRepliesStayIsolated(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/private", staged{status: http.StatusNotFound, ct: "application/json", body: `{"error":"missing"}`})
	def := h.addDef("private", "/private", gossiprpc.FetchDef{IsActive: true, JSONPath: []string{"v"}})
	h.p.defs = tenantDefs{"one": {"private": def}, "two": {"private": def}}
	require.Equal(t, gossiprpc.FetchUpstreamError, call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "private"}).Status)
	h.route(t, "/private", staged{status: http.StatusOK, ct: "application/json", body: `{"v":"healthy"}`})
	require.Equal(t, []string{"healthy"}, call(t, h, gossiprpc.Request{ChannelID: "two", DefID: "private"}).Values)
	require.Equal(t, gossiprpc.FetchUpstreamError, call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "private"}).Status)
	require.Equal(t, int32(2), h.hits.Load())
}

func TestFetchDoesNotReadLegacyTenantlessEntry(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/private", staged{status: http.StatusOK, ct: "application/json", body: `{"v":"legacy-tenant"}`})
	def := h.addDef("private", "/private", gossiprpc.FetchDef{IsActive: true, JSONPath: []string{"v"}})
	call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "private"})
	key := resultKey("one", def, def.JSONPath)
	entry, found, err := h.store.Get(context.Background(), key)
	require.NoError(t, err)
	require.True(t, found)
	// Seed the exact pre-fix key with a valid cache envelope, then remove
	// the v2 entry so this request proves the old cache is never consulted.
	sum := sha256.Sum256([]byte("private"))
	require.NoError(t, h.store.Set(context.Background(), "gossip:custom:fetch:"+hex.EncodeToString(sum[:8]), entry, time.Minute))
	require.NoError(t, h.store.Del(context.Background(), key))
	h.route(t, "/private", staged{status: http.StatusOK, ct: "application/json", body: `{"v":"current-tenant"}`})
	require.Equal(t, []string{"current-tenant"}, call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "private"}).Values)
	require.Equal(t, int32(2), h.hits.Load())
}

func TestFetchTenantMissesDoNotShareAuthenticatedFlights(t *testing.T) {
	h := newHarness(t)
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := "two"
		if r.Header.Get(authHeaderName) == "Bearer secret-for-one" {
			owner = "one"
			close(started)
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"owner":%q}`, owner)
	}))
	t.Cleanup(server.Close)
	def := gossiprpc.FetchDef{Name: "private", URL: server.URL, JSONPath: []string{"owner"}, KeyLabel: "prod", IsActive: true}
	h.p.defs = tenantDefs{"one": {"private": def}, "two": {"private": def}}
	h.p.keys = tenantKeys{}
	first := make(chan any, 1)
	go func() {
		first <- h.p.fetch(context.Background(), gossiprpc.Request{ChannelID: "one", DefID: "private"})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first tenant never reached upstream")
	}
	second := make(chan any, 1)
	go func() {
		second <- h.p.fetch(context.Background(), gossiprpc.Request{ChannelID: "two", DefID: "private"})
	}()
	select {
	case reply := <-second:
		require.Equal(t, []string{"two"}, reply.(gossiprpc.CustomFetchReply).Values)
	case <-time.After(time.Second):
		t.Fatal("second tenant joined the first tenant's blocked flight")
	}
	close(release)
	select {
	case reply := <-first:
		require.Equal(t, []string{"one"}, reply.(gossiprpc.CustomFetchReply).Values)
	case <-time.After(time.Second):
		t.Fatal("first tenant did not finish after release")
	}
}

func TestFetchPathCaseAndDefinitionChangesInvalidateCache(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/json", staged{status: http.StatusOK, ct: "application/json", body: `{"Value":"upper","value":"lower"}`})
	h.addDef("wx", "/json", gossiprpc.FetchDef{IsActive: true, JSONPath: []string{"Value"}})
	require.Equal(t, []string{"upper"}, call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "WX.Value"}).Values)
	require.Equal(t, []string{"lower"}, call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "wx.value"}).Values)
	require.Equal(t, []string{"upper"}, call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "wx..Value"}).Values)
	require.Equal(t, int32(2), h.hits.Load(), "equivalent token syntax shares the canonical path")
	require.Equal(t, []string{"upper"}, call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "wx"}).Values)
	def := h.defs["wx"]
	def.JSONPath = []string{"value"}
	h.defs["wx"] = def
	require.Equal(t, []string{"lower"}, call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "wx"}).Values)
	h.route(t, "/new", staged{status: http.StatusOK, ct: "application/json", body: `{"value":"new-url"}`})
	def.URL = h.srv.URL + "/new"
	h.defs["wx"] = def
	require.Equal(t, []string{"new-url"}, call(t, h, gossiprpc.Request{ChannelID: "one", DefID: "wx"}).Values)
}

func TestResultKeyBindsKeyLabelAndAbandonsLegacyEntries(t *testing.T) {
	def := gossiprpc.FetchDef{Name: "Wx", URL: "https://api.example/path?token=private", IsActive: true, KeyLabel: "prod"}
	key := resultKey("one", def, codec.Path{"Value"})
	require.Contains(t, key, "gossip:custom:fetch:v2:")
	require.NotContains(t, key, "private")
	def.Name = "wx"
	require.Equal(t, key, resultKey("one", def, codec.Path{"Value"}))
	def.KeyLabel = "other"
	require.NotEqual(t, key, resultKey("one", def, codec.Path{"Value"}))
}
