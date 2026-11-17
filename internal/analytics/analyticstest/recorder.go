// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analyticstest

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"

	"go.uber.org/zap"
)

type Capture struct {
	Recorder *analytics.Recorder
	server   *valkeytest.Server
}

func New(t *testing.T) *Capture {
	t.Helper()
	server := valkeytest.New(t)
	server.Handle("HINCRBY", hincrby)
	server.Handle("EXPIREAT", func(*valkeytest.Server, valkeytest.Args) valkeytest.Reply { return valkeytest.Int(1) })
	server.Handle("EVAL", confirmedBatch)
	c := &Capture{server: server}
	c.Recorder = analytics.New(server.Client(), zap.NewNop(), map[catalog.Kind][]string{catalog.KindModule: catalog.ModuleNames()},
		analytics.WithoutTicker(), analytics.WithClock(func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }))
	t.Cleanup(c.Recorder.Close)
	return c
}

func hincrby(s *valkeytest.Server, a valkeytest.Args) valkeytest.Reply {
	if s.Hashes[a.Key()] == nil {
		s.Hashes[a.Key()] = map[string]string{}
	}
	current, _ := strconv.ParseInt(s.Hashes[a.Key()][a[1]], 10, 64)
	next := current + int64(a.Int(2))
	s.Hashes[a.Key()][a[1]] = strconv.FormatInt(next, 10)
	return valkeytest.Int(next)
}

func confirmedBatch(s *valkeytest.Server, a valkeytest.Args) valkeytest.Reply {
	pos := 3 + a.Int(1)
	for pos < len(a) {
		size := a.Int(pos)
		command := a[pos+1 : pos+1+size]
		if command[0] == "HINCRBY" {
			hincrby(s, command[1:])
		}
		pos += size + 1
	}
	return valkeytest.Int(1)
}

func (c *Capture) Read(key string) map[string]int64 {
	c.Recorder.Flush(context.Background())
	out := map[string]int64{}
	c.server.With(func(s *valkeytest.Server) {
		for field, value := range s.Hashes[key] {
			if n, _ := strconv.ParseInt(value, 10, 64); n != 0 {
				out[field] = n
			}
		}
	})
	return out
}

func (c *Capture) Counters() map[string]int64 {
	return c.Read("an:c:20261116")
}
