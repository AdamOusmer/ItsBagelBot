// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"sync"

	valkey_go "github.com/valkey-io/valkey-go"
)

func (c *Client) Receive(ctx context.Context, subscribe valkey_go.Completed, fn func(msg valkey_go.PubSubMessage)) error {
	if c.pubsub != nil {
		pubsub, err := c.pubsub.get()
		if err != nil {
			return err
		}
		return pubsub.Receive(ctx, subscribe, fn)
	}
	return c.Client.Receive(ctx, subscribe, fn)
}

func (c *Client) Close() {
	if c.local != nil {
		c.local.Close()
	}
	if c.pubsub != nil {
		c.pubsub.close()
	}
	c.Client.Close()
}

type valkeyClientFactory func(valkey_go.ClientOption) (valkey_go.Client, error)

type lazyValkeyClient struct {
	mu        sync.Mutex
	client    valkey_go.Client
	option    valkey_go.ClientOption
	newClient valkeyClientFactory
	closed    bool
}

func newLazyValkeyClient(option valkey_go.ClientOption, factory valkeyClientFactory) *lazyValkeyClient {
	return &lazyValkeyClient{option: option, newClient: factory}
}

func (c *lazyValkeyClient) get() (valkey_go.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, valkey_go.ErrClosing
	}
	if c.client != nil {
		return c.client, nil
	}
	client, err := c.newClient(c.option)
	if err != nil {
		return nil, err
	}
	c.client = client
	return client, nil
}

func (c *lazyValkeyClient) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	client := c.client
	c.client = nil
	c.mu.Unlock()
	if client != nil {
		client.Close()
	}
}
