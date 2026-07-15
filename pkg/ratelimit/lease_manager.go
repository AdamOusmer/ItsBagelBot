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
	shares      [profileHelixUser + 1]selfShare
}

// coversNow reports whether now falls inside the plan's guarded admission
// window [notBefore, notAfter).
func (p *activePlan) coversNow(now time.Time) bool {
	return !now.Before(p.notBefore) && now.Before(p.notAfter)
}

// matches reports whether p is the plan the given epoch/generation refers to.
// A nil plan never matches (the caller is between generations or unprovisioned).
func (p *activePlan) matches(epoch, generation uint64) bool {
	return p != nil && p.epoch == epoch && p.generation == generation
}

// shareFor returns this pod's valid share for a profile. ok is false when the
// pod owns no share (outside the plan, unknown profile, or an empty allotment).
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
		ap.shares = buildShares(len(canonical.Members), selfIndex)
	}
	m.plan.Store(ap)
	m.primeFixedBuckets(localNow, ap)
	if m.local != nil {
		// Keep the immediately previous incarnation so an unchanged holder can
		// renew without losing its token balance. Older idle buckets are safe to
		// discard; recreation starts empty.
		m.local.DeleteExpired(localNow.Add(-2 * notAfter.Sub(notBefore)).UnixNano())
	}
	return nil
}

// buildShares precomputes every profile's lease allotment for this pod under a
// plan with the given member count and this pod's stable rank.
func buildShares(members, selfIndex int) [profileHelixUser + 1]selfShare {
	var shares [profileHelixUser + 1]selfShare
	for profile := profileChat; profile <= profileHelixUser; profile++ {
		if share, ok := computeShare(profile, members, selfIndex); ok {
			shares[profile] = share
		}
	}
	return shares
}

// computeShare derives one profile's share. ok is false for a profile with no
// spec or an allotment too small to grant this pod any burst.
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

// primeFixedBuckets creates the fleet-wide Helix buckets as soon as a lease
// plan activates and renews them every epoch. Unlike per-channel chat buckets,
// these sparse control buckets must remain resident: creating them on the first
// EventSub operation would start them empty and expose only the 10% emergency
// partition to an operation that needs a larger burst.
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

// leaseOp bundles the state one lease admission reads: the clock, the committed
// plan, the target bucket, and this pod's share of it.
type leaseOp struct {
	now    time.Time
	plan   *activePlan
	bucket *LocalBucket
	share  *selfShare
}

// refreshBucket rewrites a bucket's full config from this pod's share (the pod
// is always the holder). Called when a bucket's signature no longer matches the
// committed share or a lease read reports the bucket stale.
func (m *LeaseManager) refreshBucket(bucket *LocalBucket, now time.Time, plan *activePlan, share *selfShare) {
	bucket.Update(now, BucketConfig{
		Epoch: plan.epoch, Generation: plan.generation, Holder: plan.selfPodID,
		NotBefore: plan.notBefore, NotAfter: plan.notAfter,
		SharedRate: share.sharedRate, SharedBurst: share.sharedBurst,
		StandardRate: share.standardRate, StandardBurst: share.standardBurst,
	})
}

// premiumLease takes one premium (shared/system) token, rebuilding a stale
// bucket and retrying once.
func (m *LeaseManager) premiumLease(op leaseOp) bool {
	allowed, stale := op.bucket.TryPremiumLease(op.now, op.plan.epoch, op.plan.generation)
	if stale {
		m.refreshBucket(op.bucket, op.now, op.plan, op.share)
		allowed, _ = op.bucket.TryPremiumLease(op.now, op.plan.epoch, op.plan.generation)
	}
	return allowed
}

// standardLease takes one standard+shared token pair, rebuilding a stale bucket
// and retrying once.
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
		// Hot path: one atomic load and a uint64 compare against the precomputed
		// signature. The full config is only rebuilt when the profile changes
		// (e.g. a broadcaster's mod status flips inside an epoch).
		if !bucket.MatchesSignature(share.signature) {
			m.refreshBucket(bucket, now, plan, share)
		}
		return bucket, true
	}
	candidate := NewLocalBucket()
	stableID := bucketID
	// Sonic exposes envelope strings as views into the native bus payload. Clone
	// only on the first bucket insertion so the map does not retain the whole
	// message buffer; cache hits remain allocation-free.
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

// A newly created per-channel bucket starts empty by design, so no member in a
// newly activated generation can safely spend a local burst immediately. Peer
// borrowing for that first sparse chat request only adds up to two cross-region
// round trips; use the globally serialized emergency partition directly. Pods
// outside the committed plan must still borrow, as they own no local share.
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

// validRank reports whether rank is a usable member index in [0, members).
func validRank(rank, members int) bool {
	return members > 0 && rank >= 0 && rank < members
}

// borrowTarget is one peer-borrow request: the bucket, the needs still
// outstanding, and the shared/standard specs the lender's share is derived
// from.
type borrowTarget struct {
	bucketID BucketID
	need     uint8
	shared   Spec
	standard Spec
}

// borrow tries to cover the outstanding needs from peers in local-region-first
// order, bounded to two attempts total so a cold chat request never spends more
// than two cross-region round trips.
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

// borrowOrder lists the peer indices to try, local-region peers first then
// remote, skipping this pod.
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

// peerAsk is one Borrow RPC: the plan and target, the peer's member index, and
// the needs already paid (so the request asks only for the remainder).
type peerAsk struct {
	plan   *activePlan
	target borrowTarget
	index  int
	paid   uint8
}

// askPeer sends one Borrow RPC to the peer, sized to that member's share.
// Remote peers get a longer timeout than same-region ones.
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

// GrantPermit authorizes a peer against this pod's share. The caller-supplied
// numeric allocation must exactly match the share derived from the committed
// profile and plan; a malformed peer cannot inflate this lender's bucket.
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

// validateBorrow checks a borrow request against the committed plan and this
// lender's share. status is "" on success; "stale" if the plan moved on, or
// "invalid" for an unknown profile or a share the request does not match.
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

// matchesShare reports whether the request's numeric allocation exactly equals
// the lender's committed share, so a malformed peer cannot inflate the bucket.
func (r BorrowRequest) matchesShare(share *selfShare) bool {
	return r.SharedRateMicros == limitMicros(share.sharedRate) && r.SharedBurst == share.sharedBurst &&
		r.StandardRateMicros == limitMicros(share.standardRate) && r.StandardBurst == share.standardBurst
}

// grantNeed applies the requested need against the lender's bucket and returns
// the paid mask plus the reply status.
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

// reserveEmergency acquires one globally serialized emergency slot. ok is false
// when the central limiter is absent or every slot is busy; otherwise the
// caller must invoke release when done.
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
