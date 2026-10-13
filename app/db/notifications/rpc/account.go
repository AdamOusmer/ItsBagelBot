package rpc

import (
	"ItsBagelBot/app/db/notifications/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"strconv"
)

func notificationAccount(ctx context.Context, nc *nats.Conn, subject string, req usersrpc.AdminRequest) (repository.Recipient, error) {
	reply, err := bus.RequestJSONTimeout[usersrpc.AdminReply](ctx, nc, subject, req, readBudget)
	if err != nil {
		return repository.Recipient{}, err
	}
	if reply.User == nil {
		return repository.Recipient{}, fmt.Errorf("notification recipient unavailable")
	}
	if reply.User.CreatedAt.IsZero() {
		return repository.Recipient{}, fmt.Errorf("notification recipient incarnation unavailable")
	}
	return repository.Recipient{UserID: reply.User.ID, Incarnation: strconv.FormatInt(reply.User.CreatedAt.UnixMicro(), 10), CreatedAt: reply.User.CreatedAt}, nil
}
