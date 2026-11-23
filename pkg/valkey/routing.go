// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

func (c *Client) Do(ctx context.Context, cmd valkey_go.Completed) valkey_go.ValkeyResult {
	return c.do(ctx, cmd, false, false)
}

func (c *Client) do(ctx context.Context, cmd valkey_go.Completed, primary, throughput bool) valkey_go.ValkeyResult {
	if throughput && !cmd.IsReadOnly() {
		cmd = cmd.ToPipe()
	}
	return runRoute(ctx, c, c.commandRoute(cmd.IsReadOnly(), primary, singleCommand), localFallbackCall[valkey_go.ValkeyResult]{
		send:     func(client valkey_go.Client) valkey_go.ValkeyResult { return client.Do(ctx, cmd) },
		classify: classifyValkeyResult,
		lost:     func(result valkey_go.ValkeyResult) bool { return isConnectionError(result.Error()) },
	})
}

func (c *Client) DoMulti(ctx context.Context, multi ...valkey_go.Completed) []valkey_go.ValkeyResult {
	return c.doMulti(ctx, multi, false)
}

func (c *Client) doMulti(ctx context.Context, multi []valkey_go.Completed, primary bool) []valkey_go.ValkeyResult {
	return runRoute(ctx, c, c.commandRoute(allReadOnly(multi), primary, commandBatch), localFallbackCall[[]valkey_go.ValkeyResult]{
		send:     func(client valkey_go.Client) []valkey_go.ValkeyResult { return client.DoMulti(ctx, multi...) },
		classify: classifyValkeyResults,
		lost:     anyConnectionError,
	})
}

type localFallbackCall[T any] struct {
	send     func(valkey_go.Client) T
	classify func(T) string
	lost     func(T) bool
}

func runRoute[T any](ctx context.Context, c *Client, route valkeyRoute, call localFallbackCall[T]) T {
	result := traceValkeyCall(ctx, route.operation, func() T { return call.send(route.client) }, call.classify)
	if route.client != c.local || !call.lost(result) {
		return result
	}
	c.markLocalFailure()
	return traceValkeyCall(ctx, route.operation+"_primary_fallback", func() T { return call.send(c.Client) }, call.classify)
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
		if !primary && c.localAvailable() {
			route.client = c.local
		}
	}
	if shape == commandBatch {
		route.operation += "_batch"
	}
	return route
}

func (c *Client) localAvailable() bool {
	return c.local != nil && time.Now().UnixNano() >= c.localSkipUntil.Load()
}

func (c *Client) markLocalFailure() {
	c.localSkipUntil.Store(time.Now().Add(localFailureCooldown).UnixNano())
}

func isConnectionError(err error) bool {
	if err == nil || valkey_go.IsValkeyNil(err) {
		return false
	}
	_, isServerReply := valkey_go.IsValkeyErr(err)
	return !isServerReply
}

func anyConnectionError(results []valkey_go.ValkeyResult) bool {
	for i := range results {
		if isConnectionError(results[i].Error()) {
			return true
		}
	}
	return false
}

func allReadOnly(multi []valkey_go.Completed) bool {
	for i := range multi {
		if !multi[i].IsReadOnly() {
			return false
		}
	}
	return true
}

func Primary(client valkey_go.Client) valkey_go.Client {
	return clientViewFor(client, true, false)
}

func Throughput(client valkey_go.Client) valkey_go.Client {
	return clientViewFor(client, false, true)
}

func PrimaryThroughput(client valkey_go.Client) valkey_go.Client {
	return clientViewFor(client, true, true)
}

func IsPrimary(client valkey_go.Client) bool {
	view, ok := client.(*clientView)
	return ok && view.primary
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

func (v *clientView) Close() {}
