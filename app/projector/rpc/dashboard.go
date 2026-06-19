package rpc

import (
	"context"
	"strconv"
	"sync"
	"time"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

type Dashboard struct {
	nc            *nats.Conn
	store         *projection.Store
	commandsTopic string
	modulesTopic  string
	log           *zap.Logger
	writeGate     chan struct{}
	mu            sync.Mutex
	commandMisses map[uint64]*commandInFlight
	moduleMisses  map[uint64]*moduleInFlight
}

type dashboardRequest struct {
	UserID   string              `json:"user_id"`
	Commands []projection.CommandView `json:"commands,omitempty"`
	Modules  []projection.ModuleView  `json:"modules,omitempty"`
}

type commandsReply struct {
	UserID   string              `json:"user_id"`
	Commands []projection.CommandView `json:"commands"`
	Error    string              `json:"error,omitempty"`
}

type modulesReply struct {
	UserID  string             `json:"user_id"`
	Modules []projection.ModuleView `json:"modules"`
	Error   string             `json:"error,omitempty"`
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
	queueGroup string,
	app *newrelic.Application,
	log *zap.Logger,
) error {
	writeConcurrency := env.GetInt("PROJECTOR_WRITE_CONCURRENCY", 8)
	if writeConcurrency <= 0 {
		writeConcurrency = 8
	}

	d := &Dashboard{
		nc:            nc,
		store:         store,
		commandsTopic: commandsTopic,
		modulesTopic:  modulesTopic,
		log:           log,
		writeGate:     make(chan struct{}, writeConcurrency),
		commandMisses: map[uint64]*commandInFlight{},
		moduleMisses:  map[uint64]*moduleInFlight{},
	}

	if err := bus.QueueSubscribeJSON[dashboardRequest, commandsReply](nc, prefix+".commands.get", queueGroup, 2*time.Second, app, log, d.handleCommandsGet); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[dashboardRequest, commandsReply](nc, prefix+".commands.replace", queueGroup, 2*time.Second, app, log, d.handleCommandsReplace); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[dashboardRequest, modulesReply](nc, prefix+".modules.get", queueGroup, 2*time.Second, app, log, d.handleModulesGet); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[dashboardRequest, modulesReply](nc, prefix+".modules.replace", queueGroup, 2*time.Second, app, log, d.handleModulesReplace)
}

func (d *Dashboard) handleCommandsGet(ctx context.Context, req dashboardRequest) commandsReply {
	userID, err := parseUserID(req.UserID)
	if err != nil {
		return commandsReply{Error: err.Error()}
	}

	commands, projected, err := d.store.GetCommands(ctx, userID)
	if err == nil && projected {
		return commandsReply{UserID: req.UserID, Commands: commands}
	}
	if err != nil && d.log != nil {
		d.log.Warn("projector command valkey read failed", zap.String("user_id", req.UserID), zap.Error(err))
	}

	fill := d.loadCommands(ctx, userID, req)
	if fill.err != "" {
		return commandsReply{UserID: req.UserID, Error: fill.err}
	}
	d.writeCommandsAsync(userID, fill.commands)
	return commandsReply{UserID: req.UserID, Commands: fill.commands}
}

func (d *Dashboard) handleCommandsReplace(ctx context.Context, req dashboardRequest) commandsReply {
	userID, err := parseUserID(req.UserID)
	if err != nil {
		return commandsReply{Error: err.Error()}
	}
	d.writeCommandsAsync(userID, req.Commands)
	return commandsReply{UserID: req.UserID, Commands: req.Commands}
}

func (d *Dashboard) handleModulesGet(ctx context.Context, req dashboardRequest) modulesReply {
	userID, err := parseUserID(req.UserID)
	if err != nil {
		return modulesReply{Error: err.Error()}
	}

	modules, projected, err := d.store.GetModules(ctx, userID)
	if err == nil && projected {
		return modulesReply{UserID: req.UserID, Modules: modules}
	}
	if err != nil && d.log != nil {
		d.log.Warn("projector module valkey read failed", zap.String("user_id", req.UserID), zap.Error(err))
	}

	fill := d.loadModules(ctx, userID, req)
	if fill.err != "" {
		return modulesReply{UserID: req.UserID, Error: fill.err}
	}
	d.writeModulesAsync(userID, fill.modules)
	return modulesReply{UserID: req.UserID, Modules: fill.modules}
}

func (d *Dashboard) handleModulesReplace(ctx context.Context, req dashboardRequest) modulesReply {
	userID, err := parseUserID(req.UserID)
	if err != nil {
		return modulesReply{Error: err.Error()}
	}
	d.writeModulesAsync(userID, req.Modules)
	return modulesReply{UserID: req.UserID, Modules: req.Modules}
}

func (d *Dashboard) writeCommandsAsync(userID uint64, commands []projection.CommandView) {
	commands = append([]projection.CommandView(nil), commands...)
	go func() {
		d.writeGate <- struct{}{}
		defer func() { <-d.writeGate }()

		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()

		if err := d.store.SetCommands(ctx, userID, commands); err != nil && d.log != nil {
			d.log.Warn("projector command valkey write failed", zap.Uint64("user_id", userID), zap.Error(err))
		}
	}()
}

func (d *Dashboard) writeModulesAsync(userID uint64, modules []projection.ModuleView) {
	modules = append([]projection.ModuleView(nil), modules...)
	go func() {
		d.writeGate <- struct{}{}
		defer func() { <-d.writeGate }()

		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()

		if err := d.store.SetModules(ctx, userID, modules); err != nil && d.log != nil {
			d.log.Warn("projector module valkey write failed", zap.Uint64("user_id", userID), zap.Error(err))
		}
	}()
}

func (d *Dashboard) loadCommands(ctx context.Context, userID uint64, req dashboardRequest) commandFill {
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
	source, err := bus.RequestJSONTimeout[commandsReply](ctx, d.nc, d.commandsTopic, req, 1500*time.Millisecond)
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

func (d *Dashboard) loadModules(ctx context.Context, userID uint64, req dashboardRequest) moduleFill {
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
	source, err := bus.RequestJSONTimeout[modulesReply](ctx, d.nc, d.modulesTopic, req, 1500*time.Millisecond)
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
