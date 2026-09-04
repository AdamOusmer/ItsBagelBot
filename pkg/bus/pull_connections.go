// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

// Fan-out of one pull lane over several broker connections: each extra pull
// loop can own a connection so the hub writes its deliveries as separate,
// larger records.

import (
	"context"
	"fmt"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

// pullConnections is how many connections one pod spreads its fetch loops
// over, NATS_PULL_CONNECTIONS, default 1 (every loop on the lane's connection).
//
// Measured 2026-09-01 on the production hub (mTLS, R3 memory stream): the hub
// delivered at most ~50-54k msg/s to ONE pull connection however many loops
// shared it — the server's per-connection write loop is the unit that pays a
// TLS record per delivered message at these rates — while a second pod with
// its own connection took the total to ~80k. The 2026-08-15 drain that reached
// ~90k per pod ran two lanes, i.e. two connections per pod. Loops are dealt
// round-robin over the connections; the first connection stays the one that
// provisioned the durable and publishes the floor ack.
func pullConnections() int {
	return min(positiveInt(env.GetInt("NATS_PULL_CONNECTIONS", 1), 1), 32)
}

// openExtraConnections dials the additional fetch connections and binds a
// handle on each by lookup. A lookup is not an assignment write, so the extra
// handles never add to the create race pullCreateStagger guards.
func (s *pullSubscriber) openExtraConnections(cfg flowLaneConfig) error {
	for i := 1; i < pullConnections(); i++ {
		nc, err := nats.Connect(busURL(endpoint(cfg.url)), busOptions(clientName(fmt.Sprintf("%s-pull-%d", cfg.group, i)))...)
		if err != nil {
			s.closeExtra()
			return err
		}
		handle, err := lookupPullConsumer(nc, s.stream, s.name)
		if err != nil {
			nc.Close()
			s.closeExtra()
			return err
		}
		s.extra = append(s.extra, nc)
		s.handles = append(s.handles, handle)
	}
	return nil
}

func lookupPullConsumer(nc *nats.Conn, stream, name string) (jsapi.Consumer, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pullProvisionTimeout)
	defer cancel()
	return js.Consumer(ctx, stream, name)
}

func (s *pullSubscriber) closeExtra() {
	for _, nc := range s.extra {
		nc.Close()
	}
	s.extra, s.handles = nil, nil
}

// handleFor deals loop i its fetch handle: loop 0 and every loop past the
// extra connections use the lane's own consumer; the rest take an extra
// connection round-robin.
func (s *pullSubscriber) handleFor(i int) jsapi.Consumer {
	s.handleMu.Lock()
	defer s.handleMu.Unlock()
	if i == 0 || len(s.handles) == 0 {
		return s.boundConsumer()
	}
	return s.handles[(i-1)%len(s.handles)]
}

// relookupHandles rebinds the extra handles after the durable was rebuilt, so
// a pump on an extra connection does not keep fetching a name that moved.
func (s *pullSubscriber) relookupHandles() {
	s.handleMu.Lock()
	defer s.handleMu.Unlock()
	for i, nc := range s.extra {
		if handle, err := lookupPullConsumer(nc, s.stream, s.name); err == nil {
			s.handles[i] = handle
		}
	}
}
