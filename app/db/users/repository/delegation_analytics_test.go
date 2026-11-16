// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type delegationStep struct {
	action, token string
	delegate      uint64
	want          int64
	wantErr       bool
}

func (s delegationStep) apply(t *testing.T, repo *repository.Users) error {
	t.Helper()
	ctx := t.Context()
	switch s.action {
	case "create":
		return repo.CreateDelegation(ctx, repository.DelegationLink{Token: s.token, OwnerID: 1001, OwnerLogin: "owner", Sections: []string{"commands"}})
	case "consume":
		_, err := repo.ConsumeDelegation(ctx, s.token, s.delegate, "delegate")
		return err
	case "update":
		_, err := repo.UpdateDelegationSections(ctx, s.token, 1001, []string{"modules"})
		return err
	case "revoke":
		_, err := repo.RevokeDelegation(ctx, s.token, 1001)
		return err
	case "optout":
		return repo.OptOutDelegation(ctx, 1001, s.delegate)
	case "delete":
		_, err := repo.DeleteDelegationsByOwner(ctx, 1001)
		return err
	}
	return nil
}

func TestDelegatingLevelsFollowLiveGrants(t *testing.T) {
	_, _, repo := setup(t)
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 1001, "owner", "Owner", "owner@example.test"))
	vc := valkeytest.Spawn(t)
	rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker())
	t.Cleanup(rec.Close)
	repo.SetAnalytics(rec)
	for _, s := range []delegationStep{
		{action: "create", token: "first"},
		{action: "create", token: "first", wantErr: true},
		{action: "consume", token: "first", delegate: 2001, want: 1},
		{action: "consume", token: "first", delegate: 2001, want: 1, wantErr: true},
		{action: "update", token: "first", want: 1},
		{action: "create", token: "reclaim", want: 1},
		{action: "consume", token: "reclaim", delegate: 2001, want: 1},
		{action: "create", token: "second", want: 1},
		{action: "consume", token: "second", delegate: 2002, want: 1},
		{action: "revoke", token: "first", want: 1},
		{action: "optout", delegate: 2002},
		{action: "optout", delegate: 2002, wantErr: true},
		{action: "create", token: "third"},
		{action: "consume", token: "third", delegate: 2003, want: 1},
		{action: "delete"},
		{action: "delete"},
	} {
		require.Equal(t, s.wantErr, s.apply(t, repo) != nil, "%+v", s)
		rec.Flush(ctx)
		fields, err := pkgvalkey.HashAllInts(ctx, vc, catalog.LevelKey)
		require.NoError(t, err)
		seed, err := repo.AnalyticsSeed(ctx)
		require.NoError(t, err)
		require.Equal(t, []int64{s.want, s.want}, []int64{fields["accounts.delegating"], seed["accounts.delegating"]}, "%+v", s)
	}
}

func TestDelegatingSeedCountsEligibleOwnersOnce(t *testing.T) {
	client, _, repo := setup(t)
	ctx := t.Context()
	for id, name := range []string{"owner", "test", "banned", "unused"} {
		require.NoError(t, repo.Register(ctx, uint64(id+1), name, name, name+"@example.test"))
	}
	require.NoError(t, client.User.UpdateOneID(2).SetTestAccount(true).Exec(ctx))
	require.NoError(t, client.User.UpdateOneID(3).SetBanned(true).Exec(ctx))
	now := time.Now()
	for i, owner := range []uint64{1, 1, 2, 3, 9999} {
		require.NoError(t, client.Delegation.Create().SetToken(string(rune('a'+i))).SetOwnerID(owner).SetOwnerLogin("owner").
			SetSections([]string{"commands"}).SetDelegateID(100).SetConsumedAt(now).SetExpiresAt(now.Add(-time.Hour)).Exec(ctx))
	}
	require.NoError(t, repo.CreateDelegation(ctx, repository.DelegationLink{Token: "unused", OwnerID: 4, OwnerLogin: "unused", Sections: []string{"commands"}}))
	got, err := repo.AnalyticsSeed(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, got["accounts.delegating"])
}
