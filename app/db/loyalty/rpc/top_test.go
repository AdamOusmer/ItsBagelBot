// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"

	"ItsBagelBot/app/db/loyalty/ent/balance"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleTopGetSearchesByLogin(t *testing.T) {
	h := newHarness(t)

	for _, tc := range []struct {
		name string
		req  loyaltyrpc.Request
		want []string
	}{
		{"stored login returns the balance with its rank", loyaltyrpc.Request{UserID: "2", ViewerLogin: "receiver"}, []string{"8"}},
		{"at sign and capitals are normalized", loyaltyrpc.Request{UserID: "2", ViewerLogin: "@Receiver"}, []string{"8"}},
		{"unknown login matches nothing", loyaltyrpc.Request{UserID: "2", ViewerLogin: "nobody"}, nil},
		{"login with a wildcard matches nothing", loyaltyrpc.Request{UserID: "2", ViewerLogin: "rec%"}, nil},
		{"login over 25 characters matches nothing", loyaltyrpc.Request{UserID: "2", ViewerLogin: "receiverreceiverreceiverrec"}, nil},
		{"other channel never matches", loyaltyrpc.Request{UserID: "3", ViewerLogin: "receiver"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := h.call(t, "top.get", tc.req)
			var ids []string
			for _, b := range reply.Top {
				ids = append(ids, b.ViewerID)
			}
			assert.True(t, reply.Found)
			assert.Equal(t, tc.want, ids)
		})
	}
}

func TestHandleTopGetReconcilesTheStoredLogin(t *testing.T) {
	h := newHarness(t)

	reply := h.call(t, "top.get", loyaltyrpc.Request{UserID: "2", ViewerID: "8", ViewerLogin: "@Renamed"})
	require.Len(t, reply.Top, 1)
	assert.Equal(t, "renamed", reply.Top[0].ViewerLogin)
	assert.Equal(t, int64(2), reply.Top[0].Rank)

	row, err := h.client.Balance.Query().Where(balance.UserIDEQ(2), balance.ViewerIDEQ(8)).Only(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "renamed", row.ViewerLogin)

	missing := h.call(t, "top.get", loyaltyrpc.Request{UserID: "2", ViewerID: "999", ViewerLogin: "ghost"})
	assert.Empty(t, missing.Top)
	total, err := h.client.Balance.Query().Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, total)
}

func TestHandleTopGetIgnoresAnInvalidReconcileHint(t *testing.T) {
	h := newHarness(t)

	reply := h.call(t, "top.get", loyaltyrpc.Request{UserID: "2", ViewerID: "8", ViewerLogin: "bad login!"})
	require.Len(t, reply.Top, 1)
	assert.Equal(t, "receiver", reply.Top[0].ViewerLogin)
}
