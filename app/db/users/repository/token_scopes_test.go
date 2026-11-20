// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/repository"

	"github.com/stretchr/testify/require"
)

func TestTokenScopesPersistAcrossSaves(t *testing.T) {
	_, _, repo := setup(t)
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 1, Username: "one", Email: "one@example.test"}))
	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 2, Username: "two", Email: "two@example.test"}))

	granted := []string{"channel:bot", "user:read:chat"}
	for _, test := range []struct {
		name string
		id   uint64
		save []string
		want []string
	}{
		{name: "unknown", id: 1},
		{name: "known", id: 1, save: granted, want: granted},
		{name: "omitted keeps scopes", id: 1, want: granted},
		{name: "different owner starts unknown", id: 2},
		{name: "explicit empty replaces scopes", id: 1, save: []string{}, want: []string{}},
		{name: "omitted keeps explicit empty", id: 1, want: []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			write := repository.TokenWrite{UserID: test.id, Type: tokens.TypeUserToken, Platform: tokens.PlatformTwitch,
				AccessToken: []byte("access"), Scopes: test.save}
			require.NoError(t, repo.UpsertToken(ctx, write))
			_, _, _, scopes, err := repo.Token(ctx, write.UserID, write.Type, write.Platform)
			require.NoError(t, err)
			require.Equal(t, test.want, scopes)
		})
	}
}
