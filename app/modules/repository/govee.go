package repository

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/app/modules/ent"
	"ItsBagelBot/app/modules/ent/goveecredential"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

// ErrNoGoveeKey marks a broadcaster with no Govee API key on file.
var ErrNoGoveeKey = errors.New("no govee key on record")

// GoveeCreds is the custody store for broadcaster Govee API keys, sealed at
// rest with the modules service's own AEAD keyset. It shares the service's ent
// client but is its own type so the general module store (write-behind toggles
// and configs) stays free of any crypto dependency: only this narrow surface
// touches plaintext keys, and it never caches or logs them.
type GoveeCreds struct {
	client *ent.Client
	packer domaincrypto.Packer
}

// NewGoveeCreds builds the credential store over the shared ent client.
func NewGoveeCreds(client *ent.Client, packer domaincrypto.Packer) *GoveeCreds {
	return &GoveeCreds{client: client, packer: packer}
}

// SetKey seals the broadcaster's Govee API key and upserts it. The plaintext
// never touches the database or logs; the AAD binds the ciphertext to this
// user id so an envelope copied onto another row fails to open.
func (g *GoveeCreds) SetKey(ctx context.Context, userID uint64, key string) error {
	if err := validate.UserID(userID); err != nil {
		return err
	}
	if key == "" {
		return errors.New("empty govee key")
	}

	sealed, err := g.packer.Pack([]byte(key), goveeAAD(userID))
	if err != nil {
		return err
	}

	return db.WithExec(ctx, func(ctx context.Context) error {
		return g.client.GoveeCredential.Create().
			SetUserID(userID).
			SetKeyEnc(sealed.Ciphertext).
			OnConflictColumns(goveecredential.FieldUserID).
			UpdateNewValues().
			Exec(ctx)
	})
}

// ClearKey removes the broadcaster's stored key. A missing row is a no-op: the
// end state (no key) is the same either way.
func (g *GoveeCreds) ClearKey(ctx context.Context, userID uint64) error {
	if err := validate.UserID(userID); err != nil {
		return err
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := g.client.GoveeCredential.Delete().
			Where(goveecredential.UserIDEQ(userID)).
			Exec(ctx)
		return err
	})
}

// HasKey reports whether the broadcaster has a key on file, without unsealing
// it — the status the dashboard shows ("key on file"), never the value.
func (g *GoveeCreds) HasKey(ctx context.Context, userID uint64) (bool, error) {
	if err := validate.UserID(userID); err != nil {
		return false, err
	}
	return db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return g.client.GoveeCredential.Query().
			Where(goveecredential.UserIDEQ(userID)).
			Exist(ctx)
	})
}

// Key unseals and returns the stored Govee API key. Returns ErrNoGoveeKey when
// the broadcaster has none. The plaintext is returned to the caller (the
// gateway) and deliberately never cached.
func (g *GoveeCreds) Key(ctx context.Context, userID uint64) (string, error) {
	if err := validate.UserID(userID); err != nil {
		return "", err
	}

	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.GoveeCredential, error) {
		return g.client.GoveeCredential.Query().
			Where(goveecredential.UserIDEQ(userID)).
			Only(ctx)
	})
	switch {
	case ent.IsNotFound(err):
		return "", ErrNoGoveeKey
	case err != nil:
		return "", err
	}

	plain, err := g.packer.Unpack(domaincrypto.SecureEnvelope{
		Ciphertext:   row.KeyEnc,
		AttachedData: goveeAAD(userID),
	})
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func goveeAAD(userID uint64) []byte {
	aad := make([]byte, 0, 20+len("|govee_key"))
	aad = strconv.AppendUint(aad, userID, 10)
	aad = append(aad, "|govee_key"...)
	return aad
}
