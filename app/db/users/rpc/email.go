// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

// SubscribeEmail exposes the decrypted contact email to the internal callers
// that send transactional mail. The subject is export/import-scoped at the
// NATS account level, so this stays as private as the token RPCs. A user with
// no captured email replies with an empty Email, not an error: the caller
// treats that as "skip the email channel".
func SubscribeEmail(w Wiring, subject string) error {
	repo := w.Repo
	return bus.ServeForUser[usersrpc.EmailGetRequest, usersrpc.EmailGetReply](w.Within(emailBudget), subject,
		func(ctx context.Context, _ usersrpc.EmailGetRequest, id uint64) (usersrpc.EmailGetReply, error) {
			email, err := repo.ContactEmail(ctx, id)
			if errors.Is(err, repository.ErrNoContactEmail) {
				return usersrpc.EmailGetReply{}, nil
			}
			// A surfaced error never carries the address; it is a lookup or
			// unseal failure and safe for the caller to see.
			return usersrpc.EmailGetReply{Email: email}, err
		},
	)
}
