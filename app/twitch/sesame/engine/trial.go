// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

func (p *Pipeline) processTrial(ctx context.Context, env *lane.Envelope, broadcasterID uint64) error {
	started := time.Now()
	defer func() {
		p.countTrial(ctx, env.BroadcasterUserID, "latency_samples")
		p.addTrial(ctx, env.BroadcasterUserID, "latency_total_ms", time.Since(started).Milliseconds())
	}()
	views, err := p.tracedModuleViews(ctx, env.Type, broadcasterID)
	if err != nil {
		p.countTrial(ctx, env.BroadcasterUserID, "failed")
		p.countTrial(ctx, env.BroadcasterUserID, "retried")
		return err
	}
	mctx := p.leaseContext(env, broadcasterID)
	defer PutContext(mctx)
	emission := emitState{subject: p.laneSubject(mctx.Regress), env: env}
	emit := p.newEmit(ctx, env.BroadcasterUserID, &emission)
	p.runTracedStages(ctx, mctx, views, emit, &emission)
	p.flushLegacyOutput(ctx, &emission)
	if emission.err != nil {
		p.countTrial(ctx, env.BroadcasterUserID, "failed")
		p.countTrial(ctx, env.BroadcasterUserID, "retried")
	} else {
		p.countTrial(ctx, env.BroadcasterUserID, "processed")
	}
	if mctx.Command != "" {
		p.countTrial(ctx, env.BroadcasterUserID, "answered")
	}
	return emission.err
}

func (p *Pipeline) countTrial(ctx context.Context, id, field string) { p.addTrial(ctx, id, field, 1) }

func (p *Pipeline) addTrial(ctx context.Context, id, field string, amount int64) {
	if p.trialStore == nil || id == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if err := p.trialStore.Do(ctx, p.trialStore.B().Hincrby().Key("trial:channel:"+id).Field(field).Increment(amount).Build()).Error(); err != nil {
		p.log.Warn("trial count unavailable", zap.String("broadcaster_id", id), zap.String("field", field), zap.Error(err))
	}
}

func markTrialOutput(output *outgress.Message, generation uint64) {
	output.Origin = "trial"
	output.TrialGeneration = generation
	if output.Type != outgress.TypeBatch {
		return
	}
	var batch outgress.Batch
	if err := codec.Unmarshal(output.Payload, &batch); err != nil {
		return
	}
	for i := range batch.Items {
		markTrialOutput(&batch.Items[i], generation)
	}
	if body, err := codec.Marshal(&batch); err == nil {
		output.Payload = body
	}
}
