// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/rpc"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	"ItsBagelBot/internal/utils"
)

type detachedEmission struct {
	emit  module.Emit
	state *emitState
}

func (p *Pipeline) detachedEmit(ctx context.Context, broadcasterID uint64, locale, replayKey string) detachedEmission {
	regress := module.RegressStandard
	if u, err := p.proj.User(ctx, broadcasterID); err == nil && u.Premium() {
		regress = module.RegressPremium
	}
	state := &emitState{subject: p.laneSubject(regress), locale: locale, baseDone: true, replayBase: detachedReplayBase(broadcasterID, replayKey)}
	return detachedEmission{emit: p.newEmit(ctx, strconv.FormatUint(broadcasterID, 10), state), state: state}
}

func detachedReplayBase(broadcasterID uint64, key string) string {
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strconv.FormatUint(broadcasterID, 10) + "\x00" + key))
	return "sesame:auto:" + hex.EncodeToString(sum[:16])
}

func (p *Pipeline) flushDetached(ctx context.Context, d detachedEmission) error {
	p.flushLegacyOutput(ctx, d.state)
	return d.state.err
}

func namedRunBroadcaster(req automationsrpc.RunRequest) (uint64, bool) {
	broadcasterID, err := strconv.ParseUint(req.BroadcasterID, 10, 64)
	if err != nil || broadcasterID == 0 {
		return 0, false
	}
	return broadcasterID, strings.TrimSpace(req.Name) != ""
}

func (p *Pipeline) RunNamed(ctx context.Context, req automationsrpc.RunRequest) automationsrpc.RunReply {
	broadcasterID, ok := namedRunBroadcaster(req)
	if !ok {
		return automationsrpc.RunReply{Refusal: rpc.Refused(rpc.CodeInvalid, "broadcaster_id and name are required")}
	}
	if p.automationsLockedFor(ctx, broadcasterID) {
		return automationsrpc.RunReply{Refusal: rpc.Refused(rpc.CodeForbidden, "automations require premium")}
	}
	plan, refusal := p.findInvokable(ctx, broadcasterID, req.Name)
	if refusal.Code != "" {
		return automationsrpc.RunReply{Refusal: refusal}
	}
	run, err := p.invokedRun(broadcasterID, plan, req)
	if err != nil {
		return automationsrpc.RunReply{Refusal: rpc.Refused(rpc.CodeUnavailable, err.Error())}
	}
	out := p.detachedEmit(ctx, broadcasterID, "", "rpc:"+req.RequestID)
	p.runAutomation(ctx, run, out.emit)
	if err := p.flushDetached(ctx, out); err != nil {
		return automationsrpc.RunReply{Refusal: rpc.Refused(rpc.CodeUnavailable, err.Error())}
	}
	return automationsrpc.RunReply{Accepted: true, RunID: run.ID}
}

func (p *Pipeline) findInvokable(ctx context.Context, broadcasterID uint64, name string) (*automation.Plan, rpc.Refusal) {
	if p.automations.Source == nil {
		return nil, rpc.Refused(rpc.CodeUnavailable, "automations are not wired")
	}
	list, err := p.automations.Source.Automations(ctx, broadcasterID)
	if err != nil {
		return nil, rpc.Refused(rpc.CodeUnavailable, err.Error())
	}
	for _, a := range list {
		if a.Enabled && invokableAs(a, name) {
			return p.planFor(ctx, broadcasterID, a.ID)
		}
	}
	return nil, rpc.Refused(rpc.CodeNotFound, "no enabled manual automation with that name")
}

func (p *Pipeline) planFor(ctx context.Context, broadcasterID uint64, id string) (*automation.Plan, rpc.Refusal) {
	index, err := p.automations.Source.AutomationIndex(ctx, broadcasterID)
	if err != nil {
		return nil, rpc.Refused(rpc.CodeUnavailable, err.Error())
	}
	plan := index.Plan(id)
	if plan == nil {
		return nil, rpc.Refused(rpc.CodeNotFound, "no enabled manual automation with that name")
	}
	return plan, rpc.Refusal{}
}

func invokableAs(a automation.Automation, name string) bool {
	return a.Definition.Trigger.Type == "manual" && strings.EqualFold(a.Name, name)
}

func (p *Pipeline) invokedRun(broadcasterID uint64, plan *automation.Plan, req automationsrpc.RunRequest) (*AutomationRun, error) {
	id, err := utils.NewID()
	if err != nil {
		return nil, err
	}
	return &AutomationRun{
		Plan:          plan,
		BroadcasterID: broadcasterID,
		Channel:       req.BroadcasterID,
		Viewer:        Viewer{ID: broadcasterID},
		Args:          joinArgs(req.Args),
		ArgMap:        req.Args,
		Source:        req.Source,
		ID:            id.String(),
		Identity:      "rpc:" + req.RequestID,
	}, nil
}

func joinArgs(args map[string]string) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, k := range keys {
		values = append(values, args[k])
	}
	return strings.Join(values, " ")
}
