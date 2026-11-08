// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"sync"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/transport"
	"ItsBagelBot/app/desktop/internal/oauth"
	shared "ItsBagelBot/pkg/websocket"
)

// Supervisor bounds OS-worker replacement separately from stream recovery.
// Healthy server reconnects do not restart this process. A failed sandbox never
// causes an in-process networking fallback.
type Supervisor struct {
	mu       sync.Mutex
	ctx      context.Context
	config   Config
	worker   *Client
	window   time.Time
	attempts int
	closed   bool
	gate     chan struct{}
	cancel   context.CancelFunc
}

func Start(ctx context.Context, config Config) (*Supervisor, error) {
	life, cancel := context.WithCancel(ctx)
	config.RootsPEM = append([]byte(nil), config.RootsPEM...)
	s := &Supervisor{ctx: life, cancel: cancel, config: config, gate: make(chan struct{}, 1)}
	if _, err := s.current(ctx); err != nil {
		cancel()
		return nil, err
	}
	return s, nil
}
func (s *Supervisor) current(ctx context.Context) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, ErrWorker
	}
	defer func() { <-s.gate }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	worker, err := s.currentOrReserve()
	s.mu.Unlock()
	if worker != nil || err != nil {
		return worker, err
	}
	worker, err = newClientForAttempt(s.ctx, ctx, s.config, nil)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = worker.Close()
		return nil, ErrWorker
	}
	s.worker = worker
	return worker, nil
}

// currentOrReserve is called under mu; the cancellable launch gate serializes
// setup without holding mu across process startup or an attempt deadline.
func (s *Supervisor) currentOrReserve() (*Client, error) {
	if s.closed || s.ctx.Err() != nil {
		return nil, ErrWorker
	}
	if s.worker != nil && s.worker.peer.ctx.Err() == nil {
		return s.worker, nil
	}
	now := time.Now()
	if s.window.IsZero() || now.Sub(s.window) > 30*time.Second {
		s.window = now
		s.attempts = 0
	}
	if s.attempts >= 3 {
		return nil, ErrWorker
	}
	s.attempts++
	if s.worker != nil {
		_ = s.worker.Close()
		s.worker = nil
	}
	return nil, nil
}
func (s *Supervisor) Reset() { s.mu.Lock(); s.window = time.Time{}; s.attempts = 0; s.mu.Unlock() }
func (s *Supervisor) Close() error {
	s.mu.Lock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	worker := s.worker
	s.worker = nil
	s.mu.Unlock()
	if worker != nil {
		return worker.Close()
	}
	return nil
}
func (s *Supervisor) Do(ctx context.Context, request oauth.NetworkRequest) (oauth.NetworkResponse, error) {
	worker, err := s.current(ctx)
	if err != nil {
		return oauth.NetworkResponse{}, oauth.ErrUnavailable
	}
	return worker.Do(ctx, request)
}
func (s *Supervisor) DialAuthorized(ctx context.Context, path string, auth shared.Authorization) (transport.Conn, shared.Reply, error) {
	worker, err := s.current(ctx)
	if err != nil {
		return nil, shared.Reply{}, shared.ErrDial
	}
	return worker.DialAuthorized(ctx, path, auth)
}
func (s *Supervisor) PostAuthorized(ctx context.Context, path string, auth shared.Authorization, contentType string, body []byte) (shared.Reply, error) {
	worker, err := s.current(ctx)
	if err != nil {
		return shared.Reply{}, shared.ErrDial
	}
	return worker.PostAuthorized(ctx, path, auth, contentType, body)
}
