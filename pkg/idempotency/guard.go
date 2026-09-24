// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency

import (
	"time"

	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

type Handler func(*bus.Message) error

type KeyFunc func(*bus.Message) (string, bool)

type Metrics interface {
	Duplicate()
	FailOpen()
}

type nopMetrics struct{}

func (nopMetrics) Duplicate() {}
func (nopMetrics) FailOpen()  {}

func MessageUUIDKey(m *bus.Message) (string, bool) {
	if m == nil || m.UUID == "" {
		return "", false
	}
	return m.UUID, true
}

type Config struct {
	Store   Store
	Key     KeyFunc
	TTL     time.Duration
	Log     *zap.Logger
	Metrics Metrics
}

func (cfg Config) withDefaults() Config {
	if cfg.Log == nil {
		cfg.Log = zap.NewNop()
	}
	if cfg.Metrics == nil {
		cfg.Metrics = nopMetrics{}
	}
	if cfg.Key == nil {
		cfg.Key = MessageUUIDKey
	}
	return cfg
}

func Guard(cfg Config) func(Handler) Handler {
	guard := cfg.withDefaults()
	return func(next Handler) Handler {
		return func(m *bus.Message) error {
			return guard.guardOne(next, m)
		}
	}
}

func (cfg *Config) guardOne(next Handler, m *bus.Message) error {
	k, ok := cfg.Key(m)
	if !ok {
		return next(m)
	}
	ctx := m.Context()
	seen, err := cfg.Store.Seen(ctx, k, cfg.TTL)
	if err != nil {
		cfg.Metrics.FailOpen()
		cfg.Log.Warn("idempotency guard failing open", zap.String("key", k), zap.Error(err))
		return next(m)
	}
	if seen {
		cfg.Metrics.Duplicate()
		return nil
	}
	if herr := next(m); herr != nil {
		_ = cfg.Store.Release(ctx, k)
		return herr
	}
	return nil
}
