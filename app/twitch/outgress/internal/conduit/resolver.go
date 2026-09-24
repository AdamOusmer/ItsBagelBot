// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package conduit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

type reply struct {
	ConduitID string `json:"conduit_id"`
	Error     string `json:"error"`
}

type Resolver struct {
	nc       *nats.Conn
	subject  string
	fallback string
	ttl      time.Duration
	log      *zap.Logger

	mu        sync.Mutex
	cached    string
	fetchedAt time.Time
}

func New(nc *nats.Conn, subject, fallback string, ttl time.Duration, log *zap.Logger) *Resolver {
	return &Resolver{
		nc:       nc,
		subject:  subject,
		fallback: fallback,
		ttl:      ttl,
		log:      log,
	}
}

func (r *Resolver) Get(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cached != "" && time.Since(r.fetchedAt) < r.ttl {
		return r.cached, nil
	}

	res, err := bus.RequestJSON[reply](ctx, r.nc, r.subject, struct{}{})
	if err == nil && res.ConduitID != "" && res.Error == "" {
		r.cached = res.ConduitID
		r.fetchedAt = time.Now()
		return r.cached, nil
	}

	if err != nil {
		r.log.Warn("conduit rpc failed", zap.String("subject", r.subject), zap.Error(err))
	} else {
		r.log.Warn("conduit rpc returned empty or errored reply",
			zap.String("conduit_id", res.ConduitID),
			zap.String("error", res.Error))
	}

	if r.cached != "" {
		r.log.Warn("serving stale conduit id", zap.String("conduit_id", r.cached))
		return r.cached, nil
	}

	if r.fallback != "" {
		r.log.Warn("using env fallback conduit id", zap.String("conduit_id", r.fallback))
		return r.fallback, nil
	}

	return "", fmt.Errorf("conduit id unavailable: ingress rpc unreachable and no fallback configured")
}

func (r *Resolver) Invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cached = ""
	r.fetchedAt = time.Time{}
}
