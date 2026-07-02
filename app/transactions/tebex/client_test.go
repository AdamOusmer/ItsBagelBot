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
		PrivateKey:    "private-456",
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
	var createAuth string

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/accounts/token-123/baskets", func(w http.ResponseWriter, r *http.Request) {
		createAuth = r.Header.Get("Authorization")
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

	basket, err := newTestClient(t, mux).CreateBasket(context.Background(), BasketSpec{UserID: 804932984, Username: "mavey", IPAddress: "203.0.113.10"})
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
	if _, present := custom["gifted_by"]; present {
		t.Errorf("self-purchase basket must not carry gifted_by, got %v", custom["gifted_by"])
	}
	if createBody["complete_url"] != "https://dashboard.example/billing?checkout=complete" {
		t.Errorf("complete_url = %v", createBody["complete_url"])
	}
	if createBody["username"] != "mavey" {
		t.Errorf("username = %v, want mavey", createBody["username"])
	}
	if createBody["ip_address"] != "203.0.113.10" {
		t.Errorf("ip_address = %v, want 203.0.113.10", createBody["ip_address"])
	}
	if createAuth != "Basic dG9rZW4tMTIzOnByaXZhdGUtNDU2" {
		t.Errorf("Authorization = %q, want Basic auth with public token/private key", createAuth)
	}

	if packageBody["package_id"] != float64(42) {
		t.Errorf("package_id = %v, want 42", packageBody["package_id"])
	}
	if packageBody["type"] != "subscription" {
		t.Errorf("type = %v, want subscription", packageBody["type"])
	}
}

func TestCreateBasketWithoutPrivateKeyOmitsAuthenticatedIP(t *testing.T) {
	var createBody map[string]any
	var createAuth string

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/accounts/token-123/baskets", func(w http.ResponseWriter, r *http.Request) {
		createAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"ident": "bkt-legacy",
				"links": map[string]any{"checkout": "https://pay.tebex.io/bkt-legacy"},
			},
		})
	})
	mux.HandleFunc("POST /api/baskets/bkt-legacy/packages", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"ident": "bkt-legacy",
				"links": map[string]any{"checkout": "https://pay.tebex.io/bkt-legacy"},
			},
		})
	})

	srv := httptest.NewServer(mux)
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

	_, err = client.CreateBasket(context.Background(), BasketSpec{
		UserID:    804932984,
		Username:  "mavey",
		IPAddress: "203.0.113.10",
	})
	if err != nil {
		t.Fatalf("CreateBasket: %v", err)
	}

	if _, present := createBody["ip_address"]; present {
		t.Errorf("ip_address must be omitted without private key, got %v", createBody["ip_address"])
	}
	if createAuth != "" {
		t.Errorf("Authorization = %q, want empty without private key", createAuth)
	}
}

func TestCreateBasketGiftCarriesAttribution(t *testing.T) {
	var createBody map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/accounts/token-123/baskets", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"ident": "bkt-gift",
				"links": map[string]any{"checkout": "https://pay.tebex.io/bkt-gift"},
			},
		})
	})
	mux.HandleFunc("POST /api/baskets/bkt-gift/packages", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"ident": "bkt-gift",
				"links": map[string]any{"checkout": "https://pay.tebex.io/bkt-gift"},
			},
		})
	})

	_, err := newTestClient(t, mux).CreateBasket(context.Background(), BasketSpec{
		UserID:        111,
		Username:      "recipient",
		GiftedByID:    804932984,
		GiftedByLogin: "mavey",
	})
	if err != nil {
		t.Fatalf("CreateBasket: %v", err)
	}

	custom, _ := createBody["custom"].(map[string]any)
	if custom["user_id"] != "111" {
		t.Errorf("custom.user_id = %v, want recipient 111", custom["user_id"])
	}
	if custom["gifted_by"] != "804932984" {
		t.Errorf("custom.gifted_by = %v, want 804932984", custom["gifted_by"])
	}
	if custom["gifted_by_login"] != "mavey" {
		t.Errorf("custom.gifted_by_login = %v, want mavey", custom["gifted_by_login"])
	}
}

func TestCreateBasketUpstreamError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"store disabled"}`, http.StatusForbidden)
	})

	if _, err := newTestClient(t, mux).CreateBasket(context.Background(), BasketSpec{UserID: 1}); err == nil {
		t.Fatal("expected error on 403 upstream")
	}
}

func TestCreateBasketMissingIdent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
	})

	if _, err := newTestClient(t, mux).CreateBasket(context.Background(), BasketSpec{UserID: 1}); err == nil {
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
