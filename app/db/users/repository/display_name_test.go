// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/app/db/users/repository"

	"github.com/stretchr/testify/require"
)

func TestRegisterKeepsDisplayNamesInAnyScript(t *testing.T) {
	client, _, repo := setup(t)
	ctx := t.Context()
	names := []string{"ストリーマー", "스트리머", "Стример", "主播", strings.Repeat("配", 25)}

	for i, name := range names {
		id := uint64(9001 + i)
		require.NoError(t, repo.Register(ctx, repository.Registration{ID: id, Username: "streamer" + string(rune('a'+i)), DisplayName: name, Email: fmt.Sprintf("%d@twitch.tv", id)}))
		require.Equal(t, name, client.User.GetX(ctx, id).DisplayName)
	}
}
