package tebex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := New(Config{
		WebstoreToken: "token-123",
		PackageID:     42,
		CompleteURL:   "https://dashboard.example/billing?checkout=complete",
		CancelURL:     "https://dashboard.example/billing?checkout=cancelled",
		BaseURL:       srv.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestCreateBasket(t *testing.T) {
	var createBody, packageBody map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/accounts/token-123/baskets", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"ident": "bkt-1",
				"links": map[string]any{"checkout": "https://pay.tebex.io/bkt-1"},
			},
		})
	})
	mux.HandleFunc("POST /api/baskets/bkt-1/packages", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&packageBody); err != nil {
			t.Fatalf("decode package body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"ident": "bkt-1",
				"links": map[string]any{"checkout": "https://pay.tebex.io/bkt-1-final"},
			},
		})
	})

	basket, err := newTestClient(t, mux).CreateBasket(context.Background(), 804932984, "mavey")
	if err != nil {
		t.Fatalf("CreateBasket: %v", err)
	}

	if basket.Ident != "bkt-1" {
		t.Errorf("ident = %q, want bkt-1", basket.Ident)
	}
	if basket.CheckoutURL != "https://pay.tebex.io/bkt-1-final" {
		t.Errorf("checkout url = %q", basket.CheckoutURL)
	}

	custom, _ := createBody["custom"].(map[string]any)
	if custom["user_id"] != "804932984" {
		t.Errorf("custom.user_id = %v, want 804932984", custom["user_id"])
	}
	if createBody["complete_url"] != "https://dashboard.example/billing?checkout=complete" {
		t.Errorf("complete_url = %v", createBody["complete_url"])
	}

	if packageBody["package_id"] != float64(42) {
		t.Errorf("package_id = %v, want 42", packageBody["package_id"])
	}
	if packageBody["type"] != "subscription" {
		t.Errorf("type = %v, want subscription", packageBody["type"])
	}
}

func TestCreateBasketUpstreamError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"store disabled"}`, http.StatusForbidden)
	})

	if _, err := newTestClient(t, mux).CreateBasket(context.Background(), 1, ""); err == nil {
		t.Fatal("expected error on 403 upstream")
	}
}

func TestCreateBasketMissingIdent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
	})

	if _, err := newTestClient(t, mux).CreateBasket(context.Background(), 1, ""); err == nil {
		t.Fatal("expected error on missing ident")
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(Config{PackageID: 1}); err == nil {
		t.Error("expected error without webstore token")
	}
	if _, err := New(Config{WebstoreToken: "t"}); err == nil {
		t.Error("expected error without package id")
	}
}
