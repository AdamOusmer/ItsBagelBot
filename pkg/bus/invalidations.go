// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const invalidationOriginHeader = "Bagel-Invalidation-Origin"

type Invalidations struct {
	nc       *nats.Conn
	prefix   string
	origin   string
	log      *zap.Logger
	mu       sync.RWMutex
	handlers map[string][]func(cache.Event)
	gaps     []func()
}

func NewInvalidations(nc *nats.Conn, prefix string, log *zap.Logger) *Invalidations {
	if log == nil {
		log = zap.NewNop()
	}
	h := &Invalidations{nc: nc, prefix: prefix, origin: nuid.Next(), log: log, handlers: make(map[string][]func(cache.Event))}
	if nc != nil {
		previous := nc.ReconnectHandler()
		nc.SetReconnectHandler(func(conn *nats.Conn) {
			h.gap()
			if previous != nil {
				previous(conn)
			}
		})
	}
	return h
}

func (h *Invalidations) Watch(scope string, fn func(cache.Event)) {
	h.mu.Lock()
	first := len(h.handlers[scope]) == 0
	h.handlers[scope] = append(h.handlers[scope], fn)
	h.mu.Unlock()
	if first && h.onWire(wireScope(scope)) {
		h.subscribe(wireScope(scope))
	}
}

func (h *Invalidations) onWire(scope wireScope) bool {
	return h.nc != nil && h.prefix != "" && !strings.Contains(string(scope), ".")
}

func (h *Invalidations) subscribe(scope wireScope) {
	if _, err := h.nc.Subscribe(h.prefix+"."+string(scope), h.receive); err != nil {
		h.log.Error("cache invalidation subscribe failed", zap.String("scope", string(scope)), zap.Error(err))
	}
}

func (h *Invalidations) WatchGap(fn func()) {
	h.mu.Lock()
	h.gaps = append(h.gaps, fn)
	h.mu.Unlock()
}

func (h *Invalidations) receive(msg *nats.Msg) {
	if msg.Header.Get(invalidationOriginHeader) == h.origin {
		return
	}
	var dto invalidate.DTO
	if err := codec.Unmarshal(msg.Data, &dto); err != nil {
		h.log.Debug("cache invalidation: bad payload", zap.Error(err))
		return
	}
	scope := msg.Subject[strings.LastIndexByte(msg.Subject, '.')+1:]
	id, err := strconv.ParseUint(dto.BroadcasterID, 10, 64)
	if err != nil || id == 0 {
		h.log.Debug("cache invalidation: bad broadcaster_id", zap.String("id", dto.BroadcasterID))
		return
	}
	h.dispatch(cache.Event{Scope: scope, ID: id, Keys: dto.Keys})
}

func (h *Invalidations) dispatch(e cache.Event) {
	h.mu.RLock()
	handlers := append([]func(cache.Event){}, h.handlers[e.Scope]...)
	h.mu.RUnlock()
	for _, fn := range handlers {
		fn(e)
	}
}

func (h *Invalidations) gap() {
	h.mu.RLock()
	handlers := append([]func(){}, h.gaps...)
	h.mu.RUnlock()
	for _, fn := range handlers {
		fn()
	}
}

func (h *Invalidations) Publish(scope string, id uint64, keys ...string) error {
	h.dispatch(cache.Event{Scope: scope, ID: id, Keys: keys})
	return h.publish(wireScope(scope), invalidate.DTO{BroadcasterID: strconv.FormatUint(id, 10), Keys: keys})
}

func (h *Invalidations) PublishAll(scope string) error {
	return h.publish(wireScope(scope), invalidate.DTO{BroadcasterID: "*"})
}

type wireScope string

func (h *Invalidations) publish(scope wireScope, dto invalidate.DTO) error {
	if h.nc == nil || h.prefix == "" {
		return nil
	}
	body, err := codec.Marshal(dto)
	if err == nil {
		msg := &nats.Msg{Subject: h.prefix + "." + string(scope), Data: body, Header: nats.Header{}}
		msg.Header.Set(invalidationOriginHeader, h.origin)
		err = h.nc.PublishMsg(msg)
	}
	if err != nil {
		h.log.Warn("cache invalidation publish failed", zap.String("scope", string(scope)), zap.Error(err))
	}
	return err
}

func (h *Invalidations) Changes(ctx context.Context, app *newrelic.Application, sub Subscriber, subjects ...string) error {
	for _, subject := range subjects {
		err := Consume(ctx, app, sub, subject, func(msg *Message) error {
			return h.change(Publication{Subject: subject, Payload: msg.Payload})
		}, h.log)
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *Invalidations) change(p Publication) error {
	e, err := changeEvent(p)
	if err == nil {
		h.dispatch(e)
	}
	return err
}

func changeEvent(p Publication) (cache.Event, error) {
	var dto struct {
		UserID uint64 `json:"user_id"`
	}
	err := codec.Unmarshal(p.Payload, &dto)
	return cache.Event{Scope: p.Subject, ID: dto.UserID}, err
}

func (h *Invalidations) watched(p Publication) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.handlers[p.Subject]) > 0
}

type skipInvalidation struct{}

func SkipInvalidation(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipInvalidation{}, true)
}

type invalidatingPublisher struct {
	Publisher
	hub *Invalidations
}

func (h *Invalidations) Publisher(pub Publisher) Publisher {
	return invalidatingPublisher{Publisher: pub, hub: h}
}

func (p invalidatingPublisher) event(ctx context.Context, pub Publication) func() {
	if ctx.Value(skipInvalidation{}) == true || !p.hub.watched(pub) {
		return func() {}
	}
	e, err := changeEvent(pub)
	if err != nil {
		return func() {}
	}
	return func() { p.hub.dispatch(e) }
}

func (p invalidatingPublisher) PublishOwned(ctx context.Context, subject string, payload []byte) error {
	return p.PublishOwnedWithID(ctx, subject, "", payload)
}

func (p invalidatingPublisher) PublishOwnedWithID(ctx context.Context, subject, id string, payload []byte) error {
	dispatch := p.event(ctx, Publication{Subject: subject, Payload: payload})
	send := p.Publisher.PublishOwned
	if id != "" {
		send = func(ctx context.Context, subject string, payload []byte) error {
			return p.Publisher.PublishOwnedWithID(ctx, subject, id, payload)
		}
	}
	err := send(ctx, subject, payload)
	dispatch()
	return err
}

func WithInvalidations(pub Publisher) (Publisher, *Invalidations) {
	if p, ok := pub.(invalidatingPublisher); ok {
		return pub, p.hub
	}
	h := NewInvalidations(nil, "", nil)
	return h.Publisher(pub), h
}
