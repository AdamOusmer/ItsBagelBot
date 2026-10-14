// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	expiryQueueSize      = 1024
	expiryReconnectDelay = time.Second
)

// ExpiryHub holds one expired-keyevent subscription per Valkey DB and fans keys out by prefix.
type ExpiryHub struct {
	client valkey.Client
	db     int
	log    *zap.Logger

	mu       sync.Mutex
	watchers atomic.Pointer[[]*expiryWatcher]
	stop     context.CancelFunc
	stopped  chan struct{}
}

type expiryWatcher struct {
	prefixes []string
	queue    chan string
	done     <-chan struct{}
}

func NewExpiryHub(client valkey.Client, db int, log *zap.Logger) *ExpiryHub {
	if log == nil {
		log = zap.NewNop()
	}
	return &ExpiryHub{client: client, db: db, log: log}
}

func expiryHubOr(hub *ExpiryHub, client valkey.Client, db int, log *zap.Logger) *ExpiryHub {
	if hub != nil && hub.db == db {
		return hub
	}
	return NewExpiryHub(client, db, log)
}

// Watch blocks until ctx ends, delivering each expired key matching a prefix to handler in order.
func (h *ExpiryHub) Watch(ctx context.Context, handler func(context.Context, string), prefixes ...string) {
	w := &expiryWatcher{prefixes: prefixes, queue: make(chan string, expiryQueueSize), done: ctx.Done()}
	h.add(w)
	defer h.remove(w)
	for {
		select {
		case <-ctx.Done():
			return
		case key := <-w.queue:
			handler(ctx, key)
		}
	}
}

func (h *ExpiryHub) add(w *expiryWatcher) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.watchers.Store(appendWatcher(h.watchers.Load(), w))
	if h.stop == nil {
		h.start()
	}
}

func (h *ExpiryHub) remove(w *expiryWatcher) {
	h.mu.Lock()
	defer h.mu.Unlock()
	next := withoutWatcher(h.watchers.Load(), w)
	h.watchers.Store(next)
	if len(*next) == 0 && h.stop != nil {
		h.stop()
		h.stop = nil
	}
}

func appendWatcher(current *[]*expiryWatcher, w *expiryWatcher) *[]*expiryWatcher {
	var next []*expiryWatcher
	if current != nil {
		next = append(next, *current...)
	}
	next = append(next, w)
	return &next
}

func withoutWatcher(current *[]*expiryWatcher, w *expiryWatcher) *[]*expiryWatcher {
	next := make([]*expiryWatcher, 0, len(*current))
	for _, other := range *current {
		if other != w {
			next = append(next, other)
		}
	}
	return &next
}

func (h *ExpiryHub) start() {
	ctx, cancel := context.WithCancel(context.Background())
	previous := h.stopped
	stopped := make(chan struct{})
	h.stop, h.stopped = cancel, stopped
	go func() {
		defer close(stopped)
		if previous != nil {
			<-previous
		}
		h.run(ctx)
	}()
}

func (h *ExpiryHub) run(ctx context.Context) {
	channel := "__keyevent@" + strconv.Itoa(h.db) + "__:expired"
	h.log.Info("expiry hub starting", zap.String("channel", channel))
	for ctx.Err() == nil {
		err := h.client.Receive(ctx, h.client.B().Subscribe().Channel(channel).Build(), func(msg valkey.PubSubMessage) {
			h.dispatch(msg.Message)
		})
		if ctx.Err() != nil {
			return
		}
		h.log.Warn("expiry hub dropped, reconnecting", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(expiryReconnectDelay):
		}
	}
}

func (h *ExpiryHub) dispatch(key string) {
	current := h.watchers.Load()
	if current == nil {
		return
	}
	for _, w := range *current {
		w.offer(key)
	}
}

func (w *expiryWatcher) offer(key string) {
	if !w.matches(key) {
		return
	}
	select {
	case w.queue <- key:
	case <-w.done:
	}
}

func (w *expiryWatcher) matches(key string) bool {
	for _, prefix := range w.prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
