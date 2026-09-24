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
