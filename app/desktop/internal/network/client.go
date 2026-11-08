// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/bridge/transport"
	"ItsBagelBot/app/desktop/internal/network/wire"
	"ItsBagelBot/app/desktop/internal/oauth"
	"ItsBagelBot/pkg/sandbox"
	shared "ItsBagelBot/pkg/websocket"
)

type Config struct {
	Executable, AuthEndpoint, BridgeEndpoint string
	RootsPEM                                 []byte
}
type Client struct {
	peer                          *peer
	broker                        *broker
	process                       *sandbox.Process
	cancel                        context.CancelFunc
	mu                            sync.Mutex
	streams                       [streamCount]*clientStream
	serial                        uint32
	ready                         chan *wire.Frame
	closeOnce                     sync.Once
	closed, readySeen, configured bool
}

func HelperPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", ErrWorker
	}
	directory := filepath.Dir(executable)
	switch runtime.GOOS {
	case "darwin":
		return filepath.Clean(filepath.Join(directory, "..", "Helpers", "BagelNetworkWorker.app", "Contents", "MacOS", "BagelNetworkWorker")), nil
	case "windows":
		return filepath.Join(directory, "BagelNetworkWorker.exe"), nil
	default:
		return filepath.Join(directory, "bagel-network-worker"), nil
	}
}
func validOrigin(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && !u.ForceQuery
}
func New(ctx context.Context, config Config) (*Client, error) {
	return newClient(ctx, config, nil)
}
func newClient(ctx context.Context, config Config, configure func(*broker)) (*Client, error) {
	return newClientForAttempt(ctx, ctx, config, configure)
}
func newClientForAttempt(ctx, attempt context.Context, config Config, configure func(*broker)) (*Client, error) {
	if !validOrigin(config.AuthEndpoint) || config.BridgeEndpoint != "" && !validOrigin(config.BridgeEndpoint) {
		return nil, ErrProtocol
	}
	child, cancel := context.WithCancel(ctx)
	client := &Client{cancel: cancel, ready: make(chan *wire.Frame, 1)}
	broker, files, err := newBroker(child, nil, [2]string{config.AuthEndpoint, config.BridgeEndpoint})
	if err != nil {
		cancel()
		return nil, err
	}
	client.broker = broker
	if configure != nil {
		configure(broker)
	}
	process, err := sandbox.Start(child, sandbox.Config{Executable: config.Executable, ExtraFiles: files})
	closeFiles(files)
	if err != nil {
		broker.close()
		cancel()
		return nil, err
	}
	client.process = process
	client.peer = newPeer(child, process.Output, process.Input)
	client.peer.receives = parentReceives
	client.peer.handle = client.handle
	broker.send = &client.peer.send
	broker.start()
	go client.peer.run()
	go func() { <-client.peer.done; _ = client.Close() }()
	if err = client.initialize(attempt, config); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}
func (c *Client) initialize(ctx context.Context, config Config) error {
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case message := <-c.ready:
		valid := message.Kind == hello && message.Isolated
		message.ReturnToVTPool()
		if !valid {
			return ErrWorker
		}
	case <-wait.Done():
		return ErrWorker
	case <-c.peer.ctx.Done():
		return ErrWorker
	}
	reply, err := c.peer.call(wait, &wire.Frame{Kind: bootstrap, AuthEndpoint: config.AuthEndpoint, BridgeEndpoint: config.BridgeEndpoint, RootsPem: config.RootsPEM})
	if err != nil {
		return err
	}
	defer reply.ReturnToVTPool()
	if reply.Error != 0 {
		return ErrWorker
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrWorker
	}
	c.configured = true
	return nil
}
func (c *Client) handle(message *wire.Frame) bool {
	switch message.Kind {
	case hello:
		return c.acceptHello(message)
	case openLane:
		c.mu.Lock()
		configured := c.configured && !c.closed
		c.mu.Unlock()
		if !configured {
			c.peer.cancel()
			return false
		}
		c.broker.open(message)
	case closeLane:
		c.broker.closeLane(message.Slot, message.Epoch)
	case cancelRequest:
		// Raw socket opens are already bounded by their own five-second budget.
		// A matching closeLane message fences sockets abandoned by the worker.
	case streamFrame, streamError:
		return c.deliverStream(message)
	default:
		c.peer.cancel()
	}
	return false
}
func (c *Client) acceptHello(message *wire.Frame) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.readySeen {
		c.peer.cancel()
		return false
	}
	c.readySeen = true
	select {
	case c.ready <- message:
		return true
	default:
		c.peer.cancel()
		return false
	}
}
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		streams := c.streams
		select {
		case pending := <-c.ready:
			pending.ReturnToVTPool()
		default:
		}
		c.mu.Unlock()
		c.cancel()
		if c.peer != nil {
			c.peer.cancel()
		}
		if c.process != nil {
			_ = c.process.Close()
		}
		if c.broker != nil {
			c.broker.close()
		}
		for _, stream := range streams {
			if stream != nil {
				_ = stream.Close()
			}
		}
	})
	return nil
}
func (c *Client) Do(ctx context.Context, r oauth.NetworkRequest) (oauth.NetworkResponse, error) {
	reply, err := c.peer.call(ctx, &wire.Frame{Kind: oauthRequest, Path: r.Endpoint, Proof: r.Proof, Token: r.AccessToken, Form: r.Form, Revoke: r.Revoke})
	if err != nil {
		return oauth.NetworkResponse{}, oauth.ErrUnavailable
	}
	defer reply.ReturnToVTPool()
	result := oauth.NetworkResponse{Status: int(reply.Status), Nonce: reply.Nonce}
	if reply.Oauth != nil {
		t := reply.Oauth
		result.Token = oauth.TokenReply{Access: t.Access, Type: t.TokenType, Expires: t.Expires, Refresh: t.Refresh, Grant: t.Grant, Device: t.Device, Error: t.Error}
	}
	if reply.Error != 0 {
		return result, oauthError(reply.Error)
	}
	return result, nil
}
func oauthError(code uint32) error {
	switch code {
	case authentication:
		return oauth.ErrAuthentication
	case protocolError:
		return oauth.ErrProtocol
	default:
		return oauth.ErrUnavailable
	}
}
func socketError(code uint32) error {
	switch code {
	case noError:
		return nil
	case authentication:
		return shared.ErrAuthentication
	case nonceError:
		return shared.ErrNonce
	case protocolError:
		return shared.ErrMessageType
	case closedError:
		return shared.ErrClosed
	default:
		return shared.ErrDial
	}
}
func errorCode(err error) uint32 {
	switch {
	case err == nil:
		return noError
	case errors.Is(err, shared.ErrAuthentication), errors.Is(err, oauth.ErrAuthentication):
		return authentication
	case errors.Is(err, shared.ErrNonce):
		return nonceError
	case errors.Is(err, shared.ErrMessageType), errors.Is(err, oauth.ErrProtocol):
		return protocolError
	default:
		return unavailable
	}
}
func (c *Client) PostAuthorized(ctx context.Context, path string, auth shared.Authorization, contentType string, body []byte) (shared.Reply, error) {
	if auth.Scheme != "DPoP" || auth.Proof == "" {
		return shared.Reply{}, shared.ErrCredentials
	}
	reply, err := c.peer.call(ctx, &wire.Frame{Kind: postRequest, Path: path, Token: auth.Token, Proof: auth.Proof, ContentType: contentType, Body: body})
	if err != nil {
		return shared.Reply{}, shared.ErrDial
	}
	defer reply.ReturnToVTPool()
	return shared.Reply{Status: int(reply.Status), Nonce: reply.Nonce}, socketError(reply.Error)
}
func (c *Client) DialAuthorized(ctx context.Context, path string, auth shared.Authorization) (transport.Conn, shared.Reply, error) {
	if auth.Scheme != "DPoP" || auth.Proof == "" {
		return nil, shared.Reply{}, shared.ErrCredentials
	}
	stream, err := c.reserveStream()
	if err != nil {
		return nil, shared.Reply{}, err
	}
	reply, err := c.peer.call(ctx, &wire.Frame{Kind: dialRequest, Stream: stream.id, Path: path, Token: auth.Token, Proof: auth.Proof})
	if err != nil {
		_ = stream.Close()
		return nil, shared.Reply{}, shared.ErrDial
	}
	defer reply.ReturnToVTPool()
	metadata := shared.Reply{Status: int(reply.Status), Nonce: reply.Nonce}
	if reply.Error != 0 {
		_ = stream.Close()
		return nil, metadata, socketError(reply.Error)
	}
	return stream, metadata, nil
}
func (c *Client) reserveStream() (*clientStream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrWorker
	}
	for i := range c.streams {
		if c.streams[i] == nil {
			c.serial++
			stream := &clientStream{owner: c, id: c.serial, frames: make(chan *wire.Frame, 1), done: make(chan struct{})}
			c.streams[i] = stream
			return stream, nil
		}
	}
	return nil, ErrWorker
}
func (c *Client) deliverStream(message *wire.Frame) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	for _, stream := range c.streams {
		if stream != nil && stream.id == message.Stream {
			select {
			case stream.frames <- message:
				return true
			default:
				c.peer.cancel()
				return false
			}
		}
	}
	return false
}

type clientStream struct {
	owner  *Client
	id     uint32
	frames chan *wire.Frame
	done   chan struct{}
	once   sync.Once
	ack    bool
}

func (s *clientStream) Recv(ctx context.Context, destination *protocol.Message) error {
	if destination == nil {
		return ErrProtocol
	}
	if s.ack {
		if s.owner.peer.send.write(&wire.Frame{Kind: ackStream, Stream: s.id}) != nil {
			return ErrWorker
		}
	}
	select {
	case frame := <-s.frames:
		defer frame.ReturnToVTPool()
		return s.apply(frame, destination)
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return shared.ErrClosed
	case <-s.owner.peer.ctx.Done():
		return ErrWorker
	}
}
func (s *clientStream) apply(frame *wire.Frame, destination *protocol.Message) error {
	if frame.Kind == streamError {
		if frame.Error == noError {
			return ErrProtocol
		}
		return socketError(frame.Error)
	}
	if frame.Message == nil || protocol.Validate(frame.Message) != nil {
		return ErrProtocol
	}
	moveMessage(destination, frame.Message)
	s.ack = true
	return nil
}
func moveMessage(dst, src *protocol.Message) {
	dst.ResetVT()
	dst.Version = src.Version
	dst.Kind = src.Kind
	dst.SessionId = src.SessionId
	dst.BindWithinMs = src.BindWithinMs
	dst.Migration = src.Migration
	dst.WithinMs = src.WithinMs
	dst.Target = src.Target
	dst.Through = src.Through
	dst.Event = src.Event
	src.Event = nil
	dst.Code = src.Code
}
func (s *clientStream) Close() error {
	s.once.Do(func() {
		close(s.done)
		_ = s.owner.peer.send.write(&wire.Frame{Kind: closeStream, Stream: s.id})
		s.owner.mu.Lock()
		for i, slot := range s.owner.streams {
			if slot == s {
				s.owner.streams[i] = nil
			}
		}
		select {
		case frame := <-s.frames:
			frame.ReturnToVTPool()
		default:
		}
		s.owner.mu.Unlock()
	})
	return nil
}
