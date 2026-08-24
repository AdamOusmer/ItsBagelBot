// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

// Sealed key custody for $(urlfetch) definitions: the label-addressed API-key
// rows, their AAD binding and every verb that may touch plaintext. Split from
// fetch.go so the custody surface (the only code allowed near key material)
// reads as one file, the GoveeCreds/SpotifyCreds precedent.

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/app/commands/ent"
	"ItsBagelBot/app/commands/ent/fetchkey"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"

	"go.uber.org/zap"
)

// fetchAAD binds each sealed envelope to its broadcaster AND label: "<uid>|fetch_key|<label>",
// mirroring goveeAAD. The label term stops an envelope copied onto another
// label of the same user from opening — labels are the only thing separating
// one broadcaster's keys from each other.
func fetchAAD(userID uint64, label string) []byte {
	aad := make([]byte, 0, 20+len("|fetch_key|")+len(label))
	aad = strconv.AppendUint(aad, userID, 10)
	aad = append(aad, "|fetch_key|"...)
	aad = append(aad, label...)
	return aad
}

// ListKeys returns the custody metadata (label + last4) of every stored key;
// values have no read path.
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

// KeyEntry is one label+value pair as the dashboard submits it: the label
// names the sealed row, the value is the plaintext secret that exists only
// for the duration of this call.
type KeyEntry struct {
	Label string
	Value string
}

// SetKey seals the broadcaster's API key and upserts it under the label.
// Returns the derived last4 exactly once, from the just-submitted plaintext.
// The plaintext never touches the database, logs or any reply afterwards.
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
	// The stored display suffix: the trailing 4 characters (whole value when
	// it is that short) — derived here, the one moment plaintext exists.
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

// DeleteKey removes one labeled key — always allowed. Definitions pointing at
// it keep their dangling key_label and fail closed with "no key on file" at
// fetch time until relinked; that is the documented posture, not a bug.
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

// Key unseals and returns the labeled API key, ErrNoFetchKey when none is on
// file. The plaintext serves exactly one upstream call at the caller (gossip)
// and is never cached anywhere.
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
		// AAD mismatch means corruption or tamper: terminal, logged with
		// identifiers only — never ciphertext, never plaintext.
		r.log.Error("failed to unseal fetch key",
			zap.Uint64("user_id", userID),
			zap.String("label", label),
		)
		return "", errors.New("failed to unseal fetch key")
	}
	return string(plain), nil
}
