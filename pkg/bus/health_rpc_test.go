// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/health"
)

func TestRPCHealthSubject(t *testing.T) {
	if got := RPCHealthSubject("projector"); got != "bagel.rpc.health.projector" {
		t.Fatalf("RPCHealthSubject() = %q", got)
	}
}

func TestSubscribeRPCHealthRejectsBadArgumentsBeforeDial(t *testing.T) {
	set := health.NewSet("users")
	for _, service := range []string{"", "two.tokens", "wildcard.*", "has space"} {
		if _, err := SubscribeRPCHealth(nil, service, "health", set); err == nil {
			t.Fatalf("SubscribeRPCHealth accepted invalid service %q", service)
		}
	}
	if _, err := SubscribeRPCHealth(nil, "users", "", set); err == nil {
		t.Fatal("SubscribeRPCHealth accepted an empty queue group")
	}
	if _, err := SubscribeRPCHealth(nil, "users", "users-rpc", nil); err == nil {
		t.Fatal("SubscribeRPCHealth accepted a nil health set")
	}
}

func TestReplyVerdict(t *testing.T) {
	mysqlDown := []health.CheckResult{
		{Name: "nats", OK: true},
		{Name: "mysql", OK: false, Optional: true, Error: "dial timeout"},
	}

	t.Run("ok is healthy", func(t *testing.T) {
		if err := replyVerdict(RPCHealthReply{Status: health.StatusOK, OK: true}); err != nil {
			t.Fatalf("replyVerdict(ok) = %v", err)
		}
	})

	t.Run("degraded degrades and names the cause", func(t *testing.T) {
		err := replyVerdict(RPCHealthReply{Status: health.StatusDegraded, OK: true, Checks: mysqlDown})
		if !errors.Is(err, health.ErrDegraded) {
			t.Fatalf("replyVerdict(degraded) = %v, want ErrDegraded", err)
		}
		if !strings.Contains(err.Error(), "mysql(dial timeout)") {
			t.Fatalf("degraded error %q does not name the downstream check", err)
		}
	})

	t.Run("down fails hard", func(t *testing.T) {
		err := replyVerdict(RPCHealthReply{Status: health.StatusDown, OK: false, Checks: mysqlDown})
		if err == nil {
			t.Fatal("replyVerdict(down) = nil")
		}
		if errors.Is(err, health.ErrDegraded) {
			t.Fatalf("replyVerdict(down) = %v, must not degrade", err)
		}
	})

	t.Run("pre-widening reply falls back to the bool", func(t *testing.T) {
		if err := replyVerdict(RPCHealthReply{Service: "users", OK: true}); err != nil {
			t.Fatalf("replyVerdict(legacy ok) = %v", err)
		}
		if err := replyVerdict(RPCHealthReply{Service: "users", OK: false}); err == nil {
			t.Fatal("replyVerdict(legacy not ok) = nil")
		}
	})
}

func TestHealthReplyCarriesTheServiceReport(t *testing.T) {
	set := health.NewSet("users",
		health.NATS("nats", nil),
		health.Degrades(health.Check{Name: "mysql", Probe: func(context.Context) error {
			return errors.New("dial timeout")
		}}),
	)

	var reply RPCHealthReply
	if err := codec.Unmarshal(healthReply("users", set), &reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Service != "users" {
		t.Fatalf("service = %q", reply.Service)
	}
	if reply.Status != health.StatusDegraded {
		t.Fatalf("status = %q, want %q", reply.Status, health.StatusDegraded)
	}
	if !reply.OK {
		t.Fatal("degraded service reported OK=false")
	}
	if len(reply.Checks) != 2 {
		t.Fatalf("checks = %d, want 2", len(reply.Checks))
	}
}
