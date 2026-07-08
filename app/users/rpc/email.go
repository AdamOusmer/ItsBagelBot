package rpc

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/app/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

// SubscribeEmail exposes the decrypted contact email to the internal callers
// that send transactional mail. The subject is export/import-scoped at the
// NATS account level, so this stays as private as the token RPCs. A user with
// no captured email replies with an empty Email, not an error: the caller
// treats that as "skip the email channel".
func SubscribeEmail(w Wiring, subject string) error {
	nc, repo, app, log, queueGroup := w.NC, w.Repo, w.App, w.Log, w.Queue
	return bus.QueueSubscribeJSON[usersrpc.EmailGetRequest, usersrpc.EmailGetReply](
		nc, subject, queueGroup, 3*time.Second, app, log,
		func(ctx context.Context, req usersrpc.EmailGetRequest) usersrpc.EmailGetReply {
			id, err := strconv.ParseUint(req.UserID, 10, 64)
			if err != nil {
				return usersrpc.EmailGetReply{Error: "user_id must be numeric"}
			}
			email, err := repo.ContactEmail(ctx, id)
			switch {
			case errors.Is(err, repository.ErrNoContactEmail):
				return usersrpc.EmailGetReply{}
			case err != nil:
				// The error never carries the address; it is a lookup or
				// unseal failure and safe to surface.
				return usersrpc.EmailGetReply{Error: err.Error()}
			}
			return usersrpc.EmailGetReply{Email: email}
		},
	)
}
