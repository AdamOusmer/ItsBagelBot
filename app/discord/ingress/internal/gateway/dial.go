// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
)

// DialWS opens a Discord gateway WebSocket.
func DialWS(ctx context.Context, rawURL string) (Conn, error) {
	c, _, err := websocket.Dial(ctx, rawURL, &websocket.DialOptions{
		HTTPClient: http.DefaultClient,
	})
	if err != nil {
		return nil, err
	}
	return wsConn{c: c}, nil
}

type wsConn struct {
	c *websocket.Conn
}

func (w wsConn) Read(ctx context.Context) ([]byte, error) {
	_, data, err := w.c.Read(ctx)
	return data, err
}

func (w wsConn) Write(ctx context.Context, data []byte) error {
	return w.c.Write(ctx, websocket.MessageText, data)
}

func (w wsConn) Close() error {
	return w.c.Close(websocket.StatusNormalClosure, "")
}
