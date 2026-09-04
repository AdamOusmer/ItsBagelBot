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

// CloseCode reports the close code behind err. websocket.CloseStatus returns
// -1 when err is not a close frame; that sentinel is folded into 0 ("no
// code") here so no caller has to know the library's convention.
func (w wsConn) CloseCode(err error) int {
	code := websocket.CloseStatus(err)
	if code < 0 {
		return 0
	}
	return int(code)
}

func (w wsConn) Close() error {
	return w.c.Close(websocket.StatusNormalClosure, "")
}
