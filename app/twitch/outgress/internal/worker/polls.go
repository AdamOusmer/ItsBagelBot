// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"slices"
	"strings"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const (
	pollStatusActive       = "ACTIVE"
	predictionStatusActive = "ACTIVE"
	predictionStatusLocked = "LOCKED"
)

type createLimits struct {
	titleRunes, itemRunes       int
	minItems, maxItems          int
	minSeconds, maxSeconds, def int
}

var (
	pollLimits       = createLimits{60, 25, 2, 5, 15, 1800, 60}
	predictionLimits = createLimits{45, 25, 2, 10, 30, 1800, 120}
)

var predictionEndFrom = map[string][]string{
	outgress.PredictionLocked:   {predictionStatusActive},
	outgress.PredictionResolved: {predictionStatusActive, predictionStatusLocked},
	outgress.PredictionCanceled: {predictionStatusActive, predictionStatusLocked},
}

func clipRunes(s string, limit int) string {
	return strings.TrimSpace(truncateRunes(strings.TrimSpace(s), limit))
}

func (l createLimits) items(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if item = clipRunes(item, l.itemRunes); item != "" && len(out) < l.maxItems {
			out = append(out, item)
		}
	}
	return out
}

func (l createLimits) seconds(requested int) int {
	if requested == 0 {
		return l.def
	}
	return min(max(requested, l.minSeconds), l.maxSeconds)
}

func (w *Worker) dropTyped(payload *outgress.Message, reason string) {
	w.log.Warn("dropping twitch action: "+reason,
		zap.String("type", payload.Type), zap.String("broadcaster_id", payload.BroadcasterID))
}

func (w *Worker) skipTyped(payload *outgress.Message, reason string) {
	w.log.Info("twitch action skipped: "+reason,
		zap.String("type", payload.Type), zap.String("broadcaster_id", payload.BroadcasterID))
}

type createFunc func(context.Context, string, twitch.CreateOptions) error
type createJob struct {
	limits  createLimits
	options twitch.CreateOptions
	create  createFunc
}

func (w *Worker) processPollCreate(ctx context.Context, payload *outgress.Message) error {
	var in outgress.PollCreate
	if !w.decodeTyped(payload, &in) {
		return nil
	}
	return w.runCreate(ctx, payload, createJob{pollLimits, twitch.CreateOptions{Title: in.Title, Items: in.Choices, Seconds: in.Duration}, w.twitch.CreatePoll})
}

func (w *Worker) runCreate(ctx context.Context, payload *outgress.Message, job createJob) error {
	limits, options := job.limits, job.options
	title, items := clipRunes(options.Title, limits.titleRunes), limits.items(options.Items)
	if title == "" || len(items) < limits.minItems {
		w.dropTyped(payload, "needs a title and at least two options")
		return nil
	}
	options = twitch.CreateOptions{Title: title, Items: items, Seconds: limits.seconds(options.Seconds)}
	return w.runTyped(ctx, payload, typedJob{as: outgress.AsBroadcaster, call: func(ctx context.Context) error {
		return job.create(ctx, payload.BroadcasterID, options)
	}})
}

func pollEndStatus(raw string) (string, bool) {
	status := strings.ToUpper(strings.TrimSpace(raw))
	if status == "" {
		status = outgress.PollTerminated
	}
	return status, status == outgress.PollTerminated || status == outgress.PollArchived
}

func (w *Worker) processPollEnd(ctx context.Context, payload *outgress.Message) error {
	var in outgress.PollEnd
	if !w.decodeTyped(payload, &in) {
		return nil
	}
	status, valid := pollEndStatus(in.Status)
	if !valid {
		w.dropTyped(payload, "unsupported poll end status")
		return nil
	}
	return w.runTyped(ctx, payload, typedJob{as: outgress.AsBroadcaster, call: func(ctx context.Context) error {
		return w.endLatestPoll(ctx, payload, status)
	}})
}

func (w *Worker) endLatestPoll(ctx context.Context, payload *outgress.Message, status string) error {
	poll, found, err := w.twitch.LatestPoll(ctx, payload.BroadcasterID)
	if err != nil {
		return err
	}
	if !found || poll.Status != pollStatusActive {
		w.skipTyped(payload, "no active poll")
		return nil
	}
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}
	return w.twitch.EndPoll(ctx, payload.BroadcasterID, twitch.PollResolution{ID: poll.ID, Status: status})
}

func (w *Worker) processPredictionCreate(ctx context.Context, payload *outgress.Message) error {
	var in outgress.PredictionCreate
	if !w.decodeTyped(payload, &in) {
		return nil
	}
	return w.runCreate(ctx, payload, createJob{predictionLimits, twitch.CreateOptions{Title: in.Title, Items: in.Outcomes, Seconds: in.Window}, w.twitch.CreatePrediction})
}

func winningOutcomeID(p twitch.Prediction, status string, winner int) (string, bool) {
	if status != outgress.PredictionResolved {
		return "", true
	}
	if winner < 1 || winner > len(p.Outcomes) {
		return "", false
	}
	return p.Outcomes[winner-1].ID, true
}

func (w *Worker) processPredictionEnd(ctx context.Context, payload *outgress.Message) error {
	var in outgress.PredictionEnd
	if !w.decodeTyped(payload, &in) {
		return nil
	}
	in.Status = strings.ToUpper(strings.TrimSpace(in.Status))
	if _, supported := predictionEndFrom[in.Status]; !supported {
		w.dropTyped(payload, "unsupported prediction end status")
		return nil
	}
	return w.runTyped(ctx, payload, typedJob{as: outgress.AsBroadcaster, call: func(ctx context.Context) error {
		return w.endLatestPrediction(ctx, payload, in)
	}})
}

func (w *Worker) endLatestPrediction(ctx context.Context, payload *outgress.Message, in outgress.PredictionEnd) error {
	prediction, found, err := w.twitch.LatestPrediction(ctx, payload.BroadcasterID)
	if err != nil {
		return err
	}
	if !found || !slices.Contains(predictionEndFrom[in.Status], prediction.Status) {
		w.skipTyped(payload, "no eligible prediction")
		return nil
	}
	return w.resolvePrediction(ctx, payload, prediction, in)
}

func (w *Worker) resolvePrediction(ctx context.Context, payload *outgress.Message, prediction twitch.Prediction, in outgress.PredictionEnd) error {
	winnerID, ok := winningOutcomeID(prediction, in.Status, in.Winner)
	if !ok {
		w.skipTyped(payload, "winner outcome out of range")
		return nil
	}
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}
	return w.twitch.EndPrediction(ctx, payload.BroadcasterID, twitch.PredictionResolution{ID: prediction.ID, Status: in.Status, WinningOutcomeID: winnerID})
}
