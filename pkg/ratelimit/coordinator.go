package ratelimit

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type CoordinatorConfig struct {
	Epoch          time.Duration
	Guard          time.Duration
	MinMembers     int
	Replicas       int
	ReplicaTimeout time.Duration
}

func (c CoordinatorConfig) withDefaults() CoordinatorConfig {
	if c.Epoch <= 0 {
		c.Epoch = 30 * time.Second
	}
	if c.Guard <= 0 {
		c.Guard = 250 * time.Millisecond
	}
	if c.MinMembers <= 0 {
		c.MinMembers = 1
	}
	if c.ReplicaTimeout <= 0 {
		c.ReplicaTimeout = 2 * time.Second
	}
	return c
}

type LeaseCoordinator struct {
	client  *LeaseClient
	manager *LeaseManager
	self    Member
	config  CoordinatorConfig
	log     *zap.Logger

	heartbeatEvery time.Duration
	memberTTL      time.Duration

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

const (
	leaseReconcileInterval = 2 * time.Second
	leaseMissingPlanRetry  = 250 * time.Millisecond
	leaseMinimumWake       = 10 * time.Millisecond
	membershipWriteTimeout = 2 * time.Second
)

func NewLeaseCoordinator(client valkey.Client, manager *LeaseManager, region, podID string, config CoordinatorConfig, log *zap.Logger) *LeaseCoordinator {
	if log == nil {
		log = zap.NewNop()
	}
	config = config.withDefaults()
	heartbeatEvery, memberTTL := membershipCadence(config.Epoch)
	return &LeaseCoordinator{
		client: NewLeaseClient(client), manager: manager,
		self:   Member{PodID: podID, Region: region},
		config: config, log: log,
		heartbeatEvery: heartbeatEvery, memberTTL: memberTTL,
	}
}

// membershipCadence derives the heartbeat interval and presence ttl from the
// epoch. The interval is a bounded fraction of the epoch (neither chatty nor
// sluggish); the ttl spans three intervals, so a live pod survives two missed
// refreshes while a crashed one is pruned within the ttl.
func membershipCadence(epoch time.Duration) (every, ttl time.Duration) {
	every = epoch / 6
	if every < 3*time.Second {
		every = 3 * time.Second
	}
	if every > 10*time.Second {
		every = 10 * time.Second
	}
	return every, 3 * every
}

func (c *LeaseCoordinator) Start(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	// Register self before the first reconcile so this pod counts itself in any
	// plan it proposes on the very first pass.
	c.heartbeat(ctx)
	var activated, proposed uint64
	next, err := c.reconcile(ctx, &activated, &proposed)
	if err != nil {
		cancel()
		return err
	}
	c.wg.Add(2)
	go c.loop(ctx, activated, proposed, next)
	go c.heartbeatLoop(ctx)
	return nil
}

func (c *LeaseCoordinator) Close() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	// Deregister only after the loops stop, so no in-flight heartbeat can re-add
	// self. Best-effort on a fresh context because the parent is already cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), membershipWriteTimeout)
	defer cancel()
	if err := c.client.RemoveMember(ctx, c.self); err != nil {
		c.log.Debug("membership deregister failed", zap.Error(err))
	}
}

func (c *LeaseCoordinator) heartbeat(ctx context.Context) {
	hbCtx, cancel := context.WithTimeout(ctx, membershipWriteTimeout)
	defer cancel()
	if err := c.client.Heartbeat(hbCtx, c.self, time.Now(), c.memberTTL); err != nil {
		c.log.Debug("membership heartbeat failed", zap.Error(err))
	}
}

func (c *LeaseCoordinator) heartbeatLoop(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(c.heartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.heartbeat(ctx)
		}
	}
}

func (c *LeaseCoordinator) loop(ctx context.Context, activated, proposed uint64, next time.Duration) {
	defer c.wg.Done()
	timer := time.NewTimer(next)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		next = leaseReconcileInterval
		var err error
		if next, err = c.reconcile(ctx, &activated, &proposed); err != nil {
			if !errors.Is(err, context.Canceled) {
				c.log.Debug("lease reconciliation deferred", zap.Error(err))
			}
			next = leaseReconcileInterval
		}
		timer.Reset(next)
	}
}

func (c *LeaseCoordinator) reconcile(ctx context.Context, activated, proposed *uint64) (time.Duration, error) {
	localStart := time.Now()
	serverNow, err := c.client.ServerTime(ctx)
	if err != nil {
		return leaseReconcileInterval, err
	}
	localEnd := time.Now()
	roundTrip := localEnd.Sub(localStart)
	localMidpoint := localStart.Add(roundTrip / 2)
	uncertainty := c.config.Guard + roundTrip/2
	epochMS := c.config.Epoch.Milliseconds()
	currentEpoch := uint64(serverNow.UnixMilli() / epochMS)
	currentBoundary := int64(currentEpoch) * epochMS
	nextBoundary := currentBoundary + epochMS
	next := nextLeaseReconcileDelay(serverNow, nextBoundary, uncertainty)

	if *activated != currentEpoch {
		plan, err := c.client.LoadPlan(ctx, currentEpoch)
		if err == nil {
			if err := c.manager.ActivatePlan(*plan, serverNow, localMidpoint, uncertainty); err != nil {
				return leaseReconcileInterval, err
			}
			*activated = currentEpoch
			c.log.Info("lease plan activated", zap.Uint64("epoch", plan.Epoch), zap.Int("members", len(plan.Members)))
		} else if !valkey.IsValkeyNil(err) {
			return leaseReconcileInterval, err
		} else {
			// The epoch may have just turned while its commit is propagating. Retry
			// promptly instead of leaving the fleet on emergency capacity for 2s.
			next = min(next, leaseMissingPlanRetry)
		}
	}

	nextEpoch := currentEpoch + 1
	if *proposed == nextEpoch || serverNow.UnixMilli() < nextBoundary-10_000 {
		return next, nil
	}
	members, err := c.listMembers(ctx)
	if err != nil {
		return leaseReconcileInterval, err
	}
	if len(members) < c.config.MinMembers {
		// Membership under-counted the fleet. Committing a plan now would hand the
		// known members a full local share (a lone member borrows from nobody and
		// claims the whole Twitch quota) -- fail open. Defer instead: with no fresh
		// plan every pod stays on the globally serialized emergency partition (fail
		// closed, no over-send). Return nil, not an error, so this expected degraded
		// state neither crashes startup through Start's Fatal nor hides at Debug
		// like a transient fault.
		c.log.Warn("insufficient permit-service members; deferring lease proposal",
			zap.Int("discovered", len(members)), zap.Int("min_members", c.config.MinMembers))
		return leaseReconcileInterval, nil
	}
	plan, err := BuildPlan(nextEpoch, nextBoundary, nextBoundary+epochMS, members)
	if err != nil {
		return leaseReconcileInterval, err
	}
	won, err := c.client.ProposePlan(ctx, plan, c.config.Replicas, c.config.ReplicaTimeout)
	if err != nil {
		return leaseReconcileInterval, err
	}
	if won {
		*proposed = nextEpoch
		c.log.Info("lease plan committed", zap.Uint64("epoch", nextEpoch), zap.Int("members", len(members)))
	} else if _, err := c.client.LoadPlan(ctx, nextEpoch); err == nil {
		// Only stop proposing once a committed, replicated plan exists. A winner
		// that crashed before writing its commit marker would otherwise leave
		// this epoch unrecoverable, because no peer would re-enter ProposePlan.
		*proposed = nextEpoch
	}
	return next, nil
}

// nextLeaseReconcileDelay retains the low steady-state Valkey polling rate but
// replaces the last arbitrary poll before an epoch edge with a wakeup aligned
// to the guarded activation boundary. This removes up to two seconds of avoidable
// fail-closed time while preserving the intentional clock-uncertainty gap.
func nextLeaseReconcileDelay(serverNow time.Time, nextBoundaryMS int64, uncertainty time.Duration) time.Duration {
	delay := time.UnixMilli(nextBoundaryMS).Sub(serverNow) + uncertainty
	if delay < leaseMinimumWake {
		return leaseMinimumWake
	}
	if delay > leaseReconcileInterval {
		return leaseReconcileInterval
	}
	return delay
}

// listMembers reads the live fleet from the Valkey presence registry and
// guarantees self is included. Deriving membership from the same store that owns
// the quota plans means every pod sees the identical fleet without relying on
// NATS discovery fan-out, and a stale replica or a just-missed heartbeat can
// never make a pod disown itself.
func (c *LeaseCoordinator) listMembers(ctx context.Context) ([]Member, error) {
	members, err := c.client.ListMembers(ctx, time.Now())
	if err != nil {
		return nil, err
	}
	for i := range members {
		if members[i].PodID == c.self.PodID {
			return members, nil
		}
	}
	members = append(members, c.self)
	sort.Slice(members, func(i, j int) bool { return members[i].PodID < members[j].PodID })
	return members, nil
}
