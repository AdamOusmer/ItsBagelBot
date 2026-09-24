// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"context"
	"fmt"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"

	"ItsBagelBot/app/deployer/internal/aclpush"
	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

var clusters = []ports.ClusterName{ports.ClusterHub, ports.ClusterLeaf}

type aclJWT struct{}

func (aclJWT) ID() deploy.StageID { return deploy.StageACL }

func (aclJWT) Done(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	compiled, signer, err := loadJWTTarget(ctx, rc)
	if err != nil {
		return false, nil
	}
	j := jwtPush{rc: rc, signer: signer, compiled: compiled}
	for _, cluster := range clusters {
		synced, err := j.synced(ctx, cluster)
		if err != nil || !synced {
			return false, nil
		}
	}
	return true, nil
}

func (aclJWT) Run(ctx context.Context, rc *stage.RunCtx) error {
	if err := applyMessaging(ctx, rc); err != nil {
		return err
	}
	compiled, signer, err := loadJWTTarget(ctx, rc)
	if err != nil {
		return ports.Failf(deploy.FailClaimsPush, "load account claims: %v", err)
	}
	j := jwtPush{rc: rc, signer: signer, compiled: compiled}
	return j.run(ctx)
}

// loadJWTTarget compiles accounts.yaml/accounts.keys.yaml at the run's
// pinned ref and loads the operator signing key.
func loadJWTTarget(ctx context.Context, rc *stage.RunCtx) ([]*jwt.AccountClaims, nkeys.KeyPair, error) {
	compiled, err := aclpush.Load(ctx, rc.Deps.GitHub, rc.Deps.Config, pinRef(rc.View()))
	if err != nil {
		return nil, nil, err
	}
	signer, err := aclpush.Signer(rc.Deps.Config.NATSSigningSeed)
	if err != nil {
		return nil, nil, err
	}
	return compiled, signer, nil
}

type jwtPush struct {
	rc       *stage.RunCtx
	signer   nkeys.KeyPair
	compiled []*jwt.AccountClaims
}

func (j jwtPush) synced(ctx context.Context, cluster ports.ClusterName) (bool, error) {
	pusher, err := aclpush.NewClusterPusher(ctx, j.rc.Deps.Claims, j.signer, cluster)
	if err != nil {
		return false, err
	}
	drifted, err := pusher.Drift(ctx, j.compiled)
	if err != nil {
		return false, err
	}
	return len(drifted) == 0, nil
}

func (j jwtPush) run(ctx context.Context) error {
	pushed := false
	for _, cluster := range clusters {
		did, err := j.pushCluster(ctx, cluster)
		if err != nil {
			return err
		}
		pushed = pushed || did
	}
	return j.rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.ACLPushed = pushed })
}

func (j jwtPush) pushCluster(ctx context.Context, cluster ports.ClusterName) (bool, error) {
	pusher, err := aclpush.NewClusterPusher(ctx, j.rc.Deps.Claims, j.signer, cluster)
	if err != nil {
		return false, claimsFail(cluster, err)
	}
	drifted, err := pusher.Drift(ctx, j.compiled)
	if err != nil {
		return false, claimsFail(cluster, err)
	}
	for _, c := range drifted {
		if err := pushOne(ctx, j.rc, pusher, c); err != nil {
			return false, err
		}
	}
	return len(drifted) > 0, nil
}

func pushOne(ctx context.Context, rc *stage.RunCtx, pusher *aclpush.ClusterPusher, c *jwt.AccountClaims) error {
	rc.SetItem(ctx, pushItem(pusher.Cluster(), c.Name, deploy.StateRunning, ""))
	_, err := pusher.Push(ctx, c)
	rc.SetItem(ctx, pushItem(pusher.Cluster(), c.Name, stateOf(err), detailOf(err)))
	if err != nil {
		return claimsFail(pusher.Cluster(), fmt.Errorf("account %s: %w", c.Name, err))
	}
	return nil
}

func pushItem(cluster ports.ClusterName, account string, state deploy.StageState, detail string) deploy.Item {
	key := string(cluster) + "/" + account
	return deploy.Item{Key: key, Label: key, State: state, Detail: detail}
}

func claimsFail(cluster ports.ClusterName, err error) error {
	if f, ok := ports.AsFail(err); ok {
		return f
	}
	return ports.Failf(deploy.FailClaimsPush, "%s cluster: %v", cluster, err)
}
