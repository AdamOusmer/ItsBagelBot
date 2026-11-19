// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package govee

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/google/uuid"
)

const (
	statePath = "/router/api/v1/device/state"
	holdGrace = 60 * time.Second
)

type holdLease struct {
	Owner    string                    `json:"owner"`
	Snapshot gossiprpc.GoveeLightState `json:"snapshot"`
}

func validateHold(req gossiprpc.Request) string {
	if req.HoldS < 0 || req.HoldS > 60 {
		return "hold out of range"
	}
	if req.HoldS > 0 && strings.TrimSpace(req.HoldID) == "" {
		return "missing hold id"
	}
	return ""
}

func holdKey(broadcaster, device string) string {
	return "govee:hold:" + broadcaster + ":" + strings.TrimSpace(device)
}

func distinctLights(lights []gossiprpc.GoveeLight) []gossiprpc.GoveeLight {
	seen := make(map[string]struct{}, len(lights))
	out := make([]gossiprpc.GoveeLight, 0, len(lights))
	for _, light := range lights {
		device := strings.TrimSpace(light.Device)
		if _, ok := seen[device]; ok {
			continue
		}
		seen[device] = struct{}{}
		out = append(out, light)
	}
	return out
}

func (p *api) stageHold(ctx context.Context, broadcaster string, target goveeTarget, req gossiprpc.Request) error {
	key := holdKey(broadcaster, target.device)
	if req.HoldS == 0 {
		return p.cache.DelJSON(ctx, key)
	}
	var lease holdLease
	found, err := p.cache.GetJSON(ctx, key, &lease)
	if err != nil {
		return err
	}
	snapshot := target.readState(ctx)
	if !found {
		lease.Snapshot = snapshot
	}
	lease.Owner = req.HoldID
	return p.cache.SetJSON(ctx, key, lease, time.Duration(req.HoldS)*time.Second+holdGrace)
}

type stateCapability struct {
	capabilityID
	State struct {
		Value codec.RawMessage `json:"value"`
	} `json:"state"`
}

type stateResponse struct {
	controlResponse
	Payload struct {
		Capabilities []stateCapability `json:"capabilities"`
	} `json:"payload"`
}

func (t goveeTarget) readState(ctx context.Context) gossiprpc.GoveeLightState {
	body, err := codec.Marshal(deviceRequest{RequestID: uuid.NewString(), Payload: devicePayload{SKU: t.sku, Device: t.device}})
	if err != nil {
		return gossiprpc.GoveeLightState{}
	}
	var resp stateResponse
	req := core.Request{Method: http.MethodPost, Path: statePath, Headers: t.headers, Body: body}
	if err := t.http.Do(ctx, req, &resp); err != nil {
		return gossiprpc.GoveeLightState{}
	}
	if goveeCodeError(resp.Code, resp.Message+resp.Msg) != nil {
		return gossiprpc.GoveeLightState{}
	}
	var snapshot gossiprpc.GoveeLightState
	for _, capability := range resp.Payload.Capabilities {
		capability.apply(&snapshot)
	}
	return snapshot
}

func (c stateCapability) apply(snapshot *gossiprpc.GoveeLightState) {
	switch c.capabilityID {
	case capabilityID{"devices.capabilities.online", "online"}:
		snapshot.Online = stateValue[bool](c.State.Value)
	case capabilityID{powerCapabilityType, powerInstance}:
		snapshot.Power = stateValue[int](c.State.Value) == 1
	case capabilityID{brightnessCapabilityType, brightnessInstance}:
		snapshot.Brightness = stateValue[int](c.State.Value)
	case capabilityID{colorCapabilityType, colorInstance}:
		snapshot.ColorRGB = stateValue[int](c.State.Value)
	case capabilityID{colorCapabilityType, kelvinInstance}:
		snapshot.Kelvin = stateValue[int](c.State.Value)
	}
}

func stateValue[T int | bool](raw codec.RawMessage) T {
	var value T
	_ = codec.Unmarshal(raw, &value)
	return value
}

type restoreLight struct {
	light gossiprpc.GoveeLight
	lease holdLease
	err   error
}

func (p *api) restore(ctx context.Context, req gossiprpc.Request) any {
	if msg := validateLightRequest(req); msg != "" {
		return gossiprpc.GoveeRestoreReply{Error: msg}
	}
	if strings.TrimSpace(req.HoldID) == "" {
		return gossiprpc.GoveeRestoreReply{Error: "missing hold id"}
	}
	broadcaster := strings.TrimSpace(req.ChannelID)
	lights, err := p.findHolds(ctx, broadcaster, req)
	if err != nil {
		return gossiprpc.GoveeRestoreReply{Error: friendlyControlError(err)}
	}
	if len(lights) == 0 {
		return gossiprpc.GoveeRestoreReply{OK: true}
	}
	return p.applyRestore(ctx, broadcaster, lights)
}

func (p *api) findHolds(ctx context.Context, broadcaster string, req gossiprpc.Request) ([]restoreLight, error) {
	var matches []restoreLight
	for _, light := range distinctLights(controlLights(req)) {
		var lease holdLease
		found, err := p.cache.GetJSON(ctx, holdKey(broadcaster, light.Device), &lease)
		if err != nil {
			return nil, err
		}
		if found && lease.Owner == req.HoldID {
			matches = append(matches, restoreLight{light: light, lease: lease})
		}
	}
	return matches, nil
}

func (p *api) applyRestore(ctx context.Context, broadcaster string, lights []restoreLight) gossiprpc.GoveeRestoreReply {
	key, msg := p.controlKey(ctx, broadcaster)
	if msg != "" {
		return gossiprpc.GoveeRestoreReply{Error: msg}
	}
	if err := p.enforceRate(ctx, broadcaster); err != nil {
		return p.restoreFailure(ctx, broadcaster, lights, err)
	}
	if err := p.driveRestore(ctx, broadcaster, authHeader(key), lights); err != nil {
		return p.restoreFailure(ctx, broadcaster, lights, err)
	}
	return p.finishRestore(ctx, broadcaster, lights)
}

func (p *api) driveRestore(ctx context.Context, broadcaster string, headers map[string]string, lights []restoreLight) error {
	var wg sync.WaitGroup
	for i, light := range lights {
		snapshot := light.lease.Snapshot
		if !snapshot.Online {
			continue
		}
		wg.Go(func() {
			steps := controlSteps(gossiprpc.Request{PowerOff: !snapshot.Power, Kelvin: snapshot.Kelvin, ColorRGB: snapshot.ColorRGB, Brightness: snapshot.Brightness})
			lights[i].err = p.driveLight(ctx, broadcaster, p.target(light.light, headers), steps)
		})
	}
	wg.Wait()
	for _, light := range lights {
		if rateLimited(light.err) {
			return light.err
		}
	}
	return nil
}

func rateLimited(err error) bool {
	var upstream *core.UpstreamError
	return errors.As(err, &upstream) && upstream.Status == http.StatusTooManyRequests
}

func (p *api) restoreFailure(ctx context.Context, broadcaster string, lights []restoreLight, err error) gossiprpc.GoveeRestoreReply {
	if !rateLimited(err) {
		return gossiprpc.GoveeRestoreReply{Error: friendlyControlError(err)}
	}
	for _, light := range lights {
		if err := p.cache.SetJSON(ctx, holdKey(broadcaster, light.light.Device), light.lease, p.retryAfter+holdGrace); err != nil {
			return gossiprpc.GoveeRestoreReply{Error: friendlyControlError(err)}
		}
	}
	return gossiprpc.GoveeRestoreReply{RateLimited: true, RetryAfterMS: p.retryAfter.Milliseconds()}
}

func (p *api) finishRestore(ctx context.Context, broadcaster string, lights []restoreLight) gossiprpc.GoveeRestoreReply {
	var restored int
	var failure error
	for _, light := range lights {
		if light.err != nil {
			failure = errors.Join(failure, light.err)
			continue
		}
		if light.lease.Snapshot.Online {
			restored++
		}
		failure = errors.Join(failure, p.cache.DelJSON(ctx, holdKey(broadcaster, light.light.Device)))
	}
	if failure != nil {
		return gossiprpc.GoveeRestoreReply{Error: friendlyControlError(failure), Restored: restored}
	}
	return gossiprpc.GoveeRestoreReply{OK: true, Restored: restored}
}
