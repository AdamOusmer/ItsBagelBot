package ratelimit

import (
	"context"
	"math"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func activeTestPlan(t *testing.T, members []Member, generation uint64) (Plan, time.Time) {
	t.Helper()
	now := time.Now()
	plan := Plan{
		Version: planVersion, Epoch: 1, Generation: generation,
		ValidFromMS: now.Add(-time.Second).UnixMilli(), ValidUntilMS: now.Add(time.Hour).UnixMilli(),
		Members: members,
	}
	if err := plan.ComputeDigest(); err != nil {
		t.Fatal(err)
	}
	return plan, now
}

func TestLocalSharesPreserveGlobalBudget(t *testing.T) {
	spec := NewSpec(700, 700.0/60.0)
	for members := 1; members <= 8; members++ {
		var burst int
		var refill float64
		for rank := 0; rank < members; rank++ {
			rate, memberBurst := localShare(spec, members, rank)
			burst += memberBurst
			refill += float64(rate)
		}
		if want := spec.capacity - spec.emergencyBurst; burst != want {
			t.Fatalf("members=%d burst=%d want=%d", members, burst, want)
		}
		if want := spec.refillPerSec - spec.emergencyRate; math.Abs(refill-want) > 1e-9 {
			t.Fatalf("members=%d refill=%f want=%f", members, refill, want)
		}
	}
}

func TestGenerationChangeStartsSameHolderEmpty(t *testing.T) {
	bucket := NewLocalBucket()
	start := time.Now()
	bucket.Update(start, 1, 1, "pod-a", start, start.Add(time.Hour), rate.Limit(10), 10, 0, 0)
	later := start.Add(time.Second)
	if !bucket.TryPremium(later) {
		t.Fatal("old generation did not refill")
	}
	bucket.Update(later, 2, 2, "pod-a", later, later.Add(time.Hour), rate.Limit(10), 10, 0, 0)
	if bucket.TryPremium(later) {
		t.Fatal("new generation replayed the old holder's burst")
	}
}

func TestStandardDenialDoesNotConsumeEitherBucket(t *testing.T) {
	bucket := NewLocalBucket()
	start := time.Now()
	bucket.Update(start, 1, 1, "pod-a", start, start.Add(time.Hour), 1, 1, 1, 1)
	later := start.Add(time.Second)
	if !bucket.TryPremium(later) {
		t.Fatal("shared setup debit failed")
	}
	standardBefore := bucket.standard.TokensAt(later)
	standard, shared := bucket.TryStandard(later)
	if standard || shared {
		t.Fatal("pair succeeded with an empty shared bucket")
	}
	if got := bucket.standard.TokensAt(later); got != standardBefore {
		t.Fatalf("standard tokens changed on atomic denial: before=%f after=%f", standardBefore, got)
	}
}

func TestPremiumCreatedBucketCanServeStandardTraffic(t *testing.T) {
	store := NewBucketStore(16)
	manager := NewLeaseManager(nil, store, nil, WithLeaseIdentity("local", "pod-a"))
	plan, now := activeTestPlan(t, []Member{{PodID: "pod-a", Region: "local"}}, 12)
	if err := manager.ActivatePlan(plan, now, now, 0); err != nil {
		t.Fatal(err)
	}
	bucketID := BucketID{Scope: "chat", Value: "123"}
	_ = manager.tryLocalPremium(now, manager.plan.Load(), bucketID, profileChat)
	bucket, ok := store.Load(bucketID)
	if !ok || !bucket.hasStandard {
		t.Fatal("premium-created bucket omitted its standard partition")
	}
	later := now.Add(time.Minute)
	standard, shared := manager.tryLocalStandard(later, manager.plan.Load(), bucketID, profileChat)
	if !standard || !shared {
		t.Fatal("standard traffic could not use a premium-created bucket")
	}
}

func TestCachedBucketReconfiguresWhenProfileChanges(t *testing.T) {
	store := NewBucketStore(16)
	manager := NewLeaseManager(nil, store, nil, WithLeaseIdentity("local", "pod-a"))
	plan, now := activeTestPlan(t, []Member{{PodID: "pod-a", Region: "local"}}, 13)
	if err := manager.ActivatePlan(plan, now, now, 0); err != nil {
		t.Fatal(err)
	}
	bucketID := BucketID{Scope: "chat", Value: "456"}
	_ = manager.tryLocalPremium(now, manager.plan.Load(), bucketID, profileChat)
	bucket, _ := store.Load(bucketID)
	chatRate, chatBurst := localShare(profileChatShared, 1, 0)
	chatStandardRate, chatStandardBurst := localShare(profileChatStandard, 1, 0)
	if !bucket.MatchesConfig(chatRate, chatBurst, chatStandardRate, chatStandardBurst) {
		t.Fatal("chat profile was not installed")
	}
	_ = manager.tryLocalPremium(now, manager.plan.Load(), bucketID, profileChatMod)
	modRate, modBurst := localShare(profileChatModShared, 1, 0)
	modStandardRate, modStandardBurst := localShare(profileChatModStandard, 1, 0)
	if !bucket.MatchesConfig(modRate, modBurst, modStandardRate, modStandardBurst) {
		t.Fatal("cached bucket retained the old moderator profile")
	}
}

func TestPodOutsidePlanCanBorrowButCannotGrant(t *testing.T) {
	manager := NewLeaseManager(nil, NewBucketStore(16), nil, WithLeaseIdentity("local", "pod-new"))
	plan, now := activeTestPlan(t, []Member{{PodID: "pod-a", Region: "local"}}, 14)
	if err := manager.ActivatePlan(plan, now, now, 0); err != nil {
		t.Fatalf("stateless non-holder could not install plan: %v", err)
	}
	if manager.plan.Load().selfIndex != -1 {
		t.Fatal("pod outside plan unexpectedly received a local share")
	}
	reply := manager.GrantPermit(now, BorrowRequest{
		Version: planVersion, Epoch: plan.Epoch, Generation: plan.Generation,
		Bucket: BucketID{Scope: "chat", Value: "123"}, Need: NeedShared, Profile: profileChat,
	})
	if reply.Paid != 0 || reply.Status != "invalid" {
		t.Fatalf("non-holder granted capacity: %+v", reply)
	}
}

func TestExpiredPlanFailsClosed(t *testing.T) {
	manager := NewLeaseManager(nil, NewBucketStore(16), nil, WithLeaseIdentity("local", "pod-a"))
	now := time.Now()
	plan := Plan{
		Version: planVersion, Epoch: 1, Generation: 15,
		ValidFromMS: now.Add(-time.Minute).UnixMilli(), ValidUntilMS: now.Add(-time.Second).UnixMilli(),
		Members: []Member{{PodID: "pod-a", Region: "local"}},
	}
	if err := plan.ComputeDigest(); err != nil {
		t.Fatal(err)
	}
	if err := manager.ActivatePlan(plan, now, now, 0); err != nil {
		t.Fatal(err)
	}
	allowed, err := manager.Allow(context.Background(), profileChatShared.ForDynamicKey("ratelimit:chat:", "chat", "123"))
	if err != nil || allowed {
		t.Fatalf("expired plan admitted request: allowed=%v err=%v", allowed, err)
	}
}

func TestLocalFastPathAllocatesNothing(t *testing.T) {
	store := NewBucketStore(16)
	manager := NewLeaseManager(nil, store, nil, WithLeaseIdentity("local", "pod-a"))
	plan, now := activeTestPlan(t, []Member{{PodID: "pod-a", Region: "local"}}, 11)
	if err := manager.ActivatePlan(plan, now, now, 0); err != nil {
		t.Fatal(err)
	}
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
