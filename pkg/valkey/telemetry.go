// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	"github.com/newrelic/go-agent/v3/newrelic"
	valkey_go "github.com/valkey-io/valkey-go"
)

func traceValkeyCall[T any](ctx context.Context, operation string, do func() T, classify func(T) string) T {
	txn := newrelic.FromContext(ctx)
	if txn == nil || !txn.IsSampled() {
		return do()
	}
	segment := txn.StartSegment(operation)
	result := do()
	segment.AddAttribute("result", classify(result))
	segment.End()
	return result
}

func classifyValkeyResult(result valkey_go.ValkeyResult) string {
	return valkeyResult(result.Error())
}

func classifyValkeyResults(results []valkey_go.ValkeyResult) string {
	result := "ok"
	for i := range results {
		if current := valkeyResult(results[i].Error()); current == "error" {
			result = current
			break
		} else if current == "miss" {
			result = current
		}
	}
	return result
}

func valkeyResult(err error) string {
	if err == nil {
		return "ok"
	}
	if valkey_go.IsValkeyNil(err) {
		return "miss"
	}
	return "error"
}
