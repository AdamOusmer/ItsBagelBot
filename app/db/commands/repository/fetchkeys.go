// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/fetchkey"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"

	"go.uber.org/zap"
)

func fetchAAD(userID uint64, label string) []byte {
	aad := make([]byte, 0, 20+len("|fetch_key|")+len(label))
	aad = strconv.AppendUint(aad, userID, 10)
	aad = append(aad, "|fetch_key|"...)
	aad = append(aad, label...)
	return aad
}

func (r *Fetches) ListKeys(ctx context.Context, userID uint64) ([]KeyView, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.FetchKey, error) {
		return r.client.FetchKey.Query().
			Where(fetchkey.UserIDEQ(userID)).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	out := make([]KeyView, len(rows))
	for i, row := range rows {
		out[i] = KeyView{Label: row.Label, Last4: row.Last4, CreatedAt: row.CreatedAt}
	}
	return out, nil
}

type KeyEntry struct {
	Label string
	Value string
}

// The plaintext must never reach the database, logs or any later reply.
func (r *Fetches) SetKey(ctx context.Context, userID uint64, key KeyEntry) (string, error) {
	label, value := key.Label, key.Value
	if r.packer == nil {
		return "", ErrCustodyUnavailable
	}
	if err := validate.UserID(userID); err != nil {
		return "", err
	}
	if err := validate.KeyLabel(label); err != nil {
		return "", err
	}
	if err := validate.KeyValue(value); err != nil {
		return "", err
	}

	sealed, err := r.packer.Pack([]byte(value), fetchAAD(userID, label))
	if err != nil {
		return "", err
	}
	suffix := value
	if len(value) > 4 {
		suffix = value[len(value)-4:]
	}

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.FetchKey.Create().
			SetUserID(userID).
			SetLabel(label).
			SetKeyEnc(sealed.Ciphertext).
			SetLast4(suffix).
			OnConflictColumns(fetchkey.FieldUserID, fetchkey.FieldLabel).
			UpdateNewValues().
			Exec(ctx)
	}); err != nil {
		return "", err
	}

	r.log.Info("fetch key sealed",
		zap.Uint64("user_id", userID),
		zap.String("label", label),
	)

	return suffix, nil
}

func (r *Fetches) DeleteKey(ctx context.Context, userID uint64, label string) error {
	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.KeyLabel(label); err != nil {
		return err
	}

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.FetchKey.Delete().
			Where(
				fetchkey.UserIDEQ(userID),
				fetchkey.LabelEQ(label),
			).
			Exec(ctx)
		return err
	}); err != nil {
		return err
	}

	r.log.Info("fetch key deleted",
		zap.Uint64("user_id", userID),
		zap.String("label", label),
	)
	return nil
}

func (r *Fetches) Key(ctx context.Context, userID uint64, label string) (string, error) {
	if r.packer == nil {
		return "", ErrCustodyUnavailable
	}
	if err := validate.UserID(userID); err != nil {
		return "", err
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.FetchKey, error) {
		return r.client.FetchKey.Query().
			Where(
				fetchkey.UserIDEQ(userID),
				fetchkey.LabelEQ(label),
			).
			Only(ctx)
	})
	if ent.IsNotFound(err) {
		return "", ErrNoFetchKey
	}
	if err != nil {
		return "", err
	}

	plain, err := r.packer.Unpack(domaincrypto.SecureEnvelope{
		Ciphertext:   row.KeyEnc,
		AttachedData: fetchAAD(userID, label),
	})
	if err != nil {
		// Log identifiers only, never ciphertext or plaintext.
		r.log.Error("failed to unseal fetch key",
			zap.Uint64("user_id", userID),
			zap.String("label", label),
		)
		return "", errors.New("failed to unseal fetch key")
	}
	return string(plain), nil
}
