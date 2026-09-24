// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

type selfShare struct {
	sharedRate    rate.Limit
	sharedBurst   int
	standardRate  rate.Limit
	standardBurst int
	signature     uint64
	valid         bool
}

type activePlan struct {
	epoch       uint64
	generation  uint64
	validFromMS int64
	notBefore   time.Time
	notAfter    time.Time
	// Both generations stay closed until here so their leased shares never overlap.
	nextNotBefore time.Time
	members       []Member
	selfIndex     int
	selfPodID     string
	shares        [profileHelixUser + 1]selfShare
}

func (p *activePlan) coversNow(now time.Time) bool {
	return !now.Before(p.notBefore) && now.Before(p.notAfter)
}

func (p *activePlan) matches(epoch, generation uint64) bool {
	return p != nil && p.epoch == epoch && p.generation == generation
}

func (p *activePlan) shareFor(profile uint8) (*selfShare, bool) {
	if p.selfIndex < 0 || int(profile) >= len(p.shares) {
		return nil, false
	}
	share := &p.shares[profile]
	if !share.valid {
		return nil, false
	}
	return share, true
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

type Identity struct {
	Region string
	PodID  string
}

func NewLeaseManager(central *Limiter, local *BucketStore, permit PermitBorrower, id Identity) *LeaseManager {
	if local == nil {
		local = NewBucketStore(16)
	}
	return &LeaseManager{
		central: central, local: local, permit: permit,
		region: id.Region, podID: id.PodID,
		borrowSlots: semaphore.NewWeighted(64), emergencySlots: semaphore.NewWeighted(32),
	}
}

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
	nextNotBefore := localNow.Add(time.UnixMilli(canonical.ValidUntilMS).Sub(serverNow) + guard)
	if !notBefore.Before(notAfter) {
		return errors.New("ratelimit: lease guard consumes plan interval")
	}
	ap := &activePlan{
		epoch: canonical.Epoch, generation: canonical.Generation,
		validFromMS: canonical.ValidFromMS, notBefore: notBefore, notAfter: notAfter,
		nextNotBefore: nextNotBefore,
		members:       canonical.Members, selfIndex: selfIndex,
	}
	if selfIndex >= 0 {
		ap.selfPodID = canonical.Members[selfIndex].PodID
		ap.shares = buildShares(len(canonical.Members), selfIndex)
	}
	m.plan.Store(ap)
	m.primeFixedBuckets(localNow, ap)
	if m.local != nil {
		m.local.DeleteExpired(localNow.Add(-2 * notAfter.Sub(notBefore)).UnixNano())
	}
	return nil
}

func buildShares(members, selfIndex int) [profileHelixUser + 1]selfShare {
	var shares [profileHelixUser + 1]selfShare
	for profile := profileChat; profile <= profileHelixUser; profile++ {
		if share, ok := computeShare(profile, members, selfIndex); ok {
			shares[profile] = share
		}
	}
	return shares
}

func computeShare(profile uint8, members, selfIndex int) (selfShare, bool) {
	shared, standard, ok := specsForProfile(profile)
	if !ok {
		return selfShare{}, false
	}
	sharedRate, sharedBurst := localShare(shared, members, selfIndex)
	if sharedBurst <= 0 {
		return selfShare{}, false
	}
	var standardRate rate.Limit
	var standardBurst int
	if standard.capacity != 0 {
		standardRate, standardBurst = localShare(standard, members, selfIndex)
	}
	return selfShare{
		sharedRate: sharedRate, sharedBurst: sharedBurst,
		standardRate: standardRate, standardBurst: standardBurst,
		signature: bucketConfigSignature(sharedRate, sharedBurst, standardRate, standardBurst),
		valid:     true,
	}, true
}

func (m *LeaseManager) primeFixedBuckets(now time.Time, plan *activePlan) {
	if m.local == nil || plan.selfIndex < 0 {
		return
	}
	for _, fixed := range []struct {
		id      BucketID
		profile uint8
	}{
		{BucketID{Scope: "helix:app"}, profileHelixGeneral},
		{BucketID{Scope: "helix:system"}, profileHelixSystem},
		{BucketID{Scope: "helix:user:bot"}, profileHelixUser},
	} {
		share := &plan.shares[fixed.profile]
		if !share.valid {
			continue
		}
		bucket, _ := m.configuredBucket(now, plan, fixed.id, share)
		if bucket.Generation() != plan.generation || bucket.Holder() != plan.selfPodID {
			m.refreshBucket(bucket, now, plan, share)
		} else {
			bucket.Renew(plan.epoch, plan.notBefore, plan.notAfter)
		}
	}
}

func (m *LeaseManager) Allow(ctx context.Context, req Request) (bool, error) {
	return m.allowAt(ctx, &req, time.Now())
}

func (m *LeaseManager) GuardRetryAfter() time.Duration {
	return m.guardRetryAfterAt(time.Now())
}

func (m *LeaseManager) guardRetryAfterAt(now time.Time) time.Duration {
	plan := m.plan.Load()
	if plan == nil {
		return 0
	}
	switch {
	case now.Before(plan.notBefore):
		return plan.notBefore.Sub(now)
	case !now.Before(plan.notAfter) && now.Before(plan.nextNotBefore):
		return plan.nextNotBefore.Sub(now)
	default:
		return 0
	}
}

func (m *LeaseManager) allowAt(ctx context.Context, req *Request, now time.Time) (bool, error) {
	plan := m.active(now)
	if plan == nil {
		// Guard gap or expired plan: fail closed, another generation may be spending.
		if m.plan.Load() != nil {
			return false, nil
		}
		return m.emergencyAllow(ctx, *req, 1)
	}

	bucketID := req.bucketID()
	allowed, existed := m.tryLocalPremiumState(now, plan, bucketID, req.Spec.profile)
	if allowed {
		return true, nil
	}
	if skipColdChatBorrow(plan, existed, req.Spec.profile) {
		return m.emergencyAllow(ctx, *req, plan.generation)
	}

	need := NeedShared
	if req.Spec.profile == profileHelixSystem {
		need = NeedSystem
	}
	target := borrowTarget{bucketID: bucketID, need: need, shared: req.Spec}
	if m.borrow(ctx, plan, target) == need {
		return true, nil
	}
	return m.emergencyAllow(ctx, *req, plan.generation)
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
		return m.emergencyAllowOrdered(ctx, *first, *second, 1)
	}

	bucketID := second.bucketID()
	localFirst, localShared, existed := m.tryLocalStandardState(now, plan, bucketID, second.Spec.profile)
	if localFirst && localShared {
		return 0, nil
	}
	if skipColdChatBorrow(plan, existed, second.Spec.profile) {
		return m.emergencyAllowOrdered(ctx, *first, *second, plan.generation)
	}

	target := borrowTarget{bucketID: bucketID, need: NeedStandard | NeedShared, shared: second.Spec, standard: first.Spec}
	if m.borrow(ctx, plan, target) == NeedStandard|NeedShared {
		return 0, nil
	}
	return m.emergencyAllowOrdered(ctx, *first, *second, plan.generation)
}

func (m *LeaseManager) active(now time.Time) *activePlan {
	plan := m.plan.Load()
	if plan == nil || !plan.coversNow(now) {
		return nil
	}
	return plan
}

func (m *LeaseManager) tryLocalPremium(now time.Time, plan *activePlan, bucketID BucketID, profile uint8) bool {
	allowed, _ := m.tryLocalPremiumState(now, plan, bucketID, profile)
	return allowed
}

func (m *LeaseManager) tryLocalPremiumState(now time.Time, plan *activePlan, bucketID BucketID, profile uint8) (bool, bool) {
	share, ok := plan.shareFor(profile)
	if !ok {
		return false, false
	}
	bucket, existed := m.configuredBucket(now, plan, bucketID, share)
	return m.premiumLease(leaseOp{now, plan, bucket, share}), existed
}

func (m *LeaseManager) tryLocalStandard(now time.Time, plan *activePlan, bucketID BucketID, profile uint8) (bool, bool) {
	standard, shared, _ := m.tryLocalStandardState(now, plan, bucketID, profile)
	return standard, shared
}

func (m *LeaseManager) tryLocalStandardState(now time.Time, plan *activePlan, bucketID BucketID, profile uint8) (bool, bool, bool) {
	share, ok := plan.shareFor(profile)
	if !ok || share.standardBurst <= 0 {
		return false, false, false
	}
	bucket, existed := m.configuredBucket(now, plan, bucketID, share)
	standard, shared := m.standardLease(leaseOp{now, plan, bucket, share})
	return standard, shared, existed
}

type leaseOp struct {
	now    time.Time
	plan   *activePlan
	bucket *LocalBucket
	share  *selfShare
}

func (m *LeaseManager) refreshBucket(bucket *LocalBucket, now time.Time, plan *activePlan, share *selfShare) {
	bucket.Update(now, BucketConfig{
		Epoch: plan.epoch, Generation: plan.generation, Holder: plan.selfPodID,
		NotBefore: plan.notBefore, NotAfter: plan.notAfter,
		SharedRate: share.sharedRate, SharedBurst: share.sharedBurst,
		StandardRate: share.standardRate, StandardBurst: share.standardBurst,
	})
}

func (m *LeaseManager) premiumLease(op leaseOp) bool {
	allowed, stale := op.bucket.TryPremiumLease(op.now, op.plan.epoch, op.plan.generation)
	if stale {
		m.refreshBucket(op.bucket, op.now, op.plan, op.share)
		allowed, _ = op.bucket.TryPremiumLease(op.now, op.plan.epoch, op.plan.generation)
	}
	return allowed
}

func (m *LeaseManager) standardLease(op leaseOp) (bool, bool) {
	standard, shared, stale := op.bucket.TryStandardLease(op.now, op.plan.epoch, op.plan.generation)
	if stale {
		m.refreshBucket(op.bucket, op.now, op.plan, op.share)
		standard, shared, _ = op.bucket.TryStandardLease(op.now, op.plan.epoch, op.plan.generation)
	}
	return standard, shared
}

func (m *LeaseManager) configuredBucket(now time.Time, plan *activePlan, bucketID BucketID, share *selfShare) (*LocalBucket, bool) {
	if bucket, ok := m.local.Load(bucketID); ok {
		if !bucket.MatchesSignature(share.signature) {
			m.refreshBucket(bucket, now, plan, share)
		}
		return bucket, true
	}
	candidate := NewLocalBucket()
	// bucketID aliases the bus payload buffer; clone it before the map keeps it.
	stableID := bucketID
	if stableID.Value != "" {
		stableID.Value = strings.Clone(stableID.Value)
	}
	bucket, loaded := m.local.LoadOrStore(stableID, candidate)
	if !loaded {
		m.refreshBucket(candidate, now, plan, share)
		return candidate, false
	}
	return bucket, true
}

func skipColdChatBorrow(plan *activePlan, existed bool, profile uint8) bool {
	return plan.selfIndex >= 0 && !existed && (profile == profileChat || profile == profileChatMod)
}

func localShare(spec Spec, members, rank int) (rate.Limit, int) {
	if !validRank(rank, members) {
		return 0, 0
	}
	leasedBurst := spec.capacity - spec.emergencyBurst
	leasedRate := spec.refillPerSec - spec.emergencyRate
	burst := leasedBurst / members
	if rank < leasedBurst%members {
		burst++
	}
	return rate.Limit(leasedRate / float64(members)), burst
}

func validRank(rank, members int) bool {
	return members > 0 && rank >= 0 && rank < members
}

type borrowTarget struct {
	bucketID BucketID
	need     uint8
	shared   Spec
	standard Spec
}

func (m *LeaseManager) borrow(ctx context.Context, plan *activePlan, target borrowTarget) uint8 {
	if m.permit == nil || len(plan.members) < 2 {
		return 0
	}
	if !m.borrowSlots.TryAcquire(1) {
		return 0
	}
	defer m.borrowSlots.Release(1)

	paid := uint8(0)
	for attempts, i := range m.borrowOrder(plan) {
		if attempts >= 2 {
			break
		}
		reply, err := m.askPeer(ctx, peerAsk{plan: plan, target: target, index: i, paid: paid})
		if err != nil {
			continue
		}
		paid |= reply.Paid
		if paid&target.need == target.need {
			return paid
		}
	}
	return paid
}

func (m *LeaseManager) borrowOrder(plan *activePlan) []int {
	order := make([]int, 0, len(plan.members)-1)
	for _, wantLocal := range []bool{true, false} {
		for i := range plan.members {
			if i == plan.selfIndex {
				continue
			}
			if (plan.members[i].Region == m.region) == wantLocal {
				order = append(order, i)
			}
		}
	}
	return order
}

type peerAsk struct {
	plan   *activePlan
	target borrowTarget
	index  int
	paid   uint8
}

func (m *LeaseManager) askPeer(ctx context.Context, ask peerAsk) (BorrowReply, error) {
	member := ask.plan.members[ask.index]
	members := len(ask.plan.members)
	sharedRate, sharedBurst := localShare(ask.target.shared, members, ask.index)
	standardRate, standardBurst := localShare(ask.target.standard, members, ask.index)

	timeout := 20 * time.Millisecond
	if member.Region != m.region {
		timeout = 200 * time.Millisecond
	}
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return m.permit.Borrow(attemptCtx, member, BorrowRequest{
		Version: planVersion, Epoch: ask.plan.epoch, Generation: ask.plan.generation,
		Bucket: ask.target.bucketID, Need: ask.target.need &^ ask.paid, Profile: ask.target.shared.profile,
		SharedRateMicros: limitMicros(sharedRate), SharedBurst: sharedBurst,
		StandardRateMicros: limitMicros(standardRate), StandardBurst: standardBurst,
	})
}

func (m *LeaseManager) GrantPermit(now time.Time, request BorrowRequest) BorrowReply {
	reply := BorrowReply{Version: planVersion, Epoch: request.Epoch, Status: "stale"}
	plan, share, status := m.validateBorrow(now, request)
	if status != "" {
		reply.Status = status
		return reply
	}
	bucket, _ := m.configuredBucket(now, plan, request.Bucket, share)
	reply.Paid, reply.Status = m.grantNeed(leaseOp{now, plan, bucket, share}, request.Need)
	return reply
}

func (m *LeaseManager) validateBorrow(now time.Time, request BorrowRequest) (*activePlan, *selfShare, string) {
	plan := m.active(now)
	if !plan.matches(request.Epoch, request.Generation) {
		return nil, nil, "stale"
	}
	share, ok := plan.shareFor(request.Profile)
	if !ok || !request.matchesShare(share) {
		return nil, nil, "invalid"
	}
	return plan, share, ""
}

func (r BorrowRequest) matchesShare(share *selfShare) bool {
	return r.SharedRateMicros == limitMicros(share.sharedRate) && r.SharedBurst == share.sharedBurst &&
		r.StandardRateMicros == limitMicros(share.standardRate) && r.StandardBurst == share.standardBurst
}

func (m *LeaseManager) grantNeed(op leaseOp, need uint8) (uint8, string) {
	switch need {
	case NeedShared, NeedSystem:
		if m.premiumLease(op) {
			return need, "granted"
		}
		return 0, "empty"
	case NeedStandard | NeedShared:
		if standard, shared := m.standardLease(op); standard && shared {
			return NeedStandard | NeedShared, "granted"
		}
		return 0, "empty"
	default:
		return 0, "invalid"
	}
}

func limitMicros(limit rate.Limit) int64 {
	return int64(math.Floor(float64(limit) * 1_000_000))
}

// When ok, the caller must call release.
func (m *LeaseManager) reserveEmergency() (release func(), ok bool) {
	if m.central == nil {
		return nil, false
	}
	if !m.emergencySlots.TryAcquire(1) {
		return nil, false
	}
	return func() { m.emergencySlots.Release(1) }, true
}

func (m *LeaseManager) emergencyAllow(ctx context.Context, req Request, generation uint64) (bool, error) {
	release, ok := m.reserveEmergency()
	if !ok {
		return false, nil
	}
	defer release()
	return m.central.AllowEmergency(ctx, req, generation, 0)
}

func (m *LeaseManager) emergencyAllowOrdered(ctx context.Context, first, second Request, generation uint64) (uint8, error) {
	release, ok := m.reserveEmergency()
	if !ok {
		return 2, nil
	}
	defer release()
	return m.central.AllowEmergencyOrdered(ctx, first, second, generation, 0)
}
