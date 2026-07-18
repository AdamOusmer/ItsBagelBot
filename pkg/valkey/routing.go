package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Do sends read-only commands to the local instance and everything else to the
// primary via Sentinel.
func (c *Client) Do(ctx context.Context, cmd valkey_go.Completed) valkey_go.ValkeyResult {
	return c.do(ctx, cmd, false, false)
}

func (c *Client) do(ctx context.Context, cmd valkey_go.Completed, primary, throughput bool) valkey_go.ValkeyResult {
	if throughput && !cmd.IsReadOnly() {
		cmd = cmd.ToPipe()
	}
	route := c.commandRoute(cmd.IsReadOnly(), primary, singleCommand)
	return traceValkeyCall(ctx, route.operation, func() valkey_go.ValkeyResult {
		return route.client.Do(ctx, cmd)
	}, classifyValkeyResult)
}

// DoMulti sends a batch to the local instance only when every command is
// read-only; any write in the batch routes the whole batch to the primary.
func (c *Client) DoMulti(ctx context.Context, multi ...valkey_go.Completed) []valkey_go.ValkeyResult {
	return c.doMulti(ctx, multi, false)
}

func (c *Client) doMulti(ctx context.Context, multi []valkey_go.Completed, primary bool) []valkey_go.ValkeyResult {
	route := c.commandRoute(allReadOnly(multi), primary, commandBatch)
	return traceValkeyCall(ctx, route.operation, func() []valkey_go.ValkeyResult {
		return route.client.DoMulti(ctx, multi...)
	}, classifyValkeyResults)
}

type commandShape uint8

const (
	singleCommand commandShape = iota
	commandBatch
)

type valkeyRoute struct {
	client    valkey_go.Client
	operation string
}

func (c *Client) commandRoute(readOnly, primary bool, shape commandShape) valkeyRoute {
	route := valkeyRoute{client: c.Client, operation: "valkey.write"}
	if readOnly {
		route.operation = "valkey.read"
		if !primary && c.local != nil {
			route.client = c.local
		}
	}
	if shape == commandBatch {
		route.operation += "_batch"
	}
	return route
}

func allReadOnly(multi []valkey_go.Completed) bool {
	for i := range multi {
		if !multi[i].IsReadOnly() {
			return false
		}
	}
	return true
}

// Primary returns a lightweight borrowed view whose commands are routed to the
// Sentinel-elected primary. It shares the original client's connections and
// telemetry. Closing the view is a no-op; the source client owns the shared
// connection lifecycle.
//
// Primary consistency is available for clients created by this package. A
// foreign valkey-go Client is delegated to unchanged because its native
// replica-routing policy cannot be overridden after construction.
func Primary(client valkey_go.Client) valkey_go.Client {
	return clientViewFor(client, true, false)
}

// Throughput returns a lightweight borrowed view that opts single write
// commands into valkey-go auto-pipelining with Completed.ToPipe. Node-local
// reads already auto-pipeline, while primary reads remain on the consistency
// and latency path. Explicit DoMulti calls retain their existing batch and
// routing semantics. No additional pool or connection is created.
func Throughput(client valkey_go.Client) valkey_go.Client {
	return clientViewFor(client, false, true)
}

// PrimaryThroughput combines primary-consistent routing with the selective
// single-command throughput policy of Throughput, using the same client pool.
func PrimaryThroughput(client valkey_go.Client) valkey_go.Client {
	return clientViewFor(client, true, true)
}

type clientView struct {
	valkey_go.Client
	routed     *Client
	primary    bool
	throughput bool
}

func clientViewFor(client valkey_go.Client, primary, throughput bool) valkey_go.Client {
	if view, ok := client.(*clientView); ok {
		primary = primary || view.primary
		throughput = throughput || view.throughput
		client = view.Client
	}
	routed, _ := client.(*Client)
	return &clientView{
		Client:     client,
		routed:     routed,
		primary:    primary,
		throughput: throughput,
	}
}

func (v *clientView) Do(ctx context.Context, cmd valkey_go.Completed) valkey_go.ValkeyResult {
	if v.routed != nil {
		return v.routed.do(ctx, cmd, v.primary, v.throughput)
	}
	if v.throughput && !cmd.IsReadOnly() {
		cmd = cmd.ToPipe()
	}
	return v.Client.Do(ctx, cmd)
}

func (v *clientView) DoMulti(ctx context.Context, multi ...valkey_go.Completed) []valkey_go.ValkeyResult {
	if v.routed != nil {
		return v.routed.doMulti(ctx, multi, v.primary)
	}
	return v.Client.DoMulti(ctx, multi...)
}

// Close is intentionally a no-op: clientView borrows the source client's
// connection pools and must not invalidate other users of that source.
func (v *clientView) Close() {}
