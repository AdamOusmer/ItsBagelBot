// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/projector/hydration"
	"ItsBagelBot/internal/domain/invalidate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	rpcprojection "ItsBagelBot/internal/domain/rpc/projection"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/monitor"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

type Dashboard struct {
	nc                    *nats.Conn
	store                 *projection.Store
	cacheInvalidatePrefix string
	hydrator              *hydration.Hydrator
	log                   *zap.Logger
	writeGate             chan struct{}
	commandsRead          sectionRead[[]projection.CommandView]
	modulesRead           sectionRead[[]projection.ModuleView]
}

func SubscribeDashboard(
	nc *nats.Conn,
	store *projection.Store,
	prefix string,
	commandsTopic string,
	modulesTopic string,
	cacheInvalidatePrefix string,
	hydrator *hydration.Hydrator,
	queueGroup string,
	app *newrelic.Application,
	log *zap.Logger,
) error {
	writeConcurrency := env.GetInt("PROJECTOR_WRITE_CONCURRENCY", 8)
	if writeConcurrency <= 0 {
		writeConcurrency = 8
	}

	d := &Dashboard{
		nc:                    nc,
		store:                 store,
		cacheInvalidatePrefix: cacheInvalidatePrefix,
		hydrator:              hydrator,
		log:                   log,
		writeGate:             make(chan struct{}, writeConcurrency),
	}
	commandsFlight := &sectionFlight[rpcprojection.CommandsReply, []projection.CommandView]{
		nc:    nc,
		topic: commandsTopic,
		pick:  func(r rpcprojection.CommandsReply) ([]projection.CommandView, string) { return r.Commands, r.Error },
	}
	d.commandsRead = sectionRead[[]projection.CommandView]{
		readFailure: "projector command valkey read failed",
		cached:      store.GetCommands,
		fill:        commandsFlight.load,
		seed:        hydration.CommandsSeed,
	}
	modulesFlight := &sectionFlight[rpcprojection.ModulesReply, []projection.ModuleView]{
		nc:    nc,
		topic: modulesTopic,
		pick:  func(r rpcprojection.ModulesReply) ([]projection.ModuleView, string) { return r.Modules, r.Error },
	}
	d.modulesRead = sectionRead[[]projection.ModuleView]{
		readFailure: "projector module valkey read failed",
		cached:      d.cachedModuleList,
		fill:        modulesFlight.load,
		seed:        hydration.ModulesSeed,
	}

	if err := bus.QueueSubscribeJSON[projectorrpc.DashboardRequest, rpcprojection.CommandsReply](nc, prefix+".commands.get", queueGroup, 2*time.Second, app, log, d.handleCommandsGet); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[projectorrpc.DashboardRequest, rpcprojection.CommandsReply](nc, prefix+".commands.replace", queueGroup, 2*time.Second, app, log, d.handleCommandsReplace); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[projectorrpc.DashboardRequest, rpcprojection.ModulesReply](nc, prefix+".modules.get", queueGroup, 2*time.Second, app, log, d.handleModulesGet); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[projectorrpc.DashboardRequest, rpcprojection.ModulesReply](nc, prefix+".modules.replace", queueGroup, 2*time.Second, app, log, d.handleModulesReplace)
}

func unreachable(message string) domainrpc.Refusal {
	return domainrpc.Refused(domainrpc.CodeUnavailable, message)
}

type sectionRead[T any] struct {
	readFailure string
	cached      func(ctx context.Context, userID uint64) (T, bool, error)
	fill        func(ctx context.Context, userID uint64, req projectorrpc.DashboardRequest) (T, string)
	seed        func(T) hydration.Seed
}

type sectionFill[T any] struct {
	items T
	err   string
}

type sectionFlight[Rep, T any] struct {
	group singleflight.Group
	nc    *nats.Conn
	topic string
	pick  func(Rep) (T, string)
}

func (f *sectionFlight[Rep, T]) load(ctx context.Context, userID uint64, req projectorrpc.DashboardRequest) (T, string) {
	done := f.group.DoChan(strconv.FormatUint(userID, 10), func() (any, error) {
		return f.fetch(ctx, req), nil
	})
	select {
	case res := <-done:
		fill := res.Val.(sectionFill[T])
		return fill.items, fill.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err().Error()
	}
}

func (f *sectionFlight[Rep, T]) fetch(ctx context.Context, req projectorrpc.DashboardRequest) sectionFill[T] {
	source, err := bus.RequestJSONTimeout[Rep](ctx, f.nc, f.topic, req, 1500*time.Millisecond)
	if err != nil {
		return sectionFill[T]{err: err.Error()}
	}
	items, failure := f.pick(source)
	if failure != "" {
		return sectionFill[T]{err: failure}
	}
	return sectionFill[T]{items: items}
}

type sectionResult[T any] struct {
	userID  string
	items   T
	refusal domainrpc.Refusal
}

func readSection[T any](ctx context.Context, d *Dashboard, req projectorrpc.DashboardRequest, s sectionRead[T]) sectionResult[T] {
	log := monitor.TxnLogger(ctx, d.log)
	userID, err := bus.NonZeroUserID(req.UserID)
	if err != nil {
		return sectionResult[T]{refusal: bus.Classify(err)}
	}

	items, projected, err := s.cached(ctx, userID)
	if err == nil && projected {
		d.hydrator.EnsureAsync(userID, hydration.Seed{})
		return sectionResult[T]{userID: req.UserID, items: items}
	}
	if err != nil && log != nil {
		log.Warn(s.readFailure, zap.String("user_id", req.UserID), zap.Error(err))
	}

	filled, fillErr := s.fill(ctx, userID, req)
	if fillErr != "" {
		d.hydrator.EnsureAsync(userID, hydration.Seed{})
		return sectionResult[T]{userID: req.UserID, refusal: unreachable(fillErr)}
	}
	d.hydrator.EnsureAsync(userID, s.seed(filled))
	return sectionResult[T]{userID: req.UserID, items: filled}
}

func (d *Dashboard) handleCommandsGet(ctx context.Context, req projectorrpc.DashboardRequest) rpcprojection.CommandsReply {
	r := readSection(ctx, d, req, d.commandsRead)
	return rpcprojection.CommandsReply{UserID: r.userID, Commands: r.items, Refusal: r.refusal}
}

func (d *Dashboard) handleCommandsReplace(ctx context.Context, req projectorrpc.DashboardRequest) rpcprojection.CommandsReply {
	userID, err := bus.NonZeroUserID(req.UserID)
	if err != nil {
		return rpcprojection.CommandsReply{Refusal: bus.Classify(err)}
	}
	d.writeCommandsAsync(userID, req.Commands)
	return rpcprojection.CommandsReply{UserID: req.UserID, Commands: req.Commands}
}

func (d *Dashboard) handleModulesGet(ctx context.Context, req projectorrpc.DashboardRequest) rpcprojection.ModulesReply {
	r := readSection(ctx, d, req, d.modulesRead)
	return rpcprojection.ModulesReply{UserID: r.userID, Modules: r.items, Refusal: r.refusal}
}

func (d *Dashboard) cachedModuleList(ctx context.Context, userID uint64) ([]projection.ModuleView, bool, error) {
	byName, projected, err := d.store.GetModules(ctx, userID)
	if err != nil || !projected {
		return nil, projected, err
	}
	return projection.ModuleList(byName), true, nil
}

func (d *Dashboard) handleModulesReplace(ctx context.Context, req projectorrpc.DashboardRequest) rpcprojection.ModulesReply {
	userID, err := bus.NonZeroUserID(req.UserID)
	if err != nil {
		return rpcprojection.ModulesReply{Refusal: bus.Classify(err)}
	}
	if err := d.store.SetModules(ctx, userID, req.Modules); err != nil {
		return rpcprojection.ModulesReply{Refusal: bus.Classify(err)}
	}
	// Revision and account fences may reject stale rows. Echo the state that
	// actually landed, and only invalidate after the primary write completes.
	byName, _, err := d.store.GetModulesPrimary(ctx, userID)
	if err != nil {
		return rpcprojection.ModulesReply{Refusal: bus.Classify(err)}
	}
	if d.cacheInvalidatePrefix != "" {
		_ = invalidate.PublishKeys(d.nc, d.cacheInvalidatePrefix, "modules", req.UserID)
	}
	return rpcprojection.ModulesReply{UserID: req.UserID, Modules: projection.ModuleList(byName)}
}

func (d *Dashboard) writeCommandsAsync(userID uint64, commands []projection.CommandView) {
	commands = append([]projection.CommandView(nil), commands...)
	d.writeAsync(userID, projectionWrite{
		section: "commands",
		failure: "projector command valkey write failed",
		keys:    commandKeys(commands),
		set:     func(ctx context.Context) error { return d.store.SetCommands(ctx, userID, commands) },
	})
}

func commandKeys(commands []projection.CommandView) []string {
	keys := make([]string, 0, len(commands))
	for _, c := range commands {
		keys = append(keys, c.Name)
		keys = append(keys, c.Aliases...)
	}
	return keys
}

type projectionWrite struct {
	section string
	failure string
	keys    []string
	set     func(ctx context.Context) error
}

func (d *Dashboard) writeAsync(userID uint64, w projectionWrite) {
	go func() {
		d.writeGate <- struct{}{}
		defer func() { <-d.writeGate }()

		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()

		if err := w.set(ctx); err != nil && d.log != nil {
			d.log.Warn(w.failure, zap.Uint64("user_id", userID), zap.Error(err))
		}
		if d.cacheInvalidatePrefix != "" {
			_ = invalidate.PublishKeys(d.nc, d.cacheInvalidatePrefix, w.section, strconv.FormatUint(userID, 10), w.keys...)
		}
	}()
}
