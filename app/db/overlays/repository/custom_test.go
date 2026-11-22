// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/overlays/ent"
	"ItsBagelBot/app/db/overlays/ent/enttest"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/overlays"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/objectstore"
	"ItsBagelBot/pkg/objectstore/objectstoretest"

	"github.com/stretchr/testify/require"
)

const strangerID = 1002

func setupStore(t *testing.T) (*repository.Overlays, *objectstoretest.Memory) {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	objects := objectstoretest.NewMemory()
	return repository.New(client, bustest.NewPublisher(), objects), objects
}

func seedAsset(t *testing.T, repo *repository.Overlays, objects objectstore.Store, owner uint64, id repository.AssetID) repository.Asset {
	t.Helper()
	asset, err := repo.StoreAsset(t.Context(), repository.Asset{
		ID: id, UserID: owner, Type: overlays.AssetPNG, Bytes: 10, W: 2, H: 2, SHA256: strings.Repeat(string(id[:1]), 64),
	})
	require.NoError(t, err)
	require.NoError(t, objects.Put(t.Context(), objectstore.Object{Key: asset.Key(), Body: strings.NewReader("0123456789"), Size: 10}))
	return asset
}

func mustMarshal(t *testing.T, doc overlays.Document) []byte {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	return raw
}

func customDoc(assetID repository.AssetID) overlays.Document {
	return overlays.Document{
		Version: overlays.Version, Kind: overlays.KindCustom, ID: "o1", Name: "Mine", Width: 1920, Height: 1080,
		Widgets: []overlays.Widget{{
			ID: "w1", Name: "Logo", Kind: overlays.WidgetImage, W: 100, H: 100, Opacity: 1,
			Settings: map[string]any{"media": "asset:" + string(assetID)},
		}},
		Assets: map[string]overlays.Asset{string(assetID): {}},
	}
}

func TestCreateCustomChecksAssetOwnership(t *testing.T) {
	repo, objects := setupStore(t)
	mine := seedAsset(t, repo, objects, userID, "aaaa")
	theirs := seedAsset(t, repo, objects, strangerID, "bbbb")

	cases := []struct {
		name    string
		doc     overlays.Document
		wantErr error
	}{
		{"own asset", customDoc(mine.ID), nil},
		{"another account's asset", customDoc(theirs.ID), repository.ErrForeignAsset},
		{"unknown asset", customDoc("cccc"), repository.ErrForeignAsset},
		{"system document", overlays.Document{Version: overlays.Version, Kind: overlays.KindSystem, ID: "powered", Name: "x"}, repository.ErrInvalidDocument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := repo.CreateCustom(t.Context(), userID, tc.doc)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestCreateCustomFillsAssetMetadataFromTheStoredRow(t *testing.T) {
	repo, objects := setupStore(t)
	mine := seedAsset(t, repo, objects, userID, "aaaa")

	created, token, err := repo.CreateCustom(t.Context(), userID, customDoc(mine.ID))
	require.NoError(t, err)
	doc, err := overlays.Parse(created.Document)
	require.NoError(t, err)

	require.Equal(t, map[string]overlays.Asset{"aaaa": mine.Meta()}, doc.Assets)
	require.Len(t, token, 43)
}

func TestSaveCustomKeepsTheRevisionContract(t *testing.T) {
	repo, objects := setupStore(t)
	mine := seedAsset(t, repo, objects, userID, "aaaa")
	created, _, err := repo.CreateCustom(t.Context(), userID, customDoc(mine.ID))
	require.NoError(t, err)
	next := customDoc(mine.ID)
	next.Name = "Renamed"
	next.Assets["aaaa"] = mine.Meta()
	raw := mustMarshal(t, next)

	_, stale := repo.SaveCustom(t.Context(), userID, created.ID, created.Revision+3, raw)
	saved, err := repo.SaveCustom(t.Context(), userID, created.ID, created.Revision, raw)

	require.ErrorIs(t, stale, repository.ErrRevisionConflict)
	require.NoError(t, err)
	require.Equal(t, [2]any{"Renamed", created.Revision + 1}, [2]any{saved.Name, saved.Revision})
}

func TestDeleteAssetRefusesWhileADocumentUsesIt(t *testing.T) {
	repo, objects := setupStore(t)
	mine := seedAsset(t, repo, objects, userID, "aaaa")
	created, _, err := repo.CreateCustom(t.Context(), userID, customDoc(mine.ID))
	require.NoError(t, err)

	inUse := repo.DeleteAsset(t.Context(), userID, string(mine.ID))
	require.NoError(t, repo.DeleteOverlay(t.Context(), userID, created.ID))
	released := repo.DeleteAsset(t.Context(), userID, string(mine.ID))

	require.ErrorIs(t, inUse, repository.ErrAssetInUse)
	require.NoError(t, released)
	require.Empty(t, objects.Keys())
}

func TestDeleteAccountClearsAssetRowsAndThePrefix(t *testing.T) {
	repo, objects := setupStore(t)
	seedAsset(t, repo, objects, userID, "aaaa")
	seedAsset(t, repo, objects, userID, "bbbb")
	kept := seedAsset(t, repo, objects, strangerID, "cccc")

	require.NoError(t, repo.DeleteAccount(t.Context(), userID, time.Now().Add(time.Hour)))
	left, err := repo.ListAssets(t.Context(), userID)
	require.NoError(t, err)

	require.Equal(t, [2]any{0, []string{kept.Key()}}, [2]any{len(left), objects.Keys()})
}
