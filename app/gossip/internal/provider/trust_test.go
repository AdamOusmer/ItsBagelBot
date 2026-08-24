// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package provider

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func trustDeps() Deps { return Deps{Log: zap.NewNop()} }

func noop(context.Context, gossiprpc.Request) any { return nil }

// The inverted default is the whole security posture: a provider that never
// declared .Trusted() must get WARP-lane clients, so a forgotten flag fails
// toward hidden egress, never toward exposing production IPs.
func TestClientLaneFollowsTrustDeclaration(t *testing.T) {
	trusted := NewProvider("t", trustDeps()).Trusted()
	assert.Equal(t, core.LaneDirect, trusted.Client("https://a.invalid", nil, time.Second).Lane())

	unmarked := NewProvider("u", trustDeps())
	assert.Equal(t, core.LaneWARP, unmarked.Client("https://b.invalid", nil, time.Second).Lane(),
		"the default must be the untrusted lane; inversion is the point")
}

// Trust is positional by construction: declaring it after a client exists
// would leave that client's lane decided by a flag that did not exist yet.
func TestTrustedAfterClientPanics(t *testing.T) {
	b := NewProvider("late", trustDeps())
	b.Client("https://a.invalid", nil, time.Second)
	assert.Panics(t, func() { b.Trusted() })
}

// A dead trust flag is a boot failure, not a style issue.
func TestValidateRejectsDeadTrustedFlag(t *testing.T) {
	b := NewProvider("dead", trustDeps()).Trusted()
	b.Endpoint("x").Handle(noop)

	err := b.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".Trusted()")
}

// Every construction through the Builder is recorded, in order.
func TestBuilderRecordsEveryClient(t *testing.T) {
	b := NewProvider("r", trustDeps()).Trusted()
	b.Client("https://a.invalid", nil, time.Second)
	b.Client("https://b.invalid", nil, time.Second)
	require.Len(t, b.clients, 2)
	for _, c := range b.clients {
		assert.Equal(t, core.LaneDirect, c.lane)
	}
	w := NewProvider("w", trustDeps())
	w.Client("https://c.invalid", nil, time.Second)
	w.Client("https://d.invalid", nil, time.Second)
	w.Client("https://e.invalid", nil, time.Second)
	require.Len(t, w.clients, 3)
	for _, c := range w.clients {
		assert.Equal(t, core.LaneWARP, c.lane)
	}
}

// Build logs one line per provider tallying clients and lanes — the honest
// boot record of who dials where.
func TestBuildLogsClientTally(t *testing.T) {
	t.Run("trusted", func(t *testing.T) {
		observed, logs := observer.New(zap.InfoLevel)
		d := Deps{Log: zap.New(observed)}

		b := NewProvider("govee", d).Trusted()
		b.Client("https://a.invalid", nil, time.Second)
		b.Client("https://m.invalid", nil, time.Second)
		b.Endpoint("devices").Handle(noop)
		b.Build()

		found := false
		for _, e := range logs.All() {
			if e.Message == "govee: 2 clients (trusted)" {
				found = true
			}
		}
		assert.True(t, found, "expected exact tally line, got %v", logs.All())
	})

	t.Run("warp default", func(t *testing.T) {
		observed, logs := observer.New(zap.InfoLevel)
		d := Deps{Log: zap.New(observed)}

		w := NewProvider("custom", d)
		w.Client("https://c.invalid", nil, time.Second)
		w.Endpoint("fetch").Handle(noop)
		w.Build()

		found := false
		for _, e := range logs.All() {
			if e.Message == "custom: 1 client (warp)" {
				found = true
			}
		}
		assert.True(t, found, "expected exact tally line, got %v", logs.All())
	})
}
