package ratelimit

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type CoordinatorConfig struct {
	Epoch           time.Duration
	Guard           time.Duration
	DiscoveryWindow time.Duration
	MinMembers      int
	Replicas        int
	ReplicaTimeout  time.Duration
}

func (c CoordinatorConfig) withDefaults() CoordinatorConfig {
	if c.Epoch <= 0 {
		c.Epoch = 30 * time.Second
	}
	if c.Guard <= 0 {
		c.Guard = 250 * time.Millisecond
	}
	if c.DiscoveryWindow <= 0 {
		c.DiscoveryWindow = 250 * time.Millisecond
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
	nc      *nats.Conn
	client  *LeaseClient
	manager *LeaseManager
	region  string
	podID   string
	config  CoordinatorConfig
	log     *zap.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewLeaseCoordinator(nc *nats.Conn, client valkey.Client, manager *LeaseManager, region, podID string, config CoordinatorConfig, log *zap.Logger) *LeaseCoordinator {
	if log == nil {
		log = zap.NewNop()
	}
	return &LeaseCoordinator{
		nc: nc, client: NewLeaseClient(client), manager: manager,
		region: region, podID: podID,
		config: config.withDefaults(), log: log,
	}
}

func (c *LeaseCoordinator) Start(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	var activated, proposed uint64
	if err := c.reconcile(ctx, &activated, &proposed); err != nil {
		cancel()
		return err
	}
	c.wg.Add(1)
	go c.loop(ctx, activated, proposed)
	return nil
}

func (c *LeaseCoordinator) Close() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
}

func (c *LeaseCoordinator) loop(ctx context.Context, activated, proposed uint64) {
	defer c.wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := c.reconcile(ctx, &activated, &proposed); err != nil && !errors.Is(err, context.Canceled) {
			c.log.Debug("lease reconciliation deferred", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *LeaseCoordinator) reconcile(ctx context.Context, activated, proposed *uint64) error {
	localStart := time.Now()
	serverNow, err := c.client.ServerTime(ctx)
	if err != nil {
		return err
	}
	localEnd := time.Now()
	roundTrip := localEnd.Sub(localStart)
	localMidpoint := localStart.Add(roundTrip / 2)
	uncertainty := c.config.Guard + roundTrip/2
	epochMS := c.config.Epoch.Milliseconds()
	currentEpoch := uint64(serverNow.UnixMilli() / epochMS)
	currentBoundary := int64(currentEpoch) * epochMS

	if *activated != currentEpoch {
		plan, err := c.client.LoadPlan(ctx, currentEpoch)
		if err == nil {
			if err := c.manager.ActivatePlan(*plan, serverNow, localMidpoint, uncertainty); err != nil {
				return err
			}
			*activated = currentEpoch
			c.log.Info("lease plan activated", zap.Uint64("epoch", plan.Epoch), zap.Int("members", len(plan.Members)))
		} else if !valkey.IsValkeyNil(err) {
			return err
		}
	}

	nextEpoch := currentEpoch + 1
	nextBoundary := currentBoundary + epochMS
	if *proposed == nextEpoch || serverNow.UnixMilli() < nextBoundary-10_000 {
		return nil
	}
	members, err := c.discoverMembers(ctx)
	if err != nil {
		return err
	}
	if len(members) < c.config.MinMembers {
		return errors.New("ratelimit: insufficient permit-service members")
	}
	plan, err := BuildPlan(nextEpoch, nextBoundary, nextBoundary+epochMS, members)
	if err != nil {
		return err
	}
	won, err := c.client.ProposePlan(ctx, plan, c.config.Replicas, c.config.ReplicaTimeout)
	if err != nil {
		return err
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
	return nil
}

func (c *LeaseCoordinator) discoverMembers(ctx context.Context) ([]Member, error) {
	subject, err := micro.ControlSubject(micro.InfoVerb, permitServiceName, "")
	if err != nil {
		return nil, err
	}
	inbox := c.nc.NewInbox()
	subscription, err := c.nc.SubscribeSync(inbox)
	if err != nil {
		return nil, err
	}
	defer subscription.Unsubscribe()
	if err := subscription.AutoUnsubscribe(1024); err != nil {
		return nil, err
	}
	if err := c.nc.PublishRequest(subject, inbox, nil); err != nil {
		return nil, err
	}
	if err := c.nc.FlushWithContext(ctx); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(c.config.DiscoveryWindow)
	byPod := make(map[string]Member, c.config.MinMembers)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		message, err := subscription.NextMsg(remaining)
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				break
			}
			return nil, err
		}
		var info micro.Info
		if sonic.Unmarshal(message.Data, &info) != nil {
			continue
		}
		member := Member{PodID: info.Metadata["pod_id"], Region: info.Metadata["region"]}
		if member.PodID != "" && member.Region != "" {
			byPod[member.PodID] = member
		}
	}
	if _, exists := byPod[c.podID]; !exists {
		byPod[c.podID] = Member{PodID: c.podID, Region: c.region}
	}
	members := make([]Member, 0, len(byPod))
	for _, member := range byPod {
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].PodID < members[j].PodID })
	return members, nil
}
