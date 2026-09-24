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

func SubscribeEmail(w Wiring, subject string) error {
	repo := w.Repo
	return bus.ServeForUser[usersrpc.EmailGetRequest, usersrpc.EmailGetReply](w.Within(emailBudget), subject,
		func(ctx context.Context, _ usersrpc.EmailGetRequest, id uint64) (usersrpc.EmailGetReply, error) {
			user, userErr := repo.FindUser(ctx, id)
			if userErr != nil {
				return usersrpc.EmailGetReply{}, userErr
			}
			email, err := repo.ContactEmail(ctx, id)
			if errors.Is(err, repository.ErrNoContactEmail) {
				return usersrpc.EmailGetReply{}, nil
			}
			return usersrpc.EmailGetReply{Email: email, Locale: user.Locale}, err
		},
	)
}
