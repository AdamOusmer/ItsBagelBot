// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analyticstest

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type Capture struct {
	valkey.Client
	Recorder *analytics.Recorder
	t        *testing.T
	fields   map[string]map[string]int64
}

func New(t *testing.T) *Capture {
	t.Helper()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"analytics.invalid:0"}, ForceSingleClient: true, DisableCache: true,
		DialFn: func(string, *net.Dialer, *tls.Config) (net.Conn, error) { return nil, errors.ErrUnsupported },
	})
	require.ErrorIs(t, err, errors.ErrUnsupported)
	t.Cleanup(client.Close)
	c := &Capture{Client: client, t: t, fields: map[string]map[string]int64{}}
	c.Recorder = analytics.New(c, zap.NewNop(), map[catalog.Kind][]string{catalog.KindModule: catalog.ModuleNames()},
		analytics.WithoutTicker(), analytics.WithClock(func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }))
	t.Cleanup(c.Recorder.Close)
	return c
}

func (c *Capture) DoMulti(_ context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	for _, cmd := range cmds {
		args := cmd.Commands()
		if args[0] == "EXPIREAT" {
			continue
		}
		require.Equal(c.t, "HINCRBY", args[0])
		n, err := strconv.ParseInt(args[3], 10, 64)
		require.NoError(c.t, err)
		if c.fields[args[1]] == nil {
			c.fields[args[1]] = map[string]int64{}
		}
		c.fields[args[1]][args[2]] += n
	}
	return make([]valkey.ValkeyResult, len(cmds))
}

func (c *Capture) Read(key string) map[string]int64 {
	c.Recorder.Flush(context.Background())
	out := map[string]int64{}
	for field, n := range c.fields[key] {
		if n != 0 {
			out[field] = n
		}
	}
	return out
}

func (c *Capture) Counters() map[string]int64 {
	return c.Read("an:c:20261116")
}
