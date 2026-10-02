// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/event/data"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

func setup(t *testing.T) (*ent.Client, *bustest.Publisher, *repository.Users) {
	t.Helper()

	client := testdb.Open(t, "usersent", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })

	pub := bustest.NewPublisher()

	repo := repository.NewUsers(client, dbtest.NewPacker(t), pub, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })

	return client, pub, repo
}

func TestTokenRoundTrip(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@concordia.ca"))

	plaintext := []byte("oauth-token-super-secret")
	refresh := []byte("refresh-token-super-secret")

	require.NoError(t, repo.UpsertToken(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch, plaintext, refresh, nil))

	row := client.Tokens.Query().OnlyX(ctx)
	assert.NotEqual(t, plaintext, row.Token)
	assert.NotEqual(t, refresh, row.RefreshToken)

	access, gotRefresh, _, err := repo.Token(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch)
	require.NoError(t, err)
	assert.Equal(t, plaintext, access)
	assert.Equal(t, refresh, gotRefresh)

	require.NoError(t, repo.UpsertToken(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch, []byte("new"), nil, nil))

	access, _, _, err = repo.Token(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), access, "an upsert replaces the stored token")
}

func TestTokenExpiryPersistsAndClearsOnOverwrite(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@concordia.ca"))

	expiresAt := ptrTime(time.Now().Add(4 * time.Hour).Truncate(time.Second))
	require.NoError(t, repo.UpsertToken(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch, []byte("first"), nil, expiresAt))

	_, _, gotExpiry, err := repo.Token(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch)
	require.NoError(t, err)
	require.NotNil(t, gotExpiry)
	assert.True(t, expiresAt.Equal(*gotExpiry))

	require.NoError(t, repo.UpsertToken(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch, []byte("second"), nil, nil))

	_, _, gotExpiry, err = repo.Token(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch)
	require.NoError(t, err)
	assert.Nil(t, gotExpiry)
}

func TestTokenCiphertextBoundToOwner(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1, "Alice", "Alice", "alice@test.com"))
	require.NoError(t, repo.Register(ctx, 2, "Bob", "Bob", "bob@test.com"))

	require.NoError(t, repo.UpsertToken(ctx, 1, tokens.TypeAccessToken, tokens.PlatformTwitch, []byte("alice-token"), nil, nil))

	stolen := client.Tokens.Query().OnlyX(ctx).Token

	client.Tokens.Create().
		SetUserID(2).
		SetType(tokens.TypeAccessToken).
		SetPlatform(tokens.PlatformTwitch).
		SetToken(stolen).
		ExecX(ctx)

	_, _, _, err := repo.Token(ctx, 2, tokens.TypeAccessToken, tokens.PlatformTwitch)
	assert.Error(t, err, "a ciphertext moved between users must not decrypt")
}

func TestSetStatusRefreshesViewAndPublishes(t *testing.T) {
	_, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@concordia.ca"))

	view, err := repo.Get(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "free", view.Status)

	require.NoError(t, repo.SetStatus(ctx, 1001, user.StatusVip))

	view, err = repo.Get(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "vip", view.Status, "status change must be visible immediately, not after TTL")

	assert.Len(t, pub.On(data.SubjectUserChanged), 2, "register and status change must both announce state")
}

func TestSetCommandsPageHiddenWritesThroughAndPublishes(t *testing.T) {
	_, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@concordia.ca"))

	view, err := repo.Get(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, view.CommandsPageHidden, "default is visible (D2)")

	require.NoError(t, repo.SetCommandsPageHidden(ctx, 1001, true))

	view, err = repo.Get(ctx, 1001)
	require.NoError(t, err)
	assert.True(t, view.CommandsPageHidden, "write-through: no batcher window to wait out")

	msgs := pub.On(data.SubjectUserChanged)
	require.Len(t, msgs, 2, "register and the flag change must both announce state")

	var dto data.UserChangedDTO
	require.NoError(t, codec.Unmarshal(msgs[1].Payload, &dto))
	assert.True(t, dto.CommandsPageHidden, "publishChanged must carry the new value")
}

func TestSetCreatorCodeStoresTrimsClearsAndPublishes(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@concordia.ca"))

	require.NoError(t, repo.SetCreatorCode(ctx, 1001, "  MAVEY10  "))
	repo.Close(context.Background())

	view, err := repo.Get(ctx, 1001)
	require.NoError(t, err)
	require.NotNil(t, view.CreatorCode)
	assert.Equal(t, "MAVEY10", *view.CreatorCode)
	require.NotNil(t, client.User.GetX(ctx, 1001).CreatorCode)
	assert.Equal(t, "MAVEY10", *client.User.GetX(ctx, 1001).CreatorCode)

	require.NoError(t, repo.SetCreatorCode(ctx, 1001, ""))
	repo.Close(context.Background())

	view, err = repo.Get(ctx, 1001)
	require.NoError(t, err)
	assert.Nil(t, view.CreatorCode)
	assert.Nil(t, client.User.GetX(ctx, 1001).CreatorCode)
	assert.Len(t, pub.On(data.SubjectUserChanged), 3, "register, set and clear must announce state")
}

func TestSetCreatorCodeRejectsTooLongValue(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@concordia.ca"))

	err := repo.SetCreatorCode(ctx, 1001, strings.Repeat("A", repository.CreatorCodeMaxLen+1))
	require.Error(t, err)

	view, getErr := repo.Get(ctx, 1001)
	require.NoError(t, getErr)
	assert.Nil(t, view.CreatorCode)
}

func TestApplyBillingLifecycleIsMonotonicAndProtectsAdminGrants(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@example.com"))

	started := time.Now().UTC().Truncate(time.Second)
	expires := started.AddDate(0, 1, 0)
	applied, err := repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 1001, EventID: "evt-start", Action: billingrpc.ActionActivate,
		OccurredAt: started, ExpiresAt: &expires, RecurringReference: "tbx-r-current",
	})
	require.NoError(t, err)
	assert.True(t, applied)

	view, err := repo.Get(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "paid", view.Status)
	assert.Equal(t, "tebex", view.SubscriptionSource)
	assert.Equal(t, "tbx-r-current", *view.SubscriptionRef)
	assert.Equal(t, expires, *view.SubscriptionExpiresAt)

	applied, err = repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 1001, EventID: "evt-old", Action: billingrpc.ActionRevoke,
		OccurredAt: started.Add(-time.Minute), RecurringReference: "tbx-r-current",
	})
	require.NoError(t, err)
	assert.False(t, applied)

	applied, err = repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 1001, EventID: "evt-other", Action: billingrpc.ActionRevoke,
		OccurredAt: started.Add(time.Minute), RecurringReference: "tbx-r-old",
	})
	require.NoError(t, err)
	assert.False(t, applied)

	grantExpiry := time.Now().AddDate(0, 1, 0)
	require.NoError(t, repo.SetAdminStatus(ctx, 1001, user.StatusPaid, &grantExpiry))
	applied, err = repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 1001, EventID: "evt-refund", Action: billingrpc.ActionRevoke,
		OccurredAt: time.Now().Add(time.Minute), RecurringReference: "tbx-r-current",
	})
	require.NoError(t, err)
	assert.False(t, applied, "Tebex must not revoke an operator grant")
}

func TestApplyBillingCountsGiftForGifterIdempotently(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 4001, "Gifter", "Gifter", "gifter@example.com"))
	require.NoError(t, repo.Register(ctx, 4002, "Recipient", "Recipient", "recipient@example.com"))

	when := time.Now().UTC().Truncate(time.Second)
	expires := when.AddDate(0, 1, 0)
	gift := billingrpc.ApplyRequest{
		UserID: 4002, EventID: "evt-gift-1", Action: billingrpc.ActionActivate,
		OccurredAt: when, ExpiresAt: &expires, GifterID: 4001,
	}

	applied, err := repo.ApplyBilling(ctx, gift)
	require.NoError(t, err)
	assert.True(t, applied)

	rv, err := repo.Get(ctx, 4002)
	require.NoError(t, err)
	assert.Equal(t, "paid", rv.Status, "recipient still gets premium")

	g := client.User.GetX(ctx, 4001)
	assert.Equal(t, uint32(1), g.GiftsSent, "gifter counter bumped once")

	_, err = repo.ApplyBilling(ctx, gift)
	require.NoError(t, err)
	g = client.User.GetX(ctx, 4001)
	assert.Equal(t, uint32(1), g.GiftsSent, "webhook retry must not double-count")

	_, err = repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 4001, EventID: "evt-self", Action: billingrpc.ActionActivate,
		OccurredAt: when.Add(time.Hour), ExpiresAt: &expires, GifterID: 0,
	})
	require.NoError(t, err)
	g = client.User.GetX(ctx, 4001)
	assert.Equal(t, uint32(1), g.GiftsSent, "self-purchase must not bump the counter")
}

func TestApplyBillingCancellationAndEnd(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 2002, "Bagel", "Bagel", "bagel@example.com"))

	started := time.Now().Add(-time.Hour)
	_, err := repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 2002, EventID: "evt-start", Action: billingrpc.ActionActivate,
		OccurredAt: started, RecurringReference: "tbx-r-2",
	})
	require.NoError(t, err)

	_, err = repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 2002, EventID: "evt-cancel", Action: billingrpc.ActionCancelRequested,
		OccurredAt: started.Add(time.Minute), RecurringReference: "tbx-r-2",
	})
	require.NoError(t, err)
	view, _ := repo.Get(ctx, 2002)
	assert.True(t, view.SubscriptionCancelPending)
	assert.Equal(t, "paid", view.Status)

	_, err = repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 2002, EventID: "evt-ended", Action: billingrpc.ActionRevoke,
		OccurredAt: started.Add(2 * time.Minute), RecurringReference: "tbx-r-2",
	})
	require.NoError(t, err)
	view, _ = repo.Get(ctx, 2002)
	assert.Equal(t, "free", view.Status)
	assert.Empty(t, view.SubscriptionSource)
}

func TestApplyBillingBackstopClampsUnboundedPaidGrant(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 5005, "Chargeback", "Chargeback", "chargeback@example.com"))

	started := time.Now().UTC().Truncate(time.Second)
	expires := started.AddDate(0, 1, 0)
	_, err := repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 5005, EventID: "evt-start", Action: billingrpc.ActionActivate,
		OccurredAt: started, ExpiresAt: &expires,
	})
	require.NoError(t, err)

	_, err = repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 5005, EventID: "evt-dispute-open", Action: billingrpc.ActionRevoke,
		OccurredAt: started.Add(time.Hour),
	})
	require.NoError(t, err)
	view, err := repo.Get(ctx, 5005)
	require.NoError(t, err)
	assert.Equal(t, "free", view.Status)
	assert.Nil(t, view.SubscriptionExpiresAt)

	won := started.Add(3 * 24 * time.Hour)
	applied, err := repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 5005, EventID: "evt-dispute-won", Action: billingrpc.ActionCancelAborted,
		OccurredAt: won,
	})
	require.NoError(t, err)
	assert.True(t, applied)

	view, err = repo.Get(ctx, 5005)
	require.NoError(t, err)
	assert.Equal(t, "paid", view.Status)
	require.NotNil(t, view.SubscriptionExpiresAt,
		"a nil-expiry paid update against a NULL stored expiry must not mint an unbounded grant")
	assert.Equal(t, won.AddDate(0, 1, 0), *view.SubscriptionExpiresAt)
}

func TestExpireSubscriptionsHonorsTebexGrace(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	require.NoError(t, repo.Register(ctx, 3003, "AdminGrant", "AdminGrant", "admin@example.com"))
	adminExpiry := now.Add(-time.Minute)
	require.NoError(t, repo.SetAdminStatus(ctx, 3003, user.StatusPaid, ptrTime(time.Now().Add(time.Hour))))
	require.NoError(t, client.User.UpdateOneID(3003).SetSubscriptionExpiresAt(adminExpiry).Exec(ctx))

	require.NoError(t, repo.Register(ctx, 4004, "TebexGrace", "TebexGrace", "tebex@example.com"))
	tebexExpiry := now.Add(-time.Hour)
	_, err := repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 4004, EventID: "evt-tebex", Action: billingrpc.ActionActivate,
		OccurredAt: now.Add(-48 * time.Hour), ExpiresAt: &tebexExpiry,
	})
	require.NoError(t, err)

	count, err := repo.ExpireSubscriptions(ctx, now, 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	admin, _ := repo.Get(ctx, 3003)
	tebex, _ := repo.Get(ctx, 4004)
	assert.Equal(t, "free", admin.Status)
	assert.Equal(t, "paid", tebex.Status)

	count, err = repo.ExpireSubscriptions(ctx, now.Add(24*time.Hour), 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	tebex, _ = repo.Get(ctx, 4004)
	assert.Equal(t, "free", tebex.Status)
}

func ptrTime(value time.Time) *time.Time { return &value }

func TestDeleteCascadesAndPublishes(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@concordia.ca"))
	require.NoError(t, repo.UpsertToken(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch, []byte("tok"), nil, nil))

	require.NoError(t, repo.Delete(ctx, 1001))

	assert.Equal(t, 0, client.User.Query().CountX(ctx))
	assert.Equal(t, 0, client.Tokens.Query().CountX(ctx), "tokens must cascade with the user")

	assert.Len(t, pub.On(data.SubjectUserDeleted), 1)
}

func TestDelegateCanOptOutOfConsumedDelegation(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.CreateDelegation(ctx, "share-token", 1001, "owner", []string{"commands"}, nil))

	_, err := repo.ConsumeDelegation(ctx, "share-token", 2002, "delegate")
	require.NoError(t, err)

	access, err := repo.ListAccessByDelegate(ctx, 2002)
	require.NoError(t, err)
	require.Len(t, access, 1)

	require.Error(t, repo.OptOutDelegation(ctx, 9999, 2002), "delegate cannot drop a grant for another owner")
	require.NoError(t, repo.OptOutDelegation(ctx, 1001, 2002))

	access, err = repo.ListAccessByDelegate(ctx, 2002)
	require.NoError(t, err)
	assert.Empty(t, access)
}

func TestDelegateOptOutIgnoresPendingLinks(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)

	require.NoError(t, repo.CreateDelegation(ctx, "pending-token", 1001, "owner", []string{"commands"}, &expires))

	require.Error(t, repo.OptOutDelegation(ctx, 1001, 2002), "pending invite should remain owner-managed")
}

func TestConsumeReclaimsInsteadOfDuplicatingForSameBoard(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.CreateDelegation(ctx, "link-one", 1001, "owner", []string{"commands"}, nil))
	require.NoError(t, repo.CreateDelegation(ctx, "link-two", 1001, "owner", []string{"timers"}, nil))

	first, err := repo.ConsumeDelegation(ctx, "link-one", 2002, "delegate")
	require.NoError(t, err)

	second, err := repo.ConsumeDelegation(ctx, "link-two", 2002, "delegate")
	require.NoError(t, err)
	assert.Equal(t, first.Sections, second.Sections, "reclaim returns the existing grant, not the new link")

	access, err := repo.ListAccessByDelegate(ctx, 2002)
	require.NoError(t, err)
	require.Len(t, access, 1, "no duplicate grant for the same board")

	_, err = repo.GetDelegation(ctx, "link-two")
	require.Error(t, err, "the redundant link is discarded from the db")
}

func TestConsumeStillBindsDistinctBoards(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.CreateDelegation(ctx, "board-a", 1001, "alpha", []string{"commands"}, nil))
	require.NoError(t, repo.CreateDelegation(ctx, "board-b", 3003, "beta", []string{"timers"}, nil))

	_, err := repo.ConsumeDelegation(ctx, "board-a", 2002, "delegate")
	require.NoError(t, err)
	_, err = repo.ConsumeDelegation(ctx, "board-b", 2002, "delegate")
	require.NoError(t, err)

	access, err := repo.ListAccessByDelegate(ctx, 2002)
	require.NoError(t, err)
	assert.Len(t, access, 2, "different owners are separate grants, not a reclaim")
}

func TestCannotDelegateToYourself(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.CreateDelegation(ctx, "self-token", 1001, "owner", []string{"commands"}, nil))

	_, err := repo.ConsumeDelegation(ctx, "self-token", 1001, "owner")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot delegate to yourself")
}

func TestRevokeAndMutationReturnsConsumedDelegateID(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.CreateDelegation(ctx, "token-a", 1001, "owner", []string{"commands"}, nil))
	require.NoError(t, repo.CreateDelegation(ctx, "token-b", 1001, "owner", []string{"billing"}, nil))

	delID, err := repo.RevokeDelegation(ctx, "token-b", 1001)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), delID, "unconsumed token has no delegateID")

	_, err = repo.ConsumeDelegation(ctx, "token-a", 2002, "delegate")
	require.NoError(t, err)

	delID, err = repo.UpdateDelegationSections(ctx, "token-a", 1001, []string{"commands", "modules"})
	require.NoError(t, err)
	assert.Equal(t, uint64(2002), delID)

	delID, err = repo.RevokeDelegation(ctx, "token-a", 1001)
	require.NoError(t, err)
	assert.Equal(t, uint64(2002), delID)
}

func TestDeleteDelegationsByOwnerReturnsAllConsumedDelegateIDs(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.CreateDelegation(ctx, "tok-1", 1001, "owner", []string{"commands"}, nil))
	require.NoError(t, repo.CreateDelegation(ctx, "tok-2", 1001, "owner", []string{"billing"}, nil))
	require.NoError(t, repo.CreateDelegation(ctx, "tok-pending", 1001, "owner", []string{"modules"}, nil))

	_, err := repo.ConsumeDelegation(ctx, "tok-1", 2002, "delegate1")
	require.NoError(t, err)
	_, err = repo.ConsumeDelegation(ctx, "tok-2", 3003, "delegate2")
	require.NoError(t, err)

	delegateIDs, err := repo.DeleteDelegationsByOwner(ctx, 1001)
	require.NoError(t, err)
	assert.ElementsMatch(t, []uint64{2002, 3003}, delegateIDs)

	grants, err := repo.ListDelegationsByOwner(ctx, 1001)
	require.NoError(t, err)
	assert.Empty(t, grants)
}

func TestIDByUsernameResolvesLogins(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 4001, "streamer", "streamer", "streamer@test.com"))

	for _, tc := range []struct {
		name  string
		login string
	}{
		{"resolves a known login", "streamer"},
		{"TestIDByUsernameNormalizesInput", "  STREAMER "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := repo.IDByUsername(ctx, tc.login)

			require.NoError(t, err)
			assert.Equal(t, uint64(4001), id)
		})
	}

	t.Run("TestIDByUsernameUnknownAndInvalid", func(t *testing.T) {
		for _, login := range []string{"nobody", "not a login!", ""} {
			id, err := repo.IDByUsername(ctx, login)

			assert.Error(t, err, "an unresolvable login must not fall back to some other channel: %q", login)
			assert.Zero(t, id, login)
		}
	})
}

func TestIDByUsernameTakesFreshestRowOnCollision(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 4003, "shared", "shared", "stale@test.com"))
	require.NoError(t, repo.Register(ctx, 4004, "shared", "shared", "fresh@test.com"))

	now := time.Now()
	require.NoError(t, client.User.UpdateOneID(4003).SetUpdatedAt(now.Add(-48*time.Hour)).Exec(ctx))
	require.NoError(t, client.User.UpdateOneID(4004).SetUpdatedAt(now).Exec(ctx))

	id, err := repo.IDByUsername(ctx, "shared")
	require.NoError(t, err)
	assert.Equal(t, uint64(4004), id)
}

func TestApplyBillingPaymentFailedLifecycle(t *testing.T) {
	steps := []struct {
		name       string
		action     billingrpc.Action
		reference  string
		wantFailed bool
		wantStatus string
	}{
		{"failure flags paid user", billingrpc.ActionPaymentFailed, "tbx-r-7", true, "paid"},
		{"other agreement failure ignored", billingrpc.ActionPaymentFailed, "tbx-r-other", true, "paid"},
		{"cancel request keeps flag", billingrpc.ActionCancelRequested, "tbx-r-7", true, "paid"},
		{"renewal clears flag", billingrpc.ActionActivate, "tbx-r-7", false, "paid"},
		{"failure again", billingrpc.ActionPaymentFailed, "tbx-r-7", true, "paid"},
		{"end clears flag", billingrpc.ActionRevoke, "tbx-r-7", false, "free"},
		{"failure on free user ignored", billingrpc.ActionPaymentFailed, "tbx-r-7", false, "free"},
	}
	_, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 7007, "Bagel", "Bagel", "bagel@example.com"))

	at := time.Now().Add(-time.Hour)
	_, err := repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 7007, EventID: "evt-start", Action: billingrpc.ActionActivate, OccurredAt: at, RecurringReference: "tbx-r-7",
	})
	require.NoError(t, err)

	for i, step := range steps {
		at = at.Add(time.Minute)
		_, err := repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
			UserID: 7007, EventID: fmt.Sprintf("evt-%d", i), Action: step.action, OccurredAt: at, RecurringReference: step.reference,
		})
		require.NoError(t, err, step.name)
		view, err := repo.Get(ctx, 7007)
		require.NoError(t, err, step.name)
		assert.Equal(t, step.wantFailed, view.SubscriptionPaymentFailed, step.name)
		assert.Equal(t, step.wantStatus, view.Status, step.name)
	}
}

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
