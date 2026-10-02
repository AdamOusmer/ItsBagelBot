// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ratelimit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingBorrower struct {
	calls int
}

func (b *countingBorrower) Borrow(_ context.Context, _ Member, request BorrowRequest) (BorrowReply, error) {
	b.calls++
	return BorrowReply{Version: planVersion, Epoch: request.Epoch, Paid: request.Need, Status: "granted"}, nil
}

var soloMembers = []Member{{PodID: "pod-a", Region: "local"}}

func activeTestPlan(t testing.TB, members []Member, generation uint64) (Plan, time.Time) {
	t.Helper()
	now := time.Now()
	plan := Plan{
		Version: planVersion, Epoch: 1, Generation: generation,
		ValidFromMS: now.Add(-time.Second).UnixMilli(), ValidUntilMS: now.Add(time.Hour).UnixMilli(),
		Members: members,
	}
	require.NoError(t, plan.ComputeDigest())
	return plan, now
}

func newTestManager(t *testing.T, podID string, members []Member, borrower PermitBorrower) (*LeaseManager, time.Time) {
	t.Helper()
	manager := NewLeaseManager(nil, NewBucketStore(16), borrower, Identity{Region: "local", PodID: podID})
	plan, now := activeTestPlan(t, members, 1)
	require.NoError(t, manager.ActivatePlan(plan, now, now, 0))
	return manager, now
}

func admitted(t *testing.T, manager *LeaseManager, req Request, at time.Time) int {
	t.Helper()
	count := 0
	for ; count < 10_000; count++ {
		ok, err := manager.allowAt(context.Background(), &req, at)
		require.NoError(t, err)
		if !ok {
			break
		}
	}
	return count
}

func chatRequests(userID string) (shared, standard Request) {
	return profileChatShared.ForDynamicKey("ratelimit:chat:", "chat", userID),
		profileChatStandard.ForDynamicKey("ratelimit:chat:standard:", "chat:standard", userID)
}

func TestLeasedSharesAcrossPodsStayWithinTheGlobalBudget(t *testing.T) {
	const (
		leasedBurst = 630
		leasedRate  = 10.5
		window      = 40 * time.Second
	)
	for podCount := 1; podCount <= 8; podCount++ {
		t.Run(fmt.Sprintf("%d pods", podCount), func(t *testing.T) {
			members := make([]Member, podCount)
			for i := range members {
				members[i] = Member{PodID: fmt.Sprintf("pod-%d", i), Region: "local"}
			}
			var burst, refilled int
			for _, member := range members {
				manager, now := newTestManager(t, member.PodID, members, nil)
				drained := now.Add(30 * time.Minute)
				burst += admitted(t, manager, HelixAppRequest(), drained)
				refilled += admitted(t, manager, HelixAppRequest(), drained.Add(window))
			}
			assert.Equal(t, leasedBurst, burst)
			assert.InDelta(t, leasedRate*window.Seconds(), refilled, float64(podCount))
		})
	}
}

func TestFixedSystemBucketWarmsBeforeFirstBurst(t *testing.T) {
	store := NewBucketStore(16)
	manager := NewLeaseManager(nil, store, nil, Identity{Region: "local", PodID: "pod-a"})
	plan, now := activeTestPlan(t, []Member{{PodID: "pod-a", Region: "local"}}, 31)
	if err := manager.ActivatePlan(plan, now, now, 0); err != nil {
		t.Fatal(err)
	}

	later := now.Add(time.Minute)
	req := profileHelixSystemShare.ForKey("ratelimit:helix:system")
	for i := 0; i < 20; i++ {
		allowed, err := manager.allowAt(context.Background(), &req, later)
		if err != nil || !allowed {
			t.Fatalf("system request %d denied after warmup: allowed=%v err=%v", i+1, allowed, err)
		}
	}
}

func TestFixedHelixBucketsRenewWithoutTraffic(t *testing.T) {
	store := NewBucketStore(16)
	manager := NewLeaseManager(nil, store, nil, Identity{Region: "local", PodID: "pod-a"})
	base := time.Now()
	var original *LocalBucket

	for epoch := uint64(1); epoch <= 5; epoch++ {
		now := base.Add(time.Duration(epoch-1) * 30 * time.Second)
		plan := Plan{
			Version: planVersion, Epoch: epoch, Generation: 32,
			ValidFromMS:  now.Add(-time.Second).UnixMilli(),
			ValidUntilMS: now.Add(29 * time.Second).UnixMilli(),
			Members:      []Member{{PodID: "pod-a", Region: "local"}},
		}
		if err := plan.ComputeDigest(); err != nil {
			t.Fatal(err)
		}
		if err := manager.ActivatePlan(plan, now, now, 0); err != nil {
			t.Fatal(err)
		}
		bucket, ok := store.Load(BucketID{Scope: "helix:system"})
		if !ok {
			t.Fatalf("fixed system bucket missing at epoch %d", epoch)
		}
		if original == nil {
			original = bucket
		} else if bucket != original {
			t.Fatalf("fixed system bucket was evicted and recreated at epoch %d", epoch)
		}
	}
}

func TestPremiumCreatedBucketCanServeStandardTraffic(t *testing.T) {
	manager, now := newTestManager(t, "pod-a", soloMembers, nil)
	shared, standard := chatRequests("123")

	_, err := manager.allowAt(context.Background(), &shared, now)
	require.NoError(t, err)
	denied, err := manager.allowOrderedAt(context.Background(), &standard, &shared, now.Add(time.Minute))

	require.NoError(t, err)
	assert.Zero(t, denied)
}

func TestColdChatBucketSkipsPeerBorrowOnce(t *testing.T) {
	borrower := &countingBorrower{}
	manager := NewLeaseManager(nil, NewBucketStore(16), borrower, Identity{Region: "local", PodID: "pod-a"})
	plan, now := activeTestPlan(t, []Member{
		{PodID: "pod-a", Region: "local"},
		{PodID: "pod-b", Region: "remote"},
	}, 21)
	if err := manager.ActivatePlan(plan, now, now, 0); err != nil {
		t.Fatal(err)
	}
	req := profileChatShared.ForDynamicKey("ratelimit:chat:", "chat", "123")

	allowed, err := manager.allowAt(context.Background(), &req, now)
	if err != nil || allowed {
		t.Fatalf("cold emergency decision = %v, %v; want denied without central limiter", allowed, err)
	}
	if borrower.calls != 0 {
		t.Fatalf("cold bucket made %d peer calls, want 0", borrower.calls)
	}

	allowed, err = manager.allowAt(context.Background(), &req, now)
	if err != nil || !allowed {
		t.Fatalf("warm peer decision = %v, %v; want granted", allowed, err)
	}
	if borrower.calls != 1 {
		t.Fatalf("warm bucket made %d peer calls, want 1", borrower.calls)
	}
}

func TestCachedBucketReconfiguresWhenProfileChanges(t *testing.T) {
	manager, now := newTestManager(t, "pod-a", soloMembers, nil)
	chat, _ := chatRequests("456")
	mod := profileChatModShared.ForDynamicKey("ratelimit:chat:", "chat", "456")

	for _, req := range []Request{chat, mod} {
		_, err := manager.allowAt(context.Background(), &req, now)
		require.NoError(t, err)
	}

	assert.Equal(t, 90, admitted(t, manager, mod, now.Add(30*time.Minute)))
}

func TestPodOutsidePlanCannotGrant(t *testing.T) {
	manager := NewLeaseManager(nil, NewBucketStore(16), nil, Identity{Region: "local", PodID: "pod-new"})
	plan, now := activeTestPlan(t, soloMembers, 14)
	require.NoError(t, manager.ActivatePlan(plan, now, now, 0))

	reply := manager.GrantPermit(now, BorrowRequest{
		Version: planVersion, Epoch: plan.Epoch, Generation: plan.Generation,
		Bucket: BucketID{Scope: "chat", Value: "123"}, Need: NeedShared, Profile: profileChat,
	})

	assert.Equal(t, BorrowReply{Version: planVersion, Epoch: plan.Epoch, Status: "invalid"}, reply)
}

func TestExpiredPlanFailsClosed(t *testing.T) {
	manager := NewLeaseManager(nil, NewBucketStore(16), nil, Identity{Region: "local", PodID: "pod-a"})
	now := time.Now()
	plan := Plan{
		Version: planVersion, Epoch: 1, Generation: 15,
		ValidFromMS: now.Add(-time.Minute).UnixMilli(), ValidUntilMS: now.Add(-time.Second).UnixMilli(),
		Members: soloMembers,
	}
	require.NoError(t, plan.ComputeDigest())
	require.NoError(t, manager.ActivatePlan(plan, now, now, 0))

	allowed, err := manager.Allow(context.Background(), profileChatShared.ForDynamicKey("ratelimit:chat:", "chat", "123"))

	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestGuardRetryAfterCoversBothSidesOfEpochBoundary(t *testing.T) {
	manager := NewLeaseManager(nil, NewBucketStore(16), nil, Identity{Region: "local", PodID: "pod-a"})
	serverNow := time.Now()
	localNow := serverNow
	guard := 250 * time.Millisecond
	plan := Plan{
		Version: planVersion, Epoch: 1, Generation: 16,
		ValidFromMS:  serverNow.Add(time.Second).UnixMilli(),
		ValidUntilMS: serverNow.Add(31 * time.Second).UnixMilli(),
		Members:      []Member{{PodID: "pod-a", Region: "local"}},
	}
	if err := plan.ComputeDigest(); err != nil {
		t.Fatal(err)
	}
	if err := manager.ActivatePlan(plan, serverNow, localNow, guard); err != nil {
		t.Fatal(err)
	}

	active := manager.plan.Load()
	beforeOpening := active.notBefore.Add(-40 * time.Millisecond)
	if got := manager.guardRetryAfterAt(beforeOpening); got != 40*time.Millisecond {
		t.Fatalf("opening guard retry = %v, want 40ms", got)
	}
	beforeNextGeneration := active.nextNotBefore.Add(-40 * time.Millisecond)
	if got := manager.guardRetryAfterAt(beforeNextGeneration); got != 40*time.Millisecond {
		t.Fatalf("closing guard retry = %v, want 40ms", got)
	}
	if got := manager.guardRetryAfterAt(active.notBefore.Add(time.Second)); got != 0 {
		t.Fatalf("active plan retry = %v, want zero", got)
	}
}

func TestLocalFastPathAllocatesNothing(t *testing.T) {
	manager, now := newTestManager(t, "pod-a", soloMembers, nil)
	spec := NewSpec(100, 100.0/30.0)
	req := spec.ForDynamicKey("ratelimit:chat:", "chat", "123456789")
	clock := now
	_, _ = manager.allowAt(context.Background(), &req, clock)
	allocations := testing.AllocsPerRun(1000, func() {
		clock = clock.Add(time.Second)
		dynamicRequest := spec.ForDynamicKey("ratelimit:chat:", "chat", "123456789")
		allowed, err := manager.allowAt(context.Background(), &dynamicRequest, clock)
		if err != nil || !allowed {
			panic("unexpected local denial")
		}
	})
	if allocations != 0 {
		t.Fatalf("local decision allocated %.1f objects/op, want 0", allocations)
	}
}
