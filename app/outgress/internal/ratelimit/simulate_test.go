package ratelimit_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Yiling-J/theine-go"
	"golang.org/x/time/rate"

	"ItsBagelBot/app/outgress/internal/ratelimit"
)

func TestSimulator(t *testing.T) {
	fmt.Println("Starting Rate Limit Simulator...")
	
	// Create local cache
	buckets, _ := theine.NewBuilder[string, *ratelimit.LocalBucket](1000).Build()
	
	// Pre-populate a leased chat bucket
	// Chat shared non-mod leased: 18 / 30s, burst 18
	// Chat standard non-mod leased: 9 / 30s, burst 9
	b := ratelimit.NewLocalBucket()
	now := time.Now()
	
	b.Update(now, 1, "pod-1", now.Add(-time.Minute), now.Add(time.Hour),
		rate.Limit(18.0/30.0), 18, // shared
		rate.Limit(9.0/30.0), 9,   // standard
	)
	buckets.Set("chat:test_broadcaster", b, 1)

	// We can pass nil for central since we are in leased mode with no permit service
	manager := ratelimit.NewLeaseManager(nil, buckets, nil, "leased")

	var allowedShared int32
	var allowedStandard int32
	var denied int32

	var wg sync.WaitGroup
	start := time.Now()

	// Simulate 100 concurrent requests instantly
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := ratelimit.Request{Key: "ratelimit:chat:test_broadcaster"}
			
			// Let's pretend some are standard, some are premium
			if id%2 == 0 {
				// Premium just tries shared
				ok, _ := manager.Allow(context.Background(), req)
				if ok {
					atomic.AddInt32(&allowedShared, 1)
				} else {
					atomic.AddInt32(&denied, 1)
				}
			} else {
				// Standard tries standard then shared
				reqStandard := ratelimit.Request{Key: "ratelimit:chat:standard:test_broadcaster"}
				deniedLevel, _ := manager.AllowOrdered(context.Background(), reqStandard, req)
				if deniedLevel == 0 {
					atomic.AddInt32(&allowedStandard, 1)
				} else {
					atomic.AddInt32(&denied, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Printf("Initial Burst Results (Duration: %v):\n", duration)
	fmt.Printf("Premium Allowed (Shared only): %d (Expected max ~18 combined)\n", allowedShared)
	fmt.Printf("Standard Allowed (Std+Shared): %d (Expected max ~9)\n", allowedStandard)
	fmt.Printf("Denied: %d\n", denied)

	// Ensure our metrics align with expected capacity
	if allowedShared+allowedStandard > 18 {
		t.Errorf("Leaked tokens! Allowed %d shared and %d standard (total %d), expected max 18", allowedShared, allowedStandard, allowedShared+allowedStandard)
	}

	// Wait 15 seconds to refill half the bucket
	fmt.Println("\nWaiting 15 seconds for buckets to partially refill...")
	time.Sleep(15 * time.Second)

	allowedShared = 0
	allowedStandard = 0
	denied = 0

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := ratelimit.Request{Key: "ratelimit:chat:test_broadcaster"}
			if id%2 == 0 {
				ok, _ := manager.Allow(context.Background(), req)
				if ok {
					atomic.AddInt32(&allowedShared, 1)
				} else {
					atomic.AddInt32(&denied, 1)
				}
			} else {
				reqStandard := ratelimit.Request{Key: "ratelimit:chat:standard:test_broadcaster"}
				deniedLevel, _ := manager.AllowOrdered(context.Background(), reqStandard, req)
				if deniedLevel == 0 {
					atomic.AddInt32(&allowedStandard, 1)
				} else {
					atomic.AddInt32(&denied, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	fmt.Printf("\nRefill Results (15s elapsed):\n")
	fmt.Printf("Premium Allowed (Shared only): %d (Expected ~9 combined)\n", allowedShared)
	fmt.Printf("Standard Allowed (Std+Shared): %d (Expected ~4-5)\n", allowedStandard)
	fmt.Printf("Denied: %d\n", denied)
}
