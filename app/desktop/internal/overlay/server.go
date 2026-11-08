// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlay

import (
	"io/fs"
	"net"
	"sync"
	"time"

	"ItsBagelBot/pkg/codec"

	"github.com/gofiber/fiber/v3"
)

const Address = "127.0.0.1:18947"
const sourceOrigin = "http://" + Address
const readWait = 20 * time.Second

type Server struct {
	source  *Source
	assets  map[string]asset
	app     *fiber.App
	slots   chan struct{}
	stopped chan struct{}
	once    sync.Once
	timers  sync.Pool
	done    chan error
}

func NewServer(source *Source, files fs.FS) (*Server, error) {
	assets, err := loadAssets(files)
	if err != nil {
		return nil, err
	}
	s := &Server{source: source, assets: assets, slots: make(chan struct{}, 16), stopped: make(chan struct{}), done: make(chan error, 1)}
	s.timers.New = func() any { timer := time.NewTimer(time.Hour); timer.Stop(); return timer }
	s.app = fiber.New(fiber.Config{ReadTimeout: 5 * time.Second, WriteTimeout: 25 * time.Second,
		IdleTimeout: 30 * time.Second, BodyLimit: 1024, Concurrency: 64,
		JSONEncoder: codec.Marshal, JSONDecoder: codec.Unmarshal})
	s.app.Use(boundary)
	s.app.Get("/state", s.read)
	s.app.Get("/*", s.serveAsset)
	return s, nil
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp4", Address)
	if err != nil {
		return err
	}
	ready := make(chan struct{})
	go func() {
		s.done <- s.app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true,
			BeforeServeFunc: func(_ *fiber.App) error { close(ready); return nil }})
	}()
	select {
	case <-ready:
		return nil
	case err := <-s.done:
		_ = ln.Close()
		return err
	}
}

func (s *Server) Close() error {
	s.once.Do(func() { close(s.stopped) })
	return s.app.ShutdownWithTimeout(3 * time.Second)
}

func (s *Server) read(c fiber.Ctx) error {
	current := s.source.current.Load()
	if !current.authorized(c.Get("Authorization")) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return c.SendStatus(fiber.StatusTooManyRequests)
	}
	if c.Query("after") == current.cursor && !s.wait(current.changed) {
		return c.SendStatus(fiber.StatusServiceUnavailable)
	}
	current = s.source.current.Load()
	if !current.authorized(c.Get("Authorization")) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return c.Send(current.wire)
}

func (s *Server) wait(changed <-chan struct{}) bool {
	timer := s.timers.Get().(*time.Timer)
	timer.Reset(readWait)
	defer func() { timer.Stop(); s.timers.Put(timer) }()
	select {
	case <-changed:
		return true
	case <-timer.C:
		return true
	case <-s.stopped:
		return false
	}
}
