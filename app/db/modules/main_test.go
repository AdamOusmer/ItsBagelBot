// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	domainrpc "ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/rpc/projection"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCanonicalAccountReplyRequiresProof(t *testing.T) {
	for _, test := range []struct {
		name     string
		reply    projection.UserReply
		allowed  bool
		instance int64
	}{
		{name: "known", reply: projection.UserReply{UserID: "1001", AccountCreatedAt: 101}, allowed: true, instance: 101},
		{name: "verified absent", reply: projection.UserReply{UserID: "1001", Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "gone")}, allowed: true},
		{name: "wrong absent account", reply: projection.UserReply{UserID: "1002", Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "gone")}},
		{name: "unstamped absence", reply: projection.UserReply{Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "gone")}},
		{name: "nonzero absence", reply: projection.UserReply{UserID: "1001", AccountCreatedAt: 101, Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "gone")}},
		{name: "unavailable", reply: projection.UserReply{Refusal: domainrpc.Refused(domainrpc.CodeUnavailable, "source unavailable")}},
		{name: "internal failure", reply: projection.UserReply{Refusal: domainrpc.Refused(domainrpc.CodeInternal, "source failure")}},
		{name: "unstamped success", reply: projection.UserReply{UserID: "1001"}},
		{name: "negative", reply: projection.UserReply{UserID: "1001", AccountCreatedAt: -1}},
		{name: "wrong account", reply: projection.UserReply{UserID: "1002", AccountCreatedAt: 101}},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, err := canonicalAccountInstance(test.reply, "1001")
			if test.allowed {
				require.NoError(t, err)
				require.Equal(t, test.instance, instance)
			} else {
				require.Error(t, err)
			}
		})
	}
}
