// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package transport adapts the app's typed contract to bare shared WSS and native
// HTTPS controls. All session and recovery policy stays in the supervisor.
package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/pkg/protobuf"
	shared "ItsBagelBot/pkg/websocket"
)

var ErrAuthentication = shared.ErrAuthentication
var ErrEndpoint = shared.ErrEndpoint
var ErrNative = errors.New("bridge: isolated native networking required")
var ErrControlResponse = errors.New("bridge: native control requires HTTP 204")

type Conn interface {
	Recv(context.Context, *protocol.Message) error
	Close() error
}
type Request struct{ Endpoint, Token string }
type BindRequest struct {
	SessionID, Token, Migration string
	After                       uint64
}
type MigrationRequest struct {
	SessionID, Token string
	After            uint64
}
type Dialer func(context.Context, Request) (Conn, error)

// Native is the application-owned port implemented by the isolated networking
// worker proxy. Received messages are typed; raw network frames stay outside
// the credential-owning process. Proofs are created by the caller before I/O.
type Native interface {
	DialAuthorized(context.Context, string, shared.Authorization) (Conn, shared.Reply, error)
	PostAuthorized(context.Context, string, shared.Authorization, string, []byte) (shared.Reply, error)
	Close() error
}

type Config struct {
	Endpoint  string
	Native    Native
	TLSConfig *tls.Config
	Proof     func(method, endpoint, token, nonce string) (string, error)
}

// Client owns protocol adaptation and proof/nonce policy around an explicit
// native port. Account binding occurs only after the supervisor receives welcome.
type Client struct {
	endpoint string
	native   Native
	proof    func(method, endpoint, token, nonce string) (string, error)
	mu       sync.Mutex
	nonces   [3]string
}

func NewClient(config Config) (*Client, error) {
	if !validEndpoint(config.Endpoint) {
		return nil, ErrEndpoint
	}
	if config.Native == nil {
		return nil, ErrNative
	}
	return &Client{endpoint: config.Endpoint, native: config.Native, proof: config.Proof}, nil
}

func validEndpoint(value string) bool {
	endpoint, err := url.Parse(value)
	if err != nil {
		return false
	}
	return endpoint.Scheme == "https" && endpoint.Hostname() != "" && endpoint.User == nil && endpoint.Path == "" && endpoint.RawQuery == "" && !endpoint.ForceQuery && endpoint.Fragment == "" && !strings.HasSuffix(endpoint.Host, ":")
}

func (c *Client) Dial(ctx context.Context, request Request) (Conn, error) {
	if request.Endpoint != "" && request.Endpoint != c.endpoint {
		return nil, ErrEndpoint
	}
	return c.dial(ctx, request.Token)
}
func (c *Client) Bind(ctx context.Context, request BindRequest) error {
	body := &protocol.BindAccount{SessionId: request.SessionID, Migration: request.Migration, After: request.After}
	if err := protocol.ValidateBind(body); err != nil {
		return err
	}
	return c.control(ctx, protocol.BindPath, request.Token, body)
}
func (c *Client) RequestMigration(ctx context.Context, request MigrationRequest) error {
	body := &protocol.MigrationRequest{SessionId: request.SessionID, After: request.After}
	if err := protocol.ValidateMigrationRequest(body); err != nil {
		return err
	}
	return c.control(ctx, protocol.MigrationPath, request.Token, body)
}
func (c *Client) control(ctx context.Context, path, token string, message protobuf.Message) error {
	size, err := protobuf.Size(message)
	if err != nil {
		return err
	}
	if size > protocol.MaxFrameBytes {
		return shared.ErrTooLarge
	}
	buffer := make([]byte, size)
	n, err := protobuf.MarshalTo(buffer, message)
	if err != nil {
		return err
	}
	status, err := c.post(ctx, path, token, buffer[:n])
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return ErrControlResponse
	}
	return nil
}
func (c *Client) Close() error { return c.native.Close() }

func (c *Client) authorization(method, path, token string) (shared.Authorization, error) {
	if c.proof == nil {
		return shared.Authorization{Scheme: "Bearer", Token: token}, nil
	}
	c.mu.Lock()
	nonce := c.nonces[nonceSlot(path)]
	c.mu.Unlock()
	proof, err := c.proof(method, c.endpoint+path, token, nonce)
	if err != nil {
		return shared.Authorization{}, err
	}
	return shared.Authorization{Scheme: "DPoP", Token: token, Proof: proof}, nil
}

func nonceSlot(path string) int {
	switch path {
	case protocol.BindPath:
		return 1
	case protocol.MigrationPath:
		return 2
	default:
		return 0
	}
}

func (c *Client) rememberNonce(path, nonce string) {
	c.mu.Lock()
	c.nonces[nonceSlot(path)] = nonce
	c.mu.Unlock()
}

func (c *Client) dial(ctx context.Context, token string) (Conn, error) {
	for attempt := 0; attempt < 2; attempt++ {
		auth, err := c.authorization(http.MethodGet, protocol.EventsPath, token)
		if err != nil {
			return nil, err
		}
		conn, reply, err := c.native.DialAuthorized(ctx, protocol.EventsPath, auth)
		if err == nil && conn == nil {
			return nil, shared.ErrRejected
		}
		if !errors.Is(err, shared.ErrNonce) {
			return conn, err
		}
		c.rememberNonce(protocol.EventsPath, reply.Nonce)
	}
	return nil, shared.ErrRejected
}

func (c *Client) post(ctx context.Context, path, token string, body []byte) (int, error) {
	for attempt := 0; attempt < 2; attempt++ {
		auth, err := c.authorization(http.MethodPost, path, token)
		if err != nil {
			return 0, err
		}
		reply, err := c.native.PostAuthorized(ctx, path, auth, "application/protobuf", body)
		if !errors.Is(err, shared.ErrNonce) {
			return reply.Status, err
		}
		c.rememberNonce(path, reply.Nonce)
	}
	return 0, shared.ErrRejected
}
