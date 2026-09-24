// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"net/http"

	"github.com/coder/websocket"
)

// The library's 32 KiB default is smaller than one GUILD_CREATE and loops the bot on reconnects.
const maxGatewayMessage = 16 << 20

func DialWS(ctx context.Context, rawURL string) (Conn, error) {
	c, _, err := websocket.Dial(ctx, rawURL, &websocket.DialOptions{
		HTTPClient: http.DefaultClient,
	})
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(maxGatewayMessage)
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

func (w wsConn) CloseCode(err error) int {
	code := websocket.CloseStatus(err)
	if code < 0 {
		return 0
	}
	return int(code)
}

func (w wsConn) CloseReason(err error) string {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Reason
	}
	return ""
}

// Must not be 1000 or 1001: Discord then ends the session and the next resume spends an IDENTIFY.
const reconnectingClose = websocket.StatusCode(4000)

func (w wsConn) Close() error {
	return w.c.Close(reconnectingClose, "reconnecting")
}

func (w wsConn) Shutdown() error {
	return w.c.Close(websocket.StatusNormalClosure, "")
}
