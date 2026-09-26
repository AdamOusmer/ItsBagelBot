// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func TestUserEventsIdentifyAccountIncarnation(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 1001, "viewer", "Viewer", "viewer@example.com"))
	first := client.User.Query().Where(user.IDEQ(1001)).OnlyX(ctx)
	var changed data.UserChangedDTO
	require.NoError(t, codec.Unmarshal(pub.On(data.SubjectUserChanged)[0].Payload, &changed))
	require.Equal(t, first.CreatedAt.UnixMicro(), changed.AccountCreatedAt)
	require.Positive(t, changed.AccountCreatedAt)

	// Reprojection and ordinary preferences keep the same incarnation.
	require.NoError(t, repo.SetCommandsPageHidden(ctx, 1001, true))
	require.NoError(t, repo.Reproject(ctx))
	for _, msg := range pub.On(data.SubjectUserChanged) {
		var dto data.UserChangedDTO
		require.NoError(t, codec.Unmarshal(msg.Payload, &dto))
		require.Equal(t, changed.AccountCreatedAt, dto.AccountCreatedAt)
	}

	require.NoError(t, repo.Delete(ctx, 1001))
	var deleted data.UserDeletedDTO
	require.NoError(t, codec.Unmarshal(pub.On(data.SubjectUserDeleted)[0].Payload, &deleted))
	require.Equal(t, changed.AccountCreatedAt, deleted.AccountCreatedAt)

	// Use an explicit later timestamp rather than depending on clock speed
	// or the database's timestamp precision during recreation.
	client.User.Create().SetID(1001).SetUsername("viewer").SetEmail("viewer@example.com").
		SetCreatedAt(first.CreatedAt.Add(time.Second)).SaveX(ctx)
	require.NoError(t, repo.Register(ctx, 1001, "viewer", "Viewer", "viewer@example.com"))
	msgs := pub.On(data.SubjectUserChanged)
	var recreated data.UserChangedDTO
	require.NoError(t, codec.Unmarshal(msgs[len(msgs)-1].Payload, &recreated))
	require.Greater(t, recreated.AccountCreatedAt, deleted.AccountCreatedAt)
}
