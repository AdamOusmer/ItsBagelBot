// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package oauth

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net/url"
	"sync"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/dpop"
)

type Status struct {
	Configured bool   `json:"configured"`
	SignedIn   bool   `json:"signedIn"`
	Paired     bool   `json:"paired"`
	Pending    bool   `json:"pending"`
	Error      string `json:"error"`
}

type credentials struct {
	Version  int    `json:"version"`
	Endpoint string `json:"endpoint"`
	Key      []byte `json:"key"`
	Access   string `json:"-"`
	Refresh  string `json:"refresh"`
	Expires  int64  `json:"-"`
	RenewAt  int64  `json:"-"`
	Grant    string `json:"grant"`
	Device   string `json:"device"`
}

type Manager struct {
	config          Config
	client          *tokenClient
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.Mutex
	operation       chan struct{}
	workers         sync.WaitGroup
	record          credentials
	key             *dpop.Key
	status          Status
	pairCancel      context.CancelFunc
	closed          bool
	started         bool
	dirty           bool
	generation      uint64
	signingOut      bool
	notifications   chan lifecycleNotice
	changed         chan struct{}
	needsReady      bool
	readyGeneration uint64
}

func New(parent context.Context, config Config) (*Manager, error) {
	if !validConfig(config) {
		return nil, ErrConfiguration
	}
	config.ProofTargets = append([]ProofTarget(nil), config.ProofTargets...)
	client, err := newTokenClient(config)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	m := &Manager{config: config, client: client, ctx: ctx, cancel: cancel, status: Status{Configured: true}, operation: make(chan struct{}, 1), notifications: make(chan lifecycleNotice, 4), changed: make(chan struct{}, 1)}
	return m, nil
}

func (m *Manager) Status() Status { m.mu.Lock(); defer m.mu.Unlock(); return m.status }

// Start has one lifecycle owner. It restores a protected record and refreshes
// before expiry without touching the physical event socket.
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrStopped
	}
	if m.started {
		return ErrBusy
	}
	m.started = true
	m.workers.Add(2)
	go m.run()
	go m.lifecycle()
	return nil
}

func (m *Manager) run() {
	defer m.workers.Done()
	_ = m.restore()
	for {
		timer := time.NewTimer(m.renewDelay())
		select {
		case <-m.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			_, _ = m.AccessToken(m.ctx)
		case <-m.changed:
			timer.Stop()
		}
	}
}

func (m *Manager) renewDelay() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.record.Refresh == "" {
		return 24 * time.Hour
	}
	if m.dirty {
		return time.Minute
	}
	delay := time.Until(time.Unix(m.record.RenewAt, 0))
	if delay <= 0 {
		return time.Minute
	}
	return delay
}

func (m *Manager) restore() error {
	m.mu.Lock()
	generation := m.generation
	m.mu.Unlock()
	if err := m.lock(m.ctx); err != nil {
		return err
	}
	defer m.unlock()
	raw, err := m.config.Store.Load()
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return m.problem(ErrStorage)
	}
	var record credentials
	if len(raw) > 2048 || codec.Unmarshal(raw, &record) != nil {
		return m.problem(ErrStorage)
	}
	if record.Version == -1 {
		return nil
	}
	key, err := dpop.ParseKey(record.Key)
	if err != nil || !m.validRecord(record) {
		return m.problem(ErrStorage)
	}
	m.mu.Lock()
	m.record = record
	m.key = key
	m.status.Paired = true
	m.needsReady = true
	m.readyGeneration = generation
	m.mu.Unlock()
	if _, err = m.refresh(m.ctx, record, key); err != nil {
		return err
	}
	return nil
}

func (m *Manager) validRecord(r credentials) bool {
	return r.Version == 1 && r.Endpoint == m.config.TokenEndpoint && validSecret(r.Refresh, 1024) && validSecret(r.Grant, 128) && validSecret(r.Device, 128)
}

func (m *Manager) AccessToken(ctx context.Context) (string, error) {
	joined, cancel := m.requestContext(ctx)
	defer cancel()
	if err := m.lock(joined); err != nil {
		return "", err
	}
	defer m.unlock()
	if joined.Err() != nil {
		return "", ErrStopped
	}
	m.mu.Lock()
	record, key := m.record, m.key
	closed, dirty := m.closed, m.dirty
	m.mu.Unlock()
	if closed {
		return "", ErrStopped
	}
	if key == nil || record.Refresh == "" {
		return "", ErrAuthentication
	}
	if dirty {
		if err := m.persist(record); err != nil {
			return "", m.problem(err)
		}
	}
	if time.Now().Unix() < record.RenewAt {
		return record.Access, nil
	}
	return m.refresh(ctx, record, key)
}

func (m *Manager) refresh(ctx context.Context, record credentials, key *dpop.Key) (string, error) {
	joined, cancel := m.requestContext(ctx)
	defer cancel()
	reply, err := m.client.exchange(joined, key, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {record.Refresh}})
	if err == nil && (reply.Grant != record.Grant || reply.Device != record.Device) {
		err = ErrProtocol
	}
	if err == nil {
		err = m.commit(key, reply)
	}
	if err == nil {
		return reply.Access, nil
	}
	if errors.Is(err, ErrAuthentication) {
		_ = m.clear()
		m.mu.Lock()
		generation := m.generation
		m.mu.Unlock()
		m.notify(lifecycleNotice{generation: generation})
		return "", m.problem(err)
	}
	_ = m.problem(err)
	// An early refresh failure must not discard a still-valid access token.
	if time.Now().Unix() < record.Expires {
		return record.Access, nil
	}
	return "", err
}

func (m *Manager) requestContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(m.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}

func (m *Manager) Proof(method, endpoint, token, nonce string) (string, error) {
	if !m.allowedProof(method, endpoint) {
		return "", ErrConfiguration
	}
	m.mu.Lock()
	key, closed := m.key, m.closed
	m.mu.Unlock()
	if key == nil || closed {
		return "", ErrAuthentication
	}
	return key.Proof(method, endpoint, token, nonce)
}

func (m *Manager) allowedProof(method, endpoint string) bool {
	if method == "POST" && endpoint == m.config.TokenEndpoint+RevokePath {
		return true
	}
	for _, target := range m.config.ProofTargets {
		if method == target.Method && endpoint == target.URL {
			return true
		}
	}
	return false
}

func (m *Manager) commit(key *dpop.Key, reply tokenReply) error {
	encoded, err := key.Bytes()
	if err != nil {
		return ErrStorage
	}
	now := time.Now()
	record := credentials{Version: 1, Endpoint: m.config.TokenEndpoint, Key: encoded, Access: reply.Access, Refresh: reply.Refresh, Expires: now.Unix() + reply.Expires, RenewAt: now.Unix() + reply.Expires - renewAdvance(reply.Expires), Grant: reply.Grant, Device: reply.Device}
	// Keep a rotated family in memory even if protected storage temporarily
	// fails; never retry the now-invalid previous refresh token.
	m.mu.Lock()
	m.record = record
	m.key = key
	m.dirty = true
	m.mu.Unlock()
	m.wake()
	return m.persist(record)
}

func (m *Manager) persist(record credentials) error {
	raw, err := codec.Marshal(record)
	if err != nil || m.config.Store.Save(raw) != nil {
		return ErrStorage
	}
	m.mu.Lock()
	m.dirty = false
	m.status.SignedIn = true
	m.status.Paired = true
	m.status.Error = ""
	ready, generation := m.needsReady, m.readyGeneration
	m.needsReady = false
	m.mu.Unlock()
	m.wake()
	if ready {
		m.notify(lifecycleNotice{ready: true, generation: generation})
	}
	return nil
}

func renewAdvance(lifetime int64) int64 {
	// Stable per-response randomness spreads refreshes without per-event work.
	advance := lifetime / 20
	if advance < 30 {
		advance = 30
	}
	var jitter [8]byte
	if _, err := rand.Read(jitter[:]); err == nil {
		advance += int64(binary.LittleEndian.Uint64(jitter[:]) % uint64(advance))
	}
	return advance
}

func (m *Manager) problem(err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case errors.Is(err, ErrStorage):
		m.status.Error = "Protected credential storage is unavailable."
	case errors.Is(err, ErrAuthentication):
		m.status.Error = "Sign in again to authorize this device."
	case errors.Is(err, ErrConfiguration):
		m.status.Error = "Native authorization is not configured."
	default:
		m.status.Error = "Authorization is unavailable. Try again."
	}
	return err
}

func (m *Manager) SignOut() error {
	m.mu.Lock()
	if m.signingOut {
		m.mu.Unlock()
		return ErrBusy
	}
	m.signingOut = true
	m.generation++
	generation := m.generation
	cancel := m.pairCancel
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.signingOut = false; m.mu.Unlock() }()
	if cancel != nil {
		cancel()
	}
	if err := m.lock(context.Background()); err != nil {
		return err
	}
	m.mu.Lock()
	key, access := m.key, m.record.Access
	m.mu.Unlock()
	err := m.clear()
	m.unlock()
	if stopped := m.stopSession(m.ctx, generation); stopped != nil {
		return stopped
	}
	if key != nil && access != "" {
		if m.client.revoke(m.ctx, key, access) != nil && err == nil {
			m.mu.Lock()
			m.status.Error = "Signed out locally. Remote revocation is unavailable."
			m.mu.Unlock()
		}
	}
	return err
}

func (m *Manager) clear() error {
	tombstone, _ := codec.Marshal(struct {
		Version int `json:"version"`
	}{Version: -1})
	written := m.config.Store.Save(tombstone) == nil
	err := m.config.Store.Delete()
	m.mu.Lock()
	m.record = credentials{}
	m.key = nil
	m.dirty = false
	m.status.SignedIn = false
	m.status.Paired = false
	m.status.Error = ""
	m.needsReady = false
	m.mu.Unlock()
	m.wake()
	if err != nil && !written {
		return m.problem(ErrStorage)
	}
	return nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.cancel()
	m.workers.Wait()
	_ = m.lock(context.Background())
	m.unlock()
	return nil
}

func (m *Manager) lock(ctx context.Context) error {
	select {
	case m.operation <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ErrStopped
	}
}
func (m *Manager) unlock() { <-m.operation }

func (m *Manager) wake() {
	select {
	case m.changed <- struct{}{}:
	default:
	}
}

func (m *Manager) Retry() error {
	m.mu.Lock()
	paired := m.status.Paired
	m.mu.Unlock()
	if !paired {
		return m.restore()
	}
	_, err := m.AccessToken(m.ctx)
	return err
}
