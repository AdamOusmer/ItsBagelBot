package ratelimit

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)

type PermitBorrower interface {
	Borrow(context.Context, Member, BorrowRequest) (BorrowReply, error)
}

// selfShare is this pod's precomputed lease allotment for one bucket profile.
// Shares depend only on the plan (member count and stable order), so they are
// derived once at activation and read lock-free on every admission.
type selfShare struct {
	sharedRate    rate.Limit
	sharedBurst   int
	standardRate  rate.Limit
	standardBurst int
	signature     uint64 // precomputed bucketConfigSignature for lock-free hit checks
	valid         bool
}

type activePlan struct {
	epoch       uint64
	generation  uint64
	validFromMS int64
	notBefore   time.Time
	notAfter    time.Time
	members     []Member
	selfIndex   int
	selfPodID   string
	shares      [profileHelixSystem + 1]selfShare
}

type LeaseManager struct {
	central        *Limiter
	local          *BucketStore
	permit         PermitBorrower
	podID          string
	region         string
	plan           atomic.Pointer[activePlan]
	borrowSlots    *semaphore.Weighted
	emergencySlots *semaphore.Weighted
}

type LeaseOption func(*LeaseManager)

func WithLeaseIdentity(region, podID string) LeaseOption {
	return func(manager *LeaseManager) {
		manager.region = region
		manager.podID = podID
	}
}

func NewLeaseManager(central *Limiter, local *BucketStore, permit PermitBorrower, options ...LeaseOption) *LeaseManager {
	if local == nil {
		local = NewBucketStore(16)
	}
	manager := &LeaseManager{
		central: central, local: local, permit: permit,
		borrowSlots: semaphore.NewWeighted(64), emergencySlots: semaphore.NewWeighted(32),
	}
	for _, option := range options {
		option(manager)
	}
	return manager
}

// ActivatePlan maps Valkey server time onto local monotonic deadlines. It may be
// called before the boundary; admission uses the plan only inside the guarded
// interval.
func (m *LeaseManager) ActivatePlan(plan Plan, serverNow, localNow time.Time, guard time.Duration) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	canonical := canonicalPlan(plan)
	selfIndex := -1
	for i := range canonical.Members {
		if canonical.Members[i].PodID == m.podID {
			selfIndex = i
			break
		}
	}

	notBefore := localNow.Add(time.UnixMilli(canonical.ValidFromMS).Sub(serverNow) + guard)
	notAfter := localNow.Add(time.UnixMilli(canonical.ValidUntilMS).Sub(serverNow) - guard)
	if !notBefore.Before(notAfter) {
		return errors.New("ratelimit: lease guard consumes plan interval")
	}
	ap := &activePlan{
		epoch: canonical.Epoch, generation: canonical.Generation,
		validFromMS: canonical.ValidFromMS, notBefore: notBefore, notAfter: notAfter,
		members: canonical.Members, selfIndex: selfIndex,
	}
	if selfIndex >= 0 {
		ap.selfPodID = canonical.Members[selfIndex].PodID
		members := len(canonical.Members)
		for profile := profileChat; profile <= profileHelixSystem; profile++ {
			shared, standard, ok := specsForProfile(profile)
			if !ok {
				continue
			}
			sharedRate, sharedBurst := localShare(shared, members, selfIndex)
			if sharedBurst <= 0 {
				continue
			}
			var standardRate rate.Limit
			var standardBurst int
			if standard.capacity != 0 {
				standardRate, standardBurst = localShare(standard, members, selfIndex)
			}
			ap.shares[profile] = selfShare{
				sharedRate: sharedRate, sharedBurst: sharedBurst,
				standardRate: standardRate, standardBurst: standardBurst,
				signature: bucketConfigSignature(sharedRate, sharedBurst, standardRate, standardBurst),
				valid:     true,
			}
		}
	}
	m.plan.Store(ap)
	if m.local != nil {
		// Keep the immediately previous incarnation so an unchanged holder can
		// renew without losing its token balance. Older idle buckets are safe to
		// discard; recreation starts empty.
		m.local.DeleteExpired(localNow.Add(-2 * notAfter.Sub(notBefore)).UnixNano())
	}
	return nil
}

func (m *LeaseManager) Allow(ctx context.Context, req Request) (bool, error) {
	return m.allowAt(ctx, &req, time.Now())
}

// allowAt is the admission core with an explicit clock so deterministic tests
// and benchmarks can refill local buckets without sleeping. Production callers
// go through Allow with the wall clock. req is a pointer to avoid copying the
// embedded Spec on the hot path; allowAt never retains it.
func (m *LeaseManager) allowAt(ctx context.Context, req *Request, now time.Time) (bool, error) {
	plan := m.active(now)
	if plan == nil {
		if m.plan.Load() != nil {
			// A plan exists but we are in the guard gap or past its expiry. Fail
			// closed: another generation may already be spending, and the gap is a
			// deliberate sacrifice that prevents overlap.
			return false, nil
		}
		// No plan has ever been installed; bootstrap on the emergency partition.
		return m.emergencyAllow(ctx, *req, 1, now.UnixMilli())
	}

	bucketID := req.bucketID()
	allowed, existed := m.tryLocalPremiumState(now, plan, bucketID, req.Spec.profile)
	if allowed {
		return true, nil
	}
	if skipColdChatBorrow(plan, existed, req.Spec.profile) {
		return m.emergencyAllow(ctx, *req, plan.generation, plan.validFromMS)
	}

	need := NeedShared
	if req.Spec.profile == profileHelixSystem {
		need = NeedSystem
	}
	if m.borrow(ctx, plan, bucketID, need, req.Spec, Spec{}) == need {
		return true, nil
	}
	return m.emergencyAllow(ctx, *req, plan.generation, plan.validFromMS)
}

func (m *LeaseManager) AllowOrdered(ctx context.Context, first, second Request) (uint8, error) {
	return m.allowOrderedAt(ctx, &first, &second, time.Now())
}

func (m *LeaseManager) allowOrderedAt(ctx context.Context, first, second *Request, now time.Time) (uint8, error) {
	plan := m.active(now)
	if plan == nil {
		if m.plan.Load() != nil {
			return 2, nil
		}
		return m.emergencyAllowOrdered(ctx, *first, *second, 1, now.UnixMilli())
	}

	bucketID := second.bucketID()
	localFirst, localShared, existed := m.tryLocalStandardState(now, plan, bucketID, second.Spec.profile)
	if localFirst && localShared {
		return 0, nil
	}
	if skipColdChatBorrow(plan, existed, second.Spec.profile) {
		return m.emergencyAllowOrdered(ctx, *first, *second, plan.generation, plan.validFromMS)
	}

	if m.borrow(ctx, plan, bucketID, NeedStandard|NeedShared, second.Spec, first.Spec) == NeedStandard|NeedShared {
		return 0, nil
	}
	return m.emergencyAllowOrdered(ctx, *first, *second, plan.generation, plan.validFromMS)
}

func (m *LeaseManager) active(now time.Time) *activePlan {
	plan := m.plan.Load()
	if plan == nil || now.Before(plan.notBefore) || !now.Before(plan.notAfter) {
		return nil
	}
	return plan
}

func (m *LeaseManager) tryLocalPremium(now time.Time, plan *activePlan, bucketID BucketID, profile uint8) bool {
	allowed, _ := m.tryLocalPremiumState(now, plan, bucketID, profile)
	return allowed
}

func (m *LeaseManager) tryLocalPremiumState(now time.Time, plan *activePlan, bucketID BucketID, profile uint8) (bool, bool) {
	if plan.selfIndex < 0 || int(profile) >= len(plan.shares) {
		return false, false
	}
	share := &plan.shares[profile]
	if !share.valid {
		return false, false
	}
	bucket, existed := m.configuredBucket(now, plan, bucketID, plan.selfPodID, share)
	allowed, stale := bucket.TryPremiumLease(now, plan.epoch, plan.generation)
	if stale {
		bucket.Update(now, plan.epoch, plan.generation, plan.selfPodID, plan.notBefore, plan.notAfter, share.sharedRate, share.sharedBurst, share.standardRate, share.standardBurst)
		allowed, _ = bucket.TryPremiumLease(now, plan.epoch, plan.generation)
	}
	return allowed, existed
}

func (m *LeaseManager) tryLocalStandard(now time.Time, plan *activePlan, bucketID BucketID, profile uint8) (bool, bool) {
	standard, shared, _ := m.tryLocalStandardState(now, plan, bucketID, profile)
	return standard, shared
}

func (m *LeaseManager) tryLocalStandardState(now time.Time, plan *activePlan, bucketID BucketID, profile uint8) (bool, bool, bool) {
	if plan.selfIndex < 0 || int(profile) >= len(plan.shares) {
		return false, false, false
	}
	share := &plan.shares[profile]
	if !share.valid || share.standardBurst <= 0 {
		return false, false, false
	}
	bucket, existed := m.configuredBucket(now, plan, bucketID, plan.selfPodID, share)
	standard, shared, stale := bucket.TryStandardLease(now, plan.epoch, plan.generation)
	if stale {
		bucket.Update(now, plan.epoch, plan.generation, plan.selfPodID, plan.notBefore, plan.notAfter, share.sharedRate, share.sharedBurst, share.standardRate, share.standardBurst)
		standard, shared, _ = bucket.TryStandardLease(now, plan.epoch, plan.generation)
	}
	return standard, shared, existed
}

func (m *LeaseManager) configuredBucket(now time.Time, plan *activePlan, bucketID BucketID, holder string, share *selfShare) (*LocalBucket, bool) {
	if bucket, ok := m.local.Load(bucketID); ok {
		// Hot path: one atomic load and a uint64 compare against the precomputed
		// signature. The full config is only rebuilt when the profile changes
		// (e.g. a broadcaster's mod status flips inside an epoch).
		if !bucket.MatchesSignature(share.signature) {
			bucket.Update(now, plan.epoch, plan.generation, holder, plan.notBefore, plan.notAfter, share.sharedRate, share.sharedBurst, share.standardRate, share.standardBurst)
		}
		return bucket, true
	}
	candidate := NewLocalBucket()
	stableID := bucketID
	// Sonic exposes envelope strings as views into Watermill's payload. Clone
	// only on the first bucket insertion so the map does not retain the whole
	// message buffer; cache hits remain allocation-free.
	if stableID.Value != "" {
		stableID.Value = strings.Clone(stableID.Value)
	}
	bucket, loaded := m.local.LoadOrStore(stableID, candidate)
	if !loaded {
		candidate.Update(now, plan.epoch, plan.generation, holder, plan.notBefore, plan.notAfter, share.sharedRate, share.sharedBurst, share.standardRate, share.standardBurst)
		return candidate, false
	}
	return bucket, true
}

// A newly created per-channel bucket starts empty by design, so no member in a
// newly activated generation can safely spend a local burst immediately. Peer
// borrowing for that first sparse chat request only adds up to two cross-region
// round trips; use the globally serialized emergency partition directly. Pods
// outside the committed plan must still borrow, as they own no local share.
func skipColdChatBorrow(plan *activePlan, existed bool, profile uint8) bool {
	return plan.selfIndex >= 0 && !existed && (profile == profileChat || profile == profileChatMod)
}

func localShare(spec Spec, members, rank int) (rate.Limit, int) {
	leasedBurst := spec.capacity - spec.emergencyBurst
	leasedRate := spec.refillPerSec - spec.emergencyRate
	if members <= 0 || rank < 0 || rank >= members {
		return 0, 0
	}
	burst := leasedBurst / members
	if rank < leasedBurst%members {
		burst++
	}
	return rate.Limit(leasedRate / float64(members)), burst
}

func (m *LeaseManager) borrow(ctx context.Context, plan *activePlan, bucketID BucketID, need uint8, sharedSpec, standardSpec Spec) uint8 {
	if m.permit == nil || len(plan.members) < 2 {
		return 0
	}
	if !m.borrowSlots.TryAcquire(1) {
		return 0
	}
	defer m.borrowSlots.Release(1)
	paid := uint8(0)
	attempts := 0
	for pass := 0; pass < 2 && attempts < 2; pass++ {
		for i := range plan.members {
			if i == plan.selfIndex || pass == 0 && plan.members[i].Region != m.region || pass == 1 && plan.members[i].Region == m.region {
				continue
			}
			sharedRate, sharedBurst := localShare(sharedSpec, len(plan.members), i)
			standardRate, standardBurst := localShare(standardSpec, len(plan.members), i)
			timeout := 20 * time.Millisecond
			if plan.members[i].Region != m.region {
				timeout = 200 * time.Millisecond
			}
			attemptCtx, cancel := context.WithTimeout(ctx, timeout)
			reply, err := m.permit.Borrow(attemptCtx, plan.members[i], BorrowRequest{
				Version: planVersion, Epoch: plan.epoch, Generation: plan.generation,
				Bucket: bucketID, Need: need &^ paid, Profile: sharedSpec.profile,
				SharedRateMicros: limitMicros(sharedRate), SharedBurst: sharedBurst,
				StandardRateMicros: limitMicros(standardRate), StandardBurst: standardBurst,
			})
			cancel()
			attempts++
			if err == nil {
				paid |= reply.Paid
				if paid&need == need {
					return paid
				}
			}
			if attempts == 2 {
				return paid
			}
		}
	}
	return paid
}

// GrantPermit authorizes a peer against this pod's share. The caller-supplied
// numeric allocation must exactly match the share derived from the committed
// profile and plan; a malformed peer cannot inflate this lender's bucket.
func (m *LeaseManager) GrantPermit(now time.Time, request BorrowRequest) BorrowReply {
	reply := BorrowReply{Version: planVersion, Epoch: request.Epoch, Status: "stale"}
	plan := m.active(now)
	if plan == nil || plan.epoch != request.Epoch || plan.generation != request.Generation {
		return reply
	}
	if plan.selfIndex < 0 || int(request.Profile) >= len(plan.shares) {
		reply.Status = "invalid"
		return reply
	}
	share := &plan.shares[request.Profile]
	if !share.valid {
		reply.Status = "invalid"
		return reply
	}
	if request.SharedRateMicros != limitMicros(share.sharedRate) || request.SharedBurst != share.sharedBurst ||
		request.StandardRateMicros != limitMicros(share.standardRate) || request.StandardBurst != share.standardBurst {
		reply.Status = "invalid"
		return reply
	}
	bucket, _ := m.configuredBucket(now, plan, request.Bucket, plan.selfPodID, share)
	switch request.Need {
	case NeedShared:
		allowed, stale := bucket.TryPremiumLease(now, plan.epoch, plan.generation)
		if stale {
			bucket.Update(now, plan.epoch, plan.generation, plan.selfPodID, plan.notBefore, plan.notAfter, share.sharedRate, share.sharedBurst, share.standardRate, share.standardBurst)
			allowed, _ = bucket.TryPremiumLease(now, plan.epoch, plan.generation)
		}
		if allowed {
			reply.Paid, reply.Status = NeedShared, "granted"
		} else {
			reply.Status = "empty"
		}
	case NeedSystem:
		allowed, stale := bucket.TryPremiumLease(now, plan.epoch, plan.generation)
		if stale {
			bucket.Update(now, plan.epoch, plan.generation, plan.selfPodID, plan.notBefore, plan.notAfter, share.sharedRate, share.sharedBurst, share.standardRate, share.standardBurst)
			allowed, _ = bucket.TryPremiumLease(now, plan.epoch, plan.generation)
		}
		if allowed {
			reply.Paid, reply.Status = NeedSystem, "granted"
		} else {
			reply.Status = "empty"
		}
	case NeedStandard | NeedShared:
		standard, shared, stale := bucket.TryStandardLease(now, plan.epoch, plan.generation)
		if stale {
			bucket.Update(now, plan.epoch, plan.generation, plan.selfPodID, plan.notBefore, plan.notAfter, share.sharedRate, share.sharedBurst, share.standardRate, share.standardBurst)
			standard, shared, _ = bucket.TryStandardLease(now, plan.epoch, plan.generation)
		}
		if standard && shared {
			reply.Paid, reply.Status = NeedStandard|NeedShared, "granted"
		} else {
			reply.Status = "empty"
		}
	default:
		reply.Status = "invalid"
	}
	return reply
}

func limitMicros(limit rate.Limit) int64 {
	return int64(math.Floor(float64(limit) * 1_000_000))
}

func (m *LeaseManager) emergencyAllow(ctx context.Context, req Request, generation uint64, validFromMS int64) (bool, error) {
	if m.central == nil {
		return false, nil
	}
	if !m.emergencySlots.TryAcquire(1) {
		return false, nil
	}
	defer m.emergencySlots.Release(1)
	return m.central.AllowEmergency(ctx, req, generation, validFromMS)
}

func (m *LeaseManager) emergencyAllowOrdered(ctx context.Context, first, second Request, generation uint64, validFromMS int64) (uint8, error) {
	if m.central == nil {
		return 2, nil
	}
	if !m.emergencySlots.TryAcquire(1) {
		return 2, nil
	}
	defer m.emergencySlots.Release(1)
	return m.central.AllowEmergencyOrdered(ctx, first, second, generation, validFromMS)
}
