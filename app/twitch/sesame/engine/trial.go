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

func (p *Pipeline) processTrial(ctx context.Context, env *lane.Envelope, broadcasterID uint64) (err error) {
	started := time.Now()
	answered := false
	defer func() {
		p.finishTrial(ctx, env, trialOutcome{err: err, answered: answered, duration: time.Since(started).Nanoseconds()})
	}()
	views, err := p.tracedModuleViews(ctx, env.Type, broadcasterID)
	if err != nil {
		return err
	}
	mctx := p.leaseContext(env, broadcasterID)
	defer PutContext(mctx)
	emission := emitState{subject: p.laneSubject(mctx.Regress), env: env, mctx: mctx}
	emit := p.newEmit(ctx, env.BroadcasterUserID, &emission)
	p.runTracedStages(ctx, mctx, views, emit, &emission)
	p.flushLegacyOutput(ctx, &emission)
	answered = mctx.Command != ""
	return emission.err
}

// Trial counts already use the bounded aggregate reporter. Keep its weighted
// message totals and one latency sample per envelope; capture processing time
// before reporting final outcome counters.
type trialOutcome struct {
	err      error
	answered bool
	duration int64
}

func (p *Pipeline) finishTrial(ctx context.Context, env *lane.Envelope, outcome trialOutcome) {
	id := env.BroadcasterUserID
	if outcome.err != nil {
		p.countTrialFailure(ctx, id, env.MessageCount())
	} else {
		p.addTrial(ctx, id, "processed", env.MessageCount())
	}
	if outcome.answered {
		p.countTrial(ctx, id, "answered")
	}
	p.countTrial(ctx, id, "latency_ns_samples")
	p.addTrial(ctx, id, "latency_ns_total", outcome.duration)
	tracePipelineResult(ctx, outcome.err)
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
