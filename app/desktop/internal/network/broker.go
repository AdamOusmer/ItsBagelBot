// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/app/desktop/internal/network/wire"
	"ItsBagelBot/pkg/sandbox"
)

type broker struct {
	ctx             context.Context
	cancel          context.CancelFunc
	send            *sender
	origins         [2]string
	lanes           [laneCount]brokerLane
	dial            func(context.Context, string) (net.Conn, error)
	mu              sync.Mutex
	started, closed bool
	closeOnce       sync.Once
	wg              sync.WaitGroup
}
type laneRequest struct {
	id, epoch uint64
	origin    string
}
type brokerLane struct {
	mu                          sync.Mutex
	epoch                       uint64
	conn                        net.Conn
	busy, canceled, quarantined bool
	cancel                      context.CancelFunc
	requests                    chan laneRequest
	input, output               *os.File
	send                        rawWriter
}

func newBroker(ctx context.Context, s *sender, origins [2]string) (*broker, []*os.File, error) {
	child, cancel := context.WithCancel(ctx)
	b := &broker{ctx: child, cancel: cancel, send: s, origins: origins, dial: dialPublic}
	var children []*os.File
	for i := range b.lanes {
		files, err := b.makeLane(i)
		if err != nil {
			b.close()
			closeFiles(children)
			return nil, nil, err
		}
		children = append(children, files...)
	}
	context.AfterFunc(child, b.close)
	return b, children, nil
}
func (b *broker) makeLane(index int) ([]*os.File, error) {
	childRead, parentWrite, err := sandbox.Pipe()
	if err != nil {
		return nil, err
	}
	parentRead, childWrite, err := sandbox.Pipe()
	if err != nil {
		closeFiles([]*os.File{childRead, parentWrite})
		return nil, err
	}
	b.lanes[index] = brokerLane{input: parentRead, output: parentWrite, send: rawWriter{writer: parentWrite}, requests: make(chan laneRequest, 1)}
	return []*os.File{childRead, childWrite}, nil
}
func closeFiles(files []*os.File) {
	for _, file := range files {
		if file != nil {
			_ = file.Close()
		}
	}
}
func (b *broker) start() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started || b.closed {
		return
	}
	b.started = true
	for i := range b.lanes {
		b.wg.Add(2)
		go b.upstream(&b.lanes[i])
		go b.serveLane(&b.lanes[i])
	}
}
func (b *broker) close() {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		started := b.started
		b.mu.Unlock()
		b.cancel()
		for i := range b.lanes {
			b.quarantine(&b.lanes[i])
		}
		// Closing IPC is necessary before joining: a stopped worker may not read
		// responses, and a connect worker may be blocked writing one.
		if started && b.send != nil {
			if closer, ok := b.send.writer.(io.Closer); ok {
				_ = closer.Close()
			}
		}
		b.wg.Wait()
	})
}
func (b *broker) open(request *wire.Frame) {
	if request.Id == 0 || request.Slot >= laneCount || request.Origin >= 2 || request.Epoch == 0 {
		b.reply(request.Id, protocolError)
		return
	}
	b.mu.Lock()
	allowed := !b.closed && b.started && b.ctx.Err() == nil
	if allowed {
		allowed = b.enqueue(&b.lanes[request.Slot], laneRequest{id: request.Id, epoch: request.Epoch, origin: b.origins[request.Origin]})
	}
	b.mu.Unlock()
	if !allowed {
		b.reply(request.Id, unavailable)
	}
}
func (b *broker) enqueue(l *brokerLane, request laneRequest) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if request.origin == "" || l.busy || l.conn != nil || l.quarantined || request.epoch <= l.epoch {
		return false
	}
	l.busy = true
	l.canceled = false
	l.epoch = request.epoch
	select {
	case l.requests <- request:
		return true
	default:
		l.quarantined = true
		return false
	}
}
func (b *broker) reply(id uint64, code uint32) bool {
	return b.send != nil && b.send.write(&wire.Frame{Kind: response, Id: id, Error: code}) == nil
}
func (b *broker) serveLane(l *brokerLane) {
	defer b.wg.Done()
	for {
		select {
		case request := <-l.requests:
			b.connect(l, request)
		case <-b.ctx.Done():
			return
		}
	}
}
func (b *broker) connect(l *brokerLane, request laneRequest) {
	ctx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
	defer cancel()
	l.mu.Lock()
	l.cancel = cancel
	if l.canceled || l.quarantined {
		cancel()
	}
	l.mu.Unlock()
	conn, err := b.dial(ctx, request.origin)
	if !b.install(l, request.epoch, ctx, conn, err) {
		b.release(l, request.epoch, conn)
		if !b.reply(request.id, unavailable) {
			b.quarantine(l)
		}
		return
	}
	if !b.reply(request.id, noError) {
		b.release(l, request.epoch, conn)
		b.quarantine(l)
		return
	}
	err = b.downstream(l, conn, request.epoch)
	b.release(l, request.epoch, conn)
	if err != nil || l.send.write(request.epoch, nil) != nil {
		b.quarantine(l)
	}
}
func (b *broker) install(l *brokerLane, epoch uint64, ctx context.Context, conn net.Conn, err error) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil || conn == nil || ctx.Err() != nil || l.canceled || l.quarantined || l.epoch != epoch {
		return false
	}
	l.conn = conn
	return true
}
func (b *broker) release(l *brokerLane, epoch uint64, conn net.Conn) {
	if conn != nil {
		_ = conn.Close()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.epoch == epoch {
		l.conn = nil
		l.cancel = nil
		l.busy = false
	}
}
func (b *broker) downstream(l *brokerLane, conn net.Conn, epoch uint64) error {
	var buffer [maxChunk]byte
	var budget frameBudget
	for {
		n, err := conn.Read(buffer[:])
		if n > 0 {
			if !budget.allow(n, time.Now()) {
				return ErrProtocol
			}
			if writeErr := l.send.write(epoch, buffer[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			return nil
		}
	}
}
func (b *broker) upstream(l *brokerLane) {
	defer b.wg.Done()
	var buffer [maxChunk]byte
	var budget frameBudget
	for {
		epoch, size, err := rawHeader(l.input)
		if err != nil {
			b.quarantine(l)
			return
		}
		if !budget.allow(int(size)+12, time.Now()) {
			b.cancel()
			return
		}
		if _, err = io.ReadFull(l.input, buffer[:size]); err != nil {
			b.quarantine(l)
			return
		}
		b.writeNetwork(l, epoch, buffer[:size])
	}
}
func (b *broker) writeNetwork(l *brokerLane, epoch uint64, body []byte) {
	l.mu.Lock()
	conn := l.conn
	if epoch != l.epoch {
		conn = nil
	}
	l.mu.Unlock()
	if conn == nil {
		return
	}
	if len(body) == 0 {
		_ = conn.Close()
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if writeAll(conn, body) != nil {
		_ = conn.Close()
	}
}
func (b *broker) closeLane(slot uint32, epoch uint64) {
	if slot >= laneCount {
		return
	}
	l := &b.lanes[slot]
	l.mu.Lock()
	if l.epoch == epoch {
		l.canceled = true
		if l.cancel != nil {
			l.cancel()
		}
		if l.conn != nil {
			_ = l.conn.Close()
		}
	}
	l.mu.Unlock()
}
func (b *broker) quarantine(l *brokerLane) {
	l.mu.Lock()
	l.quarantined = true
	l.canceled = true
	cancel, conn := l.cancel, l.conn
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	closeFiles([]*os.File{l.input, l.output})
}
func dialPublic(ctx context.Context, origin string) (net.Conn, error) {
	host, port, err := publicOrigin(origin)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, ErrWorker
	}
	return dialAddresses(ctx, addresses, port)
}
func publicOrigin(origin string) (string, string, error) {
	u, err := url.Parse(origin)
	if err != nil || !validOrigin(origin) || u.ForceQuery || u.Hostname() == "" || strings.HasSuffix(u.Host, ":") {
		return "", "", ErrProtocol
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	value, err := strconv.ParseUint(port, 10, 16)
	if err != nil || value == 0 {
		return "", "", ErrProtocol
	}
	return u.Hostname(), port, nil
}
func dialAddresses(ctx context.Context, addresses []netip.Addr, port string) (net.Conn, error) {
	return dialResolved(ctx, addresses, port, new(net.Dialer).DialContext)
}
func dialResolved(ctx context.Context, addresses []netip.Addr, port string, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	for i, address := range addresses {
		if i == 32 {
			break
		}
		if !publicAddress(address) {
			continue
		}
		if ctx.Err() != nil {
			return nil, ErrWorker
		}
		// Dial the already validated literal, never resolve the hostname a second time.
		conn, err := dial(ctx, "tcp", net.JoinHostPort(address.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, ErrWorker
}

// Special-purpose ranges are excluded conservatively, including globally
// reachable protocol anycast and transition mechanisms. See the IANA registries:
// https://www.iana.org/assignments/iana-ipv4-special-registry/
// https://www.iana.org/assignments/iana-ipv6-special-registry/
var blockedPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/3"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}
var globalIPv6 = netip.MustParsePrefix("2000::/3")

func publicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.Is6() && !globalIPv6.Contains(address) {
		return false
	}
	for _, prefix := range blockedPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
