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

type lease struct {
	epoch, generation uint64
	from, to          time.Duration
	shared, standard  rate.Limit
}

func newLease(epoch, generation uint64, from, to time.Duration) lease {
	return lease{epoch: epoch, generation: generation, from: from, to: to, shared: 10, standard: 5}
}

func (l lease) withRates(shared, standard rate.Limit) lease {
	l.shared, l.standard = shared, standard
	return l
}

func (l lease) config(start time.Time) BucketConfig {
	return BucketConfig{
		Epoch: l.epoch, Generation: l.generation, Holder: "pod1",
		NotBefore: start.Add(l.from), NotAfter: start.Add(l.to),
		SharedRate: l.shared, SharedBurst: max(1, int(l.shared)),
		StandardRate: l.standard, StandardBurst: max(1, int(l.standard)),
	}
}

type leaseStep func(t *testing.T, b *LocalBucket, start time.Time)

func update(at time.Duration, l lease) leaseStep {
	return func(_ *testing.T, b *LocalBucket, start time.Time) { b.Update(start.Add(at), l.config(start)) }
}

func renew(epoch uint64, l lease) leaseStep {
	return func(_ *testing.T, b *LocalBucket, start time.Time) {
		b.Renew(epoch, start.Add(l.from), start.Add(l.to))
	}
}

func epochIs(want uint64) leaseStep {
	return func(t *testing.T, b *LocalBucket, _ time.Time) { assert.Equal(t, want, b.Epoch()) }
}

func premium(at time.Duration, want bool) leaseStep {
	return func(t *testing.T, b *LocalBucket, start time.Time) {
		assert.Equal(t, want, b.TryPremium(start.Add(at)), "premium at %s", at)
	}
}

func standard(at time.Duration, want bool) leaseStep {
	return func(t *testing.T, b *LocalBucket, start time.Time) {
		standard, shared := b.TryStandard(start.Add(at))
		assert.Equal(t, [2]bool{want, want}, [2]bool{standard, shared}, "standard pair at %s", at)
	}
}

func TestLocalBucketLeaseAdmission(t *testing.T) {
	const second = time.Second
	repeat := func(n int, step leaseStep) []leaseStep {
		steps := make([]leaseStep, n)
		for i := range steps {
			steps[i] = step
		}
		return steps
	}
	tests := []struct {
		name  string
		steps []leaseStep
	}{
		{name: "starts empty and refills", steps: []leaseStep{
			update(0, newLease(1, 1, -second, second)),
			premium(0, false), standard(0, false), premium(100*time.Millisecond, true),
		}},
		{name: "standard traffic pays both buckets and falls back to shared", steps: append(append(
			[]leaseStep{update(0, newLease(1, 1, -second, time.Hour))},
			repeat(5, standard(2*second, true))...),
			standard(2*second, false), premium(2*second, true),
		)},
		{name: "admits only inside the lease window", steps: []leaseStep{
			update(0, newLease(1, 1, second, 2*second)),
			premium(0, false), standard(0, false), premium(1500*time.Millisecond, true), premium(3*second, false),
		}},
		{name: "renewal extends the window and advances the epoch", steps: []leaseStep{
			update(0, newLease(1, 1, -second, second)),
			premium(1500*time.Millisecond, false),
			renew(2, newLease(2, 1, 0, 2*second)),
			epochIs(2), premium(1500*time.Millisecond, true),
		}},
		{name: "resizing a lease keeps admitting", steps: []leaseStep{
			update(0, newLease(1, 1, -second, time.Hour)),
			update(second, newLease(2, 1, second, time.Hour).withRates(20, 10)),
			premium(second, true),
		}},
		{name: "new generation for the same holder starts empty", steps: []leaseStep{
			update(0, newLease(1, 1, 0, time.Hour)),
			premium(second, true),
			update(second, newLease(2, 2, second, time.Hour)),
			premium(second, false),
		}},
		{name: "denied standard pair leaves the standard bucket untouched", steps: []leaseStep{
			update(0, newLease(1, 1, 0, time.Hour).withRates(1, 0.1)),
			premium(20*second, true),
			standard(20*second, false),
			standard(21*second, true),
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bucket := NewLocalBucket()
			start := time.Now()
			for _, step := range tc.steps {
				step(t, bucket, start)
			}
		})
	}
}

type modelAction struct {
	below int
	apply func(b *LocalBucket, now time.Time) bool
}

func modelActions() []modelAction {
	relet := func(b *LocalBucket, now time.Time, generation uint64, holder string) {
		b.Update(now, BucketConfig{
			Epoch: b.Epoch() + 1, Generation: generation, Holder: holder,
			NotBefore: now, NotAfter: now.Add(time.Hour),
			SharedRate: 10, SharedBurst: 10, StandardRate: 5, StandardBurst: 5,
		})
	}
	return []modelAction{
		{10, func(b *LocalBucket, now time.Time) bool { b.Renew(b.Epoch()+1, now, now.Add(time.Hour)); return false }},
		{15, func(b *LocalBucket, now time.Time) bool { relet(b, now, 1, "pod-a"); return false }},
		{20, func(b *LocalBucket, now time.Time) bool { relet(b, now, 2, "pod-b"); return false }},
		{60, func(b *LocalBucket, now time.Time) bool { return b.TryPremium(now) }},
		{100, func(b *LocalBucket, now time.Time) bool {
			standard, shared := b.TryStandard(now)
			return standard && shared
		}},
	}
}

func pickAction(actions []modelAction, roll int) modelAction {
	for _, action := range actions {
		if roll < action.below {
			return action
		}
	}
	return actions[len(actions)-1]
}

func TestLocalBucketNeverAdmitsAboveTheoreticalCapacityUnderRandomLeaseChurn(t *testing.T) {
	bucket := NewLocalBucket()
	now := time.Now()
	bucket.Update(now, BucketConfig{Epoch: 1, Generation: 1, Holder: "pod-a", NotBefore: now, NotAfter: now.Add(time.Hour), SharedRate: 10, SharedBurst: 10, StandardRate: 5, StandardBurst: 5})
	actions := modelActions()
	rng := rand.New(rand.NewSource(42))

	admissions := 0
	for range 1000 {
		now = now.Add(time.Duration(rng.Intn(50)+1) * time.Millisecond)
		if pickAction(actions, rng.Intn(100)).apply(bucket, now) {
			admissions++
		}
	}

	assert.LessOrEqual(t, admissions, 500)
}
