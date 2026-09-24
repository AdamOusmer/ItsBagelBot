// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

func pullConnections() int {
	return min(positiveInt(env.GetInt("NATS_PULL_CONNECTIONS", 1), 1), 32)
}

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

func (s *pullSubscriber) handleFor(i int) jsapi.Consumer {
	s.handleMu.Lock()
	defer s.handleMu.Unlock()
	if i == 0 || len(s.handles) == 0 {
		return s.boundConsumer()
	}
	return s.handles[(i-1)%len(s.handles)]
}

func (s *pullSubscriber) relookupHandles() {
	s.handleMu.Lock()
	defer s.handleMu.Unlock()
	for i, nc := range s.extra {
		if handle, err := lookupPullConsumer(nc, s.stream, s.name); err == nil {
			s.handles[i] = handle
		}
	}
}
