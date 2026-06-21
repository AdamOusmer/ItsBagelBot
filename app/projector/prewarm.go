package main

import (
	"context"
	"fmt"
	"time"

	rpcprojection "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// Prewarmer owns the concern of pre-filling the Valkey projection cache when a
// broadcaster goes live. It fans out three concurrent RPC calls (users,
// modules, commands) and logs a warning for any that fail rather than
// silently dropping the error.
type Prewarmer struct {
	store         *projection.Store
	nc            *nats.Conn
	usersTopic    string
	modulesTopic  string
	commandsTopic string
	log           *zap.Logger
}

// NewPrewarmer constructs a Prewarmer with the given dependencies.
func NewPrewarmer(store *projection.Store, nc *nats.Conn, usersTopic, modulesTopic, commandsTopic string, log *zap.Logger) *Prewarmer {
	return &Prewarmer{
		store:         store,
		nc:            nc,
		usersTopic:    usersTopic,
		modulesTopic:  modulesTopic,
		commandsTopic: commandsTopic,
		log:           log,
	}
}

// Prewarm fires three concurrent RPCs to populate the projection cache for the
// given broadcaster. Each goroutine has its own 5-second timeout. RPC errors
// are logged as warnings rather than silently dropped; a cache miss just means
// the next real request will hydrate the cache instead.
func (pw *Prewarmer) Prewarm(ctx context.Context, userID uint64) {
	req := map[string]string{"user_id": fmt.Sprint(userID)}

	// 1. Fetch and cache the user record.
	go func() {
		rpcCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		reply, err := bus.RequestJSON[rpcprojection.UserReply](rpcCtx, pw.nc, pw.usersTopic, req)
		if err != nil {
			pw.log.Warn("prewarm: users RPC failed", zap.String("topic", pw.usersTopic), zap.Uint64("user_id", userID), zap.Error(err))
			return
		}
		if err := pw.store.SetUser(rpcCtx, userID, reply.Status, reply.IsActive, reply.Banned); err != nil {
			pw.log.Warn("prewarm: SetUser failed", zap.Uint64("user_id", userID), zap.Error(err))
		}
	}()

	// 2. Fetch and cache module settings.
	go func() {
		rpcCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		reply, err := bus.RequestJSON[rpcprojection.ModulesReply](rpcCtx, pw.nc, pw.modulesTopic, req)
		if err != nil {
			pw.log.Warn("prewarm: modules RPC failed", zap.String("topic", pw.modulesTopic), zap.Uint64("user_id", userID), zap.Error(err))
			return
		}
		if err := pw.store.SetModules(rpcCtx, userID, reply.Modules); err != nil {
			pw.log.Warn("prewarm: SetModules failed", zap.Uint64("user_id", userID), zap.Error(err))
		}
	}()

	// 3. Fetch and cache command definitions.
	go func() {
		rpcCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		reply, err := bus.RequestJSON[rpcprojection.CommandsReply](rpcCtx, pw.nc, pw.commandsTopic, req)
		if err != nil {
			pw.log.Warn("prewarm: commands RPC failed", zap.String("topic", pw.commandsTopic), zap.Uint64("user_id", userID), zap.Error(err))
			return
		}
		if err := pw.store.SetCommands(rpcCtx, userID, reply.Commands); err != nil {
			pw.log.Warn("prewarm: SetCommands failed", zap.Uint64("user_id", userID), zap.Error(err))
		}
	}()
}
