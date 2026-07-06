package ratelimit

import (
	"context"
	"testing"
	"time"
)

func BenchmarkLeaseManagerLocalPremium(b *testing.B) {
	buckets := NewBucketStore(16)
	manager := NewLeaseManager(nil, buckets, nil, WithLeaseIdentity("local", "pod-a"))
	now := time.Now()
	plan := Plan{
		Version: planVersion, Epoch: 1, Generation: 1,
		// Wide window: the bench advances a fake clock per iteration to refill the
		// bucket, so the plan must stay valid across all b.N iterations.
		ValidFromMS: now.Add(-time.Second).UnixMilli(), ValidUntilMS: now.Add(100 * 365 * 24 * time.Hour).UnixMilli(),
		Members: []Member{{PodID: "pod-a", Region: "local"}},
	}
	if err := plan.ComputeDigest(); err != nil {
		b.Fatal(err)
	}
	if err := manager.ActivatePlan(plan, now, now, 0); err != nil {
		b.Fatal(err)
	}
	spec := NewSpec(100, 100.0/30.0)
	req := spec.ForDynamicKey("ratelimit:chat:", "chat", "123456789")
	ctx := context.Background()
	clock := now
	_, _ = manager.allowAt(ctx, &req, clock) // create and drain the local bucket

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Advance the clock so the realistic per-pod refill keeps a token
		// available; this isolates the hot-path cost, not bucket exhaustion.
		clock = clock.Add(time.Second)
		req = spec.ForDynamicKey("ratelimit:chat:", "chat", "123456789")
		allowed, err := manager.allowAt(ctx, &req, clock)
		if err != nil || !allowed {
			b.Fatalf("allow = %v, %v", allowed, err)
		}
	}
}

func BenchmarkLeaseManagerLocalStandard(b *testing.B) {
	buckets := NewBucketStore(16)
	manager := NewLeaseManager(nil, buckets, nil, WithLeaseIdentity("local", "pod-a"))
	now := time.Now()
	plan := Plan{
		Version: planVersion, Epoch: 1, Generation: 1,
		// Wide window: the bench advances a fake clock per iteration to refill the
		// bucket, so the plan must stay valid across all b.N iterations.
		ValidFromMS: now.Add(-time.Second).UnixMilli(), ValidUntilMS: now.Add(100 * 365 * 24 * time.Hour).UnixMilli(),
		Members: []Member{{PodID: "pod-a", Region: "local"}},
	}
	if err := plan.ComputeDigest(); err != nil {
		b.Fatal(err)
	}
	if err := manager.ActivatePlan(plan, now, now, 0); err != nil {
		b.Fatal(err)
	}
	sharedSpec := NewSpec(100, 100.0/30.0)
	standardSpec := NewSpec(50, 50.0/30.0)
	shared := sharedSpec.ForDynamicKey("ratelimit:chat:", "chat", "123456789")
	standard := standardSpec.ForDynamicKey("ratelimit:chat:standard:", "chat:standard", "123456789")
	ctx := context.Background()
	clock := now
	_, _ = manager.allowOrderedAt(ctx, &standard, &shared, clock)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clock = clock.Add(time.Second)
		shared = sharedSpec.ForDynamicKey("ratelimit:chat:", "chat", "123456789")
		standard = standardSpec.ForDynamicKey("ratelimit:chat:standard:", "chat:standard", "123456789")
		denied, err := manager.allowOrderedAt(ctx, &standard, &shared, clock)
		if err != nil || denied != 0 {
			b.Fatalf("denied = %d, %v", denied, err)
		}
	}
}
