// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tebex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type basketRequests struct {
	create, addPackage map[string]any
	auth               string
}

func clientConfig(srv *httptest.Server) Config {
	return Config{
		WebstoreToken:   "token-123",
		PrivateKey:      "private-456",
		IncludeUsername: true,
		PackageID:       42,
		CompleteURL:     "https://dashboard.example/billing?checkout=complete",
		CancelURL:       "https://dashboard.example/billing?checkout=cancelled",
		BaseURL:         srv.URL,
	}
}

func basketServer(t *testing.T, linksOnCreate bool) (*httptest.Server, *basketRequests) {
	t.Helper()
	seen := &basketRequests{}
	checkout := map[string]any{"checkout": "https://pay.tebex.io/bkt-1-final"}
	createLinks, packageLinks := any([]any{}), any(checkout)
	if linksOnCreate {
		createLinks, packageLinks = checkout, map[string]any{}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/accounts/token-123/baskets", func(w http.ResponseWriter, r *http.Request) {
		seen.auth = r.Header.Get("Authorization")
		if err := codec.NewDecoder(r.Body).Decode(&seen.create); err != nil {
			t.Errorf("decode create body: %v", err)
		}
		_ = codec.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"ident": "bkt-1", "links": createLinks}})
	})
	mux.HandleFunc("POST /api/baskets/bkt-1/packages", func(w http.ResponseWriter, r *http.Request) {
		if err := codec.NewDecoder(r.Body).Decode(&seen.addPackage); err != nil {
			t.Errorf("decode package body: %v", err)
		}
		_ = codec.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"ident": "bkt-1", "links": packageLinks}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, seen
}

func requireFields(t *testing.T, got, want map[string]any) {
	t.Helper()
	for key, value := range want {
		assert.Equal(t, value, got[key], key)
	}
}

func requireAbsent(t *testing.T, got map[string]any, keys []string) {
	t.Helper()
	for _, key := range keys {
		assert.NotContains(t, got, key)
	}
}

func TestCreateBasketCarriesAttributionAndAuthentication(t *testing.T) {
	const basicAuth = "Basic dG9rZW4tMTIzOnByaXZhdGUtNDU2"
	for _, tc := range []struct {
		name          string
		spec          BasketSpec
		noKey         bool
		noUsername    bool
		linksOnCreate bool
		auth          string
		create        map[string]any
		custom        map[string]any
		noCreate      []string
		noCustom      []string
		pkg           map[string]any
	}{
		{
			name:   "self purchase carries the buyer, username and authenticated IP",
			spec:   BasketSpec{UserID: 804932984, Username: "mavey", IPAddress: "203.0.113.10"},
			auth:   basicAuth,
			create: map[string]any{"complete_url": "https://dashboard.example/billing?checkout=complete", "username": "mavey", "ip_address": "203.0.113.10"},
			custom: map[string]any{"user_id": "804932984"}, noCustom: []string{"gifted_by"},
			pkg: map[string]any{"package_id": float64(42), "type": "subscription"},
		},
		{
			name: "without a private key the IP and the top-level username are omitted", noKey: true, noUsername: true, linksOnCreate: true,
			spec:     BasketSpec{UserID: 804932984, Username: "mavey", IPAddress: "203.0.113.10"},
			noCreate: []string{"ip_address", "username"},
			custom:   map[string]any{"username": "mavey"},
		},
		{
			name:     "a gift carries the recipient and the buyer attribution",
			spec:     BasketSpec{UserID: 111, Username: "recipient", GiftedByID: 804932984, GiftedByLogin: "mavey", PackageType: "single"},
			auth:     basicAuth,
			custom:   map[string]any{"user_id": "111", "gifted_by": "804932984", "gifted_by_login": "mavey"},
			noCustom: []string{"gift_message"},
			pkg:      map[string]any{"type": "single"},
		},
		{
			name:   "a gift carries its message",
			spec:   BasketSpec{UserID: 111, Username: "recipient", GiftedByID: 804932984, GiftedByLogin: "mavey", PackageType: "single", GiftMessage: "happy streaming!"},
			auth:   basicAuth,
			custom: map[string]any{"gift_message": "happy streaming!"},
		},
		{
			name:     "a self purchase ignores a message",
			spec:     BasketSpec{UserID: 804932984, Username: "mavey", GiftMessage: "note"},
			auth:     basicAuth,
			noCustom: []string{"gift_message"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := basketServer(t, tc.linksOnCreate)
			cfg := clientConfig(srv)
			if tc.noKey {
				cfg.PrivateKey = ""
			}
			if tc.noUsername {
				cfg.IncludeUsername = false
			}
			client, err := New(cfg)
			require.NoError(t, err)

			basket, err := client.CreateBasket(context.Background(), tc.spec)

			require.NoError(t, err)
			assert.Equal(t, Basket{Ident: "bkt-1", CheckoutURL: "https://pay.tebex.io/bkt-1-final"}, basket)
			assert.Equal(t, tc.auth, seen.auth)
			custom, _ := seen.create["custom"].(map[string]any)
			requireFields(t, seen.create, tc.create)
			requireFields(t, custom, tc.custom)
			requireFields(t, seen.addPackage, tc.pkg)
			requireAbsent(t, seen.create, tc.noCreate)
			requireAbsent(t, custom, tc.noCustom)
		})
	}
}

func TestCreateBasketRefusesAnUnusableUpstream(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"an upstream refusal", http.StatusForbidden, `{"message":"store disabled"}`},
		{"a basket response without an ident", http.StatusOK, `{"data":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			client, err := New(clientConfig(srv))
			require.NoError(t, err)

			_, err = client.CreateBasket(context.Background(), BasketSpec{UserID: 1})

			require.Error(t, err)
		})
	}
}

func TestNewRejectsAnIncompleteConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"without a webstore token", Config{PackageID: 1}},
		{"without a package id", Config{WebstoreToken: "t"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg)
			require.Error(t, err)
		})
	}
}
