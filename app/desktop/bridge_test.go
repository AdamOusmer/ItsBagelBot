// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge"
	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/bridge/transport"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type nativeControl struct{}

func (nativeControl) Bind(context.Context, bridge.BindRequest) error                  { return nil }
func (nativeControl) RequestMigration(context.Context, bridge.MigrationRequest) error { return nil }

type nativeTokens struct{}

func (nativeTokens) AccessToken(context.Context) (string, error) { return "native-secret", nil }

type nativeConsumer struct{}

func (nativeConsumer) Apply(*protocol.Event) error { return nil }

func TestBridgeNativeLifecycle(t *testing.T) {
	service := &BridgeService{}
	if service.Status().Configured {
		t.Fatal("unpaired app must not connect")
	}
	if service.Retry() == nil {
		t.Fatal("unpaired retry must fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	config := bridge.Config{Endpoint: "https://bridge.example", Control: nativeControl{}, Tokens: nativeTokens{}, Consumer: nativeConsumer{}, Dial: func(ctx context.Context, _ transport.Request) (transport.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	if err := service.install(config); err != nil {
		t.Fatal(err)
	}
	if !service.Status().Configured {
		t.Fatal("native install did not register runtime")
	}
	old := service.runtime
	if err := service.install(bridge.Config{}); !errors.Is(err, bridge.ErrConfiguration) {
		t.Fatal("invalid replacement accepted")
	}
	if service.runtime != old {
		t.Fatal("invalid config displaced active native session")
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if service.Status().Configured {
		t.Fatal("shutdown retained session")
	}
}

type nativeChannel struct {
	workerFinished atomic.Bool
	closes         atomic.Int32
}

func (c *nativeChannel) Close() error {
	c.closes.Add(1)
	if !c.workerFinished.Load() {
		return errors.New("channel closed before worker joined")
	}
	return nil
}

func nativeOwnedConfig(channel *nativeChannel, started chan<- struct{}) bridge.Config {
	return bridge.Config{
		Endpoint: "https://bridge.example", Control: nativeControl{}, Tokens: nativeTokens{}, Consumer: nativeConsumer{},
		Dial: func(ctx context.Context, _ transport.Request) (transport.Conn, error) {
			close(started)
			<-ctx.Done()
			channel.workerFinished.Store(true)
			return nil, ctx.Err()
		},
	}
}

func awaitNativeWorker(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("native worker did not start")
	}
}

func TestBridgeReplacementJoinsBeforeClosingAccountChannel(t *testing.T) {
	service := &BridgeService{}
	if err := service.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.ServiceShutdown() })
	old, next := &nativeChannel{}, &nativeChannel{}
	oldStarted, nextStarted := make(chan struct{}), make(chan struct{})
	if err := service.installConfig(nativeOwnedConfig(old, oldStarted), old); err != nil {
		t.Fatal(err)
	}
	awaitNativeWorker(t, oldStarted)
	if err := service.install(bridge.Config{}); !errors.Is(err, bridge.ErrConfiguration) {
		t.Fatal("invalid replacement accepted")
	}
	if old.closes.Load() != 0 {
		t.Fatal("invalid replacement closed the active account channel")
	}
	if err := service.installConfig(nativeOwnedConfig(next, nextStarted), next); err != nil {
		t.Fatal(err)
	}
	awaitNativeWorker(t, nextStarted)
	if old.closes.Load() != 1 || next.closes.Load() != 0 {
		t.Fatal("replacement did not transfer channel ownership")
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if next.closes.Load() != 1 {
		t.Fatal("shutdown retained the account channel")
	}
}
