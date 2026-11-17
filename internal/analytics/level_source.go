// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/bus"
	"golang.org/x/sync/singleflight"
)

// A paged seed can outlast one RPC deadline; it keeps running so the next request finds it cached.
const levelLoadTimeout = 5 * time.Minute

type Seed func(context.Context) (Tally, error)
type Owned []string

func (o Owned) owns(field string) bool {
	return slices.ContainsFunc(o, func(prefix string) bool {
		return field == prefix || strings.HasPrefix(field, prefix+"|")
	})
}

type Owner struct {
	Service rpc.LevelService
	Owned   Owned
	Seed    Seed
}

type LevelSource struct {
	owner   Owner
	now     func() time.Time
	mu      sync.Mutex
	values  Tally
	expires time.Time
	flight  singleflight.Group
}

type LevelSourceOption func(*LevelSource)

func WithLevelClock(now func() time.Time) LevelSourceOption {
	return func(s *LevelSource) { s.now = now }
}

func NewLevelSource(owner Owner, opts ...LevelSourceOption) *LevelSource {
	s := &LevelSource{owner: owner, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *LevelSource) cached() (Tally, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.values), s.now().Before(s.expires)
}

func (s *LevelSource) Levels(ctx context.Context) (Tally, error) {
	if values, ok := s.cached(); ok {
		return values, nil
	}
	result := s.flight.DoChan("levels", func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), levelLoadTimeout)
		defer cancel()
		return s.load(loadCtx)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case loaded := <-result:
		if loaded.Err != nil {
			return nil, loaded.Err
		}
		return maps.Clone(loaded.Val.(Tally)), nil
	}
}

func (s *LevelSource) load(ctx context.Context) (Tally, error) {
	if values, ok := s.cached(); ok {
		return values, nil
	}
	totals, err := s.owner.Seed(ctx)
	if err != nil {
		return nil, err
	}
	values := Tally{}
	for field, n := range totals {
		if s.owner.Owned.owns(field) && validLevel(field) {
			values[field] = max(n, 0)
		}
	}
	s.mu.Lock()
	s.values, s.expires = values, s.now().Add(time.Minute)
	s.mu.Unlock()
	return values, nil
}

func validLevel(field string) bool {
	metric, dims := catalog.ParseField(field)
	return catalog.Valid(metric, catalog.Level, dims, nil)
}

func (s *LevelSource) Serve(w bus.RPCWiring) error {
	return bus.Serve(w, s.owner.Service.LevelsSubject(), func(ctx context.Context, _ rpc.LevelsRequest) rpc.LevelsReply {
		values, err := s.Levels(ctx)
		if err != nil {
			return rpc.LevelsReply{Refusal: rpc.Refused(rpc.CodeInternal, err.Error())}
		}
		return rpc.LevelsReply{Levels: values}
	})
}
