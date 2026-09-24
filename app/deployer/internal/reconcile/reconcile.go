// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"go.uber.org/zap"

	"ItsBagelBot/app/deployer/internal/aclpush"
	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const reconcileTimeout = 2 * time.Minute

type Deps struct {
	Store  ports.Store
	GitHub ports.GitHub
	Claims ports.ClaimsPusher
	Config ports.Config
	Log    *zap.Logger
}

// Run reconciles at start and every Config.ACLReconcileEvery, until ctx ends.
func Run(ctx context.Context, d Deps) {
	once(ctx, d)
	tick := time.NewTicker(d.Config.ACLReconcileEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			once(ctx, d)
		}
	}
}

func once(ctx context.Context, d Deps) {
	bounded, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()
	if err := tick(bounded, d); err != nil {
		d.Log.Warn("acl reconcile failed", zap.Error(err))
	}
}

func tick(ctx context.Context, d Deps) error {
	locked, err := clusterLocked(ctx, d.Store)
	if err != nil || locked {
		return err
	}
	ref, ok, err := lastSuccessfulRef(ctx, d.Store)
	if err != nil || !ok {
		return err
	}
	return reconcileRef(ctx, d, ref)
}

func clusterLocked(ctx context.Context, store ports.Store) (bool, error) {
	_, held, err := store.LockOwner(ctx)
	return held, err
}

// lastSuccessfulRef picks the pin of the most recent successful run: that is
// the ref the live cluster was last deployed from, so drift is measured
// against it rather than against main, which may be ahead of what is live.
func lastSuccessfulRef(ctx context.Context, store ports.Store) (ports.Ref, bool, error) {
	runs, err := store.List(ctx, deploy.KeepRuns)
	if err != nil {
		return "", false, err
	}
	id, ok := lastSucceededID(runs)
	if !ok {
		return "", false, nil
	}
	run, _, err := store.Get(ctx, id)
	if err != nil {
		return "", false, err
	}
	return refOf(run), true, nil
}

func lastSucceededID(runs []deploy.RunSummary) (deploy.RunID, bool) {
	for _, r := range runs {
		if r.State == deploy.RunSucceeded {
			return r.ID, true
		}
	}
	return "", false
}

func refOf(run deploy.Run) ports.Ref {
	if run.Outputs.PinSHA != "" {
		return ports.Ref(run.Outputs.PinSHA)
	}
	return ports.Ref(run.TargetSHA)
}

func reconcileRef(ctx context.Context, d Deps, ref ports.Ref) error {
	compiled, err := aclpush.Load(ctx, d.GitHub, d.Config, ref)
	if err != nil {
		return err
	}
	signer, err := aclpush.Signer(d.Config.NATSSigningSeed)
	if err != nil {
		return err
	}
	j := job{d: d, signer: signer, compiled: compiled}
	for _, cluster := range []ports.ClusterName{ports.ClusterHub, ports.ClusterLeaf} {
		if err := j.cluster(ctx, cluster); err != nil {
			return err
		}
	}
	return nil
}

type job struct {
	d        Deps
	signer   nkeys.KeyPair
	compiled []*jwt.AccountClaims
}

func (j job) cluster(ctx context.Context, cluster ports.ClusterName) error {
	pusher, err := aclpush.NewClusterPusher(ctx, j.d.Claims, j.signer, cluster)
	if err != nil {
		return err
	}
	drifted, err := pusher.Drift(ctx, j.compiled)
	if err != nil {
		return err
	}
	for _, c := range drifted {
		if _, err := pusher.Push(ctx, c); err != nil {
			return fmt.Errorf("push %s to %s: %w", c.Name, cluster, err)
		}
		j.d.Log.Info("acl reconcile pushed drifted account claims",
			zap.String("cluster", string(cluster)), zap.String("account", c.Name))
	}
	return nil
}
