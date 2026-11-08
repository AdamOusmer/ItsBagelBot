// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/dpop"
	"github.com/gofiber/fiber/v3"
)

const pairingTimeout = 3 * time.Minute

type pairing struct {
	key                       *dpop.Key
	state, verifier, redirect string
	listener                  net.Listener
	app                       *fiber.App
	result                    chan string
	generation                uint64
	consumed                  atomic.Bool
}

func randomText(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", ErrUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func newPairing() (*pairing, error) {
	key, err := dpop.NewKey()
	if err != nil {
		return nil, ErrUnavailable
	}
	state, err := randomText(32)
	if err != nil {
		return nil, err
	}
	verifier, err := randomText(32)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, ErrUnavailable
	}
	p := &pairing{key: key, state: state, verifier: verifier, listener: listener, result: make(chan string, 1)}
	p.redirect = "http://127.0.0.1:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port) + "/callback"
	p.app = p.callbackApp()
	return p, nil
}

func (p *pairing) callbackApp() *fiber.App {
	app := fiber.New(fiber.Config{ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, BodyLimit: 4096, Concurrency: 8, JSONEncoder: codec.Marshal, JSONDecoder: codec.Unmarshal})
	app.Use(func(c fiber.Ctx) error {
		c.Set("Cache-Control", "no-store")
		c.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Set("Referrer-Policy", "no-referrer")
		if string(c.Request().Host()) != p.listener.Addr().String() {
			return c.SendStatus(fiber.StatusForbidden)
		}
		return c.Next()
	})
	app.Get("/callback", p.callback)
	return app
}

func (p *pairing) callback(c fiber.Ctx) error {
	if !sameSecret(c.Query("state"), p.state) {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	code := c.Query("code")
	if c.Query("error") != "" {
		code = ""
	} else if !validSecret(code, 512) {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	if !p.consumed.CompareAndSwap(false, true) {
		return c.SendStatus(fiber.StatusConflict)
	}
	// Fiber query strings borrow pooled request storage. The exchange worker
	// owns its code after this request returns.
	p.result <- strings.Clone(code)
	return c.SendString("Device approval received. You can return to ItsBagelBot.")
}

func sameSecret(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (p *pairing) authorizeURL(config Config) string {
	challenge := sha256.Sum256([]byte(p.verifier))
	q := url.Values{"client_id": {ClientID}, "response_type": {"code"}, "redirect_uri": {p.redirect}, "state": {p.state}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "dpop_jkt": {p.key.Thumbprint()}, "device_name": {config.DeviceName}, "platform": {config.Platform}}
	return config.AuthorizeEndpoint + AuthorizePath + "?" + q.Encode()
}

func (m *Manager) StartPairing() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrStopped
	}
	if !m.started || m.signingOut {
		return ErrStopped
	}
	if m.status.Pending {
		return ErrBusy
	}
	p, err := newPairing()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(m.ctx, pairingTimeout)
	m.generation++
	p.generation = m.generation
	m.pairCancel = cancel
	m.status.Pending = true
	m.status.Error = ""
	m.workers.Add(1)
	go m.pair(ctx, cancel, p)
	return nil
}

func (m *Manager) pair(ctx context.Context, cancel context.CancelFunc, p *pairing) {
	defer m.workers.Done()
	defer cancel()
	defer p.close()
	defer func() { m.mu.Lock(); m.status.Pending = false; m.pairCancel = nil; m.mu.Unlock() }()
	done := make(chan error, 1)
	go func() { done <- p.app.Listener(p.listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
	if m.config.OpenBrowser(p.authorizeURL(m.config)) != nil {
		_ = m.problem(ErrUnavailable)
		return
	}
	select {
	case code := <-p.result:
		if code == "" {
			_ = m.problem(ErrAuthentication)
			return
		}
		if err := m.finishPairing(ctx, p, code); err != nil && ctx.Err() == nil {
			_ = m.problem(err)
		}
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			_ = m.problem(ErrUnavailable)
		}
	case <-done:
		_ = m.problem(ErrUnavailable)
	}
}

func (m *Manager) finishPairing(ctx context.Context, p *pairing, code string) error {
	reply, err := m.client.exchange(ctx, p.key, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {p.verifier}, "redirect_uri": {p.redirect}})
	if err != nil {
		return err
	}
	// Retain the old credential family until all old account workers are joined.
	if err = m.stopSession(ctx, p.generation); err != nil {
		return err
	}
	if err = m.lock(ctx); err != nil {
		return err
	}
	defer m.unlock()
	m.mu.Lock()
	valid := m.generation == p.generation && !m.signingOut
	m.mu.Unlock()
	if ctx.Err() != nil || !valid {
		return ErrStopped
	}
	m.mu.Lock()
	m.needsReady = true
	m.readyGeneration = p.generation
	m.mu.Unlock()
	if err = m.commit(p.key, reply); err != nil {
		return err
	}
	return nil
}

func (p *pairing) close() {
	_ = p.listener.Close()
	_ = p.app.ShutdownWithTimeout(time.Second)
}
