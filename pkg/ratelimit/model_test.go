// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ratelimit

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestModel_Property(t *testing.T) {

	b := NewLocalBucket()
	now := time.Now()

	var successfulAdmissions int

	b.Update(now, BucketConfig{Epoch: 1, Generation: 1, Holder: "pod-a", NotBefore: now, NotAfter: now.Add(time.Hour), SharedRate: rate.Limit(10), SharedBurst: 10, StandardRate: rate.Limit(5), StandardBurst: 5})

	rng := rand.New(rand.NewSource(42))

	for i := 0; i < 1000; i++ {
		step := time.Duration(rng.Intn(50)+1) * time.Millisecond
		now = now.Add(step)

		action := rng.Intn(100)

		if action < 10 {
			b.Renew(b.Epoch()+1, now, now.Add(time.Hour))
		} else if action < 15 {
			b.Update(now, BucketConfig{Epoch: b.Epoch() + 1, Generation: 1, Holder: "pod-a", NotBefore: now, NotAfter: now.Add(time.Hour), SharedRate: rate.Limit(10), SharedBurst: 10, StandardRate: rate.Limit(5), StandardBurst: 5})
		} else if action < 20 {
			b.Update(now, BucketConfig{Epoch: b.Epoch() + 1, Generation: 2, Holder: "pod-b", NotBefore: now, NotAfter: now.Add(time.Hour), SharedRate: rate.Limit(10), SharedBurst: 10, StandardRate: rate.Limit(5), StandardBurst: 5})
		} else if action < 60 {
			if b.TryPremium(now) {
				successfulAdmissions++
			}
		} else {
			st, sh := b.TryStandard(now)
			if st && sh {
				successfulAdmissions++
			}
		}
	}

	assert.LessOrEqual(t, successfulAdmissions, 500, "Should not exceed theoretical max capacity")
}
