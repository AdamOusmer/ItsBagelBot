// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package codm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/valkey"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkeygo "github.com/valkey-io/valkey-go"
)

func init() { core.SetSSRFCheckForTests(false) }

func endpoint(t *testing.T, p provider.Provider) provider.HandlerFunc {
	return providertest.Endpoint(t, p, "profile")
}

func newCODM(base string, store *providertest.MemStore) provider.Provider {
	return New(Config{BaseURL: base}, providertest.Deps(store))
}

func TestProfileRedirectsAndPreservesExactIDs(t *testing.T) {
	providertest.NewFakeSOCKS(t)
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/validate", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var got map[string]any
		require.NoError(t, codec.Unmarshal(body, &got))
		require.Len(t, got, 5)
		for _, key := range []string{"country", "voucherTypeName", "whiteLabelId", "deviceId", "userId"} {
			require.Contains(t, got, key)
		}
		mu.Lock()
		bodies = append(bodies, got)
		mu.Unlock()
		if got["country"] == "IN" {
			_, _ = io.WriteString(w, `{"success":false,"errorCode":-200,"errorMsg":"country mismatch","homeBaseCountry2Name":"CA"}`)
			return
		}
		_, _ = io.WriteString(w, `{"result":{"type":"SUCCESS","result":0,"countryId":124,"level":414,"nickname":"StreamerMode","rankClass":21,"customReadableMpRank":"Master I","rating":4590,"shortId":"TEST01"}}`)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, Country: "IN"}, providertest.Deps(providertest.NewMemStore()))
	h := endpoint(t, p)
	ids := []string{"7000000000000000000", "  Élite玩家  "}
	for _, id := range ids {
		got := providertest.Decode[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: id}))
		wantPlayer := strings.TrimSpace(id)
		assert.Equal(t, gossiprpc.CODMProfileReply{
			Player: wantPlayer, Level: 414, Rank: "Master I", RankClass: 21,
			Rating: 4590, Country: "CA", ShortID: "TEST01",
		}, got)
		assert.NotEqual(t, "StreamerMode", got.Player)
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 4)
	for i, id := range ids {
		initial := bodies[i*2]
		redirected := bodies[i*2+1]
		wantPlayer := strings.TrimSpace(id)
		assert.Equal(t, "IN", initial["country"])
		assert.Equal(t, "CA", redirected["country"])
		assert.Equal(t, wantPlayer, initial["userId"])
		assert.Equal(t, wantPlayer, redirected["userId"])
		assert.Equal(t, "CALL_OF_DUTY_MOBILE_WL", initial["voucherTypeName"])
		assert.Equal(t, "1", initial["whiteLabelId"])
		assert.NotEmpty(t, initial["deviceId"])
		assert.Equal(t, initial["deviceId"], redirected["deviceId"])
	}
}

func TestProfileNeverFollowsHTTPRedirects(t *testing.T) {
	providertest.NewFakeSOCKS(t)
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var forbiddenCalls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/validate" {
					forbiddenCalls.Add(1)
				}
				http.Redirect(w, r, "/purchase", status)
			}))
			defer srv.Close()
			p := newCODM(srv.URL, providertest.NewMemStore())
			got := providertest.Decode[gossiprpc.CODMProfileReply](t, endpoint(t, p)(context.Background(), gossiprpc.Request{Account: "7081192462291238913"}))
			require.Equal(t, "profile lookup failed", got.Error)
			assert.Zero(t, forbiddenCalls.Load())
		})
	}
}

func TestProfileCacheIsCaseSensitiveAndUsesFiveMinuteFreshTTL(t *testing.T) {
	providertest.NewFakeSOCKS(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"success":true,"result":{"type":"SUCCESS","result":0,"countryId":124,"level":1,"nickname":"StreamerMode","rankClass":1,"customReadableMpRank":"Rookie I","rating":10,"shortId":"TEST01"}}`)
	}))
	defer srv.Close()

	store := providertest.NewMemStore()
	h := endpoint(t, newCODM(srv.URL, store))
	first := providertest.Decode[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "CaseID"}))
	second := providertest.Decode[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "CaseID"}))
	third := providertest.Decode[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "caseid"}))

	assert.Equal(t, first, second)
	assert.Equal(t, "CaseID", first.Player)
	assert.Equal(t, "caseid", third.Player)
	assert.Equal(t, first.Level, third.Level)
	assert.Equal(t, first.Rank, third.Rank)
	assert.Equal(t, first.RankClass, third.RankClass)
	assert.Equal(t, first.Rating, third.Rating)
	assert.Equal(t, first.Country, third.Country)
	assert.Equal(t, first.ShortID, third.ShortID)
	assert.Equal(t, 2, calls, "different account case must not share the cache entry")
	assert.Equal(t, 2*profileTTL, store.Retention("gossip:codm:profile:CaseID"))
}

func TestProfileFailuresAreAnsweredInChat(t *testing.T) {
	for _, tc := range []struct {
		name      string
		account   string
		body      string
		wantError string
		wantCalls int
	}{
		{"a response without a result is a lookup failure", "valid", `{"success":true}`, "profile lookup failed", 1},
		{"a result without a profile is a lookup failure", "valid", `{"result":{"type":"SUCCESS","result":0,"countryId":124}}`, "profile lookup failed", 1},
		{"a country redirect without a country is a lookup failure", "valid", `{"success":false,"errorCode":-200}`, "profile lookup failed", 1},
		{"an empty account never leaves the pod", "", `{}`, "invalid account", 0},
		{"a blank account never leaves the pod", "  ", `{}`, "invalid account", 0},
		{"an account with a control character never leaves the pod", "bad\nname", `{}`, "invalid account", 0},
		{"an account that is not utf-8 never leaves the pod", string([]byte{0xff}), `{}`, "invalid account", 0},
		{"an over-long account never leaves the pod", strings.Repeat("x", maxAccountRunes+1), `{}`, "invalid account", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providertest.NewFakeSOCKS(t)
			upstream := providertest.NewSequence(t, providertest.Reply{Body: tc.body})
			if tc.wantCalls == 0 {
				upstream = providertest.NewSequence(t)
			}
			srv := httptest.NewServer(upstream)
			defer srv.Close()
			h := endpoint(t, newCODM(srv.URL, providertest.NewMemStore()))

			got := providertest.Decode[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: tc.account}))

			assert.Equal(t, tc.wantError, got.Error)
			assert.Equal(t, tc.wantCalls, upstream.Hits())
		})
	}
}

func TestProfileEgressesThroughTheWARPSidecar(t *testing.T) {
	socks := providertest.NewFakeSOCKS(t)
	srv := httptest.NewServer(providertest.Respond(http.StatusOK,
		`{"result":{"type":"SUCCESS","result":0,"countryId":124,"level":1,"nickname":"x","rankClass":1,"customReadableMpRank":"Rookie I","rating":10,"shortId":"TEST01"}}`))
	defer srv.Close()

	got := providertest.Decode[gossiprpc.CODMProfileReply](t,
		endpoint(t, newCODM(srv.URL, providertest.NewMemStore()))(context.Background(), gossiprpc.Request{Account: "viaWarp"}))

	assert.Empty(t, got.Error)
	assert.Positive(t, socks.Conns(), "validation calls must leave through the WARP lane, never direct egress")
}

func TestProfileRedirectLoopsAreBounded(t *testing.T) {
	providertest.NewFakeSOCKS(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = io.WriteString(w, `{"errorCode":-200,"homeBaseCountry2Name":"CA"}`)
			return
		}
		_, _ = io.WriteString(w, `{"errorCode":-200,"homeBaseCountry2Name":"IN"}`)
	}))
	defer srv.Close()
	h := endpoint(t, newCODM(srv.URL, providertest.NewMemStore()))
	got := providertest.Decode[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "loop"}))
	assert.Equal(t, "profile lookup failed", got.Error)
	assert.Equal(t, 2, calls, "redirecting back to the original country must stop immediately")

	calls = 0
	countries := []string{"CA", "GB", "AU", "JP", "BR"}
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		country := countries[calls-1]
		_, _ = io.WriteString(w, `{"errorCode":-200,"homeBaseCountry2Name":"`+country+`"}`)
	})
	got = providertest.Decode[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "bounded"}))
	assert.Equal(t, "profile lookup failed", got.Error)
	assert.Equal(t, maxRedirects+1, calls, "country redirects must have a hard bound")
}

func TestProfileMapsUpstream429AndDoesNotRefetchDuringThrottleCache(t *testing.T) {
	providertest.NewFakeSOCKS(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"too many requests"}`)
	}))
	defer srv.Close()
	store := providertest.NewMemStore()
	d := providertest.Deps(store)
	h := endpoint(t, New(Config{BaseURL: srv.URL}, d))

	first := providertest.Decode[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "busy"}))
	second := providertest.Decode[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "busy"}))
	assert.Equal(t, "stats provider is rate limiting us, try again in a minute", first.Error)
	assert.Equal(t, first, second)
	otherReplica := endpoint(t, New(Config{BaseURL: srv.URL}, d))
	other := providertest.Decode[gossiprpc.CODMProfileReply](t, otherReplica(context.Background(), gossiprpc.Request{Account: "different-player"}))
	assert.Equal(t, first.Error, other.Error)
	assert.Equal(t, 90*time.Second, store.Retention(cooldownKey))
	assert.Equal(t, 1, calls)
}

func TestLocalDenialDoesNotArmCooldown(t *testing.T) {
	store := providertest.NewMemStore()
	d := providertest.Deps(store)
	p := newAPI(Config{}, d, provider.NewProvider(providerName, d))
	p.recordThrottle(context.Background(), &core.UpstreamError{Status: 429, LocalDeny: true})
	_, found, err := store.Get(context.Background(), cooldownKey)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestValidationRateAccountingIntegration(t *testing.T) {
	providertest.NewFakeSOCKS(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request validateRequest
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, codec.Unmarshal(body, &request))
		if request.Country == "IN" {
			_, _ = io.WriteString(w, `{"errorCode":-200,"homeBaseCountry2Name":"CA"}`)
			return
		}
		_, _ = io.WriteString(w, `{"result":{"type":"SUCCESS","result":0,"countryId":124,"level":414,"nickname":"StreamerMode","rankClass":21,"customReadableMpRank":"Master I","rating":4590}}`)
	}))
	defer srv.Close()
	p, client := rateTestAPI(t, srv.URL)
	ctx := context.Background()
	_, err := p.fetchProfile(ctx, gossiprpc.Request{}, provider.ID{Display: "first"})
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "one country redirect sends exactly two validations")
	raw, err := client.Do(ctx, client.B().Hget().Key(rateTestHTTPKey(p)).Field("tokens").Build()).ToString()
	require.NoError(t, err)
	tokens, err := strconv.ParseFloat(raw, 64)
	require.NoError(t, err)
	assert.InDelta(t, 2.0, tokens, 0.5, "only two of the four burst tokens were spent")
	otherReplica := *p
	_, err = otherReplica.fetchProfile(ctx, gossiprpc.Request{}, provider.ID{Display: "second"})
	require.NoError(t, err)
	_, err = p.fetchProfile(ctx, gossiprpc.Request{}, provider.ID{Display: "third"})
	var denial *core.UpstreamError
	require.ErrorAs(t, err, &denial)
	assert.True(t, denial.LocalDeny)
	assert.Equal(t, 4, calls, "replicas share the HTTP burst and a denial sends nothing")
}

func TestRateAdmissionPreservesPremiumReserveIntegration(t *testing.T) {
	p, client := rateTestAPI(t, "https://example.test")
	ctx := context.Background()
	key := "test:codm:" + p.deviceID + ":lookups:standard"
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	err := client.Do(ctx, client.B().Hset().Key(key).FieldValue().
		FieldValue("tokens", "0").FieldValue("last_ms", future).Build()).Error()
	require.NoError(t, err)
	var denial *core.UpstreamError
	require.ErrorAs(t, p.admit(ctx, gossiprpc.Request{}), &denial)
	assert.True(t, denial.LocalDeny)
	require.NoError(t, p.admit(ctx, gossiprpc.Request{IsPremium: true}))
	require.NoError(t, p.spendValidation(ctx), "actual HTTP budget is independent of the flight winner's lane")
}

func rateTestAPI(t *testing.T, base string) (*api, valkeygo.Client) {
	t.Helper()
	address := os.Getenv("VALKEY_TEST_ADDR")
	if address == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(address, os.Getenv("VALKEY_TEST_PASSWORD"))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	d := providertest.Deps(providertest.NewMemStore())
	d.Limiter = ratelimit.New(client)
	p := newAPI(Config{BaseURL: base}, d, provider.NewProvider(providerName, d))
	p.deviceID = uuid.NewString()
	p.buckets = p.buckets.WithKey("test:codm:" + p.deviceID + ":lookups")
	p.requests = p.requests.WithKey(rateTestHTTPKey(p))
	return p, client
}

func rateTestHTTPKey(p *api) string { return "test:codm:" + p.deviceID + ":http" }
