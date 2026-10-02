// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package hypixel

import (
	"testing"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providertest"

	"github.com/stretchr/testify/assert"
)

func TestMojangCarriesItsOwnBudget(t *testing.T) {
	d := providertest.Deps(providertest.NewMemStore())
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(Config{APIKey: "k"}, d, b)
	assert.NotEqual(t, p.buckets, p.mojangBuckets, "the resolve hop must not share the Hypixel key's bucket")
	assert.NotEqual(t, core.Buckets{}, p.mojangBuckets, "the resolve hop must be metered")
}
