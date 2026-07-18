package valkey

import (
	"context"
	"sync"
	"sync/atomic"

	valkey_go "github.com/valkey-io/valkey-go"
)

type recordingValkeyClient struct {
	valkey_go.Client
	mu       sync.Mutex
	commands []valkey_go.Completed
	batches  [][]valkey_go.Completed
	receives atomic.Int64
	closes   atomic.Int64
}

func (c *recordingValkeyClient) Do(_ context.Context, cmd valkey_go.Completed) valkey_go.ValkeyResult {
	c.mu.Lock()
	c.commands = append(c.commands, cmd)
	c.mu.Unlock()
	return valkey_go.ValkeyResult{}
}

func (c *recordingValkeyClient) DoMulti(_ context.Context, multi ...valkey_go.Completed) []valkey_go.ValkeyResult {
	c.mu.Lock()
	c.batches = append(c.batches, append([]valkey_go.Completed(nil), multi...))
	c.mu.Unlock()
	return make([]valkey_go.ValkeyResult, len(multi))
}

func (c *recordingValkeyClient) Receive(context.Context, valkey_go.Completed, func(valkey_go.PubSubMessage)) error {
	c.receives.Add(1)
	return nil
}

func (c *recordingValkeyClient) Close() {
	c.closes.Add(1)
}
