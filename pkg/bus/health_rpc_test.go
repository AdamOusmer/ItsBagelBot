// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/health"

	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRPCHealthSubject(t *testing.T) {
	assert.Equal(t, "bagel.rpc.health.projector", RPCHealthSubject("projector"))
}

func TestSubscribeRPCHealthRejectsBadArgumentsBeforeDial(t *testing.T) {
	set := health.NewSet("users")

	for _, tc := range []struct {
		name    string
		service string
		queue   string
		set     *health.Set
	}{
		{"an empty service", "", "health", set},
		{"a multi-token service", "two.tokens", "health", set},
		{"a wildcard service", "wildcard.*", "health", set},
		{"a service with whitespace", "has space", "health", set},
		{"an empty queue group", "users", "", set},
		{"a nil health set", "users", "users-rpc", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SubscribeRPCHealth(nil, tc.service, tc.queue, tc.set)

			assert.Error(t, err)
		})
	}
}

func TestHealthProbeMapsTheServiceReportToAVerdict(t *testing.T) {
	nc := testnats.Connect(t)
	mysqlDown := func(context.Context) error { return errors.New("dial timeout") }

	for _, tc := range []struct {
		name         string
		set          *health.Set
		wantFailed   bool
		wantDegraded bool
		wantText     string
	}{
		{"a healthy service is healthy", health.NewSet("svc", health.Bool("db", func() bool { return true })), false, false, ""},
		{"an optional dependency failing degrades and names the cause",
			health.NewSet("svc", health.Degrades(health.Check{Name: "mysql", Probe: mysqlDown})), true, true, "mysql(dial timeout)"},
		{"a required dependency failing fails hard",
			health.NewSet("svc", health.Check{Name: "mysql", Probe: mysqlDown}), true, false, "mysql(dial timeout)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := "svc" + nuid.Next()
			_, err := SubscribeRPCHealth(nc, service, "health", tc.set)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			verdict := HealthProbe(nc, service).Probe(ctx)

			assert.Equal(t, tc.wantFailed, verdict != nil)
			assert.Equal(t, tc.wantDegraded, errors.Is(verdict, health.ErrDegraded))
			if tc.wantText != "" {
				assert.ErrorContains(t, verdict, tc.wantText)
			}
		})
	}
}

func TestHealthProbeFailsWhenNoServiceAnswers(t *testing.T) {
	nc := testnats.Connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assert.ErrorContains(t, HealthProbe(nc, "nobody"+nuid.Next()).Probe(ctx), "health rpc")
}
