package rpc

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"ItsBagelBot/pkg/bus"
)

// PersonalityWiring bundles what SubscribePersonality needs, mirroring the
// quotes wiring.
type PersonalityWiring struct {
	NC         *nats.Conn
	Repo       *repository.Personality
	Prefix     string // subject prefix, e.g. "bagel.rpc.modules.personality"
	QueueGroup string
	App        *newrelic.Application
	Log        *zap.Logger
}

// SubscribePersonality answers the personality verbs under w.Prefix. There is
// one: feed, the permanent global feed-counter bump. It rides the same
// MODULES_RPC account export as the quote verbs, so no ACL change is needed
// for sesame to call it.
func SubscribePersonality(w PersonalityWiring) error {
	handler := func(ctx context.Context, _ modulesrpc.FeedBumpRequest) modulesrpc.FeedBumpReply {
		total, err := w.Repo.FeedBump(ctx)
		if err != nil {
			return modulesrpc.FeedBumpReply{Error: err.Error()}
		}
		return modulesrpc.FeedBumpReply{Total: total}
	}
	subject := w.Prefix + ".feed"
	return bus.QueueSubscribeJSON[modulesrpc.FeedBumpRequest, modulesrpc.FeedBumpReply](w.NC, subject, w.QueueGroup, 2*time.Second, w.App, w.Log, handler)
}
