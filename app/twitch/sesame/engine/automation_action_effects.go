// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
)

const (
	customPostEndpoint   = "post"
	goveeProvider        = "govee"
	goveeControlEndpoint = "control"
	goveeRestoreEndpoint = "restore"
	restoreRetryMS       = 1000
	noViewer             = "no_viewer"
)

type actionTarget struct {
	id    uint64
	login string
}

func usesLegacyLogin(p automation.TargetAction) bool {
	return p.Who == "" || p.Who == "login"
}

func (x *actionExec) target(p automation.TargetAction, selfWhenEmpty bool) (actionTarget, error) {
	switch p.Who {
	case "viewer":
		return x.viewerTarget()
	case "mention":
		return x.mentionTarget()
	case "user":
		id, err := strconv.ParseUint(p.UserID, 10, 64)
		if err != nil || id == 0 {
			return actionTarget{}, failure("no_target")
		}
		return actionTarget{id: id, login: scope.NormalizeLogin(p.Login)}, nil
	case "", "login":
		return x.loginTarget(p.Login, selfWhenEmpty)
	default:
		return actionTarget{}, failure("no_target")
	}
}

func (x *actionExec) viewerTarget() (actionTarget, error) {
	viewer := x.run.Viewer
	login := scope.NormalizeLogin(viewer.Login)
	if viewer.ID == 0 || login == "" {
		return actionTarget{}, failure(noViewer)
	}
	return actionTarget{id: viewer.ID, login: login}, nil
}

func (x *actionExec) mentionTarget() (actionTarget, error) {
	if x.run.Env == nil {
		return actionTarget{}, failure("no_target")
	}
	mentions := x.run.Env.Mentions
	id := scope.MentionedViewerID(mentions, "")
	if id == 0 {
		return actionTarget{}, failure("no_target")
	}
	return actionTarget{id: id, login: scope.NormalizeLogin(mentions[0].UserLogin)}, nil
}

func (x *actionExec) loginTarget(template string, selfWhenEmpty bool) (actionTarget, error) {
	login := scope.NormalizeLogin(x.expand(template))
	viewer := x.run.Viewer
	if login == "" && selfWhenEmpty {
		return x.viewerTarget()
	}
	if login == "" {
		return actionTarget{}, failure("empty_login")
	}
	if viewer.ID != 0 && login == scope.NormalizeLogin(viewer.Login) {
		return actionTarget{id: viewer.ID, login: login}, nil
	}
	return x.lookupLogin(login)
}

func (x *actionExec) lookupLogin(login string) (actionTarget, error) {
	if x.p.accounts == nil {
		return actionTarget{}, failure("no_lookup")
	}
	raw, found, err := x.p.accounts.ResolveLogin(x.ctx, login)
	if err != nil {
		return actionTarget{}, failure("lookup_failed")
	}
	id, parseErr := strconv.ParseUint(raw, 10, 64)
	if !found {
		return actionTarget{}, failure("not_found")
	}
	if parseErr != nil || id == 0 {
		return actionTarget{}, failure("not_found")
	}
	return actionTarget{id: id, login: login}, nil
}

func (x *actionExec) effectRef(s step) EffectRef {
	identity := x.identity()
	if usesHoldIdentity(s) {
		identity = x.holdIdentity()
	}
	return EffectRef{Identity: identity, Effect: s.EffectKey + x.iter}
}

func hasLightsHold(s step) bool { return s.Type == "lights" && len(s.Branches) > 0 }

func usesHoldIdentity(s step) bool { return s.Internal || hasLightsHold(s) }

func (x *actionExec) holdIdentity() string {
	identity := x.identity()
	if anonymousRun(identity) {
		return x.run.ID
	}
	return identity
}

func (x *actionExec) once(s step, do func() error) error {
	ran := false
	var detail error
	err := x.p.dedup.Effect(x.ctx, x.effectRef(s), func(ctx context.Context) error {
		ran = true
		previous := x.ctx
		x.ctx = ctx
		defer func() { x.ctx = previous }()
		detail = do()
		return effectOutcome(detail)
	})
	if err != nil {
		return err
	}
	if !ran {
		return skipped("duplicate")
	}
	return detail
}

func effectOutcome(err error) error {
	status, detail := classify(err)
	switch {
	case status == automationrpc.StatusOK, status == automationrpc.StatusSent, status == automationrpc.StatusSkipped:
		return nil
	case detail == "store_error", detail == noViewer:
		return nil
	default:
		return err
	}
}

func storeResult(err error, found bool) error {
	if err != nil {
		return loyaltyStoreFailure(err)
	}
	if !found {
		return failure("not_found")
	}
	return nil
}

func loyaltyStoreFailure(err error) error {
	var reply bus.RPCReplyError
	if errors.As(err, &reply) && reply.Message == loyaltyrpc.CounterTargetRequired {
		return failure(noViewer)
	}
	return failure("store_error")
}

func (x *actionExec) pointsAdd(s step, p automation.PointsAddAction) error {
	return x.adjustPoints(s, p.TargetAction, p.Amount, false)
}

func (x *actionExec) pointsSet(s step, p automation.PointsSetAction) error {
	return x.adjustPoints(s, p.TargetAction, p.Amount, true)
}

func (x *actionExec) adjustPoints(s step, target automation.TargetAction, amount int64, absolute bool) error {
	if x.p.loyalty == nil {
		return failure("no_loyalty")
	}
	t, err := x.target(target, true)
	if err != nil {
		return err
	}
	if target.Who == "user" && t.login == "" {
		return failure("empty_login")
	}
	return x.once(s, func() error {
		_, found, err := x.p.loyalty.BalanceAdjustViewer(x.ctx, BalanceAdjustment{
			BroadcasterID: x.run.BroadcasterID,
			ViewerID:      t.id,
			ViewerLogin:   t.login,
			Value:         amount,
			Absolute:      absolute,
		})
		return storeResult(err, found)
	})
}

func (x *actionExec) loyaltyOnce(s step, apply func(LoyaltyStore) (found bool, err error)) error {
	if x.p.loyalty == nil {
		return failure("no_loyalty")
	}
	return x.once(s, func() error {
		found, err := apply(x.p.loyalty)
		return storeResult(err, found)
	})
}

func (x *actionExec) counterAdd(s step, p automation.CounterAddAction) error {
	bump := CounterBump{
		BroadcasterID: x.run.BroadcasterID, Name: p.Name, Viewer: x.run.Viewer, Command: x.run.Plan.Automation.ID, Delta: p.Amount,
	}
	return x.loyaltyOnce(s, func(l LoyaltyStore) (bool, error) {
		_, err := l.CounterBump(x.ctx, bump)
		return true, err
	})
}

func (x *actionExec) counterSet(s step, p automation.CounterSetAction) error {
	set := CounterAssignment{
		BroadcasterID: x.run.BroadcasterID, Name: p.Name, ViewerID: x.run.Viewer.ID,
		Command: x.run.Plan.Automation.ID, Value: p.Value, RequireTarget: true,
	}
	return x.loyaltyOnce(s, func(l LoyaltyStore) (bool, error) {
		return l.CounterSet(x.ctx, set)
	})
}

func (x *actionExec) lights(s step, p automation.LightsAction) error {
	if refusal := x.lightsRefusal(p); refusal != nil {
		return refusal
	}
	req := lightsRequest(x.bid, p)
	if len(s.Branches) > 0 {
		req.HoldS, req.HoldID = p.HoldS, x.lightHoldID(&s.Branches[0][1])
	}
	return x.once(s, func() error { return x.controlLights(s, req) })
}

func (x *actionExec) controlLights(s step, req gossiprpc.Request) error {
	if err := x.sendLights(req); err != nil {
		return err
	}
	if req.HoldS > 0 {
		x.execPath(s, 0, 0)
	}
	return nil
}

func (x *actionExec) lightHoldID(restore step) string {
	ref := x.effectRef(restore)
	return nestedReplayKey(ref.Identity, ref.Effect)
}

func (x *actionExec) lightsRestore(s step, p automation.LightsRestoreAction) error {
	if x.p.gossip == nil {
		return failure("no_gossip")
	}
	req := lightsTargetRequest(x.bid, p)
	req.HoldID = x.lightHoldID(s)
	var reply gossiprpc.GoveeRestoreReply
	err := x.once(s, func() error { return x.sendLightsRestore(req, &reply) })
	if reply.RateLimited {
		x.signal, x.delayMS = signalRetry, int(reply.RetryAfterMS)
		if x.delayMS <= 0 {
			x.delayMS = restoreRetryMS
		}
	}
	return err
}

func (x *actionExec) sendLightsRestore(req gossiprpc.Request, reply *gossiprpc.GoveeRestoreReply) error {
	err := x.p.gossip.Call(x.ctx, GossipRoute{Provider: goveeProvider, Endpoint: goveeRestoreEndpoint}, req, reply)
	switch {
	case err != nil:
		return gossipFailure(err)
	case reply.RateLimited:
		return limited("rate_limited")
	case reply.Error != "":
		return failure(reply.Error)
	case !reply.OK:
		return failure("gossip_error")
	}
	return nil
}

func (x *actionExec) requireLive() error {
	if x.p.live == nil {
		return skipped("offline")
	}
	live, err := x.p.live.IsLive(x.ctx, x.run.BroadcasterID)
	switch {
	case err != nil:
		return err
	case !live:
		return skipped("offline")
	}
	return nil
}

func hasLightTarget(p automation.LightsAction) bool { return p.Device != "" && p.SKU != "" }

func (x *actionExec) lightsRefusal(p automation.LightsAction) error {
	switch {
	case x.p.gossip == nil:
		return failure("no_gossip")
	case !hasLightTarget(p):
		return failure("no_device")
	case !p.AnyTime:
		return x.requireLive()
	}
	return nil
}

func (x *actionExec) sendLights(req gossiprpc.Request) error {
	var reply gossiprpc.GoveeControlReply
	if err := x.p.gossip.Call(x.ctx, GossipRoute{Provider: goveeProvider, Endpoint: goveeControlEndpoint}, req, &reply); err != nil {
		return gossipFailure(err)
	}
	switch {
	case reply.Error != "":
		return failure(reply.Error)
	case req.Scene != nil && !reply.SceneApplied:
		return failure("gossip_outdated")
	case !reply.OK:
		return failure("gossip_error")
	}
	return nil
}

var lightsRequests = map[string]func(*gossiprpc.Request, automation.LightsAction){
	automation.LightsModeColor: func(req *gossiprpc.Request, p automation.LightsAction) { req.ColorRGB = p.RGB },
	automation.LightsModeScene: func(req *gossiprpc.Request, p automation.LightsAction) {
		req.Scene = &gossiprpc.GoveeScene{
			ID: p.SceneID, Name: p.SceneName, Type: p.SceneType, Instance: p.SceneInstance, Value: codec.RawMessage(p.SceneValue),
		}
	},
	automation.LightsModeWhite: func(req *gossiprpc.Request, p automation.LightsAction) { req.Kelvin = p.Kelvin },
	automation.LightsModeOn:    func(req *gossiprpc.Request, _ automation.LightsAction) { req.KeepLook = true },
	automation.LightsModeOff:   func(req *gossiprpc.Request, _ automation.LightsAction) { req.PowerOff = true },
}

func lightsRequest(channelID string, p automation.LightsAction) gossiprpc.Request {
	req := lightsTargetRequest(channelID, automation.LightsRestoreAction{Device: p.Device, SKU: p.SKU, ExtraLights: p.ExtraLights})
	if apply := lightsRequests[p.Mode]; apply != nil {
		apply(&req, p)
	}
	if !req.PowerOff {
		req.Brightness = p.Brightness
	}
	return req
}

func lightsTargetRequest(channelID string, p automation.LightsRestoreAction) gossiprpc.Request {
	return gossiprpc.Request{ChannelID: channelID, Device: p.Device, SKU: p.SKU, Lights: lightGroup(p)}
}

func lightGroup(p automation.LightsRestoreAction) []gossiprpc.GoveeLight {
	if len(p.ExtraLights) == 0 {
		return nil
	}
	lights := make([]gossiprpc.GoveeLight, 0, len(p.ExtraLights)+1)
	lights = append(lights, gossiprpc.GoveeLight{Device: p.Device, SKU: p.SKU})
	seen := map[string]bool{p.Device: true}
	for _, ref := range p.ExtraLights {
		device, sku, ok := strings.Cut(ref, "/")
		if !ok || seen[device] {
			continue
		}
		seen[device] = true
		lights = append(lights, gossiprpc.GoveeLight{Device: device, SKU: sku})
	}
	return lights
}

func gossipFailure(err error) error {
	var replied bus.RPCReplyError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &replied):
		return failure(replied.Message)
	}
	return failure("gossip_error")
}

func (x *actionExec) httpPost(s step, p automation.HTTPPostAction) error {
	target := x.expand(p.URL)
	if validate.FetchURL(target) != nil {
		return x.settleFetch(s, p.SaveAs, fetchResult{failure: string(gossiprpc.FetchBadDef)})
	}
	if x.p.gossip == nil {
		return x.settleFetch(s, p.SaveAs, fetchResult{failure: "no_gossip"})
	}
	req := gossiprpc.Request{Account: x.bid, ChannelID: x.bid, IsPremium: x.run.Regress.IsPremium(), Def: &gossiprpc.FetchDef{URL: target}, Body: x.expand(p.Body), CaptureBody: p.SaveAs != ""}
	var result fetchResult
	err := x.once(s, func() error {
		result = x.callFetch(customPostEndpoint, req)
		if result.failure != "" {
			return failure(result.failure)
		}
		return nil
	})
	if status, _ := classify(err); status == automationrpc.StatusSkipped {
		return err
	}
	return x.settleFetch(s, p.SaveAs, result)
}

func (x *actionExec) httpGet(s step, p automation.HTTPGetAction) error {
	target := x.expand(p.URL)
	if validate.FetchURL(target) != nil {
		return x.settleFetch(s, p.SaveAs, fetchResult{failure: string(gossiprpc.FetchBadDef)})
	}
	if x.p.gossip == nil {
		return x.settleFetch(s, p.SaveAs, fetchResult{failure: "no_gossip"})
	}
	req := gossiprpc.Request{Account: x.bid, ChannelID: x.bid, IsPremium: x.run.Regress.IsPremium(), Def: &gossiprpc.FetchDef{
		Name: "get:" + x.bid, URL: target, JSONPath: p.JSONPath, IsActive: true,
	}}
	return x.settleFetch(s, p.SaveAs, x.callFetch(customFetchEndpoint, req))
}

func (x *actionExec) callFetch(endpoint string, req gossiprpc.Request) fetchResult {
	var reply gossiprpc.CustomFetchReply
	if err := x.p.gossip.Call(x.ctx, GossipRoute{Provider: customFetchProvider, Endpoint: endpoint}, req, &reply); err != nil {
		return fetchResult{failure: "gossip_error"}
	}
	return replyResult(reply.Status, reply.Values)
}
