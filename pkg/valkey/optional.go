// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	vk "github.com/valkey-io/valkey-go"
)

var errOptionalUnavailable = errors.New("optional Valkey connection unavailable")

type optionalConnection struct{ client vk.Client }
type optionalClient struct {
	active   atomic.Pointer[optionalConnection]
	cancel   context.CancelFunc
	done     chan struct{}
	once     sync.Once
	fallback vk.Client
}

// NewOptionalClient is for services whose correctness state is elsewhere.
// Invalid TLS configuration still fails startup. An unreachable server does
// not: commands fail promptly until the normal Sentinel client connects.
func NewOptionalClient(address, password string) (vk.Client, error) {
	tlsConfig, err := clientTLSConfig()
	if err != nil {
		return nil, err
	}
	option := withWritePool(buildOption(secureAddress(address, tlsConfig != nil), password, tlsConfig))
	connect := func() (vk.Client, error) { return vk.NewClient(option) }
	if client, err := connect(); err == nil {
		return &Client{Client: client}, nil
	}
	// No node-local replica path: optional reads must remain primary-consistent
	// when a Primary view is taken before the background connection is ready.
	return &Client{Client: newOptionalClient(connect)}, nil
}

func newOptionalClient(connect func() (vk.Client, error)) *optionalClient {
	// ForceSingleClient explicitly returns a usable client even on dial failure.
	// It supplies native command builders/error results without a fake server or
	// background network I/O. Never retry its deliberately rejected commands.
	fallback, _ := vk.NewClient(vk.ClientOption{
		InitAddress: []string{"unavailable:0"}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true,
		DialCtxFn: unavailableDial,
	})
	ctx, cancel := context.WithCancel(context.Background())
	c := &optionalClient{fallback: fallback, cancel: cancel, done: make(chan struct{})}
	c.active.Store(&optionalConnection{client: fallback})
	go c.connect(ctx, connect)
	return c
}

func unavailableDial(context.Context, string, *net.Dialer, *tls.Config) (net.Conn, error) {
	return nil, errOptionalUnavailable
}

func (c *optionalClient) connect(ctx context.Context, connect func() (vk.Client, error)) {
	defer close(c.done)
	for {
		client, err := connect()
		if err == nil {
			c.active.Store(&optionalConnection{client: client})
			return
		}
		if client != nil {
			client.Close()
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (c *optionalClient) current() vk.Client { return c.active.Load().client }
func (c *optionalClient) Close() {
	c.once.Do(func() {
		c.cancel()
		<-c.done
		if client := c.current(); client != c.fallback {
			client.Close()
		}
		c.fallback.Close()
	})
}

func (c *optionalClient) B() vk.Builder { return c.fallback.B() }
func (c *optionalClient) Do(ctx context.Context, cmd vk.Completed) vk.ValkeyResult {
	return c.current().Do(ctx, cmd)
}
func (c *optionalClient) DoMulti(ctx context.Context, cmds ...vk.Completed) []vk.ValkeyResult {
	return c.current().DoMulti(ctx, cmds...)
}
func (c *optionalClient) DoCache(ctx context.Context, cmd vk.Cacheable, ttl time.Duration) vk.ValkeyResult {
	return c.current().DoCache(ctx, cmd, ttl)
}
func (c *optionalClient) DoMultiCache(ctx context.Context, cmds ...vk.CacheableTTL) []vk.ValkeyResult {
	return c.current().DoMultiCache(ctx, cmds...)
}
func (c *optionalClient) DoStream(ctx context.Context, cmd vk.Completed) vk.ValkeyResultStream {
	return c.current().DoStream(ctx, cmd)
}
func (c *optionalClient) DoMultiStream(ctx context.Context, cmds ...vk.Completed) vk.MultiValkeyResultStream {
	return c.current().DoMultiStream(ctx, cmds...)
}
func (c *optionalClient) Receive(ctx context.Context, cmd vk.Completed, fn func(vk.PubSubMessage)) error {
	return c.current().Receive(ctx, cmd, fn)
}
func (c *optionalClient) Dedicated(fn func(vk.DedicatedClient) error) error {
	return c.current().Dedicated(fn)
}
func (c *optionalClient) Dedicate() (vk.DedicatedClient, func()) { return c.current().Dedicate() }
func (c *optionalClient) Nodes() map[string]vk.Client            { return c.current().Nodes() }
func (c *optionalClient) Mode() vk.ClientMode                    { return c.current().Mode() }
