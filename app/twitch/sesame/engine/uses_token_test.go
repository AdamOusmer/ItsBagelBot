// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func usesPipeline(t *testing.T, response string, uses int64) *Pipeline {
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

func TestUsesTokenExcludesTheCurrentRun(t *testing.T) {
	p := usesPipeline(t, "{uses}", 4)
	assert.Equal(t, "4", expandViewer(t, p, "!hug"))
	assert.Equal(t, "4", expandViewer(t, p, "!hug"))
}

func TestUsesTokenRendersZeroForANeverRunCommand(t *testing.T) {
	p := usesPipeline(t, "used {uses|never} times", 0)
	assert.Equal(t, "used 0 times", expandViewer(t, p, "!hug"))
}

func TestUsesTokenWithAPayloadStaysLiteral(t *testing.T) {
	p := usesPipeline(t, "{uses:other}", 9)
	assert.Equal(t, "{uses:other}", expandViewer(t, p, "!hug"))
}
