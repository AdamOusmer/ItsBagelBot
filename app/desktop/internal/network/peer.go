// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"io"
	"sync"

	"ItsBagelBot/app/desktop/internal/network/wire"
)

type pendingCall struct {
	id    uint64
	kind  uint32
	reply chan *wire.Frame
}
type peer struct {
	ctx      context.Context
	cancel   context.CancelFunc
	read     frames
	send     sender
	mu       sync.Mutex
	serial   uint64
	pending  [requestCount]pendingCall
	done     chan struct{}
	handle   func(*wire.Frame) bool
	cancels  chan uint64
	helpers  sync.WaitGroup
	receives uint32
}

func newPeer(ctx context.Context, input io.Reader, output io.Writer) *peer {
	child, cancel := context.WithCancel(ctx)
	return &peer{ctx: child, cancel: cancel, read: frames{reader: input}, send: sender{writer: output, ctx: child}, done: make(chan struct{}), cancels: make(chan uint64, requestCount)}
}
func (p *peer) run() {
	defer func() { p.cancel(); p.helpers.Wait(); close(p.done) }()
	p.helpers.Add(2)
	go p.closeOnCancel()
	go p.sendCancels()
	for {
		message, err := p.read.read()
		if err != nil {
			return
		}
		if !validFrame(message) || p.receives != 0 && p.receives&(1<<message.Kind) == 0 {
			message.ReturnToVTPool()
			return
		}
		retained := false
		if message.Kind == response {
			retained = p.deliver(message)
		} else if p.handle != nil {
			retained = p.handle(message)
		}
		if !retained {
			message.ReturnToVTPool()
		}
		if p.ctx.Err() != nil {
			return
		}
	}
}
func (p *peer) closeOnCancel() {
	defer p.helpers.Done()
	<-p.ctx.Done()
	if input, ok := p.read.reader.(io.Closer); ok {
		_ = input.Close()
	}
	if output, ok := p.send.writer.(io.Closer); ok {
		_ = output.Close()
	}
}
func (p *peer) sendCancels() {
	defer p.helpers.Done()
	for {
		select {
		case id := <-p.cancels:
			if p.send.write(&wire.Frame{Kind: cancelRequest, Id: id}) != nil {
				p.cancel()
				return
			}
		case <-p.ctx.Done():
			return
		}
	}
}
func (p *peer) cancelCall(id uint64) {
	select {
	case p.cancels <- id:
	default:
		p.cancel()
	}
}
func (p *peer) deliver(m *wire.Frame) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.pending {
		slot := &p.pending[i]
		if slot.id == m.Id && m.Id != 0 {
			if !validResponseFor(m, slot.kind) {
				p.cancel()
				return false
			}
			select {
			case slot.reply <- m:
				return true
			default:
				p.cancel()
				return false
			}
		}
	}
	return false
}
func (p *peer) reserve(kind uint32) (uint64, chan *wire.Frame, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx.Err() != nil {
		return 0, nil, ErrWorker
	}
	for i := range p.pending {
		if p.pending[i].id == 0 {
			p.serial++
			ch := make(chan *wire.Frame, 1)
			p.pending[i] = pendingCall{id: p.serial, kind: kind, reply: ch}
			return p.serial, ch, nil
		}
	}
	return 0, nil, ErrWorker
}
func (p *peer) release(id uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.pending {
		if p.pending[i].id != id {
			continue
		}
		select {
		case frame := <-p.pending[i].reply:
			frame.ReturnToVTPool()
		default:
		}
		p.pending[i] = pendingCall{}
		return
	}
}
func (p *peer) call(ctx context.Context, message *wire.Frame) (*wire.Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.ctx.Err() != nil {
		return nil, ErrWorker
	}
	id, ch, err := p.reserve(message.Kind)
	if err != nil {
		return nil, err
	}
	defer p.release(id)
	message.Id = id
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if p.ctx.Err() != nil {
		return nil, ErrWorker
	}
	if err = p.send.writeContext(ctx, message); err != nil {
		p.cancel()
		return nil, ErrWorker
	}
	select {
	case reply := <-ch:
		return reply, nil
	case <-ctx.Done():
		p.cancelCall(id)
		return nil, ctx.Err()
	case <-p.ctx.Done():
		return nil, ErrWorker
	}
}
func (p *peer) reply(request *wire.Frame, reply *wire.Frame) {
	reply.Kind = response
	reply.Id = request.Id
	if p.send.write(reply) != nil {
		p.cancel()
	}
}
