// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package main

import (
	"context"

	"ItsBagelBot/app/desktop/internal/overlay"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Only these desktop bindings can write scenes. The OBS listener has no write API.
type OverlayService struct {
	source *overlay.Source
	server *overlay.Server
}

func (s *OverlayService) SourceURL() string          { return s.source.SourceURL() }
func (s *OverlayService) Current() overlay.Scene     { return s.source.Current() }
func (s *OverlayService) Publish(scene string) error { return s.source.Publish(scene) }

func (s *OverlayService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	return s.server.Start()
}
func (s *OverlayService) ServiceShutdown() error          { return s.server.Close() }
func (s *OverlayService) ResetSourceURL() (string, error) { return s.source.ResetSourceURL() }
