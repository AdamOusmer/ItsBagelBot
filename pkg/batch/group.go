// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package batch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

var ErrClosed = errors.New("commit group closed")
var ErrItemTooLarge = errors.New("item exceeds commit group byte budget")

const defaultFlushTimeout = 5 * time.Second
const closeRetryBackoff = 100 * time.Millisecond

type GroupConfig[T any] struct {
	Window             time.Duration
	MaxRows            int
	MaxBytes           int
	Size               func(T) int
	Key                func(T) any
	Timeout            time.Duration
	QueueTimeout       time.Duration
	AfterCommitTimeout time.Duration
	Attributable       func(error) bool
	AfterCommit        func(context.Context, []T)
	Concurrency        int
	Log                *zap.Logger
}

// A failed flush is replayed during isolation; commit every item atomically and retain stable identities.
type GroupFlush[T any] func(context.Context, []T) error

type submission[T any] struct {
	item     T
	bytes    int
	arrived  time.Time
	result   chan error
	previous *submission[T]
}

type Group[T any] struct {
	mu      sync.Mutex
	pending []*submission[T]
	bytes   int
	closed  bool
	config  GroupConfig[T]
	flush   GroupFlush[T]
	wake    chan struct{}
	done    chan struct{}
	commits sync.WaitGroup
	slots   chan struct{}
}

func NewGroup[T any](config GroupConfig[T], flush GroupFlush[T]) *Group[T] {
	if !config.valid() {
		panic("commit group requires positive row and byte budgets and a size function")
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultFlushTimeout
	}
	if config.AfterCommitTimeout <= 0 {
		config.AfterCommitTimeout = defaultFlushTimeout
	}
	if config.Log == nil {
		config.Log = zap.NewNop()
	}
	g := &Group[T]{config: config, flush: flush, wake: make(chan struct{}, 1), done: make(chan struct{}), slots: make(chan struct{}, max(1, config.Concurrency))}
	go g.run()
	return g
}

func (c GroupConfig[T]) valid() bool {
	return c.MaxRows > 0 && c.MaxBytes > 0 && c.Size != nil
}

// Cancellation after admission can race a commit; retries must retain the item's idempotency identity.
func (g *Group[T]) Submit(ctx context.Context, item T) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s := &submission[T]{item: item, bytes: g.config.Size(item), arrived: time.Now(), result: make(chan error, 1)}
	if s.bytes < 0 || s.bytes > g.config.MaxBytes {
		return ErrItemTooLarge
	}
	if err := g.admit(s); err != nil {
		return err
	}
	select {
	case err := <-s.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *Group[T]) admit(s *submission[T]) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return ErrClosed
	}
	g.pending = append(g.pending, s)
	g.bytes += s.bytes
	g.signal()
	return nil
}

func (g *Group[T]) signal() {
	select {
	case g.wake <- struct{}{}:
	default:
	}
}

func (g *Group[T]) Close(ctx context.Context) error {
	g.mu.Lock()
	g.closed = true
	g.signal()
	g.mu.Unlock()
	select {
	case <-g.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *Group[T]) ready() (time.Duration, bool) {
	if len(g.pending) == 0 {
		return -1, g.closed
	}
	if g.full() {
		return 0, false
	}
	window := g.config.Window
	if g.config.QueueTimeout > 0 {
		window = min(window, g.config.QueueTimeout)
	}
	return max(0, time.Until(g.pending[0].arrived.Add(window))), false
}

func (g *Group[T]) full() bool {
	return g.closed || g.budgetReached()
}

func (g *Group[T]) budgetReached() bool {
	return len(g.pending) >= g.config.MaxRows || g.bytes >= g.config.MaxBytes
}

func (g *Group[T]) run() {
	defer close(g.done)
	defer g.commits.Wait()
	for {
		g.mu.Lock()
		delay, done := g.ready()
		g.mu.Unlock()
		if done {
			return
		}
		if delay == 0 {
			g.start(g.take())
			continue
		}
		g.wait(delay)
	}
}

func (g *Group[T]) start(items []*submission[T]) {
	ctx := context.Background()
	if g.config.QueueTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, items[0].arrived.Add(g.config.QueueTimeout))
		defer cancel()
	}
	select {
	case g.slots <- struct{}{}:
		g.startAdmitted(ctx, items)
	case <-ctx.Done():
		completeGroup(items, ctx.Err())
	}
}

func (g *Group[T]) startAdmitted(ctx context.Context, items []*submission[T]) {
	if err := ctx.Err(); err != nil {
		<-g.slots
		completeGroup(items, err)
		return
	}
	g.dispatch(items)
}

func (g *Group[T]) dispatch(items []*submission[T]) {
	g.commits.Add(1)
	go g.commit(items)
}

func (g *Group[T]) wait(delay time.Duration) {
	if delay < 0 {
		<-g.wake
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-g.wake:
	}
}

func (g *Group[T]) take() []*submission[T] {
	g.mu.Lock()
	defer g.mu.Unlock()
	n, bytes := 0, 0
	for _, s := range g.pending {
		if n == g.config.MaxRows || bytes+s.bytes > g.config.MaxBytes {
			break
		}
		n++
		bytes += s.bytes
	}
	items := g.pending[:n:n]
	g.pending = g.pending[n:]
	g.bytes -= bytes
	return items
}

func (g *Group[T]) commit(items []*submission[T]) {
	defer g.commits.Done()
	defer func() { <-g.slots }()
	ctx, cancel := context.WithTimeout(context.Background(), g.config.Timeout)
	defer cancel()
	var landed []*submission[T]
	g.isolate(ctx, g.coalesce(items), &landed)
	if len(landed) > 0 && g.config.AfterCommit != nil {
		g.afterCommit(ctx, submissionValues(landed))
	}
	completeGroup(landed, nil)
}

func (g *Group[T]) coalesce(items []*submission[T]) []*submission[T] {
	if g.config.Key == nil {
		return items
	}
	positions := make(map[any]int, len(items))
	merged := make([]*submission[T], 0, len(items))
	for _, s := range items {
		key := g.config.Key(s.item)
		if i, ok := positions[key]; ok {
			s.previous = merged[i]
			merged[i] = s
			continue
		}
		positions[key] = len(merged)
		merged = append(merged, s)
	}
	return merged
}

type flushPanic struct{ value any }

func (p *flushPanic) Error() string { return fmt.Sprintf("commit group panic: %v", p.value) }

func (p *flushPanic) Unwrap() error {
	err, _ := p.value.(error)
	return err
}

func (g *Group[T]) attempt(ctx context.Context, values []T) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = &flushPanic{value: p}
		}
	}()
	return g.flush(ctx, values)
}

func submissionValues[T any](items []*submission[T]) []T {
	values := make([]T, len(items))
	for i, s := range items {
		values[i] = s.item
	}
	return values
}

func (g *Group[T]) isolate(ctx context.Context, items []*submission[T], landed *[]*submission[T]) {
	if err := ctx.Err(); err != nil {
		completeGroup(items, err)
		return
	}
	err := g.attempt(ctx, submissionValues(items))
	if g.shouldSplit(err, len(items)) {
		mid := len(items) / 2
		g.isolate(ctx, items[:mid], landed)
		g.isolate(ctx, items[mid:], landed)
		return
	}
	if g.fallback(ctx, items, err, landed) {
		return
	}
	if err == nil {
		*landed = append(*landed, items...)
		return
	}
	completeGroup(items, err)
}

func (g *Group[T]) fallback(ctx context.Context, items []*submission[T], err error, landed *[]*submission[T]) bool {
	if !g.shouldSplit(err, 2) || len(items) != 1 {
		return false
	}
	winner := items[0]
	if winner.previous == nil {
		return false
	}
	winner.result <- err
	g.isolate(ctx, []*submission[T]{winner.previous}, landed)
	return true
}

func completeGroup[T any](items []*submission[T], err error) {
	for i, s := range items {
		if s == nil {
			continue
		}
		for current := s; current != nil; current = current.previous {
			current.result <- err
		}
		items[i] = nil
	}
}

func (g *Group[T]) afterCommit(ctx context.Context, values []T) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), g.config.AfterCommitTimeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			g.config.Log.Error("commit group publication panic", zap.Any("panic", p))
		}
	}()
	g.config.AfterCommit(ctx, values)
}

func (g *Group[T]) shouldSplit(err error, count int) bool {
	if err == nil || count < 2 {
		return false
	}
	var panicErr *flushPanic
	if errors.As(err, &panicErr) {
		return true
	}
	if g.config.Attributable == nil {
		return false
	}
	return g.config.Attributable(err)
}
