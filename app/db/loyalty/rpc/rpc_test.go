// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"database/sql"
	"math"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
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

type harness struct {
	nc     *nats.Conn
	repo   *loyaltyrepo.Loyalty
	client *ent.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db, err := sql.Open(testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB("sqlite3", db)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(context.Background()))
	for _, seed := range []struct {
		viewerID uint64
		login    string
		points   int64
	}{{7, "sender", 1000}, {8, "receiver", 100}} {
		require.NoError(t, client.Balance.Create().SetUserID(2).SetViewerID(seed.viewerID).SetViewerLogin(seed.login).SetPoints(seed.points).Exec(context.Background()))
	}
	repo := loyaltyrepo.NewLoyalty(client, drv, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	nc := testnats.Connect(t)
	require.NoError(t, Subscribe(Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop()}, Repo: repo}, "loyalty"))
	return &harness{nc: nc, repo: repo, client: client}
}

func (h *harness) raw(t *testing.T, verb string, req loyaltyrpc.Request) []byte {
	t.Helper()
	payload, err := codec.Marshal(req)
	require.NoError(t, err)
	msg, err := h.nc.Request("loyalty."+verb, payload, 3*time.Second)
	require.NoError(t, err)
	return msg.Data
}

func (h *harness) call(t *testing.T, verb string, req loyaltyrpc.Request) loyaltyrpc.Reply {
	t.Helper()
	var reply loyaltyrpc.Reply
	require.NoError(t, codec.Unmarshal(h.raw(t, verb, req), &reply))
	return reply
}

func (h *harness) requireStored(t *testing.T, points map[uint64]int64) {
	t.Helper()
	for viewerID, want := range points {
		row, found, err := h.repo.BalanceGet(t.Context(), 2, viewerID)
		require.NoError(t, err)
		require.True(t, found, "viewer %d", viewerID)
		assert.Equal(t, want, row.Points, "viewer %d", viewerID)
	}
}

func (h *harness) fund(t *testing.T, points int64) {
	t.Helper()
	_, _, err := h.repo.BalanceAdjustViewer(t.Context(), loyaltyrepo.BalanceAdjustment{UserID: 2, ViewerID: 7, ViewerLogin: "sender", Value: points, Absolute: true})
	require.NoError(t, err)
}

func balanceOf(viewerID, login string, points int64) *loyaltyrpc.Balance {
	return &loyaltyrpc.Balance{ViewerID: viewerID, ViewerLogin: login, Points: points, PointsExact: strconv.FormatInt(points, 10)}
}

func requireRefused(t *testing.T, reply loyaltyrpc.Reply) {
	t.Helper()
	assert.Equal(t, domainrpc.CodeInvalid, reply.Code)
	assert.NotEmpty(t, reply.Error)
	assert.False(t, reply.Found)
	assert.False(t, reply.Spent)
}

func TestHandleBalanceAdjustCreatesResolvedViewer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		verb   string
		value  int64
		points int64
	}{
		{"set", "balance.set", 5000, 5000},
		{"add", "balance.add", 5000, 5000},
		{"remove", "balance.add", -5000, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := newHarness(t).call(t, tc.verb, loyaltyrpc.Request{UserID: "2", ViewerID: "7", TargetViewerID: "9", ViewerLogin: "@Blemmyz", Value: tc.value})
			assert.Equal(t, loyaltyrpc.Reply{Found: true, Balance: balanceOf("9", "blemmyz", tc.points)}, reply)
		})
	}
}

func TestHandleBalanceAdjustResolvedDeltaAndLegacy(t *testing.T) {
	h := newHarness(t)
	for _, step := range []struct {
		name string
		verb string
		req  loyaltyrpc.Request
		want loyaltyrpc.Reply
	}{
		{"adds a delta to a resolved viewer and refreshes the login", "balance.add", loyaltyrpc.Request{UserID: "2", TargetViewerID: "8", ViewerLogin: "newlogin", Value: 25}, loyaltyrpc.Reply{Found: true, Balance: balanceOf("8", "newlogin", 125)}},
		{"never drops a resolved viewer below zero", "balance.add", loyaltyrpc.Request{UserID: "2", TargetViewerID: "8", ViewerLogin: "newlogin", Value: -200}, loyaltyrpc.Reply{Found: true, Balance: balanceOf("8", "newlogin", 0)}},
		{"legacy login callers do not create viewers", "balance.set", loyaltyrpc.Request{UserID: "2", ViewerLogin: "ghost", Value: 100}, loyaltyrpc.Reply{}},
		{"legacy login callers still set stored viewers", "balance.set", loyaltyrpc.Request{UserID: "2", ViewerLogin: "sender", Value: 100}, loyaltyrpc.Reply{Found: true, Balance: balanceOf("7", "sender", 100)}},
	} {
		t.Run(step.name, func(t *testing.T) {
			assert.Equal(t, step.want, h.call(t, step.verb, step.req))
		})
	}
}

func TestHandleBalanceAdjustRejectsInvalidResolvedViewer(t *testing.T) {
	h := newHarness(t)
	for _, id := range []string{"0", "-1", "nope", "18446744073709551616"} {
		t.Run(id, func(t *testing.T) {
			requireRefused(t, h.call(t, "balance.set", loyaltyrpc.Request{UserID: "2", TargetViewerID: id, ViewerLogin: "blemmyz", Value: 100}))
		})
	}
}

func TestHandleBalanceTransfer(t *testing.T) {
	untouched := map[uint64]int64{7: 1000, 8: 100}
	for _, tc := range []struct {
		name    string
		req     loyaltyrpc.Request
		want    loyaltyrpc.Reply
		refused bool
		stored  map[uint64]int64
	}{
		{
			name:   "moves points to a recipient found by login",
			req:    loyaltyrpc.Request{UserID: "2", ViewerID: "7", ViewerLogin: "@Receiver", Value: 400},
			want:   loyaltyrpc.Reply{Found: true, Spent: true, Balance: balanceOf("7", "sender", 600), TargetBalance: balanceOf("8", "receiver", 500)},
			stored: map[uint64]int64{7: 600, 8: 500},
		},
		{
			name:   "refuses a transfer above the balance without crediting anyone",
			req:    loyaltyrpc.Request{UserID: "2", ViewerID: "7", ViewerLogin: "receiver", Value: 5000},
			want:   loyaltyrpc.Reply{Found: true, Balance: balanceOf("7", "sender", 1000)},
			stored: untouched,
		},
		{
			name:   "reports an unknown recipient as not found",
			req:    loyaltyrpc.Request{UserID: "2", ViewerID: "7", ViewerLogin: "ghost", Value: 10},
			stored: untouched,
		},
		{
			name:    "refuses a transfer to oneself",
			req:     loyaltyrpc.Request{UserID: "2", ViewerID: "7", ViewerLogin: "sender", Value: 10},
			refused: true,
			stored:  untouched,
		},
		{
			name:    "refuses a request without a sender",
			req:     loyaltyrpc.Request{UserID: "2", ViewerLogin: "receiver", Value: 10},
			refused: true,
			stored:  untouched,
		},
		{
			name:   "TestHandleBalanceTransferResolvedRecipient",
			req:    loyaltyrpc.Request{UserID: "2", ViewerID: "7", TargetViewerID: "9", ViewerLogin: "blemmyz", Value: 400},
			want:   loyaltyrpc.Reply{Found: true, Spent: true, Balance: balanceOf("7", "sender", 600), TargetBalance: balanceOf("9", "blemmyz", 400)},
			stored: map[uint64]int64{7: 600, 8: 100, 9: 400},
		},
		{
			name:   "TestHandleBalanceTransferMissingSenderIsInsufficient",
			req:    loyaltyrpc.Request{UserID: "2", ViewerID: "99", TargetViewerID: "9", ViewerLogin: "blemmyz", Value: 400},
			want:   loyaltyrpc.Reply{Found: true, Balance: balanceOf("99", "", 0)},
			stored: untouched,
		},
		{name: "TestHandleBalanceTransferRejectsInvalidTargetID zero", req: transferTo("0"), refused: true, stored: untouched},
		{name: "TestHandleBalanceTransferRejectsInvalidTargetID negative", req: transferTo("-1"), refused: true, stored: untouched},
		{name: "TestHandleBalanceTransferRejectsInvalidTargetID text", req: transferTo("nope"), refused: true, stored: untouched},
		{name: "TestHandleBalanceTransferRejectsInvalidTargetID overflow", req: transferTo("18446744073709551616"), refused: true, stored: untouched},
		{name: "TestHandleBalanceTransferRejectsInvalidTargetID sender", req: transferTo("7"), refused: true, stored: untouched},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			reply := h.call(t, "balance.transfer", tc.req)
			if tc.refused {
				requireRefused(t, reply)
			} else {
				assert.Equal(t, tc.want, reply)
			}
			h.requireStored(t, tc.stored)
		})
	}
}

func transferTo(targetID string) loyaltyrpc.Request {
	return loyaltyrpc.Request{UserID: "2", ViewerID: "7", TargetViewerID: targetID, ViewerLogin: "renamed", Value: 400}
}

func TestHandleBalanceWager(t *testing.T) {
	stake := func(viewerID string, value int64) loyaltyrpc.Request {
		return loyaltyrpc.Request{UserID: "2", ViewerID: viewerID, Value: value, Won: true}
	}
	for _, tc := range []struct {
		name    string
		fund    int64
		req     loyaltyrpc.Request
		want    loyaltyrpc.Reply
		refused bool
	}{
		{
			name: "TestHandleBalanceWagerOutcomes win",
			req:  stake("7", 400),
			want: loyaltyrpc.Reply{Found: true, Spent: true, Balance: balanceOf("7", "sender", 1400)},
		},
		{
			name: "TestHandleBalanceWagerOutcomes loss",
			req:  loyaltyrpc.Request{UserID: "2", ViewerID: "7", Value: 400},
			want: loyaltyrpc.Reply{Found: true, Spent: true, Balance: balanceOf("7", "sender", 600)},
		},
		{
			name: "TestHandleBalanceWagerRefusesWithoutDebit stake above the balance",
			req:  stake("7", 5000),
			want: loyaltyrpc.Reply{Found: true, Balance: balanceOf("7", "sender", 1000)},
		},
		{
			name: "TestHandleBalanceWagerRefusesWithoutDebit win above the points limit",
			fund: math.MaxInt64,
			req:  stake("7", 1),
			want: loyaltyrpc.Reply{Found: true, LimitExceeded: true, Balance: balanceOf("7", "sender", math.MaxInt64)},
		},
		{
			name: "TestHandleBalanceWagerRefusesWithoutDebit unknown viewer",
			req:  stake("99", 1),
		},
		{name: "TestHandleBalanceWagerInvalid user zero", req: loyaltyrpc.Request{UserID: "0", ViewerID: "7", Value: 1}, refused: true},
		{name: "TestHandleBalanceWagerInvalid viewer zero", req: loyaltyrpc.Request{UserID: "2", ViewerID: "0", Value: 1}, refused: true},
		{name: "TestHandleBalanceWagerInvalid viewer text", req: loyaltyrpc.Request{UserID: "2", ViewerID: "invalid", Value: 1}, refused: true},
		{name: "TestHandleBalanceWagerInvalid zero stake", req: loyaltyrpc.Request{UserID: "2", ViewerID: "7"}, refused: true},
		{name: "TestHandleBalanceWagerInvalid negative stake", req: loyaltyrpc.Request{UserID: "2", ViewerID: "7", Value: -1}, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if tc.fund != 0 {
				h.fund(t, tc.fund)
			}
			reply := h.call(t, "balance.wager", tc.req)
			if tc.refused {
				requireRefused(t, reply)
			} else {
				assert.Equal(t, tc.want, reply)
			}
		})
	}
}

func TestBalanceReplyPreservesExactPointDigits(t *testing.T) {
	for _, points := range []int64{9007199254740993, math.MaxInt64, math.MinInt64} {
		t.Run(strconv.FormatInt(points, 10), func(t *testing.T) {
			h := newHarness(t)
			require.NoError(t, h.client.Balance.Update().SetPoints(points).Exec(t.Context()))

			var wire struct {
				Balance map[string]codec.RawMessage `json:"balance"`
			}
			require.NoError(t, codec.Unmarshal(h.raw(t, "balance.get", loyaltyrpc.Request{UserID: "2", ViewerID: "8"}), &wire))

			assert.Equal(t, strconv.FormatInt(points, 10), string(wire.Balance["points"]), "legacy field stays numeric")
			assert.Equal(t, `"`+strconv.FormatInt(points, 10)+`"`, string(wire.Balance["points_exact"]), "browser field stays decimal text")
		})
	}
}

func TestCounterSetDecimalValue(t *testing.T) {
	h := newHarness(t)
	created := h.call(t, "counter.create", loyaltyrpc.Request{UserID: "2", Name: "deaths"})
	require.True(t, created.Found)
	stored := int64(0)
	for _, tc := range []struct {
		name    string
		req     loyaltyrpc.Request
		want    int64
		refused bool
	}{
		{name: "keeps a decimal beyond the JavaScript range", req: loyaltyrpc.Request{CounterValue: "9223372036854775807", Value: 1}, want: math.MaxInt64},
		{name: "falls back to the numeric value", req: loyaltyrpc.Request{Value: 42}, want: 42},
		{name: "rejects a negative decimal", req: loyaltyrpc.Request{CounterValue: "-1"}, refused: true},
		{name: "rejects a decimal above int64", req: loyaltyrpc.Request{CounterValue: "9223372036854775808"}, refused: true},
		{name: "rejects a fraction", req: loyaltyrpc.Request{CounterValue: "1.5"}, refused: true},
		{name: "rejects text", req: loyaltyrpc.Request{CounterValue: "NaN"}, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.UserID, tc.req.Name = "2", "deaths"
			reply := h.call(t, "counter.set", tc.req)
			if tc.refused {
				requireRefused(t, reply)
			} else {
				assert.Equal(t, loyaltyrpc.Reply{Found: true}, reply)
				stored = tc.want
			}
			got := h.call(t, "counter.get", loyaltyrpc.Request{UserID: "2", Name: "deaths"})
			assert.Equal(t, stored, got.Counter.Value)
		})
	}
}
