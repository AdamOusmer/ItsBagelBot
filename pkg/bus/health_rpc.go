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

// RPCHealthPrefix is a fleet-wide, side-effect-free request/reply surface. It
// is kept separate from business RPCs so a health probe never reads a database,
// calls Twitch, or mutates state.
const RPCHealthPrefix = "bagel.rpc.health"

// rpcHealthTimeout bounds the responder's own check run. It sits under
// pkg/health's 5s checkTimeout on purpose: the requester is itself inside a
// probe with that budget, so a responder that answers "slow" late enough is
// indistinguishable from one that never answered, and the useful reply is the
// one that arrives while the caller is still listening.
const rpcHealthTimeout = 4 * time.Second

// rpcHealthTTL is the floor between two real check runs on the responder.
//
// The responder used to reply with a constant, so a probe cost nothing and
// callers fan out freely: the admin console's latency panel probes eleven
// services on every page load. Now that the reply carries the service's real
// report, an uncached responder would turn one page load into eleven MySQL
// pings and eleven NATS round trips, and would put the panel's 1500ms timeout
// underneath this responder's own 4s ceiling.
//
// One second is far below every consumer's polling interval — Better Stack
// checks every 180s, the panel on human page loads — so no caller can observe
// a staler answer than it would have got by asking a moment earlier, while the
// cost of being asked stops scaling with how often anyone asks.
const rpcHealthTTL = time.Second

// RPCHealthReply carries the responding service's whole report rather than a
// liveness flag. A service that folds a sibling into its own /status has to
// know *how* that sibling is unwell: with only a bool, a users service degraded
// by its own MySQL answers ok:true and the impairment vanishes from the
// aggregate that health.itsbagelbot.com/db exists to surface.
//
// OK is kept and now means "not down". The admin console's RPC latency panel
// reads that field and only cares whether the service answered at all, so
// widening the struct instead of replacing it leaves that panel untouched.
type RPCHealthReply struct {
	Service string               `json:"service"`
	OK      bool                 `json:"ok"`
	Status  string               `json:"status,omitempty"`
	Checks  []health.CheckResult `json:"checks,omitempty"`
}

func RPCHealthSubject(service string) string {
	return RPCHealthPrefix + "." + service
}

// SubscribeRPCHealth registers one queue-balanced health responder for a
// service and returns the Check that watches that registration.
//
// The responder answers from set, so the report a sibling folds in over RPC is
// the one this service would have served on /status at the same instant. It
// rides the service's existing RPC connection, so a successful response covers
// the same leaf route, account import/export, connection and subscriber
// dispatch path as its real RPC handlers.
//
// The returned Check is deliberately local rather than a request to the subject
// this call just registered. A self-request would recurse: the responder now
// answers from set, and this check goes into set. What it watches instead is
// the failure this fleet actually hits — a NATS permission violation kills the
// subscription asynchronously, and the service keeps running, publishing into
// the void, with nothing in its own health surface changing. That has happened
// twice, both times found by hand. An invalid subscription is down; a
// subscription merely dropping messages is degraded, since it is still
// answering.
//
// Call it after building set and add the returned Check to it:
//
//	set := health.NewSet(serviceName, health.NATS("nats", nc))
//	rpc, err := bus.SubscribeRPCHealth(nc, serviceName, "sesame-rpc", set)
//	set.Add(rpc)
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

// reportCache holds the last encoded reply so a burst of probes costs one check
// run. Deliveries on a subscription are serial (see QueueSubscribeRPC), but the
// generic and node-local subjects are two subscriptions feeding this handler,
// so the mutex is load-bearing rather than decorative.
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

// healthReply runs the service's own checks and encodes them. A marshal failure
// is unreachable with this shape; answering with the bare verdict beats leaving
// the caller to time out, which it would read as the service being gone.
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

// subscriptionsUsable reports whether the responder is still registered. Both
// subjects matter: QueueSubscribeRPC registers the generic HA subject and this
// pod's node-local one, and requestLocalFirst prefers the local subject, so
// losing only that one silently routes every probe the long way round.
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

// HealthProbe returns a Check that folds another service's report into this
// one's, so one public endpoint answers for a whole vertical:
// health.itsbagelbot.com/twitch is sesame's own report plus this probe against
// twitch-ingress and outgress.
//
// It carries the downstream's verdict through instead of flattening it to a
// bool. Down fails this check outright — the vertical cannot do its job without
// that service. Degraded degrades this service too, through health.ErrDegraded,
// and the downstream's failing check names travel up in the error string so
// /status names the actual cause instead of only the hop that noticed. No
// answer at all is down rather than degraded: with the responder gone there is
// no evidence the service is doing anything.
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

// replyVerdict maps a downstream's report onto this service's check outcome. An
// empty Status means the responder still speaks the pre-widening shape, where
// OK was the whole answer; read it as the bool it is rather than as an unknown.
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
