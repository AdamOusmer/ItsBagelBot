// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"net"
	"time"
)

// Above the observed p99.9 connect (1.05s): a fresh attempt skips the kernel's SYN backoff.
const dialAttemptTimeout = 1500 * time.Millisecond

var tcpKeepAlive = net.KeepAliveConfig{
	Enable: true,
	// Idle + Interval*Count must outlast the ~15s WAN stalls.
	Idle:     10 * time.Second,
	Interval: 5 * time.Second,
	Count:    4,
}

type retryDialer struct {
	attempt    func(ctx context.Context, network, addr string) (net.Conn, error)
	perAttempt time.Duration
}

func newRetryDialer() retryDialer {
	dialer := &net.Dialer{KeepAliveConfig: tcpKeepAlive}
	return retryDialer{attempt: dialer.DialContext, perAttempt: dialAttemptTimeout}
}

func (d retryDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	for {
		conn, err := d.dialOnce(ctx, network, addr)
		if err == nil || ctx.Err() != nil {
			return conn, err
		}
	}
}

func (d retryDialer) dialOnce(ctx context.Context, network, addr string) (net.Conn, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, d.perAttempt)
	defer cancel()

	conn, err := d.attempt(attemptCtx, network, addr)
	if err != nil {
		// Holds a fast failure (refused) to one attempt per window instead of a hot loop.
		<-attemptCtx.Done()
	}
	return conn, err
}
