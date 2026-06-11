package repository_test

import (
	"bytes"
	"context"
	"testing"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/enttest"
	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/ent/user"
	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/internal/domain/event/data"
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
