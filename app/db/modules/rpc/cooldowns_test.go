// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"

	domainrpc "ItsBagelBot/internal/domain/rpc"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCooldownVerbsClaimRefuseRelease(t *testing.T) {
	h := newStateHarness(t)
	claim := modulesrpc.CooldownRequest{UserID: "2", Windows: []modulesrpc.CooldownWindow{{Key: "k", TTLSeconds: 86_401}}}

	var won modulesrpc.CooldownReply
	h.call(t, "cooldown.claim", claim, &won)
	require.True(t, won.Claimed)
	require.Len(t, won.Windows, 1)
	assert.Equal(t, "k", won.Windows[0].Key)
	assert.NotZero(t, won.Windows[0].ExpiresAtMS)

	var refused modulesrpc.CooldownReply
	h.call(t, "cooldown.claim", claim, &refused)
	assert.False(t, refused.Claimed)
	assert.Empty(t, refused.Error)
	assert.Equal(t, won.Windows, refused.Windows, "the refusal says when the window ends")

	var release modulesrpc.CooldownReply
	h.call(t, "cooldown.release", modulesrpc.CooldownRequest{UserID: "2", Windows: won.Windows}, &release)
	assert.True(t, release.Found)
	var again modulesrpc.CooldownReply
	h.call(t, "cooldown.claim", claim, &again)
	assert.True(t, again.Claimed)
}

func TestCooldownVerbsRefuseBadInput(t *testing.T) {
	h := newStateHarness(t)
	for name, req := range map[string]modulesrpc.CooldownRequest{
		"bad channel": {UserID: "nope", Windows: []modulesrpc.CooldownWindow{{Key: "k", TTLSeconds: 86_401}}},
		"over thirty": {UserID: "2", Windows: []modulesrpc.CooldownWindow{{Key: "k", TTLSeconds: 2_592_001}}},
		"no windows":  {UserID: "2"},
	} {
		var reply modulesrpc.CooldownReply
		h.call(t, "cooldown.claim", req, &reply)
		assert.Equal(t, domainrpc.CodeInvalid, reply.Code, name)
	}
}
