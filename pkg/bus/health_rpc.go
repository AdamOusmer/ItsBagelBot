// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/health"

	"github.com/nats-io/nats.go"
)

const RPCHealthPrefix = "bagel.rpc.health"

// Must stay under pkg/health's checkTimeout, or the reply is never heard.
const rpcHealthTimeout = 4 * time.Second

const rpcHealthTTL = time.Second

type RPCHealthReply struct {
	Service string               `json:"service"`
	OK      bool                 `json:"ok"`
	Status  string               `json:"status,omitempty"`
	Checks  []health.CheckResult `json:"checks,omitempty"`
}

func RPCHealthSubject(service string) string {
	return RPCHealthPrefix + "." + service
}

func SubscribeRPCHealth(nc *nats.Conn, service, queueGroup string, set *health.Set) (health.Check, error) {
	if service == "" || strings.ContainsAny(service, ".*> \t\r\n") {
		return health.Check{}, fmt.Errorf("invalid rpc health service token %q", service)
	}
	if queueGroup == "" {
		return health.Check{}, errors.New("rpc health queue group is required")
	}
	if set == nil {
		return health.Check{}, errors.New("rpc health set is required")
	}

	cache := &reportCache{service: service, set: set}
	subject := RPCHealthSubject(service)
	subs, err := subscribeRPCSubjects(nc, RPCSubscription{Subject: subject, QueueGroup: queueGroup},
		func(msg *nats.Msg) { _ = msg.Respond(cache.reply()) })
	if err != nil {
		return health.Check{}, fmt.Errorf("subscribe %s: %w", subject, err)
	}
	if err := nc.Flush(); err != nil {
		return health.Check{}, fmt.Errorf("flush subscription %s: %w", subject, err)
	}

	return health.Check{
		Name:  "rpc",
		Probe: func(context.Context) error { return subscriptionsUsable(subs) },
	}, nil
}

type reportCache struct {
	service string
	set     *health.Set

	mu    sync.Mutex
	body  []byte
	taken time.Time
}

func (c *reportCache) reply() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.taken) < rpcHealthTTL && c.body != nil {
		return c.body
	}
	c.body, c.taken = healthReply(c.service, c.set), time.Now()
	return c.body
}

func healthReply(service string, set *health.Set) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), rpcHealthTimeout)
	defer cancel()

	report := set.Snapshot(ctx)
	body, err := codec.Marshal(RPCHealthReply{
		Service: service,
		OK:      report.Status != health.StatusDown,
		Status:  report.Status,
		Checks:  report.Checks,
	})
	if err != nil {
		return []byte(`{"service":"` + service + `","ok":false,"status":"` + report.Status + `"}`)
	}
	return body
}

func subscriptionsUsable(subs []*nats.Subscription) error {
	for _, sub := range subs {
		if !sub.IsValid() {
			return fmt.Errorf("subscription %s is closed", sub.Subject)
		}
		if dropped, err := sub.Dropped(); err == nil && dropped > 0 {
			return fmt.Errorf("%w: subscription %s dropped %d", health.ErrDegraded, sub.Subject, dropped)
		}
	}
	return nil
}

func HealthProbe(nc *nats.Conn, service string) health.Check {
	return health.Check{Name: service, Probe: func(ctx context.Context) error {
		msg, err := RequestMsgWithContext(ctx, nc, &nats.Msg{Subject: RPCHealthSubject(service)})
		if err != nil {
			return fmt.Errorf("health rpc: %w", err)
		}
		var reply RPCHealthReply
		if err := codec.Unmarshal(msg.Data, &reply); err != nil {
			return fmt.Errorf("decode health reply: %w", err)
		}
		return replyVerdict(reply)
	}}
}

func replyVerdict(reply RPCHealthReply) error {
	switch reply.Status {
	case health.StatusOK:
		return nil
	case health.StatusDegraded:
		return fmt.Errorf("%w: %s", health.ErrDegraded, failingChecks(reply.Checks))
	case health.StatusDown:
		return errors.New("down: " + failingChecks(reply.Checks))
	}
	if !reply.OK {
		return errors.New("not ok")
	}
	return nil
}

func failingChecks(checks []health.CheckResult) string {
	names := make([]string, 0, len(checks))
	for _, c := range checks {
		if !c.OK {
			names = append(names, c.Name+"("+c.Error+")")
		}
	}
	if len(names) == 0 {
		return "no failing check reported"
	}
	return strings.Join(names, ", ")
}
