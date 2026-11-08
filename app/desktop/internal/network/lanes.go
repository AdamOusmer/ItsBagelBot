// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/app/desktop/internal/network/wire"
	"ItsBagelBot/pkg/sandbox"
)

type virtualDialer struct {
	peer    *peer
	origins [2]string
	mu      sync.Mutex
	lanes   [laneCount]*virtualLane
}
type virtualLane struct {
	input, output     *os.File
	writer            rawWriter
	epoch             uint64
	busy, quarantined bool
}
type virtualConn struct {
	owner                *virtualDialer
	lane                 *virtualLane
	slot                 uint32
	epoch                uint64
	readMu, writeMu      sync.Mutex
	buffer               [maxChunk]byte
	header               [12]byte
	headerRead, bodyRead int
	start, end           int
	eof                  bool
	mu                   sync.Mutex
	closed, opened       bool
}

func parseWorkerFiles(args []string) ([]*os.File, error) {
	raw, err := workerDescriptorArgument(args)
	if err != nil {
		return nil, err
	}
	values := strings.Split(raw, ",")
	if len(values) != laneCount*2 {
		return nil, ErrProtocol
	}
	var descriptors [laneCount * 2]uintptr
	for i, value := range values {
		fd, err := strconv.ParseUint(value, 10, strconv.IntSize)
		if err != nil || fd < 3 || duplicateDescriptor(descriptors[:i], uintptr(fd)) {
			return nil, ErrProtocol
		}
		descriptors[i] = uintptr(fd)
	}
	files := make([]*os.File, len(values))
	for i, fd := range descriptors {
		files[i], err = sandbox.OpenPipe(fd, "network-pipe")
		if err != nil {
			closeFiles(files)
			return nil, ErrProtocol
		}
	}
	return files, nil
}
func workerDescriptorArgument(args []string) (string, error) {
	var raw string
	found := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "--sandbox-fds=") {
			if found {
				return "", ErrProtocol
			}
			found = true
			raw = strings.TrimPrefix(arg, "--sandbox-fds=")
		}
	}
	if !found {
		return "", ErrProtocol
	}
	return raw, nil
}
func duplicateDescriptor(descriptors []uintptr, value uintptr) bool {
	for _, descriptor := range descriptors {
		if descriptor == value {
			return true
		}
	}
	return false
}
func newVirtualDialer(p *peer, files []*os.File, origins [2]string) *virtualDialer {
	d := &virtualDialer{peer: p, origins: origins}
	for i := range d.lanes {
		d.lanes[i] = &virtualLane{input: files[i*2], output: files[i*2+1], writer: rawWriter{writer: files[i*2+1]}}
	}
	return d
}
func (d *virtualDialer) origin(address string) (uint32, bool) {
	for i, origin := range d.origins {
		u, err := url.Parse(origin)
		if err != nil || u.Hostname() == "" {
			continue
		}
		port := u.Port()
		if port == "" {
			port = "443"
		}
		if address == net.JoinHostPort(u.Hostname(), port) {
			return uint32(i), true
		}
	}
	return 0, false
}
func (d *virtualDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	origin, ok := d.origin(address)
	if !ok || network != "tcp" {
		return nil, ErrProtocol
	}
	connection, err := d.reserve()
	if err != nil {
		return nil, err
	}
	message, err := d.peer.call(ctx, &wire.Frame{Kind: openLane, Slot: connection.slot, Epoch: connection.epoch, Origin: origin})
	if err != nil {
		connection.quarantine()
		_ = connection.Close()
		return nil, err
	}
	defer message.ReturnToVTPool()
	if message.Error != noError {
		_ = connection.Close()
		return nil, ErrWorker
	}
	connection.mu.Lock()
	connection.opened = true
	connection.mu.Unlock()
	if err = ctx.Err(); err != nil {
		_ = connection.Close()
		return nil, err
	}
	return connection, nil
}
func (d *virtualDialer) reserve() (*virtualConn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, lane := range d.lanes {
		if lane.busy || lane.quarantined || lane.epoch == math.MaxUint64 {
			continue
		}
		if lane.input.SetReadDeadline(time.Time{}) != nil || lane.output.SetWriteDeadline(time.Time{}) != nil {
			lane.quarantined = true
			closeFiles([]*os.File{lane.input, lane.output})
			continue
		}
		lane.busy = true
		lane.epoch++
		return &virtualConn{owner: d, lane: lane, slot: uint32(i), epoch: lane.epoch}, nil
	}
	return nil, ErrWorker
}
func (c *virtualConn) Read(dst []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if len(dst) == 0 {
		return 0, nil
	}
	if c.isClosed() {
		return 0, net.ErrClosed
	}
	if c.start == c.end {
		if err := c.nextPayload(); err != nil {
			return 0, err
		}
	}
	n := copy(dst, c.buffer[c.start:c.end])
	c.start += n
	return n, nil
}
func (c *virtualConn) nextPayload() error {
	for {
		if c.isClosed() {
			return net.ErrClosed
		}
		if c.eof {
			return io.EOF
		}
		epoch, size, err := c.readFrame()
		if err != nil {
			c.readFailure(err)
			return err
		}
		if epoch < c.epoch {
			continue
		}
		if epoch != c.epoch {
			c.quarantine()
			return ErrProtocol
		}
		if size == 0 {
			c.eof = true
			return io.EOF
		}
		c.start = 0
		c.end = int(size)
		return nil
	}
}

// Preserve partial framing across deadlines: a subsequent read or Close drain
// resumes at the exact byte offset instead of mistaking a payload for a header.
func (c *virtualConn) readFrame() (uint64, uint32, error) {
	n, err := io.ReadFull(c.lane.input, c.header[c.headerRead:])
	c.headerRead += n
	if err != nil {
		return 0, 0, err
	}
	epoch := binary.BigEndian.Uint64(c.header[:8])
	size := binary.BigEndian.Uint32(c.header[8:])
	if epoch == 0 || size > maxChunk {
		return 0, 0, ErrProtocol
	}
	n, err = io.ReadFull(c.lane.input, c.buffer[c.bodyRead:size])
	c.bodyRead += n
	if err != nil {
		return 0, 0, err
	}
	c.headerRead = 0
	c.bodyRead = 0
	return epoch, size, nil
}
func (c *virtualConn) readFailure(err error) {
	var failure net.Error
	if !errors.As(err, &failure) || !failure.Timeout() {
		c.quarantine()
	}
}
func (c *virtualConn) Write(body []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	total := 0
	for len(body) > 0 {
		if c.isClosed() {
			return total, net.ErrClosed
		}
		size := len(body)
		if size > maxChunk {
			size = maxChunk
		}
		if err := c.lane.writer.write(c.epoch, body[:size]); err != nil {
			c.quarantine()
			return total, err
		}
		total += size
		body = body[size:]
	}
	if c.isClosed() {
		return total, net.ErrClosed
	}
	return total, nil
}
func (c *virtualConn) isClosed() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.closed }
func (c *virtualConn) Close() error {
	opened, first := c.beginClose()
	if !first {
		return nil
	}
	if opened && c.owner.peer.ctx.Err() == nil {
		if c.owner.peer.send.write(&wire.Frame{Kind: closeLane, Slot: c.slot, Epoch: c.epoch}) != nil {
			c.quarantine()
		}
	} else if opened {
		c.quarantine()
	}
	c.readMu.Lock()
	if opened && !c.eof && !c.drain() {
		c.quarantine()
	}
	c.readMu.Unlock()
	c.writeMu.Lock()
	c.writeMu.Unlock()
	c.owner.mu.Lock()
	c.lane.busy = false
	c.owner.mu.Unlock()
	return nil
}
func (c *virtualConn) beginClose() (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false, false
	}
	c.closed = true
	deadline := time.Now().Add(2 * time.Second)
	if c.lane.input.SetReadDeadline(deadline) != nil || c.lane.output.SetWriteDeadline(deadline) != nil {
		c.quarantine()
	}
	return c.opened, true
}
func (c *virtualConn) drain() bool {
	c.start = 0
	c.end = 0
	for {
		epoch, size, err := c.readFrame()
		if err != nil {
			return false
		}
		if epoch > c.epoch {
			return false
		}
		if epoch == c.epoch && size == 0 {
			c.eof = true
			return true
		}
	}
}
func (c *virtualConn) quarantine() {
	c.owner.mu.Lock()
	c.lane.quarantined = true
	c.owner.mu.Unlock()
	closeFiles([]*os.File{c.lane.input, c.lane.output})
}

type pipeAddress struct{}

func (pipeAddress) Network() string         { return "isolated-tcp" }
func (pipeAddress) String() string          { return "isolated" }
func (c *virtualConn) LocalAddr() net.Addr  { return pipeAddress{} }
func (c *virtualConn) RemoteAddr() net.Addr { return pipeAddress{} }
func (c *virtualConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}
func (c *virtualConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	return c.lane.input.SetReadDeadline(t)
}
func (c *virtualConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	return c.lane.output.SetWriteDeadline(t)
}
