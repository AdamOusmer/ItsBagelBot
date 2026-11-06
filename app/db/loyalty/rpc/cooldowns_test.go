// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"

	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCooldownVerbsClaimRefuseRelease(t *testing.T) {
	h := newHarness(t)
	claim := loyaltyrpc.Request{UserID: "2", Cooldowns: []loyaltyrpc.CooldownWindow{{Key: "k", TTLSeconds: 86_401}}}

	won := h.call(t, "cooldown.claim", claim)
	require.True(t, won.Claimed)
	require.Len(t, won.Cooldowns, 1)
	assert.Equal(t, "k", won.Cooldowns[0].Key)
	assert.NotZero(t, won.Cooldowns[0].ExpiresAtMS)

	refused := h.call(t, "cooldown.claim", claim)
	assert.False(t, refused.Claimed)
	assert.Empty(t, refused.Error)

	release := h.call(t, "cooldown.release", loyaltyrpc.Request{UserID: "2", Cooldowns: won.Cooldowns})
	assert.True(t, release.Found)
	assert.True(t, h.call(t, "cooldown.claim", claim).Claimed)
}

func TestCooldownVerbsRefuseBadInput(t *testing.T) {
	h := newHarness(t)
	for name, req := range map[string]loyaltyrpc.Request{
		"bad channel": {UserID: "nope", Cooldowns: []loyaltyrpc.CooldownWindow{{Key: "k", TTLSeconds: 86_401}}},
		"over thirty": {UserID: "2", Cooldowns: []loyaltyrpc.CooldownWindow{{Key: "k", TTLSeconds: 2_592_001}}},
		"no windows":  {UserID: "2"},
	} {
		assert.Equal(t, domainrpc.CodeInvalid, h.call(t, "cooldown.claim", req).Code, name)
	}
}
