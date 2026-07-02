package rpc

import (
	"context"
	"time"

	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

type BillingApplier struct {
	nc      *nats.Conn
	subject string
}

func NewBillingApplier(nc *nats.Conn, subject string) *BillingApplier {
	return &BillingApplier{nc: nc, subject: subject}
}

func (a *BillingApplier) Apply(ctx context.Context, req billingrpc.ApplyRequest) error {
	_, err := bus.RequestJSONTimeout[billingrpc.ApplyReply](ctx, a.nc, a.subject, req, 5*time.Second)
	return err
}
