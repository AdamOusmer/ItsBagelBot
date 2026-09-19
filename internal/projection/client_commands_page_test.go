// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEvictScopeCommandsPageDropsUserEntry pins the client.go addition to
// evictScope's "status"/"grant"/"live"/"locale" case (spec §4.2): the
// commands-page flag lives on the same cached User, so the scope must drop
// it too, closing the projector-fold race the scope exists for.
func TestEvictScopeCommandsPageDropsUserEntry(t *testing.T) {
	c := NewClient(Config{Store: NewStore(nil), TTL: time.Minute})
	t.Cleanup(c.Close)

	const userID = 55
	c.users.Set(key("user", userID), User{Status: "vip", CommandsPageHidden: true})

	loaded := false
	reload := func(context.Context) (User, error) {
		loaded = true
		return User{}, nil
	}

	// Sanity: before eviction the seeded entry serves without the loader.
	v, err := c.users.GetOrLoad(context.Background(), key("user", userID), reload)
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.True(t, v.CommandsPageHidden)

	c.evictScope("commands_page", userID, nil)

	loaded = false
	_, err = c.users.GetOrLoad(context.Background(), key("user", userID), reload)
	require.NoError(t, err)
	assert.True(t, loaded, `evictScope("commands_page") must drop the cached user entry`)
}
