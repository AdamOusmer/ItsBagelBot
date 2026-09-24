// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"testing"
	"time"
)

func TestAdoptableStored(t *testing.T) {
	future := time.Now().Add(time.Hour)
	nearExpiry := time.Now().Add(refreshMargin - time.Second)

	cases := []struct {
		name   string
		stored StoredLoad
		wantOK bool
	}{
		{"no access token", StoredLoad{AccessTokenExpiresAt: &future}, false},
		{"no expiry -- must never mean valid forever", StoredLoad{AccessToken: "tok"}, false},
		{"expiry inside refreshMargin", StoredLoad{AccessToken: "tok", AccessTokenExpiresAt: &nearExpiry}, false},
		{"expiry comfortably in the future", StoredLoad{AccessToken: "tok", AccessTokenExpiresAt: &future}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, ttl, ok := adoptableStored(tc.stored)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if token != tc.stored.AccessToken {
				t.Fatalf("token = %q, want %q", token, tc.stored.AccessToken)
			}
			if ttl <= 0 {
				t.Fatalf("ttl = %v, want > 0", ttl)
			}
		})
	}
}

func TestStoredUserTokenSourceAdoptsStoredAccessToken(t *testing.T) {
	expiresAt := time.Now().Add(2 * time.Hour)
	persistCalled := false

	src := NewStoredUserTokenSource(ClientCredentials{}, "", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			return StoredLoad{
				AccessToken:          "stored-access-token",
				AccessTokenExpiresAt: &expiresAt,
				RefreshToken:         "stored-refresh-token",
			}
		},
		Persist: func(context.Context, string, string, time.Time) error {
			persistCalled = true
			return nil
		},
	}, MintLease{})

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "stored-access-token" {
		t.Fatalf("token = %q, want %q", token, "stored-access-token")
	}
	if persistCalled {
		t.Fatal("Persist was called; adopting a stored token must never re-mint or re-persist")
	}
	if remaining := src.ExpiresIn(); remaining <= refreshMargin {
		t.Fatalf("ExpiresIn() = %v, want > refreshMargin (%v)", remaining, refreshMargin)
	}
}

func TestStoredUserTokenSourceFallsBackWithoutStoredExpiry(t *testing.T) {
	src := NewStoredUserTokenSource(ClientCredentials{}, "", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			return StoredLoad{AccessToken: "stored-access-token"}
		},
		Persist: func(context.Context, string, string, time.Time) error {
			t.Fatal("Persist must not be called when there is nothing to mint")
			return nil
		},
	}, MintLease{})

	_, err := src.Token(context.Background())
	if err != ErrNoRefreshToken {
		t.Fatalf("err = %v, want ErrNoRefreshToken", err)
	}
}

func TestStoredUserTokenSourceFallsBackWhenStoredTokenNearExpiry(t *testing.T) {
	nearExpiry := time.Now().Add(refreshMargin - time.Second)

	src := NewStoredUserTokenSource(ClientCredentials{}, "", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			return StoredLoad{AccessToken: "stored-access-token", AccessTokenExpiresAt: &nearExpiry}
		},
		Persist: func(context.Context, string, string, time.Time) error {
			t.Fatal("Persist must not be called when there is nothing to mint")
			return nil
		},
	}, MintLease{})

	_, err := src.Token(context.Background())
	if err != ErrNoRefreshToken {
		t.Fatalf("err = %v, want ErrNoRefreshToken", err)
	}
}
