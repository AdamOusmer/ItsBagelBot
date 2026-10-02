// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingBorrower struct {
	requests []BorrowRequest
}

func (b *recordingBorrower) Borrow(_ context.Context, _ Member, request BorrowRequest) (BorrowReply, error) {
	b.requests = append(b.requests, request)
	return BorrowReply{}, errors.New("recorded")
}

func peerShareRequest(t *testing.T, members []Member) BorrowRequest {
	t.Helper()
	recorder := &recordingBorrower{}
	manager, _ := newTestManager(t, members[0].PodID, members, recorder)
	_, err := manager.AllowOrdered(context.Background(), profileHelixStandard.ForKey("ratelimit:helix:app:standard"), HelixAppRequest())
	require.NoError(t, err)
	require.Len(t, recorder.requests, 1)
	return recorder.requests[0]
}

func requestPermit(t *testing.T, nc *nats.Conn, subject string, request BorrowRequest) BorrowReply {
	t.Helper()
	data, err := codec.Marshal(&request)
	require.NoError(t, err)
	message, err := nc.Request(subject, data, time.Second)
	require.NoError(t, err)
	var reply BorrowReply
	require.NoError(t, codec.Unmarshal(message.Data, &reply))
	return reply
}

func TestPermitServiceGrantsPeerLeasesAndDedupesRetries(t *testing.T) {
	const donorSubject = "bagel.outgress.permit.v2.local.pod-b"
	members := []Member{{PodID: "pod-a", Region: "local"}, {PodID: "pod-b", Region: "local"}}
	request := peerShareRequest(t, members)
	request.RequestID = "req-1"
	request.DeadlineMS = time.Now().Add(time.Minute).UnixMilli()

	nc := testnats.Connect(t)
	donor, _ := newTestManager(t, "pod-b", members, nil)
	service, err := NewPermitService(nc, "local", "pod-b", nil)
	require.NoError(t, err)
	t.Cleanup(service.Close)
	service.SetGrantor(donor)

	var first BorrowReply
	require.Eventually(t, func() bool {
		first = requestPermit(t, nc, donorSubject, request)
		return first.Status == "granted"
	}, 5*time.Second, 20*time.Millisecond, "donor never accrued a token")
	assert.Equal(t, NeedStandard|NeedShared, first.Paid)
	assert.NotEmpty(t, first.GrantID)

	duplicate := requestPermit(t, nc, donorSubject, request)
	assert.Equal(t, first.GrantID, duplicate.GrantID)

	var borrowed BorrowReply
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		reply, err := service.Borrow(ctx, members[1], request)
		borrowed = reply
		return err == nil && reply.Paid != 0
	}, 5*time.Second, 20*time.Millisecond, "borrow never paid")
	assert.Equal(t, NeedStandard|NeedShared, borrowed.Paid)
}
