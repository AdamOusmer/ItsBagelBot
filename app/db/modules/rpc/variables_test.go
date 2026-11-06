// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	_ "ItsBagelBot/app/db/modules/ent/runtime"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type stateHarness struct{ nc *nats.Conn }

func newStateHarness(t *testing.T) *stateHarness {
	t.Helper()
	raw, err := sql.Open(testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	drv := entsql.OpenDB("sqlite3", raw)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(context.Background()))
	nc := testnats.Connect(t)
	require.NoError(t, SubscribeRecipeState(bus.RPCWiring{NC: nc, Log: zap.NewNop()}, repository.NewRecipeState(client, drv), "mods"))
	return &stateHarness{nc: nc}
}

func (h *stateHarness) call(t *testing.T, verb string, req, reply any) {
	t.Helper()
	payload, err := codec.Marshal(req)
	require.NoError(t, err)
	msg, err := h.nc.Request("mods."+verb, payload, 3*time.Second)
	require.NoError(t, err)
	require.NoError(t, codec.Unmarshal(msg.Data, reply))
}

func (h *stateHarness) variable(t *testing.T, verb string, req modulesrpc.VariableRequest) modulesrpc.VariableReply {
	t.Helper()
	var reply modulesrpc.VariableReply
	h.call(t, "variable."+verb, req, &reply)
	return reply
}

func TestVariableVerbsRoundTrip(t *testing.T) {
	h := newStateHarness(t)
	channel := modulesrpc.VariableRequest{UserID: "2", Scope: automation.VariableScopeChannel, Name: "Deaths", Text: "{arg:1}"}
	assert.True(t, h.variable(t, "set", channel).Found)

	viewer := modulesrpc.VariableRequest{UserID: "2", ViewerID: "7", Scope: automation.VariableScopeViewer, Name: "mine", Value: 4}
	assert.True(t, h.variable(t, "add", viewer).Found)

	got := h.variable(t, "get", modulesrpc.VariableRequest{UserID: "2", Scope: automation.VariableScopeChannel, Names: []string{"deaths", "absent"}})
	assert.Equal(t, map[string]string{"deaths": "{arg:1}"}, got.Variables)

	got = h.variable(t, "get", modulesrpc.VariableRequest{UserID: "2", ViewerID: "7", Scope: automation.VariableScopeViewer, Names: []string{"mine"}})
	assert.Equal(t, map[string]string{"mine": "4"}, got.Variables)

	h.variable(t, "delete", modulesrpc.VariableRequest{UserID: "2", Scope: automation.VariableScopeChannel, Name: "deaths"})
	got = h.variable(t, "get", modulesrpc.VariableRequest{UserID: "2", Scope: automation.VariableScopeChannel, Names: []string{"deaths"}})
	assert.Empty(t, got.Variables)
}

func TestVariableVerbsReportLimitsAndBadInput(t *testing.T) {
	h := newStateHarness(t)
	over := modulesrpc.VariableRequest{UserID: "2", Scope: automation.VariableScopeChannel, Name: "x", Text: strings.Repeat("é", automation.MaxVariableValueRunes+1)}
	reply := h.variable(t, "set", over)
	assert.True(t, reply.LimitExceeded)
	assert.Empty(t, reply.Error)

	bad := h.variable(t, "set", modulesrpc.VariableRequest{UserID: "2", Scope: "global", Name: "x"})
	assert.Equal(t, domainrpc.CodeInvalid, bad.Code)
	bad = h.variable(t, "get", modulesrpc.VariableRequest{UserID: "nope", Scope: automation.VariableScopeChannel})
	assert.Equal(t, domainrpc.CodeInvalid, bad.Code)
	bad = h.variable(t, "get", modulesrpc.VariableRequest{UserID: "2", ViewerID: "nope", Scope: automation.VariableScopeViewer})
	assert.Equal(t, domainrpc.CodeInvalid, bad.Code)
}
