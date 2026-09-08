// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"net/http"

	"github.com/coder/websocket"
)

// maxGatewayMessage is the per-message read cap handed to the websocket
// library. Its default is 32 KiB, which is smaller than a single GUILD_CREATE:
// on 2026-09-04 the live ingress logged "message too big: read limited at
// 32769 bytes" right after READY and resumed every 2s forever, so the bot
// never served an event. Discord documents no upper bound; a fully populated
// guild (channels + roles + presences + voice states) runs to several MiB,
// and GUILD_MEMBERS_CHUNK can be larger. 16 MiB is the cap discord.js and
// serenity effectively run with (no limit) rounded to something that still
// bounds a hostile frame.
const maxGatewayMessage = 16 << 20

// DialWS opens a Discord gateway WebSocket.
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

// CloseReason reports the human text Discord attached to the close frame
// behind err, empty when err is not a close frame at all.
//
// The code alone is not the fault. Discord reuses 4000 ("Unknown error") for
// several unrelated conditions and puts the actionable half in the reason:
// "Session is no longer valid." and "Heartbeat ACK not received." arrive as
// the same number. The socket-end log threw this string away, which is why a
// 24h sample of 21,575 identical socket-end lines (2026-09-07) named no
// cause at all. See sessionEnd.
func (w wsConn) CloseReason(err error) string {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Reason
	}
	return ""
}

// reconnectingClose is the code this client sends when it closes a socket it
// intends to come straight back on.
//
// It is deliberately NOT StatusNormalClosure. Discord documents 1000 and 1001
// from a client as "terminate the session", not "the transport went away": the
// session is discarded and the next connect must IDENTIFY. Every teardown path
// here sent 1000 (the deferred close in oneSocket, socket.writeFailed), so the
// op 6 RESUME that followed was answered with op 9 d:false, resumeState
// invalidated, and a full IDENTIFY went out -- against the 1000/day allowance
// that already got this token reset once (see budget.go). The 24h sample of
// 2026-09-07 was 21,575 reconnects on one process, every one of them spending
// an identify and dropping whatever Discord had buffered.
//
// 4000-4999 is RFC 6455's private-use range, which coder/websocket accepts
// from Close (validWireCloseCode in close.go admits 3000-4999) and which
// Discord treats as an abnormal transport close, leaving the session
// resumable. CloseNow would also avoid the 1000 but sends no frame at all,
// which leaves the peer to time the session out rather than being told.
const reconnectingClose = websocket.StatusCode(4000)

// Close ends the socket in a way that leaves the Discord session resumable.
// It is the close every reconnect path takes; see reconnectingClose.
func (w wsConn) Close() error {
	return w.c.Close(reconnectingClose, "reconnecting")
}

// Shutdown ends the socket for good, with the protocol's own 1000. This is
// the one path where telling Discord to discard the session is correct: the
// process is stopping and nothing is coming back to resume it.
func (w wsConn) Shutdown() error {
	return w.c.Close(websocket.StatusNormalClosure, "")
}
