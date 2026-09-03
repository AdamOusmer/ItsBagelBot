// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// Handler receives one dispatched gateway event.
type Handler interface {
	Dispatch(ctx context.Context, eventType string, raw []byte) error
	Ready(ctx context.Context, applicationID, botUserID string) error
}

// Conn is one WebSocket. Tests inject a scripted implementation.
type Conn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
	Close() error
}

// Dial opens a gateway WebSocket.
type Dial func(ctx context.Context, url string) (Conn, error)

// Session is the long-lived Discord gateway loop: Hello, Identify,
// heartbeat, dispatch. A dropped socket reconnects until ctx is cancelled.
type Session struct {
	Token  string
	Dial   Dial
	Handle Handler
	Log    *zap.Logger
	URL    string
}

// Run identifies and pumps events until ctx is done.
func (s Session) Run(ctx context.Context) error {
	if s.Token == "" {
		return fmt.Errorf("dingress: empty bot token")
	}
	if s.Dial == nil {
		return fmt.Errorf("dingress: nil dial")
	}
	url := s.URL
	if url == "" {
		url = gatewayURL
	}
	for {
		err := s.oneSocket(ctx, url)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.log().Warn("discord gateway socket ended; reconnecting", zap.Error(err))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (s Session) oneSocket(ctx context.Context, url string) error {
	conn, err := s.Dial(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return s.pump(ctx, conn)
}

func (s Session) pump(ctx context.Context, conn Conn) error {
	beats := make(chan struct{}, 1)
	defer close(beats)
	for {
		raw, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var pkt packet
		if err := codec.Unmarshal(raw, &pkt); err != nil {
			return fmt.Errorf("dingress: decode gateway packet: %w", err)
		}
		if pkt.Op == opHello {
			if err := s.onHello(ctx, conn, pkt, beats); err != nil {
				return err
			}
			continue
		}
		if pkt.Op == opReconnect || pkt.Op == opInvalidSession {
			return fmt.Errorf("dingress: gateway requested reconnect (op %d)", pkt.Op)
		}
		if pkt.Op != opDispatch {
			continue
		}
		if err := s.onDispatch(ctx, pkt); err != nil {
			s.log().Warn("discord dispatch failed", zap.String("t", pkt.T), zap.Error(err))
		}
	}
}

func (s Session) onHello(ctx context.Context, conn Conn, pkt packet, beats chan struct{}) error {
	var hello helloData
	if err := codec.Unmarshal(pkt.D, &hello); err != nil {
		return err
	}
	if err := writeJSON(ctx, conn, identifyBody(s.Token)); err != nil {
		return err
	}
	go s.heartbeat(ctx, conn, hello.HeartbeatInterval, beats)
	return nil
}

func (s Session) onDispatch(ctx context.Context, pkt packet) error {
	if pkt.T == eventReady {
		var ready readyData
		if err := codec.Unmarshal(pkt.D, &ready); err != nil {
			return err
		}
		if s.Handle == nil {
			return nil
		}
		return s.Handle.Ready(ctx, ready.Application.ID, ready.User.ID)
	}
	if s.Handle == nil {
		return nil
	}
	return s.Handle.Dispatch(ctx, pkt.T, pkt.D)
}

func (s Session) heartbeat(ctx context.Context, conn Conn, intervalMS int, stop <-chan struct{}) {
	if intervalMS <= 0 {
		return
	}
	t := time.NewTicker(time.Duration(intervalMS) * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-t.C:
			if err := writeJSON(ctx, conn, heartbeatBody(nil)); err != nil {
				return
			}
		}
	}
}

func (s Session) log() *zap.Logger {
	if s.Log != nil {
		return s.Log
	}
	return zap.NewNop()
}

func writeJSON(ctx context.Context, conn Conn, v any) error {
	raw, err := codec.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, raw)
}
