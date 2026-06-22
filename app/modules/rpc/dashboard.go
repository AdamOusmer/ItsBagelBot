package rpc

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/bus"
)

type dashboardRPC struct {
	repo *repository.Modules
	log  *zap.Logger
}

// SubscribeDashboard wires the modules dashboard verbs (list, upsert) under
// prefix, mirroring the commands service so the console manages modules the same
// way it manages commands.
func SubscribeDashboard(nc *nats.Conn, repo *repository.Modules, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	d := &dashboardRPC{repo: repo, log: log}

	verbs := []struct {
		verb    string
		handler func(context.Context, modulesrpc.DashboardRequest) modulesrpc.DashboardReply
	}{
		{"list", d.handleList},
		{"upsert", d.handleUpsert},
	}

	for _, v := range verbs {
		subject := prefix + "." + v.verb
		if err := bus.QueueSubscribeJSON[modulesrpc.DashboardRequest, modulesrpc.DashboardReply](nc, subject, queueGroup, 2*time.Second, app, log, v.handler); err != nil {
			return err
		}
	}
	return nil
}

func (d *dashboardRPC) parseUserID(req modulesrpc.DashboardRequest) (uint64, bool, modulesrpc.DashboardReply) {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return 0, false, modulesrpc.DashboardReply{Error: "invalid user_id"}
	}
	return id, true, modulesrpc.DashboardReply{}
}

func (d *dashboardRPC) handleList(ctx context.Context, req modulesrpc.DashboardRequest) modulesrpc.DashboardReply {
	id, ok, reply := d.parseUserID(req)
	if !ok {
		return reply
	}
	views, err := d.repo.List(ctx, id)
	if err != nil {
		return modulesrpc.DashboardReply{Error: err.Error()}
	}
	return modulesrpc.DashboardReply{Modules: views}
}

func (d *dashboardRPC) handleUpsert(ctx context.Context, req modulesrpc.DashboardRequest) modulesrpc.DashboardReply {
	id, ok, reply := d.parseUserID(req)
	if !ok {
		return reply
	}

	if err := d.repo.Set(id, req.Name, req.IsEnabled, req.Configs); err != nil {
		return modulesrpc.DashboardReply{Error: err.Error()}
	}

	return modulesrpc.DashboardReply{}
}
