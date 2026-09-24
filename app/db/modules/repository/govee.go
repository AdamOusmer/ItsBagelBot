// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"os"
	"strconv"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/goveecredential"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"

	"go.uber.org/zap"
)

var ErrNoGoveeKey = errors.New("no govee key on record")

type GoveeCreds struct {
	client *ent.Client
	packer domaincrypto.Packer
}

func NewGoveeCreds(client *ent.Client, packer domaincrypto.Packer) *GoveeCreds {
	return &GoveeCreds{client: client, packer: packer}
}

func NewGoveeCredsFromEnv(client *ent.Client, log *zap.Logger) *GoveeCreds {
	path := env.Get("TINK_KEYSET_PATH", "")
	if path == "" {
		log.Warn("govee key custody disabled: TINK_KEYSET_PATH not set")
		return nil
	}
	keysetJSON, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		log.Warn("govee key custody disabled: keyset not provisioned yet", zap.String("path", path))
		return nil
	}
	if err != nil {
		log.Fatal("failed to read tink keyset", zap.Error(err))
	}
	packer, err := crypto.NewCrypto(keysetJSON)
	if err != nil {
		log.Fatal("failed to initialize crypto", zap.Error(err))
	}
	return NewGoveeCreds(client, packer)
}

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

func (g *GoveeCreds) keyRow(ctx context.Context, userID uint64) (*ent.GoveeCredential, error) {
	if err := validate.UserID(userID); err != nil {
		return nil, err
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.GoveeCredential, error) {
		return g.client.GoveeCredential.Query().
			Where(goveecredential.UserIDEQ(userID)).
			Only(ctx)
	})
	if ent.IsNotFound(err) {
		return nil, ErrNoGoveeKey
	}
	return row, err
}

func (g *GoveeCreds) HasKey(ctx context.Context, userID uint64) (bool, error) {
	_, err := g.keyRow(ctx, userID)
	switch {
	case errors.Is(err, ErrNoGoveeKey):
		return false, nil
	case err != nil:
		return false, err
	default:
		return true, nil
	}
}

func (g *GoveeCreds) Key(ctx context.Context, userID uint64) (string, error) {
	row, err := g.keyRow(ctx, userID)
	if err != nil {
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
