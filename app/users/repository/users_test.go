package repository_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/enttest"
	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/ent/user"
	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/internal/domain/event/data"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/crypto"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
)

func newPacker(t *testing.T) *crypto.Crypto {
	t.Helper()

	handle, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	require.NoError(t, insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(buf)))

	packer, err := crypto.NewCrypto(buf.Bytes())
	require.NoError(t, err)

	return packer
}

func setup(t *testing.T) (*ent.Client, *bustest.Publisher, *repository.Users) {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:usersent?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	pub := bustest.NewPublisher()

	repo := repository.NewUsers(client, newPacker(t), pub)
	t.Cleanup(repo.Close)

	return client, pub, repo
}

func TestTokenRoundTrip(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "mavey@concordia.ca"))

	plaintext := []byte("oauth-token-super-secret")
	refresh := []byte("refresh-token-super-secret")

	require.NoError(t, repo.UpsertToken(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch, plaintext, refresh))

	// What landed in the database must be ciphertext, not the token.
	row := client.Tokens.Query().OnlyX(ctx)
	assert.NotEqual(t, plaintext, row.Token)
	assert.NotEqual(t, refresh, row.RefreshToken)

	access, gotRefresh, err := repo.Token(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch)
	require.NoError(t, err)
	assert.Equal(t, plaintext, access)
	assert.Equal(t, refresh, gotRefresh)
}

func TestTokenUpsertReplacesExisting(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "mavey@concordia.ca"))

	require.NoError(t, repo.UpsertToken(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch, []byte("old"), nil))
	require.NoError(t, repo.UpsertToken(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch, []byte("new"), nil))

	access, _, err := repo.Token(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), access)
}

// A ciphertext copied onto another user's row must fail decryption: the
// associated data binds every envelope to its owner.
func TestTokenCiphertextBoundToOwner(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1, "Alice", "alice@test.com"))
	require.NoError(t, repo.Register(ctx, 2, "Bob", "bob@test.com"))

	require.NoError(t, repo.UpsertToken(ctx, 1, tokens.TypeAccessToken, tokens.PlatformTwitch, []byte("alice-token"), nil))

	stolen := client.Tokens.Query().OnlyX(ctx).Token

	client.Tokens.Create().
		SetUserID(2).
		SetType(tokens.TypeAccessToken).
		SetPlatform(tokens.PlatformTwitch).
		SetToken(stolen).
		ExecX(ctx)

	_, _, err := repo.Token(ctx, 2, tokens.TypeAccessToken, tokens.PlatformTwitch)
	assert.Error(t, err, "a ciphertext moved between users must not decrypt")
}

func TestSetStatusRefreshesViewAndPublishes(t *testing.T) {
	_, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "mavey@concordia.ca"))

	view, err := repo.Get(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "free", view.Status)

	require.NoError(t, repo.SetStatus(ctx, 1001, user.StatusVip))

	view, err = repo.Get(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "vip", view.Status, "status change must be visible immediately, not after TTL")

	assert.Len(t, pub.On(data.SubjectUserChanged), 2, "register and status change must both announce state")
}

func TestSetCreatorCodeStoresTrimsClearsAndPublishes(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "mavey@concordia.ca"))

	require.NoError(t, repo.SetCreatorCode(ctx, 1001, "  MAVEY10  "))
	view, err := repo.Get(ctx, 1001)
	require.NoError(t, err)
	require.NotNil(t, view.CreatorCode)
	assert.Equal(t, "MAVEY10", *view.CreatorCode)
	require.NotNil(t, client.User.GetX(ctx, 1001).CreatorCode)
	assert.Equal(t, "MAVEY10", *client.User.GetX(ctx, 1001).CreatorCode)

	require.NoError(t, repo.SetCreatorCode(ctx, 1001, ""))
	view, err = repo.Get(ctx, 1001)
	require.NoError(t, err)
	assert.Nil(t, view.CreatorCode)
	assert.Nil(t, client.User.GetX(ctx, 1001).CreatorCode)
	assert.Len(t, pub.On(data.SubjectUserChanged), 3, "register, set and clear must announce state")
}

func TestSetCreatorCodeRejectsTooLongValue(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "mavey@concordia.ca"))

	err := repo.SetCreatorCode(ctx, 1001, strings.Repeat("A", repository.CreatorCodeMaxLen+1))
	require.Error(t, err)

	view, getErr := repo.Get(ctx, 1001)
	require.NoError(t, getErr)
	assert.Nil(t, view.CreatorCode)
}

func TestApplyBillingLifecycleIsMonotonicAndProtectsAdminGrants(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "mavey@example.com"))

	started := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
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

	// An older delivery cannot roll back current state.
	applied, err = repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 1001, EventID: "evt-old", Action: billingrpc.ActionRevoke,
		OccurredAt: started.Add(-time.Minute), RecurringReference: "tbx-r-current",
	})
	require.NoError(t, err)
	assert.False(t, applied)

	// Nor can a late event for another recurring reference revoke this one.
	applied, err = repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 1001, EventID: "evt-other", Action: billingrpc.ActionRevoke,
		OccurredAt: started.Add(time.Minute), RecurringReference: "tbx-r-old",
	})
	require.NoError(t, err)
	assert.False(t, applied)

	grantExpiry := expires.AddDate(0, 1, 0)
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
	require.NoError(t, repo.Register(ctx, 4001, "Gifter", "gifter@example.com"))
	require.NoError(t, repo.Register(ctx, 4002, "Recipient", "recipient@example.com"))

	when := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
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

	// A Tebex retry of the exact same webhook must not double-count.
	_, err = repo.ApplyBilling(ctx, gift)
	require.NoError(t, err)
	g = client.User.GetX(ctx, 4001)
	assert.Equal(t, uint32(1), g.GiftsSent, "webhook retry must not double-count")

	// A self-purchase (no gifter) must not bump the counter.
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
	require.NoError(t, repo.Register(ctx, 2002, "Bagel", "bagel@example.com"))

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

func TestExpireSubscriptionsHonorsTebexGrace(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	require.NoError(t, repo.Register(ctx, 3003, "AdminGrant", "admin@example.com"))
	adminExpiry := now.Add(-time.Minute)
	require.NoError(t, repo.SetAdminStatus(ctx, 3003, user.StatusPaid, ptrTime(time.Now().Add(time.Hour))))
	require.NoError(t, client.User.UpdateOneID(3003).SetSubscriptionExpiresAt(adminExpiry).Exec(ctx))

	require.NoError(t, repo.Register(ctx, 4004, "TebexGrace", "tebex@example.com"))
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

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "mavey@concordia.ca"))
	require.NoError(t, repo.UpsertToken(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch, []byte("tok"), nil))

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

	// Second link for a board the invitee already manages: reclaim returns the
	// grant they already hold and never mints a duplicate.
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
