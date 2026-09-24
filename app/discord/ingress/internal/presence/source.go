// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package presence

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

type Fetch func(ctx context.Context) (total int, err error)

type Source struct {
	Fetch Fetch
	Log   *zap.Logger

	mu   sync.Mutex
	last string
}

func (s *Source) Refresh(ctx context.Context) (string, bool) {
	total, err := s.Fetch(ctx)
	if err != nil {
		s.log().Warn("discord presence count fetch failed", zap.Error(err))
		return "", false
	}

	name := activityName(total)
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == s.last {
		return "", false
	}
	s.last = name
	return name, true
}

func (s *Source) Forget() {
	s.mu.Lock()
	s.last = ""
	s.mu.Unlock()
}

func (s *Source) log() *zap.Logger {
	if s.Log != nil {
		return s.Log
	}
	return zap.NewNop()
}
