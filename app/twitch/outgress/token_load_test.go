// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"errors"
	"fmt"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/tokenstore"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

func TestStoredLoadKeepsRefusalsAsNoToken(t *testing.T) {
	outage := fmt.Errorf("tokens get rpc: %w", nats.ErrTimeout)
	refused := fmt.Errorf("tokens get rpc: %w", bus.RPCReplyError{Message: "ent: tokens not found"})

	tests := []struct {
		name        string
		loaded      tokenstore.Loaded
		err         error
		wantRefresh string
		wantErr     error
	}{
		{"loaded token passes through", tokenstore.Loaded{RefreshToken: "refresh"}, nil, "refresh", nil},
		{"refusal reads as no token stored", tokenstore.Loaded{}, refused, "", nil},
		{"outage carries the error", tokenstore.Loaded{}, outage, "", outage},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := storedLoad(tc.loaded, tc.err)
			if got.RefreshToken != tc.wantRefresh || !errors.Is(got.Err, tc.wantErr) {
				t.Fatalf("storedLoad = {RefreshToken:%q Err:%v}, want {RefreshToken:%q Err:%v}",
					got.RefreshToken, got.Err, tc.wantRefresh, tc.wantErr)
			}
		})
	}
}
