// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"

	"go.uber.org/zap"
)

// Reconcile only via the jetstream API: legacy UpdateStream clears allow_atomic and staged batches.

const (
	streamReconcileTimeout = 20 * time.Second
	streamReconcileRetry   = 30 * time.Second
	startupBudget          = 45 * time.Second
	startupRetryInterval   = 2 * time.Second
)

func EnsureStreams(ctx context.Context, url string, specs []StreamSpec, log *zap.Logger) error {
	guardian := &streamGuardian{specs: specs, log: log}

	nc, err := nats.Connect(busURL(endpoint(url)), busOptions("stream-guardian")...)
	if err != nil {
		return fmt.Errorf("bus: connect for provisioning: %w", err)
	}

	guardian.js, err = jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		nc.Close()
		return fmt.Errorf("bus: jetstream context: %w", err)
	}

	if err := guardian.provisionAtStartup(ctx); err != nil {
		nc.Close()
		return err
	}

	// Not a connect option: with RetryOnFailedConnect it could fire before guardian.js exists.
	nc.SetReconnectHandler(func(*nats.Conn) {
		log.Info("nats reconnected; re-provisioning jetstream streams")
		guardian.reconcileAll(ctx)
	})

	go guardian.retryUntilConverged(ctx)

	go func() {
		<-ctx.Done()
		nc.Close()
	}()

	return nil
}

type streamGuardian struct {
	js    streamProvisioner
	specs []StreamSpec
	log   *zap.Logger
	dirty atomic.Bool
}

// No delete: a leaked runtime credential must not be able to erase a stream.
type streamProvisioner interface {
	Stream(ctx context.Context, name string) (jsapi.Stream, error)
	CreateStream(ctx context.Context, cfg jsapi.StreamConfig) (jsapi.Stream, error)
	UpdateStream(ctx context.Context, cfg jsapi.StreamConfig) (jsapi.Stream, error)
}

func (g *streamGuardian) provisionAtStartup(ctx context.Context) error {
	startupCtx, cancel := context.WithTimeout(ctx, startupBudget)
	defer cancel()
	for {
		lastErr := g.reconcileOnce(startupCtx)
		if lastErr == nil {
			return nil
		}
		select {
		case <-startupCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("bus: failed to provision streams within %v: %w", startupBudget, lastErr)
		case <-time.After(startupRetryInterval):
			g.log.Warn("waiting for jetstream quorum to provision streams", zap.Error(lastErr))
		}
	}
}

func (g *streamGuardian) reconcileOnce(ctx context.Context) error {
	for _, spec := range g.specs {
		if err := g.reconcile(ctx, spec); err != nil {
			return err
		}
	}
	return nil
}

func (g *streamGuardian) reconcileAll(ctx context.Context) {
	for _, spec := range g.specs {
		if err := g.reconcile(ctx, spec); err != nil {
			g.log.Error("jetstream stream reconcile failed; retrying",
				zap.String("stream", spec.Name),
				zap.Duration("retry_in", streamReconcileRetry),
				zap.Error(err))
			g.dirty.Store(true)
		}
	}
}

func (g *streamGuardian) reconcile(ctx context.Context, spec StreamSpec) error {
	ctx, cancel := context.WithTimeout(ctx, streamReconcileTimeout)
	defer cancel()
	return reconcileStream(ctx, g.js, spec, g.log)
}

func (g *streamGuardian) retryUntilConverged(ctx context.Context) {
	ticker := time.NewTicker(streamReconcileRetry)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if g.dirty.Swap(false) {
				g.reconcileAll(ctx)
			}
		}
	}
}

type streamReconciler struct {
	js  streamProvisioner
	log *zap.Logger
}

func reconcileStream(ctx context.Context, js streamProvisioner, spec StreamSpec, log *zap.Logger) error {
	return streamReconciler{js: js, log: log}.reconcile(ctx, spec)
}

func (r streamReconciler) reconcile(ctx context.Context, spec StreamSpec) error {
	desired := streamConfig(spec)

	stream, err := r.js.Stream(ctx, spec.Name)
	switch {
	case err == nil:
		info := stream.CachedInfo()
		if info == nil {
			return fmt.Errorf("bus: inspect stream %q: no cached configuration", spec.Name)
		}
		return r.converge(ctx, spec, streamShape{live: info.Config, desired: desired})

	case errors.Is(err, jsapi.ErrStreamNotFound):
		return r.create(ctx, spec, desired)

	default:
		return fmt.Errorf("bus: inspect stream %q: %w", spec.Name, err)
	}
}

type streamShape struct {
	live    jsapi.StreamConfig
	desired jsapi.StreamConfig
}

func (r streamReconciler) create(ctx context.Context, spec StreamSpec, desired jsapi.StreamConfig) error {
	if _, err := r.js.CreateStream(ctx, desired); err != nil {
		if errors.Is(err, jsapi.ErrStreamNameAlreadyInUse) {
			return nil
		}
		return fmt.Errorf("bus: create stream %q: %w", spec.Name, err)
	}
	r.log.Info("provisioned jetstream stream",
		zap.String("stream", spec.Name),
		zap.Strings("subjects", spec.Subjects),
		zap.String("retention", desired.Retention.String()),
	)
	return nil
}

func (r streamReconciler) converge(ctx context.Context, spec StreamSpec, shape streamShape) error {
	r.warnStorageDrift(spec, shape)

	update := effectiveDesired(shape)

	if streamMatches(shape.live, update) {
		return nil
	}

	if err := checkRetentionMigration(spec, shape); err != nil {
		return err
	}

	if _, err := r.js.UpdateStream(ctx, update); err != nil {
		if shape.desired.Retention == jsapi.WorkQueuePolicy {
			return fmt.Errorf(
				"bus: update work-queue stream %q (operator-managed delete/recreate required; runtime credentials cannot delete streams): %w",
				spec.Name, err,
			)
		}
		return fmt.Errorf("bus: update stream %q: %w", spec.Name, err)
	}
	r.log.Info("converged jetstream stream",
		zap.String("stream", spec.Name),
		zap.Strings("subjects", spec.Subjects),
		zap.String("retention", shape.desired.Retention.String()),
	)
	return nil
}

func (r streamReconciler) warnStorageDrift(spec StreamSpec, shape streamShape) {
	if shape.live.Storage == shape.desired.Storage {
		return
	}
	r.log.Warn("jetstream stream storage differs from spec; manual recreate required to converge",
		zap.String("stream", spec.Name),
		zap.String("current", shape.live.Storage.String()),
		zap.String("desired", shape.desired.Storage.String()),
	)
}

func effectiveDesired(shape streamShape) jsapi.StreamConfig {
	desired := shape.desired
	desired.Storage = shape.live.Storage
	desired.AllowMsgTTL = desired.AllowMsgTTL || shape.live.AllowMsgTTL
	desired.AllowMsgSchedules = desired.AllowMsgSchedules || shape.live.AllowMsgSchedules
	return serverNormalized(desired)
}

func checkRetentionMigration(spec StreamSpec, shape streamShape) error {
	if shape.live.Retention == shape.desired.Retention {
		return nil
	}
	if shape.live.Retention != jsapi.WorkQueuePolicy && shape.desired.Retention != jsapi.WorkQueuePolicy {
		return nil
	}
	return fmt.Errorf(
		"bus: stream %q retention change from %s to %s requires an operator-managed delete/recreate; runtime credentials cannot delete streams",
		spec.Name, shape.live.Retention, shape.desired.Retention,
	)
}
