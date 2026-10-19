// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleBalanceSpendIDWinsOverLogin(t *testing.T) {
	h := newHarness(t)

	reply := h.call(t, "balance.spend", loyaltyrpc.Request{UserID: "2", ViewerID: "7", ViewerLogin: "receiver", Value: 300})

	assert.Equal(t, loyaltyrpc.Reply{Found: true, Spent: true, Balance: balanceOf("7", "receiver", 700)}, reply)
	h.requireStored(t, map[uint64]int64{7: 700, 8: 100})
}

func TestHandleBalanceSpendRefusesLoginOnly(t *testing.T) {
	h := newHarness(t)

	requireRefused(t, h.call(t, "balance.spend", loyaltyrpc.Request{UserID: "2", ViewerLogin: "sender", Value: 300}))

	h.requireStored(t, map[uint64]int64{7: 1000})
}

func TestHandleCounterSetRefusesLoginOnly(t *testing.T) {
	h := newHarness(t)
	h.call(t, "counter.create", loyaltyrpc.Request{UserID: "2", Name: "hugs", Scope: "viewer"})

	requireRefused(t, h.call(t, "counter.set", loyaltyrpc.Request{UserID: "2", Name: "hugs", ViewerLogin: "receiver", Value: 3}))

	assert.True(t, h.call(t, "counter.set", loyaltyrpc.Request{UserID: "2", Name: "hugs", ViewerID: "8", ViewerLogin: "receiver", Value: 3}).Found)
}

func TestHandleBalanceSpendByIDRefusals(t *testing.T) {
	h := newHarness(t)

	unknown := h.call(t, "balance.spend", loyaltyrpc.Request{UserID: "2", ViewerID: "99", ViewerLogin: "sender", Value: 1})
	short := h.call(t, "balance.spend", loyaltyrpc.Request{UserID: "2", ViewerID: "8", ViewerLogin: "sender", Value: 500})
	invalid := h.call(t, "balance.spend", loyaltyrpc.Request{UserID: "2", ViewerID: "nope", Value: 1})

	assert.Equal(t, loyaltyrpc.Reply{}, unknown, "a matching login never rescues an unknown ID")
	assert.Equal(t, loyaltyrpc.Reply{Found: true, Balance: balanceOf("8", "receiver", 100)}, short)
	requireRefused(t, invalid)
	h.requireStored(t, map[uint64]int64{7: 1000, 8: 100})
}

func TestHandleBalanceAddIDWinsOverLogin(t *testing.T) {
	h := newHarness(t)

	reply := h.call(t, "balance.add", loyaltyrpc.Request{UserID: "2", TargetViewerID: "7", ViewerLogin: "receiver", Value: 5})

	require.True(t, reply.Found)
	assert.Equal(t, balanceOf("7", "receiver", 1005), reply.Balance)
	h.requireStored(t, map[uint64]int64{7: 1005, 8: 100})
}

func TestHandleBalanceTransferIDWinsOverLogin(t *testing.T) {
	h := newHarness(t)

	reply := h.call(t, "balance.transfer", loyaltyrpc.Request{UserID: "2", ViewerID: "7", TargetViewerID: "8", ViewerLogin: "sender2", Value: 400})

	assert.Equal(t, loyaltyrpc.Reply{Found: true, Spent: true, Balance: balanceOf("7", "sender", 600), TargetBalance: balanceOf("8", "sender2", 500)}, reply)
}
