package repository

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/user"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

// ErrNoContactEmail marks a user row that exists but has no captured contact
// email yet (the user has not logged in since email capture shipped).
var ErrNoContactEmail = errors.New("no contact email on record")

// SetContactEmail seals the real Twitch account email with the service AEAD
// keyset and stores it on the user row. The plaintext never touches the
// database or logs; the AAD binds the ciphertext to this user id so an
// envelope copied onto another row fails to open.
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

// ContactEmail opens and returns the stored contact email for the user.
// Returns ErrNoContactEmail when the user never logged in post-capture.
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
