// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"slices"
	"testing"
	"time"

	domainrpc "ItsBagelBot/internal/domain/rpc"
	rpcprojection "ItsBagelBot/internal/domain/rpc/projection"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCommandKeys(t *testing.T) {
	commands := []projection.CommandView{
		{Name: "uptime", Aliases: []string{"live", "up"}},
		{Name: "socials"},
		{Name: "lurk", Aliases: []string{"hide"}},
	}

	got := commandKeys(commands)
	want := []string{"uptime", "live", "up", "socials", "lurk", "hide"}
	if !slices.Equal(got, want) {
		t.Fatalf("commandKeys() = %v, want %v", got, want)
	}
}

func TestCommandKeysEmpty(t *testing.T) {
	if got := commandKeys(nil); len(got) != 0 {
		t.Fatalf("commandKeys(nil) = %v, want empty", got)
	}
}

func TestDashboardRefusesMalformedUserIDAsInvalid(t *testing.T) {
	nc := bustest.NATS(t)
	require.NoError(t, SubscribeDashboard(nc, nil, "test.dashboard", "test.commands", "test.modules", "", nil, "dashboard-invalid-id", nil, zap.NewNop()))

	for _, verb := range []string{"commands.get", "commands.replace", "modules.get", "modules.replace"} {
		for _, userID := range []string{"", "abc", "0"} {
			_, err := bus.RequestJSONTimeout[rpcprojection.CommandsReply](context.Background(), nc, "test.dashboard."+verb, projectorrpc.DashboardRequest{UserID: userID}, time.Second)

			var refusal bus.RPCReplyError
			require.ErrorAs(t, err, &refusal, "%s user_id %q", verb, userID)
			require.Equal(t, domainrpc.CodeInvalid, refusal.Code, "%s user_id %q", verb, userID)
		}
	}
}
