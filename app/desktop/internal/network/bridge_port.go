// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"sync"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/bridge/transport"
	shared "ItsBagelBot/pkg/websocket"
)

type bridgeProvider interface {
	DialAuthorized(context.Context, string, shared.Authorization) (transport.Conn, shared.Reply, error)
	PostAuthorized(context.Context, string, shared.Authorization, string, []byte) (shared.Reply, error)
}

// Bridge grants an account-owned event/control scope. Closing this scope never
// closes the application-owned parser supervisor or its OAuth transport.
func (s *Supervisor) Bridge() transport.Native { return newBridgePort(s.ctx, s) }

type bridgePort struct {
	provider   bridgeProvider
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	streams    [streamCount]*bridgeStream
	operations [4]*bridgeOperation
	closed     bool
	closeOnce  sync.Once
}

type bridgeStream struct {
	owner  *bridgePort
	slot   int
	ctx    context.Context
	cancel context.CancelFunc
	stop   func() bool
	conn   transport.Conn
	closed bool
	once   sync.Once
}

type bridgeOperation struct {
	slot   int
	ctx    context.Context
	cancel context.CancelFunc
	stop   func() bool
}

func newBridgePort(parent context.Context, provider bridgeProvider) *bridgePort {
	ctx, cancel := context.WithCancel(parent)
	return &bridgePort{ctx: ctx, cancel: cancel, provider: provider}
}

func (p *bridgePort) reserveStream(parent context.Context) (*bridgeStream, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.ctx.Err() != nil {
		return nil, shared.ErrClosed
	}
	for i, slot := range p.streams {
		if slot != nil {
			continue
		}
		ctx, cancel := context.WithCancel(parent)
		stream := &bridgeStream{owner: p, slot: i, ctx: ctx, cancel: cancel, stop: context.AfterFunc(p.ctx, cancel)}
		p.streams[i] = stream
		return stream, nil
	}
	return nil, ErrWorker
}

func (p *bridgePort) DialAuthorized(ctx context.Context, path string, auth shared.Authorization) (transport.Conn, shared.Reply, error) {
	stream, err := p.reserveStream(ctx)
	if err != nil {
		return nil, shared.Reply{}, err
	}
	conn, reply, err := p.provider.DialAuthorized(stream.ctx, path, auth)
	if err != nil || conn == nil {
		return nil, reply, p.failedDial(stream, conn, err)
	}
	if !p.attach(stream, conn) {
		_ = conn.Close()
		_ = stream.Close()
		return nil, reply, shared.ErrClosed
	}
	return stream, reply, nil
}

func (p *bridgePort) failedDial(stream *bridgeStream, conn transport.Conn, err error) error {
	if conn != nil {
		_ = conn.Close()
	}
	_ = stream.Close()
	if err == nil {
		return shared.ErrDial
	}
	return err
}

func (p *bridgePort) attach(stream *bridgeStream, conn transport.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || stream.closed || stream.ctx.Err() != nil {
		return false
	}
	stream.conn = conn
	return true
}

func (p *bridgePort) reserveOperation(parent context.Context) (*bridgeOperation, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.ctx.Err() != nil {
		return nil, shared.ErrClosed
	}
	for i, slot := range p.operations {
		if slot != nil {
			continue
		}
		ctx, cancel := context.WithCancel(parent)
		op := &bridgeOperation{slot: i, ctx: ctx, cancel: cancel, stop: context.AfterFunc(p.ctx, cancel)}
		p.operations[i] = op
		return op, nil
	}
	return nil, ErrWorker
}

func (p *bridgePort) PostAuthorized(ctx context.Context, path string, auth shared.Authorization, contentType string, body []byte) (shared.Reply, error) {
	op, err := p.reserveOperation(ctx)
	if err != nil {
		return shared.Reply{}, err
	}
	defer p.releaseOperation(op)
	return p.provider.PostAuthorized(op.ctx, path, auth, contentType, body)
}

func (p *bridgePort) releaseOperation(op *bridgeOperation) {
	op.stop()
	op.cancel()
	p.mu.Lock()
	if p.operations[op.slot] == op {
		p.operations[op.slot] = nil
	}
	p.mu.Unlock()
}

func (s *bridgeStream) Recv(ctx context.Context, message *protocol.Message) error {
	s.owner.mu.Lock()
	conn, closed := s.conn, s.closed
	s.owner.mu.Unlock()
	if closed || conn == nil {
		return shared.ErrClosed
	}
	return conn.Recv(ctx, message)
}

func (s *bridgeStream) Close() error {
	var result error
	s.once.Do(func() {
		s.owner.mu.Lock()
		s.closed = true
		conn := s.conn
		s.owner.mu.Unlock()
		s.stop()
		s.cancel()
		if conn != nil {
			result = conn.Close()
		}
		s.owner.mu.Lock()
		if s.owner.streams[s.slot] == s {
			s.owner.streams[s.slot] = nil
		}
		s.owner.mu.Unlock()
	})
	return result
}

func (p *bridgePort) Close() error {
	var result error
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		streams, operations := p.streams, p.operations
		p.mu.Unlock()
		p.cancel()
		for _, op := range operations {
			if op != nil {
				op.cancel()
			}
		}
		for _, stream := range streams {
			if stream != nil {
				if err := stream.Close(); result == nil {
					result = err
				}
			}
		}
	})
	return result
}
