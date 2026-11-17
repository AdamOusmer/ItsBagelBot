// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"ItsBagelBot/pkg/bus"
)

type Result struct {
	Err   error
	Final bool
}

type Handoff struct {
	mu       sync.Mutex
	result   Result
	watchers map[*Subscription]struct{}
	id       uint64
}

type Subscription struct {
	handoff *Handoff
	notify  func(Result)
}

var handoffIDs atomic.Uint64

func (h *Handoff) Watch(f func(Result)) *Subscription {
	s := &Subscription{handoff: h, notify: f}
	h.mu.Lock()
	if h.result.Final {
		r := h.result
		h.mu.Unlock()
		f(r)
		return s
	}
	if h.watchers == nil {
		h.watchers = make(map[*Subscription]struct{})
	}
	h.watchers[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (s *Subscription) Cancel() {
	s.handoff.mu.Lock()
	delete(s.handoff.watchers, s)
	s.handoff.mu.Unlock()
}

func (h *Handoff) identity() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.id == 0 {
		h.id = handoffIDs.Add(1)
	}
	return h.id
}

func (h *Handoff) Notify(err error)  { h.send(Result{Err: err}) }
func (h *Handoff) Resolve(err error) { h.send(Result{Err: err, Final: true}) }

func (h *Handoff) send(r Result) {
	h.mu.Lock()
	if h.result.Final {
		h.mu.Unlock()
		return
	}
	h.result = r
	fs := make([]*Subscription, 0, len(h.watchers))
	for s := range h.watchers {
		fs = append(fs, s)
	}
	if r.Final {
		h.watchers = nil
	}
	h.mu.Unlock()
	for _, s := range fs {
		s.notify(r)
	}
}

type workKey struct{}

type Work struct {
	Handoff
	mu       sync.Mutex
	pending  int
	deferred bool
	joined   map[*Handoff]*Subscription
	failure  error
	moved    bool
}

func Tracking(ctx context.Context) bool {
	_, ok := ctx.Value(workKey{}).(*Work)
	return ok
}

func Failure(ctx context.Context) error {
	w, _ := ctx.Value(workKey{}).(*Work)
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failure
}

func NewWork(ctx context.Context) (context.Context, *Work) {
	w := &Work{pending: 1}
	return context.WithValue(ctx, workKey{}, w), w
}

func Join(ctx context.Context, h *Handoff) {
	w, _ := ctx.Value(workKey{}).(*Work)
	if w == nil {
		return
	}
	w.mu.Lock()
	if _, exists := w.joined[h]; exists {
		w.mu.Unlock()
		return
	}
	if w.joined == nil {
		w.joined = make(map[*Handoff]*Subscription)
	}
	w.joined[h] = nil
	w.pending++
	w.deferred = true
	w.mu.Unlock()
	s := h.Watch(func(r Result) { w.receive(h, r) })
	w.mu.Lock()
	if _, exists := w.joined[h]; exists {
		w.joined[h] = s
	}
	w.mu.Unlock()
}

func (w *Work) Deferred() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.deferred
}

func Fail(ctx context.Context, err error) {
	w, _ := ctx.Value(workKey{}).(*Work)
	if w != nil && err != nil {
		w.mu.Lock()
		w.failure = err
		w.mu.Unlock()
	}
}

func (w *Work) receive(h *Handoff, r Result) {
	if !r.Final {
		return
	}
	w.mu.Lock()
	delete(w.joined, h)
	done := false
	if !w.moved {
		done = w.sealLocked(r.Err)
	}
	err := w.failure
	w.mu.Unlock()
	if done {
		w.Resolve(err)
	}
}

func (w *Work) Seal(err error) {
	w.mu.Lock()
	done := w.sealLocked(err)
	err = w.failure
	w.mu.Unlock()
	if done {
		w.Resolve(err)
	}
}

func (w *Work) sealLocked(err error) bool {
	if err != nil {
		w.failure = err
	}
	w.pending--
	return w.pending == 0
}

func (w *Work) SealOnReturn(err *error) {
	if value := recover(); value != nil {
		w.Seal(fmt.Errorf("effect panicked: %v", value))
		panic(value)
	}
	w.Seal(*err)
}

func Async(ctx context.Context, task func(context.Context) error) func() {
	finish := bus.BeginDeliveryAsync(ctx)
	child, w := NewWork(context.WithoutCancel(ctx))
	Join(ctx, &w.Handoff)
	return func() {
		defer finish()
		var err error
		defer w.SealOnReturn(&err)
		err = task(child)
	}
}
