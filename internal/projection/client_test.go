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

func TestInvalidationRefreshesTheCachedUser(t *testing.T) {
	cases := []struct {
		scope string
		field fakeField
		want  User
	}{
		{"locale", fakeField{"locale", "fr"}, User{Status: "standard", IsActive: true, Locale: "fr"}},
		{"status", fakeField{"status", "premium"}, User{Status: "premium", IsActive: true}},
		{"grant", fakeField{"status", "vip"}, User{Status: "vip", IsActive: true}},
		{"live", fakeField{"status", "paid"}, User{Status: "paid", IsActive: true}},
		{"commands_page", fakeField{"commands_page_hidden", "1"}, User{Status: "standard", IsActive: true, CommandsPageHidden: true}},
	}
	for _, tc := range cases {
		t.Run(tc.scope+" scope drops the cached user", func(t *testing.T) {
			store, f := newTestStore(t)
			f.seed("settings:55", fakeField{"status", "standard"})
			f.seed("settings:55", fakeField{"active", "1"})
			c, evict := invalidatedClient(t, store)
			ctx := context.Background()
			user, err := c.User(ctx, 55)
			require.NoError(t, err)
			require.Equal(t, User{Status: "standard", IsActive: true}, user)

			f.seed("settings:55", tc.field)
			cached, err := c.User(ctx, 55)
			require.NoError(t, err)
			assert.Equal(t, user, cached, "the stale entry is served until the invalidation arrives")
			evict(tc.scope, 55)

			require.Eventually(t, func() bool {
				got, err := c.User(ctx, 55)
				return err == nil && got == tc.want
			}, 2*time.Second, 5*time.Millisecond)
		})
	}
}
