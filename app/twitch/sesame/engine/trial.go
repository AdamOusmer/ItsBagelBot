// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

func (p *Pipeline) processTrial(ctx context.Context, env *lane.Envelope, broadcasterID uint64) error {
	started := time.Now()
	id, messages := env.BroadcasterUserID, env.MessageCount()
	defer func() {
		p.countTrial(ctx, id, "latency_ns_samples")
		p.addTrial(ctx, id, "latency_ns_total", time.Since(started).Nanoseconds())
	}()
	views, err := p.tracedModuleViews(ctx, env.Type, broadcasterID)
	if err != nil {
		p.countTrialFailure(ctx, id, messages)
		return err
	}
	mctx := p.leaseContext(env, broadcasterID)
	defer PutContext(mctx)
	emission := emitState{subject: p.laneSubject(mctx.Regress), env: env}
	emit := p.newEmit(ctx, env.BroadcasterUserID, &emission)
	p.runTracedStages(ctx, mctx, views, emit, &emission)
	p.flushLegacyOutput(ctx, &emission)
	if emission.err != nil {
		p.countTrialFailure(ctx, id, messages)
	} else {
		p.addTrial(ctx, id, "processed", messages)
	}
	if mctx.Command != "" {
		p.countTrial(ctx, id, "answered")
	}
	return emission.err
}

func (p *Pipeline) countTrialFailure(ctx context.Context, id string, messages int64) {
	p.addTrial(ctx, id, "failed", messages)
	p.addTrial(ctx, id, "retried", messages)
}

func (p *Pipeline) countTrial(ctx context.Context, id, field string) { p.addTrial(ctx, id, field, 1) }

func (p *Pipeline) addTrial(_ context.Context, id, field string, amount int64) {
	if p.trialCounts == nil || amount == 0 {
		return
	}
	broadcasterID, err := strconv.ParseUint(id, 10, 64)
	if err != nil || broadcasterID == 0 {
		return
	}
	p.trialCounts.BumpChannel(broadcasterID, data.TrialCounterPrefix+field, amount)
}

func markTrialOutput(output *outgress.Message, generation uint64) int {
	output.Origin = "trial"
	output.TrialGeneration = generation
	if output.Type != outgress.TypeBatch {
		return 1
	}
	var batch outgress.Batch
	if err := codec.Unmarshal(output.Payload, &batch); err != nil {
		return 1
	}
	marked := 0
	for i := range batch.Items {
		marked += markTrialOutput(&batch.Items[i], generation)
	}
	if body, err := codec.Marshal(&batch); err == nil {
		output.Payload = body
	}
	return marked
}
