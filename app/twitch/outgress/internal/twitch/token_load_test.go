// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestStoredTokenLoadFailure(t *testing.T) {
	loadErr := errors.New("tokens get rpc: nats: timeout")
	future := time.Now().Add(time.Hour)
	stored := StoredLoad{AccessToken: "stored-access", AccessTokenExpiresAt: &future}

	tests := []struct {
		name     string
		seed     string
		loads    []StoredLoad
		wantTok  string
		wantErr  error
		wantDead bool
	}{
		{"failed load is transient", "", []StoredLoad{{Err: loadErr}}, "", loadErr, false},
		{"failed load never mints from a known refresh token", "seed-refresh", []StoredLoad{{Err: loadErr}}, "", loadErr, false},
		{"nothing stored stays dead", "", []StoredLoad{{}}, "", ErrNoRefreshToken, true},
		{"next call retries the load", "", []StoredLoad{{Err: loadErr}, stored}, "stored-access", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mints, persists, loadCalls int32
			fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
				atomic.AddInt32(&mints, 1)
				return fakeOAuthResponse(`{"access_token":"minted","expires_in":14400}`), nil
			})
			src := NewStoredUserTokenSource(ClientCredentials{}, tc.seed, StoredTokenIO{
				Load: func(context.Context) StoredLoad {
					return tc.loads[atomic.AddInt32(&loadCalls, 1)-1]
				},
				Persist: func(context.Context, string, string, time.Time) error {
					atomic.AddInt32(&persists, 1)
					return nil
				},
			}, MintLease{})

			var token string
			var err error
			for range tc.loads {
				token, err = src.Token(context.Background())
			}

			if token != tc.wantTok || !errors.Is(err, tc.wantErr) {
				t.Fatalf("Token() = %q, %v; want %q, %v", token, err, tc.wantTok, tc.wantErr)
			}
			if GrantDead(err) != tc.wantDead {
				t.Fatalf("GrantDead(%v) = %v, want %v", err, !tc.wantDead, tc.wantDead)
			}
			if m, p := atomic.LoadInt32(&mints), atomic.LoadInt32(&persists); m != 0 || p != 0 {
				t.Fatalf("mints = %d, persists = %d; a failed or empty load must not mint or persist", m, p)
			}
		})
	}
}
