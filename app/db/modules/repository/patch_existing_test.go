// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatchExistingNeverCreatesMissingRevisionZeroRow(t *testing.T) {
	client, pub, repo := setup(t)
	defer repo.Close(t.Context())
	res, err := repo.PatchExisting(t.Context(), 2001, "triggers", true, map[string]codec.RawMessage{"message": []byte(`"{triggers:user}"`)}, 0, 123)
	require.NoError(t, err)
	assert.True(t, res.Conflict)
	assert.Empty(t, client.Modules.Query().AllX(t.Context()))
	assert.Empty(t, pub.On(data.SubjectModuleChanged))
}

func TestPatchExistingRejectsDeletedOrReplacedRowAtSameRevision(t *testing.T) {
	for _, replace := range []bool{false, true} {
		name := "deleted"
		if replace {
			name = "recreated"
		}
		t.Run(name, func(t *testing.T) {
			client, pub, repo := setup(t)
			defer repo.Close(t.Context())
			ctx := t.Context()
			old := client.Modules.Create().SetUserID(2001).SetName("triggers").SetIsEnabled(true).SetRevision(0).SetConfigs([]byte(`{"message":"{user}","rules":[]}`)).SaveX(ctx)
			client.Modules.DeleteOneID(old.ID).ExecX(ctx)
			if replace {
				client.Modules.Create().SetUserID(old.UserID).SetName(old.Name).SetIsEnabled(false).SetRevision(old.Revision).SetConfigs([]byte(`{"message":"replacement","rules":[]}`)).ExecX(ctx)
			}
			res, err := repo.PatchExisting(ctx, old.UserID, old.Name, true, map[string]codec.RawMessage{"message": []byte(`"{triggers:user}"`)}, old.Revision, old.ID)
			require.NoError(t, err)
			assert.True(t, res.Conflict)
			assert.Empty(t, pub.On(data.SubjectModuleChanged))
			rows := client.Modules.Query().AllX(ctx)
			if !replace {
				assert.Empty(t, rows)
				return
			}
			require.Len(t, rows, 1)
			assert.NotEqual(t, old.ID, rows[0].ID)
			assert.Zero(t, rows[0].Revision)
			assert.False(t, rows[0].IsEnabled)
			assert.JSONEq(t, `{"message":"replacement","rules":[]}`, string(rows[0].Configs))
		})
	}
}

func TestPatchExistingUpdatesIdentifiedRowAndPreservesUnrelatedConfig(t *testing.T) {
	client, pub, repo := setup(t)
	defer repo.Close(t.Context())
	ctx := t.Context()
	row := client.Modules.Create().SetUserID(2001).SetName("triggers").SetIsEnabled(false).SetRevision(0).SetConfigs([]byte(`{"message":"{user}","rules":[]}`)).SaveX(ctx)
	res, err := repo.PatchExisting(ctx, row.UserID, row.Name, false, map[string]codec.RawMessage{"message": []byte(`"{triggers:user}"`)}, row.Revision, row.ID)
	require.NoError(t, err)
	assert.False(t, res.Conflict)
	assert.Equal(t, 1, res.Rev)
	updated := client.Modules.GetX(ctx, row.ID)
	assert.False(t, updated.IsEnabled)
	assert.JSONEq(t, `{"message":"{triggers:user}","rules":[],"__rev":1}`, string(updated.Configs))
	assert.Len(t, pub.On(data.SubjectModuleChanged), 1)
	stale, err := repo.PatchExisting(ctx, row.UserID, row.Name, false, map[string]codec.RawMessage{"message": []byte(`"stale"`)}, 0, row.ID)
	require.NoError(t, err)
	assert.True(t, stale.Conflict)
	assert.Equal(t, 1, stale.Rev)
	assert.Len(t, pub.On(data.SubjectModuleChanged), 1)
}

func TestPatchExistingPreservesLoyaltyConfigAcrossAccountOwnershipChecks(t *testing.T) {
	for _, epoch := range []int64{0, 100, 101} {
		t.Run(fmt.Sprint(epoch), func(t *testing.T) {
			client, pub, repo := setup(t)
			defer repo.Close(t.Context())
			repo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return 101, nil })
			config := []byte(fmt.Sprintf(`{"__account_created_at":%d,"message":"{user}","watchtime_points":50}`, epoch))
			row := client.Modules.Create().SetUserID(2001).SetName("loyalty").SetIsEnabled(false).SetRevision(0).SetConfigs(config).SaveX(t.Context())
			res, err := repo.PatchExisting(t.Context(), row.UserID, row.Name, row.IsEnabled, map[string]codec.RawMessage{"message": []byte(`"{loyalty:user}"`)}, row.Revision, row.ID)
			require.NoError(t, err)
			updated := client.Modules.GetX(t.Context(), row.ID)
			if epoch != 101 {
				assert.True(t, res.Conflict)
				assert.Zero(t, updated.Revision)
				assert.JSONEq(t, string(config), string(updated.Configs))
				assert.Empty(t, pub.On(data.SubjectModuleChanged))
				return
			}
			assert.False(t, res.Conflict)
			assert.Equal(t, 1, updated.Revision)
			assert.JSONEq(t, `{"__account_created_at":101,"message":"{loyalty:user}","watchtime_points":50,"__rev":1}`, string(updated.Configs))
			assert.Len(t, pub.On(data.SubjectModuleChanged), 1)
		})
	}
}
