// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

const laneAlignDefaultInterval = 30 * time.Second

type laneAlignment struct {
	enabled  bool
	interval time.Duration
	watch    leaderWatch
	next     time.Time
	failures uint
}

func newLaneAlignment() laneAlignment {
	return laneAlignment{
		enabled:  env.Get("NATS_LANE_ALIGN", "on") != "off",
		interval: positiveDuration(env.GetDuration("NATS_LANE_ALIGN_INTERVAL", laneAlignDefaultInterval), laneAlignDefaultInterval),
	}
}

type laneRequestArgs struct {
	subject string
	body    []byte
}

func laneRequest(ctx context.Context, nc *nats.Conn, request laneRequestArgs, response any) error {
	msg, err := nc.RequestWithContext(ctx, request.subject, request.body)
	if err != nil {
		return err
	}
	return json.Unmarshal(msg.Data, response)
}

type laneStepdownResponse struct {
	Success bool            `json:"success"`
	Error   *jsapi.APIError `json:"error"`
}

func (a *laneAlignment) reconcile(h *hotLaneBinding, streamLeader string, info *jsapi.ConsumerInfo) {
	consumerLeader := consumerLeaderName(info)
	stats := consumeTelemetry.register(nil, h.subject, consumeNRSampleRate)
	aligned := streamLeader != "" && consumerLeader == streamLeader
	stats.setAlignment(aligned)
	stable := a.watch.agrees(streamLeader)
	if !a.canStepDown(info, stable, aligned) || consumerLeader == "" {
		return
	}
	moved, err := h.stepDown(streamLeader)
	if moved {
		stats.alignStepdowns.Add(1)
	}
	a.schedule(h, err)
}

func (a *laneAlignment) canStepDown(info *jsapi.ConsumerInfo, stable, aligned bool) bool {
	return a.enabled && stable && !aligned && !info.Paused && !time.Now().Before(a.next)
}

func consumerLeaderName(info *jsapi.ConsumerInfo) string {
	if info.Cluster == nil {
		return ""
	}
	return info.Cluster.Leader
}

func (h *hotLaneBinding) stepDown(preferred string) (bool, error) {
	ctx, cancel := context.WithTimeout(h.ctx, time.Second)
	defer cancel()
	subject := "$JS.API.CONSUMER.LEADER.STEPDOWN." + h.stream + "." + h.name
	body, _ := json.Marshal(map[string]map[string]string{"placement": {"preferred": preferred}})
	var response laneStepdownResponse
	if err := laneRequest(ctx, h.nc, laneRequestArgs{subject, body}, &response); err != nil {
		return false, err
	}
	if response.Success {
		return true, nil
	}
	if response.Error == nil {
		return false, errors.New("bus: unexpected consumer leader stepdown response")
	}
	if strings.HasSuffix(response.Error.Description, " is already leader") {
		return false, nil
	}
	return false, response.Error
}

func (a *laneAlignment) schedule(h *hotLaneBinding, err error) {
	delay := a.interval
	if err == nil {
		a.failures = 0
	} else {
		h.log.Warn("lane leader alignment failed", zap.String("consumer", h.name), zap.Error(err))
		a.failures = min(a.failures+1, 16)
		delay = max(a.interval, min(min(a.interval, 5*time.Minute)<<a.failures, 5*time.Minute))
	}
	jitter := time.Duration(rand.Int64N(int64(h.heartbeatInterval())))
	a.next = time.Now().Add(delay + jitter)
}
