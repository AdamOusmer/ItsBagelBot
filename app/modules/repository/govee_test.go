package repository_test

import (
	"bytes"
	"context"
	"testing"

	"ItsBagelBot/app/modules/ent"
	"ItsBagelBot/app/modules/ent/enttest"
	"ItsBagelBot/app/modules/ent/goveecredential"
	"ItsBagelBot/app/modules/repository"
	"ItsBagelBot/pkg/crypto"

	_ "github.com/mattn/go-sqlite3" // in-memory DB
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

func goveeSetup(t *testing.T) (*ent.Client, *repository.GoveeCreds) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", "file:goveecreds?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	return client, repository.NewGoveeCreds(client, newPacker(t))
}

func TestGoveeKeyRoundTrip(t *testing.T) {
	client, creds := goveeSetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetKey(ctx, 1001, "govee-secret-key"))

	got, err := creds.Key(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "govee-secret-key", got)

	// The plaintext must never sit in the column.
	row := client.GoveeCredential.Query().Where(goveecredential.UserIDEQ(1001)).OnlyX(ctx)
	assert.NotContains(t, string(row.KeyEnc), "govee-secret-key", "key must be sealed at rest")
	assert.NotEmpty(t, row.KeyEnc)
}

func TestGoveeKeyUpsertReplaces(t *testing.T) {
	client, creds := goveeSetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetKey(ctx, 1001, "first"))
	require.NoError(t, creds.SetKey(ctx, 1001, "second"))

	got, err := creds.Key(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "second", got)

	rows := client.GoveeCredential.Query().Where(goveecredential.UserIDEQ(1001)).AllX(ctx)
	require.Len(t, rows, 1, "a second set must replace, not duplicate")
}

func TestGoveeKeyStatusAndClear(t *testing.T) {
	_, creds := goveeSetup(t)
	ctx := context.Background()

	present, err := creds.HasKey(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, present)

	require.NoError(t, creds.SetKey(ctx, 1001, "k"))
	present, err = creds.HasKey(ctx, 1001)
	require.NoError(t, err)
	assert.True(t, present)

	require.NoError(t, creds.ClearKey(ctx, 1001))
	present, err = creds.HasKey(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, present)

	_, err = creds.Key(ctx, 1001)
	assert.ErrorIs(t, err, repository.ErrNoGoveeKey)
}

func TestGoveeKeyMissing(t *testing.T) {
	_, creds := goveeSetup(t)
	_, err := creds.Key(context.Background(), 4242)
	assert.ErrorIs(t, err, repository.ErrNoGoveeKey)
}

func TestGoveeKeyAADBindsToUser(t *testing.T) {
	client, creds := goveeSetup(t)
	ctx := context.Background()

	require.NoError(t, creds.SetKey(ctx, 1001, "owner-key"))

	// Copy user 1001's ciphertext onto user 2002's row: the AAD binds the
	// envelope to 1001, so opening it as 2002 must fail rather than leak.
	row := client.GoveeCredential.Query().Where(goveecredential.UserIDEQ(1001)).OnlyX(ctx)
	client.GoveeCredential.Create().SetUserID(2002).SetKeyEnc(row.KeyEnc).ExecX(ctx)

	_, err := creds.Key(ctx, 2002)
	assert.Error(t, err, "a stolen envelope must not open under another user id")
	assert.NotErrorIs(t, err, repository.ErrNoGoveeKey)
}

func TestGoveeKeyClearMissingIsNoop(t *testing.T) {
	_, creds := goveeSetup(t)
	assert.NoError(t, creds.ClearKey(context.Background(), 9999))
}
