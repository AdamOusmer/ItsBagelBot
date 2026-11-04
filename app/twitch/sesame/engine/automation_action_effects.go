// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"errors"
	"strconv"
	"strings"

	"ItsBagelBot/internal/domain/automation"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
)

const (
	customPostEndpoint   = "post"
	goveeProvider        = "govee"
	goveeControlEndpoint = "control"
)

type actionTarget struct {
	id    uint64
	login string
}

func normalizeLogin(s string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "@"))
}

func (x *actionExec) target(template string, selfWhenEmpty bool) (actionTarget, error) {
	login := normalizeLogin(x.expand(template))
	viewer := x.run.Viewer
	if login == "" && selfWhenEmpty {
		login = normalizeLogin(viewer.Login)
		if viewer.ID == 0 {
			return actionTarget{}, failure("no_viewer")
		}
		return actionTarget{id: viewer.ID, login: login}, nil
	}
	if login == "" {
		return actionTarget{}, failure("empty_login")
	}
	if viewer.ID != 0 && login == normalizeLogin(viewer.Login) {
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

func (x *actionExec) once(s step, do func() error) error {
	dup, release := x.p.dedup.Claim(x.ctx, EffectRef{
		Identity: x.identity(),
		Effect:   s.EffectKey,
	})
	if dup {
		return skipped("duplicate")
	}
	if err := do(); err != nil {
		release()
		return err
	}
	return nil
}

func storeResult(err error, found bool) error {
	if err != nil {
		return failure("store_error")
	}
	if !found {
		return failure("not_found")
	}
	return nil
}

func (x *actionExec) pointsAdd(s step, p automation.PointsAddAction) error {
	return x.adjustPoints(s, p.Login, p.Amount, false)
}

func (x *actionExec) pointsSet(s step, p automation.PointsSetAction) error {
	return x.adjustPoints(s, p.Login, p.Amount, true)
}

func (x *actionExec) adjustPoints(s step, login string, amount int64, absolute bool) error {
	if x.p.loyalty == nil {
		return failure("no_loyalty")
	}
	t, err := x.target(login, true)
	if err != nil {
		return err
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
	bump := CounterBump{BroadcasterID: x.run.BroadcasterID, Name: p.Name, Viewer: x.run.Viewer, Delta: p.Amount}
	return x.loyaltyOnce(s, func(l LoyaltyStore) (bool, error) {
		_, err := l.CounterBump(x.ctx, bump)
		return true, err
	})
}

func (x *actionExec) counterSet(s step, p automation.CounterSetAction) error {
	set := CounterAssignment{BroadcasterID: x.run.BroadcasterID, Name: p.Name, ViewerID: x.run.Viewer.ID, Value: p.Value}
	return x.loyaltyOnce(s, func(l LoyaltyStore) (bool, error) {
		return l.CounterSet(x.ctx, set)
	})
}

func (x *actionExec) lights(s step, p automation.LightsAction) error {
	if refusal := x.lightsRefusal(p); refusal != nil {
		return refusal
	}
	req := lightsRequest(x.bid, p)
	return x.once(s, func() error { return x.sendLights(req) })
}

func hasLightTarget(p automation.LightsAction) bool { return p.Device != "" && p.SKU != "" }

func (x *actionExec) lightsRefusal(p automation.LightsAction) error {
	switch {
	case x.p.gossip == nil:
		return failure("no_gossip")
	case !hasLightTarget(p):
		return failure("no_device")
	}
	if ok, err := x.p.automations.Limiter.AllowLights(x.ctx, x.run.BroadcasterID); err == nil && !ok {
		return limited("lights_rate")
	}
	return nil
}

func (x *actionExec) sendLights(req gossiprpc.Request) error {
	var reply gossiprpc.GoveeControlReply
	if err := x.p.gossip.Call(x.ctx, GossipRoute{Provider: goveeProvider, Endpoint: goveeControlEndpoint}, req, &reply); err != nil {
		return gossipFailure(err)
	}
	if req.Scene != nil && !reply.SceneApplied {
		return failure("gossip_outdated")
	}
	return nil
}

func lightsRequest(channelID string, p automation.LightsAction) gossiprpc.Request {
	req := gossiprpc.Request{ChannelID: channelID, Device: p.Device, SKU: p.SKU, ColorRGB: p.RGB}
	if p.Mode == automation.LightsModeScene {
		req.Scene = &gossiprpc.GoveeScene{
			ID: p.SceneID, Name: p.SceneName, Type: p.SceneType, Instance: p.SceneInstance, Value: codec.RawMessage(p.SceneValue),
		}
	}
	return req
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
	if x.p.gossip == nil {
		return failure("no_gossip")
	}
	body := x.expand(p.Body)
	return x.once(s, func() error {
		var reply gossiprpc.CustomFetchReply
		req := gossiprpc.Request{Account: x.bid, ChannelID: x.bid, Def: &gossiprpc.FetchDef{URL: p.URL}, Body: body}
		route := GossipRoute{Provider: customFetchProvider, Endpoint: customPostEndpoint}
		if err := x.p.gossip.Call(x.ctx, route, req, &reply); err != nil {
			return failure("gossip_error")
		}
		if reply.Status != gossiprpc.FetchOK {
			return failure(string(reply.Status))
		}
		return nil
	})
}
