package valkey

import (
	"context"
	"sync"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Receive routes pub/sub to the master-pinned client. Keyspace notifications
// (the expired events the timer + live-recheck watchers subscribe to) are
// emitted only by the master. A separate lazy client isolates the long-lived
// subscription from the ordinary command pool without charging services that
// never subscribe.
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

// Close releases the primary, node-local read and lazy pub/sub clients.
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

// lazyValkeyClient avoids allocating a dedicated master-pinned connection pool
// in services that never subscribe. Holding the mutex during construction makes
// first use single-flight and prevents Close from racing a newly created client.
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
