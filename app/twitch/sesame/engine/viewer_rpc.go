// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const viewerFetchRPCTimeout = 3500 * time.Millisecond

type viewerSnapshotState int

const (
	viewerSnapshotCold viewerSnapshotState = iota
	viewerSnapshotOK
	viewerSnapshotMissingScope
)

type ViewerLookup interface {
	Snapshot(ctx context.Context, broadcasterID uint64) ([]chattersSnapshotEntry, viewerSnapshotState)
}

type ViewerRPC struct {
	store   *ValkeyChatters
	request func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error)
	log     *zap.Logger

	mu           sync.Mutex
	missingScope map[uint64]bool
	down         map[uint64]bool
}

func NewViewerRPC(nc *nats.Conn, prefix string, store *ValkeyChatters, log *zap.Logger) *ViewerRPC {
	subject := strings.TrimSuffix(prefix, ".") + ".chatters.get"
	if log == nil {
		log = zap.NewNop()
	}
	return &ViewerRPC{
		store: store,
		request: func(ctx context.Context, req manage.ChattersRequest) (manage.ChattersReply, error) {
			return bus.RequestJSONTimeout[manage.ChattersReply](ctx, nc, subject, req, viewerFetchRPCTimeout)
		},
		log:          log,
		missingScope: make(map[uint64]bool),
		down:         make(map[uint64]bool),
	}
}

func (r *ViewerRPC) Snapshot(ctx context.Context, broadcasterID uint64) ([]chattersSnapshotEntry, viewerSnapshotState) {
	entries, ok, err := r.store.Snapshot(ctx, broadcasterID)
	if ok {
		r.clearMissingScope(broadcasterID)
		return entries, viewerSnapshotOK
	}
	if err != nil {
		r.logDownOnce(broadcasterID, err)
	}
	if r.isMissingScope(broadcasterID) {
		return nil, viewerSnapshotMissingScope
	}
	if r.store.TryFetchLock(ctx, broadcasterID) {
		go r.fetch(broadcasterID)
	}
	return nil, viewerSnapshotCold
}

func (r *ViewerRPC) fetch(broadcasterID uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), viewerFetchRPCTimeout)
	defer cancel()
	reply, err := r.request(ctx, manage.ChattersRequest{BroadcasterID: strconv.FormatUint(broadcasterID, 10)})
	if err != nil {
		r.abortFetch(broadcasterID, err)
		return
	}
	if reply.MissingScope {
		r.markMissingScope(broadcasterID)
		r.store.ReleaseFetchLock(context.Background(), broadcasterID)
		return
	}
	if reply.Error != "" {
		r.abortFetch(broadcasterID, errors.New(reply.Error))
		return
	}
	r.store.Store(context.Background(), broadcasterID, viewerSnapshotEntries(reply.Chatters))
}

func (r *ViewerRPC) abortFetch(broadcasterID uint64, err error) {
	r.log.Warn("viewer: chatters fetch failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	r.store.ReleaseFetchLock(context.Background(), broadcasterID)
}

func viewerSnapshotEntries(chatters []manage.Chatter) []chattersSnapshotEntry {
	out := make([]chattersSnapshotEntry, 0, len(chatters))
	for _, ch := range chatters {
		id, err := strconv.ParseUint(ch.ID, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		out = append(out, chattersSnapshotEntry{ID: id, Login: ch.Login, Name: ch.Login})
	}
	return out
}

func (r *ViewerRPC) isMissingScope(broadcasterID uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.missingScope[broadcasterID]
}

func (r *ViewerRPC) markMissingScope(broadcasterID uint64) {
	r.mu.Lock()
	already := r.missingScope[broadcasterID]
	r.missingScope[broadcasterID] = true
	r.mu.Unlock()
	if !already {
		r.log.Warn("viewer: chatters missing scope, degrading to roster draws", zap.Uint64("broadcaster_id", broadcasterID))
	}
}

func (r *ViewerRPC) clearMissingScope(broadcasterID uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.missingScope, broadcasterID)
}

func (r *ViewerRPC) logDownOnce(broadcasterID uint64, err error) {
	r.mu.Lock()
	already := r.down[broadcasterID]
	r.down[broadcasterID] = true
	r.mu.Unlock()
	if !already {
		r.log.Warn("viewer: chatters snapshot read failed, degrading to roster draws", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}
