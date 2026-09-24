// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"
	"sync"
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
)

type Dashboard struct {
	nc                    *nats.Conn
	store                 *projection.Store
	commandsTopic         string
	modulesTopic          string
	cacheInvalidatePrefix string
	hydrator              *hydration.Hydrator
	log                   *zap.Logger
	writeGate             chan struct{}
	mu                    sync.Mutex
	commandMisses         map[uint64]*commandInFlight
	moduleMisses          map[uint64]*moduleInFlight
	commandsRead          sectionRead[[]projection.CommandView]
	modulesRead           sectionRead[[]projection.ModuleView]
}

type commandFill struct {
	commands []projection.CommandView
	err      string
}

type moduleFill struct {
	modules []projection.ModuleView
	err     string
}

type commandInFlight struct {
	done chan struct{}
	fill commandFill
}

type moduleInFlight struct {
	done chan struct{}
	fill moduleFill
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
		commandsTopic:         commandsTopic,
		modulesTopic:          modulesTopic,
		cacheInvalidatePrefix: cacheInvalidatePrefix,
		hydrator:              hydrator,
		log:                   log,
		writeGate:             make(chan struct{}, writeConcurrency),
		commandMisses:         map[uint64]*commandInFlight{},
		moduleMisses:          map[uint64]*moduleInFlight{},
	}
	d.commandsRead = sectionRead[[]projection.CommandView]{
		readFailure: "projector command valkey read failed",
		cached:      store.GetCommands,
		fill: func(ctx context.Context, userID uint64, req projectorrpc.DashboardRequest) ([]projection.CommandView, string) {
			fill := d.loadCommands(ctx, userID, req)
			return fill.commands, fill.err
		},
		seed: hydration.CommandsSeed,
	}
	d.modulesRead = sectionRead[[]projection.ModuleView]{
		readFailure: "projector module valkey read failed",
		cached:      d.cachedModuleList,
		fill: func(ctx context.Context, userID uint64, req projectorrpc.DashboardRequest) ([]projection.ModuleView, string) {
			fill := d.loadModules(ctx, userID, req)
			return fill.modules, fill.err
		},
		seed: hydration.ModulesSeed,
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

type sectionResult[T any] struct {
	userID  string
	items   T
	refusal domainrpc.Refusal
}

func readSection[T any](ctx context.Context, d *Dashboard, req projectorrpc.DashboardRequest, s sectionRead[T]) sectionResult[T] {
	log := monitor.TxnLogger(ctx, d.log)
	userID, err := parseUserID(req.UserID)
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
	userID, err := parseUserID(req.UserID)
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
	userID, err := parseUserID(req.UserID)
	if err != nil {
		return rpcprojection.ModulesReply{Refusal: bus.Classify(err)}
	}
	d.writeModulesAsync(userID, req.Modules)
	return rpcprojection.ModulesReply{UserID: req.UserID, Modules: req.Modules}
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

func (d *Dashboard) writeModulesAsync(userID uint64, modules []projection.ModuleView) {
	modules = append([]projection.ModuleView(nil), modules...)
	d.writeAsync(userID, projectionWrite{
		section: "modules",
		failure: "projector module valkey write failed",
		set:     func(ctx context.Context) error { return d.store.SetModules(ctx, userID, modules) },
	})
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

func (d *Dashboard) loadCommands(ctx context.Context, userID uint64, req projectorrpc.DashboardRequest) commandFill {
	inFlight, owner := d.commandFillSlot(userID)
	if !owner {
		select {
		case <-inFlight.done:
			return inFlight.fill
		case <-ctx.Done():
			return commandFill{err: ctx.Err().Error()}
		}
	}

	fill := commandFill{}
	source, err := bus.RequestJSONTimeout[rpcprojection.CommandsReply](ctx, d.nc, d.commandsTopic, req, 1500*time.Millisecond)
	switch {
	case err != nil:
		fill.err = err.Error()
	case source.Error != "":
		fill.err = source.Error
	default:
		fill.commands = source.Commands
	}

	d.finishCommandFill(userID, inFlight, fill)
	return fill
}

func (d *Dashboard) commandFillSlot(userID uint64) (*commandInFlight, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if inFlight, ok := d.commandMisses[userID]; ok {
		return inFlight, false
	}
	inFlight := &commandInFlight{done: make(chan struct{})}
	d.commandMisses[userID] = inFlight
	return inFlight, true
}

func (d *Dashboard) finishCommandFill(userID uint64, inFlight *commandInFlight, fill commandFill) {
	d.mu.Lock()
	delete(d.commandMisses, userID)
	inFlight.fill = fill
	d.mu.Unlock()

	close(inFlight.done)
}

func (d *Dashboard) loadModules(ctx context.Context, userID uint64, req projectorrpc.DashboardRequest) moduleFill {
	inFlight, owner := d.moduleFillSlot(userID)
	if !owner {
		select {
		case <-inFlight.done:
			return inFlight.fill
		case <-ctx.Done():
			return moduleFill{err: ctx.Err().Error()}
		}
	}

	fill := moduleFill{}
	source, err := bus.RequestJSONTimeout[rpcprojection.ModulesReply](ctx, d.nc, d.modulesTopic, req, 1500*time.Millisecond)
	switch {
	case err != nil:
		fill.err = err.Error()
	case source.Error != "":
		fill.err = source.Error
	default:
		fill.modules = source.Modules
	}

	d.finishModuleFill(userID, inFlight, fill)
	return fill
}

func (d *Dashboard) moduleFillSlot(userID uint64) (*moduleInFlight, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if inFlight, ok := d.moduleMisses[userID]; ok {
		return inFlight, false
	}
	inFlight := &moduleInFlight{done: make(chan struct{})}
	d.moduleMisses[userID] = inFlight
	return inFlight, true
}

func (d *Dashboard) finishModuleFill(userID uint64, inFlight *moduleInFlight, fill moduleFill) {
	d.mu.Lock()
	delete(d.moduleMisses, userID)
	inFlight.fill = fill
	d.mu.Unlock()

	close(inFlight.done)
}

func parseUserID(raw string) (uint64, error) {
	if raw == "" {
		return 0, bus.RPCReplyError{Message: "bad request"}
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, bus.RPCReplyError{Message: "invalid user_id"}
	}
	return id, nil
}
