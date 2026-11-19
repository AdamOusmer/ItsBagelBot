// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

func WaitForStream(ctx context.Context, url string, spec StreamSpec) error {
	nc, err := nats.Connect(busURL(endpoint(url)), busOptions("stream-waiter")...)
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return err
	}
	for {
		_, err := js.Stream(ctx, spec.Name)
		if err == nil {
			return nil
		}
		if !sleepOrDone(ctx, 200*time.Millisecond) {
			return ctx.Err()
		}
	}
}
