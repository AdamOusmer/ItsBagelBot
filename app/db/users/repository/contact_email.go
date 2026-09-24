// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

var ErrNoContactEmail = errors.New("no contact email on record")

// Plaintext must never reach the database or logs.
func (r *Users) SetContactEmail(ctx context.Context, id uint64, email string) error {

	if err := validate.UserID(id); err != nil {
		return err
	}
	if err := validate.Email(email); err != nil {
		return err
	}

	sealed, err := r.packer.Pack([]byte(email), contactEmailAAD(id))
	if err != nil {
		return err
	}

	return db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.User.UpdateOneID(id).
			SetEmailEnc(sealed.Ciphertext).
			Exec(ctx)
	})
}

func (r *Users) ContactEmail(ctx context.Context, id uint64) (string, error) {

	if err := validate.UserID(id); err != nil {
		return "", err
	}

	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().
			Where(user.IDEQ(id)).
			Select(user.FieldEmailEnc).
			Only(ctx)
	})
	if err != nil {
		return "", err
	}
	if len(row.EmailEnc) == 0 {
		return "", ErrNoContactEmail
	}

	plain, err := r.packer.Unpack(domaincrypto.SecureEnvelope{
		Ciphertext:   row.EmailEnc,
		AttachedData: contactEmailAAD(id),
	})
	if err != nil {
		return "", err
	}

	return string(plain), nil
}

func contactEmailAAD(userID uint64) []byte {

	aad := make([]byte, 0, 20+len("|contact_email"))

	aad = strconv.AppendUint(aad, userID, 10)
	aad = append(aad, "|contact_email"...)

	return aad
}
