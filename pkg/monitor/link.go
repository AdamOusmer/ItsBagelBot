// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package monitor

import (
	"context"
	"net/http"

	"github.com/newrelic/go-agent/v3/newrelic"
)

type Link struct {
	app     *newrelic.Application
	headers http.Header
}

func LinkFrom(ctx context.Context) Link {
	txn := newrelic.FromContext(ctx)
	if txn == nil || !txn.IsSampled() {
		return Link{}
	}
	headers := http.Header{}
	txn.InsertDistributedTraceHeaders(headers)
	return Link{app: txn.Application(), headers: headers}
}

func (l Link) Start(ctx context.Context, name string) (context.Context, *newrelic.Transaction) {
	if l.app == nil {
		return newrelic.NewContext(ctx, nil), nil
	}
	txn := l.app.StartTransaction(name)
	txn.AcceptDistributedTraceHeaders(newrelic.TransportOther, l.headers)
	return newrelic.NewContext(ctx, txn), txn
}

func Concurrent(ctx context.Context) context.Context {
	if txn := newrelic.FromContext(ctx); txn != nil {
		return newrelic.NewContext(ctx, txn.NewGoroutine())
	}
	return ctx
}
