// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"slices"
	"time"

	jsapi "github.com/nats-io/nats.go/jetstream"
)

func streamConfig(spec StreamSpec) jsapi.StreamConfig {
	duplicateWindow := 2 * time.Minute
	if spec.Duplicates > 0 {
		duplicateWindow = spec.Duplicates
	}
	if spec.MaxAge > 0 && spec.MaxAge < duplicateWindow {
		duplicateWindow = spec.MaxAge
	}
	storage := jsapi.FileStorage
	if spec.Storage != 0 {
		storage = jsapi.MemoryStorage
	}
	return serverNormalized(jsapi.StreamConfig{
		Name:              spec.Name,
		Subjects:          spec.Subjects,
		Storage:           storage,
		Retention:         jsapi.RetentionPolicy(spec.Retention),
		Discard:           jsapi.DiscardOld,
		MaxAge:            spec.MaxAge,
		MaxBytes:          spec.MaxBytes,
		MaxMsgsPerSubject: spec.MaxMsgsPer,
		Replicas:          spec.Replicas,
		Duplicates:        duplicateWindow,
		Placement:         placement(spec.PlacementTags),
		// Both batch flags must converge in one update; clearing them in a second pass destroys staged batches.
		AllowAtomicPublish: spec.BatchPublish,
		AllowBatchPublish:  spec.BatchPublish,
		AllowMsgSchedules:  spec.MsgSchedules,
		AllowMsgTTL:        spec.MsgSchedules,
	})
}

func serverNormalized(cfg jsapi.StreamConfig) jsapi.StreamConfig {
	cfg.MaxMsgs = unlimitedIfUnset(cfg.MaxMsgs)
	cfg.MaxMsgsPerSubject = unlimitedIfUnset(cfg.MaxMsgsPerSubject)
	cfg.MaxBytes = unlimitedIfUnset(cfg.MaxBytes)
	cfg.MaxMsgSize = int32(unlimitedIfUnset(int64(cfg.MaxMsgSize)))
	cfg.MaxConsumers = int(unlimitedIfUnset(int64(cfg.MaxConsumers)))
	if cfg.Replicas <= 0 {
		cfg.Replicas = 1
	}
	cfg.Duplicates = dedupWindow(cfg.Duplicates, cfg.MaxAge)
	if cfg.AllowMsgSchedules {
		cfg.AllowRollup, cfg.DenyPurge = true, false
	}
	cfg.Placement = normalizedPlacement(cfg.Placement)
	return cfg
}

func unlimitedIfUnset(value int64) int64 {
	if value == 0 || value < -1 {
		return -1
	}
	return value
}

func dedupWindow(window, maxAge time.Duration) time.Duration {
	if window != 0 {
		return window
	}
	if maxAge != 0 && maxAge < 2*time.Minute {
		return maxAge
	}
	return 2 * time.Minute
}

func placement(tags []string) *jsapi.Placement {
	if len(tags) == 0 {
		return nil
	}
	return &jsapi.Placement{Tags: slices.Clone(tags)}
}

func normalizedPlacement(p *jsapi.Placement) *jsapi.Placement {
	if placementConstrainsNothing(p) {
		return nil
	}
	return p
}

func placementConstrainsNothing(p *jsapi.Placement) bool {
	if p == nil {
		return true
	}
	return p.Cluster == "" && len(p.Tags) == 0
}

func streamMatches(got, want jsapi.StreamConfig) bool {
	return sameStreamLimits(got, want) &&
		sameStreamFeatures(got, want) &&
		sameSubjects(got.Subjects, want.Subjects) &&
		samePlacement(got.Placement, want.Placement)
}

func sameStreamLimits(got, want jsapi.StreamConfig) bool {
	return got.Retention == want.Retention &&
		got.MaxAge == want.MaxAge &&
		got.MaxBytes == want.MaxBytes &&
		got.MaxMsgsPerSubject == want.MaxMsgsPerSubject &&
		got.Replicas == want.Replicas &&
		got.Duplicates == want.Duplicates
}

func sameStreamFeatures(got, want jsapi.StreamConfig) bool {
	return got.AllowAtomicPublish == want.AllowAtomicPublish &&
		got.AllowBatchPublish == want.AllowBatchPublish &&
		got.AllowMsgSchedules == want.AllowMsgSchedules &&
		got.AllowMsgTTL == want.AllowMsgTTL
}

func samePlacement(a, b *jsapi.Placement) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Cluster == b.Cluster && sameSubjects(a.Tags, b.Tags)
}

func sameSubjects(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
