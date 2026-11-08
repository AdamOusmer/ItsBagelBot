// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package transport

import (
	"context"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/pkg/protobuf"
	shared "ItsBagelBot/pkg/websocket"
)

// NewDirectClient is an explicit local-fixture constructor. Production code
// must inject its isolated Native port through NewClient; the import guard
// rejects direct-constructor references in production files.
func NewDirectClient(config Config) (*Client, error) {
	if config.Native != nil {
		return nil, ErrNative
	}
	native, err := shared.NewClient(shared.Config{Endpoint: config.Endpoint, TLSConfig: config.TLSConfig, MaxMessageBytes: protocol.MaxFrameBytes, Subprotocol: protocol.Subprotocol})
	if err != nil {
		return nil, err
	}
	config.Native = &nativeDirect{client: native}
	client, err := NewClient(config)
	if err != nil {
		_ = native.Close()
		return nil, err
	}
	return client, nil
}

type nativeDirect struct{ client *shared.Client }

func (n *nativeDirect) DialAuthorized(ctx context.Context, path string, auth shared.Authorization) (Conn, shared.Reply, error) {
	socket, reply, err := n.client.DialAuthorized(ctx, path, auth)
	if err != nil {
		return nil, reply, err
	}
	return &connection{socket: socket}, reply, nil
}

func (n *nativeDirect) PostAuthorized(ctx context.Context, path string, auth shared.Authorization, contentType string, body []byte) (shared.Reply, error) {
	return n.client.PostAuthorized(ctx, path, auth, contentType, body)
}

func (n *nativeDirect) Close() error { return n.client.Close() }

type connection struct {
	socket *shared.Conn
	buffer [protocol.MaxFrameBytes]byte
}

func (c *connection) Recv(ctx context.Context, message *protocol.Message) error {
	if _, err := protobuf.Check(message); err != nil {
		return err
	}
	n, err := c.socket.Read(ctx, c.buffer[:])
	if err != nil {
		_ = protobuf.Reset(message)
		return err
	}
	if err = protobuf.Unmarshal(c.buffer[:n], message); err != nil {
		_ = c.Close()
		return err
	}
	return nil
}
func (c *connection) Close() error { return c.socket.Close() }
