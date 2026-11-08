// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"

	"ItsBagelBot/app/desktop/internal/network"
	"ItsBagelBot/app/desktop/internal/oauth"
	"ItsBagelBot/pkg/credential"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// AuthService exposes actions and sanitized status only. Endpoint configuration,
// protected credentials, the installation key and token providers stay native.
type AuthService struct {
	mu      sync.Mutex
	config  oauth.Config
	manager *oauth.Manager
	failed  bool
	network *network.Supervisor
}

type protectedDeviceStore struct{ store *credential.Store }

func (s protectedDeviceStore) Load() ([]byte, error) {
	raw, err := s.store.Read("device-session")
	if errors.Is(err, credential.ErrNotFound) {
		return nil, oauth.ErrNotFound
	}
	return raw, err
}
func (s protectedDeviceStore) Save(raw []byte) error { return s.store.Write("device-session", raw) }
func (s protectedDeviceStore) Delete() error         { return s.store.Delete("device-session") }

func nativeAuthConfig(open func(string) error, bridge *BridgeService) oauth.Config {
	authorize := os.Getenv("BAGEL_DESKTOP_AUTHORIZE_ENDPOINT")
	if authorize == "" {
		authorize = "https://dashboard.itsbagelbot.com"
	}
	platform := runtime.GOOS
	if platform == "darwin" {
		platform = "macos"
	}
	config := oauth.Config{AuthorizeEndpoint: authorize, TokenEndpoint: os.Getenv("BAGEL_DESKTOP_AUTH_ENDPOINT"), Platform: platform, DeviceName: "ItsBagelBot on " + platform, OpenBrowser: open, SignedOut: bridge.Disconnect}
	store, err := credential.New("ItsBagelBot.desktop.oauth")
	if err == nil {
		config.Store = protectedDeviceStore{store: store}
	}
	return config
}

func (s *AuthService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.Network == nil {
		client, err := startNativeNetwork(ctx, s.config.TokenEndpoint)
		if err != nil {
			s.failed = true
			return nil
		}
		s.network = client
		s.config.Network = client
	}
	manager, err := oauth.New(ctx, s.config)
	if err != nil {
		if s.network != nil {
			_ = s.network.Close()
			s.network = nil
		}
		s.failed = true
		return nil
	}
	s.manager = manager
	return manager.Start()
}
func startNativeNetwork(ctx context.Context, authEndpoint string) (*network.Supervisor, error) {
	helper, err := network.HelperPath()
	if err != nil {
		return nil, err
	}
	return network.Start(ctx, network.Config{Executable: helper, AuthEndpoint: authEndpoint, BridgeEndpoint: os.Getenv("BAGEL_DESKTOP_BRIDGE_ENDPOINT")})
}

func (s *AuthService) ServiceShutdown() error {
	s.mu.Lock()
	manager := s.manager
	client := s.network
	s.manager = nil
	s.network = nil
	s.mu.Unlock()
	var err error
	if manager != nil {
		err = manager.Close()
	}
	if client != nil {
		_ = client.Close()
	}
	return err
}

func (s *AuthService) Status() oauth.Status {
	s.mu.Lock()
	manager := s.manager
	failed := s.failed
	s.mu.Unlock()
	if manager != nil {
		return manager.Status()
	}
	if failed {
		return oauth.Status{Error: "Native authorization or its required secure network sandbox is unavailable."}
	}
	return oauth.Status{}
}

func (s *AuthService) StartPairing() error {
	s.mu.Lock()
	manager := s.manager
	s.mu.Unlock()
	if manager == nil {
		return oauth.ErrConfiguration
	}
	return manager.StartPairing()
}

func (s *AuthService) SignOut() error {
	s.mu.Lock()
	manager := s.manager
	s.mu.Unlock()
	if manager == nil {
		return nil
	}
	return manager.SignOut()
}

func (s *AuthService) Retry() error {
	s.mu.Lock()
	manager := s.manager
	s.mu.Unlock()
	if manager == nil {
		return oauth.ErrConfiguration
	}
	return manager.Retry()
}
