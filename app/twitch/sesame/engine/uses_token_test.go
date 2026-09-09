// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// usesPipeline runs one custom command whose projected row already carries a
// lifetime use count, which is the only place {uses} reads from.
func usesPipeline(t *testing.T, response string, uses uint64) *Pipeline {
	t.Helper()
	d := Deps{
		Proj: fakeReader{
			cmd:      projection.Command{Name: "hug", Response: response, IsActive: true, Perm: "everyone", Uses: uses},
			cmdFound: true,
		},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Log:      zap.NewNop(),
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

func TestUsesTokenRendersTheProjectedCount(t *testing.T) {
	p := usesPipeline(t, "this hug has been given {uses} times", 128)
	assert.Equal(t, "this hug has been given 128 times", expandViewer(t, p, "!hug"))
}

// The run doing the rendering is not in the number (pinned): its tick is
// published after the reply, so two runs against one projected row print the
// same count. Pinning it here is what stops a later "helpful" +1 from being
// added in the chain.
func TestUsesTokenExcludesTheCurrentRun(t *testing.T) {
	p := usesPipeline(t, "{uses}", 4)
	assert.Equal(t, "4", expandViewer(t, p, "!hug"))
	assert.Equal(t, "4", expandViewer(t, p, "!hug"))
}

// A command nobody has run prints "0", and its fallback does not fire: zero is
// the answer, not a lookup that came back empty.
func TestUsesTokenRendersZeroForANeverRunCommand(t *testing.T) {
	p := usesPipeline(t, "used {uses|never} times", 0)
	assert.Equal(t, "used 0 times", expandViewer(t, p, "!hug"))
}

// A payload is not a spelling this token has, so chat sees the token.
func TestUsesTokenWithAPayloadStaysLiteral(t *testing.T) {
	p := usesPipeline(t, "{uses:other}", 9)
	assert.Equal(t, "{uses:other}", expandViewer(t, p, "!hug"))
}
