package rpc

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/internal/domain/invalidate"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// SubscribeBilling exposes the narrow private write surface used by the
// transactions service after it has verified a Tebex webhook signature.
func SubscribeBilling(nc *nats.Conn, repo *repository.Users, subject, invalidationPrefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	return bus.QueueSubscribeJSON[billingrpc.ApplyRequest, billingrpc.ApplyReply](
		nc, subject, queueGroup, 5*time.Second, app, log,
		func(ctx context.Context, req billingrpc.ApplyRequest) billingrpc.ApplyReply {
			applied, err := repo.ApplyBilling(ctx, req)
			if err != nil {
				return billingrpc.ApplyReply{Error: err.Error()}
			}
			if applied {
				if err := invalidate.Publish(nc, invalidationPrefix, "status", fmt.Sprint(req.UserID)); err != nil {
					log.Warn("billing cache invalidation publish failed", zap.Error(err))
				}
			}
			return billingrpc.ApplyReply{Applied: applied}
		},
	)
}
