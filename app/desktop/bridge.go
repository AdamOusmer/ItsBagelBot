// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge"
	"ItsBagelBot/app/desktop/internal/bridge/session"
	"ItsBagelBot/app/desktop/internal/bridge/transport"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type ConnectionStatus struct {
	Configured bool   `json:"configured"`
	State      string `json:"state"`
	Error      string `json:"error"`
	Attempts   int    `json:"attempts"`
}

// BridgeService is a status/action facade. Only native account pairing may
// install credentials and a consumer; no bound method accepts either secret.
type BridgeService struct {
	mu       sync.Mutex
	lifetime context.Context
	runtime  *bridge.Runtime
	channel  io.Closer
	cancel   context.CancelFunc
}

func (s *BridgeService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lifetime = ctx
	return nil
}
func (s *BridgeService) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stop()
}
func (s *BridgeService) stop() error {
	if s.runtime == nil {
		return nil
	}
	s.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.runtime.Wait(ctx); err != nil {
		return err
	}
	s.runtime = nil
	s.cancel = nil
	if s.channel != nil {
		channel := s.channel
		s.channel = nil
		return channel.Close()
	}
	return nil
}

// install is deliberately unexported: Wails cannot bind account tokens or
// replace the native event consumer.
func (s *BridgeService) install(config bridge.Config) error {
	return s.installConfig(config, nil)
}

// installNative is called only by native account pairing. The account lifetime
// owns the shared client; individual workers close only their own event socket.
func (s *BridgeService) installNative(endpoint string, tokens bridge.TokenProvider, consumer bridge.Consumer, native transport.Native) error {
	proof, ok := tokens.(interface {
		Proof(string, string, string, string) (string, error)
	})
	if !ok {
		return bridge.ErrConfiguration
	}
	client, err := transport.NewClient(transport.Config{Endpoint: endpoint, Proof: proof.Proof, Native: native})
	if err != nil {
		return err
	}
	err = s.installConfig(bridge.Config{Endpoint: endpoint, Tokens: tokens, Consumer: consumer, Dial: client.Dial, Control: client}, client)
	if err != nil {
		_ = client.Close()
	}
	return err
}

func (s *BridgeService) installConfig(config bridge.Config, channel io.Closer) error {
	next, err := bridge.New(config)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifetime == nil || s.lifetime.Err() != nil {
		return errors.New("connection service is not running")
	}
	if err = s.stop(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(s.lifetime)
	s.runtime = next
	s.channel = channel
	s.cancel = cancel
	go next.Run(ctx)
	if err = next.Connect(ctx); err != nil {
		return errors.Join(err, s.stop())
	}
	return nil
}
func (s *BridgeService) Status() ConnectionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtime == nil {
		return ConnectionStatus{State: "Not configured"}
	}
	view := s.runtime.Status()
	return ConnectionStatus{Configured: true, State: connectionState(view.State), Error: connectionProblem(view.Problem), Attempts: view.Attempts}
}
func (s *BridgeService) Retry() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtime == nil {
		return errors.New("native account pairing is required")
	}
	return s.runtime.Retry(s.lifetime)
}
func (s *BridgeService) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stop()
}
func connectionState(state session.State) string {
	switch state {
	case session.Connecting:
		return "Connecting"
	case session.Binding:
		return "Binding account"
	case session.Online:
		return "Online"
	case session.Migrating:
		return "Moving connection"
	case session.Reconnecting:
		return "Reconnecting"
	case session.Failed:
		return "Unable to connect"
	case session.AuthenticationRequired:
		return "Sign in again"
	default:
		return "Disconnected"
	}
}
func connectionProblem(problem session.Problem) string {
	switch problem {
	case session.ConnectionFailed:
		return "Unable to connect. Retry when your connection is available."
	case session.MigrationFailed:
		return "Migration failed. The existing connection remains active."
	case session.Unauthorized:
		return "Your session needs authentication. Sign in again."
	case session.ProtocolFailure:
		return "The connection protocol could not be processed."
	default:
		return ""
	}
}
