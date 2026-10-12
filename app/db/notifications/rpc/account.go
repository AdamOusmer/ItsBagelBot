package rpc

import (
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"strconv"
	"time"
)

func notificationAccount(ctx context.Context, nc *nats.Conn, subject, userID, username string) (uint64, string, time.Time, error) {
	reply, err := bus.RequestJSONTimeout[usersrpc.AdminReply](ctx, nc, subject, usersrpc.AdminRequest{UserID: userID, Username: username}, readBudget)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	if reply.User == nil || reply.User.CreatedAt.IsZero() {
		return 0, "", time.Time{}, fmt.Errorf("notification recipient incarnation unavailable")
	}
	return reply.User.ID, strconv.FormatInt(reply.User.CreatedAt.UnixMicro(), 10), reply.User.CreatedAt, nil
}
