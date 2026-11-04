// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package govee

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	devicesTTL  = 60 * time.Second
	scenesTTL   = 60 * time.Second
	negativeTTL = 10 * time.Second

	httpTimeout    = 6 * time.Second
	devicesTimeout = 8 * time.Second
	scenesTimeout  = 8 * time.Second
	controlTimeout = 12 * time.Second

	rateWindowSeconds = 60.0
	defaultRateLimit  = 8.0

	apiKeyHeader = "Govee-API-Key"

	devicesPath = "/router/api/v1/user/devices"
	controlPath = "/router/api/v1/device/control"

	scenesPath    = "/router/api/v1/device/scenes"
	diyScenesPath = "/router/api/v1/device/diy-scenes"

	powerCapabilityType = "devices.capabilities.on_off"
	powerInstance       = "powerSwitch"
	colorCapabilityType = "devices.capabilities.color_setting"
	colorInstance       = "colorRgb"

	dynamicSceneCapabilityType = "devices.capabilities.dynamic_scene"
	diySceneCapabilityType     = "devices.capabilities.diy_color_setting"
	lightSceneInstance         = "lightScene"
	diySceneInstance           = "diyScene"
	snapshotInstance           = "snapshot"
)

type Config struct {
	BaseURL   string
	RateLimit float64
}

const providerName = "govee"

type api struct {
	http    *core.HTTPClient
	cache   *core.Cache
	keys    provider.BroadcasterKeyResolver
	log     *zap.Logger
	limiter *ratelimit.Limiter

	buckets core.Buckets
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(cfg, d, b)
	b.Endpoint("devices").Timeout(devicesTimeout).Handle(p.devices)
	b.Endpoint("scenes").Timeout(scenesTimeout).Handle(p.scenes)
	b.Endpoint("control").Timeout(controlTimeout).Handle(p.control)
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps, b *provider.Builder) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://openapi.api.govee.com"
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = defaultRateLimit
	}
	return &api{
		http:    b.Client(base, nil, httpTimeout),
		cache:   d.Cache,
		keys:    d.GoveeKeys,
		log:     d.Logger(),
		limiter: d.Limiter,
		buckets: core.NewBuckets("ratelimit:gossip:govee", cfg.RateLimit, rateWindowSeconds),
	}
}

func (p *api) resolveKey(ctx context.Context, broadcasterID string) (string, error) {
	if strings.TrimSpace(broadcasterID) == "" {
		return "", nil
	}
	return p.keys.Key(ctx, broadcasterID)
}

type deviceListResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []struct {
		SKU          string `json:"sku"`
		Device       string `json:"device"`
		DeviceName   string `json:"deviceName"`
		Capabilities []struct {
			Type     string `json:"type"`
			Instance string `json:"instance"`
		} `json:"capabilities"`
	} `json:"data"`
}

func cachedBytesFor(fresh bool) func(context.Context, *core.Cache, string, func(context.Context) error, func(context.Context) ([]byte, time.Duration, error)) ([]byte, error) {
	if fresh {
		return core.CachedBytesFresh
	}
	return core.CachedBytes
}

func (p *api) devices(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	broadcaster := strings.TrimSpace(req.ChannelID)
	if broadcaster == "" {
		return gossiprpc.GoveeDevicesReply{Error: "missing channel"}
	}
	key, err := p.resolveKey(ctx, broadcaster)
	if err != nil {
		log.Warn("govee key resolve failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return gossiprpc.GoveeDevicesReply{Error: "could not read your Govee key"}
	}
	if key == "" {
		return gossiprpc.GoveeDevicesReply{Error: "no Govee API key on file"}
	}

	cacheKey := core.Key(providerName, "devices", broadcaster)
	b, err := cachedBytesFor(req.Fresh)(ctx, p.cache, cacheKey, nil, func(ctx context.Context) ([]byte, time.Duration, error) {
		b, ttl, _, err := core.BuildReplyWithMapper(ctx, devicesTTL, negativeTTL,
			p.fetchDevices(key),
			func(msg string) any { return gossiprpc.GoveeDevicesReply{Error: msg} },
			devicesErrMapper,
		)
		return b, ttl, err
	})
	if err == nil {
		return codec.RawMessage(b)
	}
	log.Warn("govee devices fetch failed", zap.String("broadcaster", broadcaster), zap.Error(err))
	if keyRejected(err) {
		return gossiprpc.GoveeDevicesReply{Error: "Govee did not accept this API key", KeyRejected: true}
	}
	return gossiprpc.GoveeDevicesReply{Error: "device lookup failed"}
}

func keyRejected(err error) bool {
	var ue *core.UpstreamError
	return errors.As(err, &ue) && (ue.Status == http.StatusUnauthorized || ue.Status == http.StatusForbidden)
}

func devicesErrMapper(err error) (string, core.Pin) {
	if keyRejected(err) {
		return "", core.PinNone
	}
	return core.FriendlyUpstream(err)
}

func (p *api) fetchDevices(key string) func(context.Context) (any, error) {
	return func(ctx context.Context) (any, error) {
		var resp deviceListResponse
		req := core.Request{Method: http.MethodGet, Path: devicesPath, Headers: authHeader(key)}
		if err := p.http.Do(ctx, req, &resp); err != nil {
			return nil, err
		}
		if err := goveeCodeError(resp.Code, resp.Message); err != nil {
			return nil, err
		}
		return buildDevicesReply(resp), nil
	}
}

func buildDevicesReply(resp deviceListResponse) gossiprpc.GoveeDevicesReply {
	out := gossiprpc.GoveeDevicesReply{Devices: make([]gossiprpc.GoveeDevice, 0, len(resp.Data))}
	for _, d := range resp.Data {
		color := false
		for _, c := range d.Capabilities {
			if c.Type == colorCapabilityType && c.Instance == colorInstance {
				color = true
				break
			}
		}
		out.Devices = append(out.Devices, gossiprpc.GoveeDevice{
			Device: d.Device,
			SKU:    d.SKU,
			Name:   d.DeviceName,
			Color:  color,
		})
	}
	return out
}

type sceneOption struct {
	Name  string           `json:"name"`
	Value codec.RawMessage `json:"value"`
}

type sceneCapability struct {
	Type       string `json:"type"`
	Instance   string `json:"instance"`
	Parameters struct {
		Options []sceneOption `json:"options"`
	} `json:"parameters"`
}

type sceneListRequest struct {
	RequestID string           `json:"requestId"`
	Payload   sceneListPayload `json:"payload"`
}

type sceneListPayload struct {
	SKU    string `json:"sku"`
	Device string `json:"device"`
}

type sceneListResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
	Payload struct {
		Capabilities []sceneCapability `json:"capabilities"`
	} `json:"payload"`
}

type sceneDeviceListResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []struct {
		SKU          string            `json:"sku"`
		Device       string            `json:"device"`
		Capabilities []sceneCapability `json:"capabilities"`
	} `json:"data"`
}

type scenePair struct {
	ParamID int64 `json:"paramId"`
	ID      int64 `json:"id"`
}

func (p *api) scenes(ctx context.Context, req gossiprpc.Request) any {
	broadcaster := strings.TrimSpace(req.ChannelID)
	if broadcaster == "" {
		return gossiprpc.GoveeScenesReply{Error: "missing channel"}
	}
	target := sceneListPayload{SKU: strings.TrimSpace(req.SKU), Device: strings.TrimSpace(req.Device)}
	if target.SKU == "" || target.Device == "" {
		return gossiprpc.GoveeScenesReply{Error: "missing device"}
	}
	key, msg := p.controlKey(ctx, broadcaster)
	if msg != "" {
		return gossiprpc.GoveeScenesReply{Error: msg}
	}

	cacheKey := core.Key(providerName, "scenes", core.CacheID(broadcaster, target.SKU, target.Device))
	b, err := cachedBytesFor(req.Fresh)(ctx, p.cache, cacheKey, nil, func(ctx context.Context) ([]byte, time.Duration, error) {
		b, ttl, _, err := core.BuildReplyWithMapper(ctx, scenesTTL, negativeTTL,
			p.fetchScenes(key, target),
			func(msg string) any { return gossiprpc.GoveeScenesReply{Error: msg} },
			scenesErrMapper,
		)
		return b, ttl, err
	})
	if err == nil {
		return codec.RawMessage(b)
	}
	monitor.TxnLogger(ctx, p.log).Warn("govee scenes fetch failed", zap.String("broadcaster", broadcaster), zap.Error(err))
	if keyRejected(err) {
		return gossiprpc.GoveeScenesReply{Error: "Govee did not accept this API key", KeyRejected: true}
	}
	return gossiprpc.GoveeScenesReply{Error: "scene lookup failed"}
}

func scenesErrMapper(err error) (string, core.Pin) {
	var ue *core.UpstreamError
	if errors.As(err, &ue) && ue.Status == http.StatusTooManyRequests {
		return "Govee is rate limiting scene lookups, try again in a minute", core.PinThrottle
	}
	return "", core.PinNone
}

func (p *api) fetchScenes(key string, target sceneListPayload) func(context.Context) (any, error) {
	return func(ctx context.Context) (any, error) {
		var dynamic, diy, listed []sceneCapability
		g, gctx := errgroup.WithContext(ctx)
		g.Go(func() (err error) {
			dynamic, err = p.fetchSceneList(gctx, key, scenesPath, target)
			return err
		})
		g.Go(func() (err error) {
			diy, err = p.fetchSceneList(gctx, key, diyScenesPath, target)
			return err
		})
		g.Go(func() (err error) {
			listed, err = p.fetchListedScenes(gctx, key, target)
			return err
		})
		if err := g.Wait(); err != nil {
			return nil, err
		}
		return buildScenesReply(dynamic, diy, listed), nil
	}
}

func (p *api) fetchSceneList(ctx context.Context, key, path string, target sceneListPayload) ([]sceneCapability, error) {
	body, err := codec.Marshal(sceneListRequest{RequestID: uuid.NewString(), Payload: target})
	if err != nil {
		return nil, err
	}
	var resp sceneListResponse
	req := core.Request{Method: http.MethodPost, Path: path, Headers: authHeader(key), Body: body}
	err = p.http.Do(ctx, req, &resp)
	if err == nil {
		err = goveeCodeError(resp.Code, resp.Message+resp.Msg)
	}
	if sceneKindUnsupported(err) {
		return nil, nil
	}
	return resp.Payload.Capabilities, err
}

func (p *api) fetchListedScenes(ctx context.Context, key string, target sceneListPayload) ([]sceneCapability, error) {
	var resp sceneDeviceListResponse
	req := core.Request{Method: http.MethodGet, Path: devicesPath, Headers: authHeader(key)}
	if err := p.http.Do(ctx, req, &resp); err != nil {
		return nil, err
	}
	if err := goveeCodeError(resp.Code, resp.Message); err != nil {
		return nil, err
	}
	for _, d := range resp.Data {
		if d.SKU == target.SKU && d.Device == target.Device {
			return d.Capabilities, nil
		}
	}
	return nil, nil
}

func sceneKindUnsupported(err error) bool {
	var ue *core.UpstreamError
	return errors.As(err, &ue) && (ue.Status == http.StatusBadRequest || ue.Status == http.StatusNotFound)
}

func buildScenesReply(lists ...[]sceneCapability) gossiprpc.GoveeScenesReply {
	out := gossiprpc.GoveeScenesReply{Scenes: []gossiprpc.GoveeScene{}}
	seen := map[string]struct{}{}
	for _, list := range lists {
		for _, c := range list {
			if !sceneCapabilityAllowed(c.Type, c.Instance) {
				continue
			}
			out.Scenes = appendSceneOptions(out.Scenes, seen, c)
		}
	}
	return out
}

func appendSceneOptions(scenes []gossiprpc.GoveeScene, seen map[string]struct{}, c sceneCapability) []gossiprpc.GoveeScene {
	for _, o := range c.Parameters.Options {
		_, id, ok := parseSceneValue(c.Instance, o.Value)
		if !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		scenes = append(scenes, gossiprpc.GoveeScene{ID: id, Name: o.Name, Type: c.Type, Instance: c.Instance, Value: o.Value})
	}
	return scenes
}

func sceneCapabilityAllowed(capType, instance string) bool {
	switch capType {
	case dynamicSceneCapabilityType:
		return instance == lightSceneInstance || instance == diySceneInstance || instance == snapshotInstance
	case diySceneCapabilityType:
		return instance == diySceneInstance
	}
	return false
}

func parseSceneValue(instance string, raw codec.RawMessage) (value any, id string, ok bool) {
	var number *int64
	if err := codec.Unmarshal(raw, &number); err == nil && number != nil && *number >= 0 {
		return *number, instance + ":" + strconv.FormatInt(*number, 10), true
	}
	var pair struct {
		ParamID *int64 `json:"paramId"`
		ID      *int64 `json:"id"`
	}
	if err := codec.Unmarshal(raw, &pair); err != nil || pair.ParamID == nil || pair.ID == nil || *pair.ParamID < 0 || *pair.ID < 0 {
		return nil, "", false
	}
	id = instance + ":" + strconv.FormatInt(*pair.ParamID, 10) + ":" + strconv.FormatInt(*pair.ID, 10)
	return scenePair{ParamID: *pair.ParamID, ID: *pair.ID}, id, true
}

type controlRequest struct {
	RequestID string         `json:"requestId"`
	Payload   controlPayload `json:"payload"`
}

type controlPayload struct {
	SKU        string            `json:"sku"`
	Device     string            `json:"device"`
	Capability controlCapability `json:"capability"`
}

type controlCapability struct {
	Type     string `json:"type"`
	Instance string `json:"instance"`
	Value    any    `json:"value"`
}

type controlResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (p *api) control(ctx context.Context, req gossiprpc.Request) any {
	if msg := validateControlInput(req); msg != "" {
		return gossiprpc.GoveeControlReply{Error: msg}
	}
	broadcaster := strings.TrimSpace(req.ChannelID)

	key, msg := p.controlKey(ctx, broadcaster)
	if msg != "" {
		return gossiprpc.GoveeControlReply{Error: msg}
	}
	if err := p.enforceRate(ctx, broadcaster); err != nil {
		return gossiprpc.GoveeControlReply{Error: friendlyControlError(err)}
	}

	target := goveeTarget{http: p.http, headers: authHeader(key), sku: strings.TrimSpace(req.SKU), device: strings.TrimSpace(req.Device)}
	for _, step := range controlSteps(req) {
		if err := target.set(ctx, step.capType, step.instance, step.value); err != nil {
			monitor.TxnLogger(ctx, p.log).Warn("govee control step failed", zap.String("broadcaster", broadcaster), zap.String("capability", step.capType), zap.Error(err))
			return gossiprpc.GoveeControlReply{Error: controlStepError(req, err)}
		}
	}
	return gossiprpc.GoveeControlReply{OK: true}
}

func validateControlInput(req gossiprpc.Request) string {
	if strings.TrimSpace(req.ChannelID) == "" {
		return "missing channel"
	}
	if strings.TrimSpace(req.Device) == "" || strings.TrimSpace(req.SKU) == "" {
		return "missing device"
	}
	if req.Scene != nil {
		return validateScene(req)
	}
	if !req.PowerOff && !validColorRGB(req.ColorRGB) {
		return "colour out of range"
	}
	return ""
}

func validateScene(req gossiprpc.Request) string {
	if req.PowerOff {
		return "scene cannot be combined with power off"
	}
	if !sceneCapabilityAllowed(req.Scene.Type, req.Scene.Instance) {
		return "invalid scene"
	}
	if _, _, ok := parseSceneValue(req.Scene.Instance, req.Scene.Value); !ok {
		return "invalid scene"
	}
	return ""
}

func validColorRGB(rgb int) bool {
	return rgb >= 0 && rgb <= 0xFFFFFF
}

func (p *api) controlKey(ctx context.Context, broadcaster string) (key, msg string) {
	key, err := p.resolveKey(ctx, broadcaster)
	if err != nil {
		p.log.Warn("govee key resolve failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return "", "could not read your Govee key"
	}
	if key == "" {
		return "", "no Govee API key on file"
	}
	return key, ""
}

func (p *api) enforceRate(ctx context.Context, broadcaster string) error {
	return p.buckets.WithKey("ratelimit:gossip:govee:"+broadcaster).Enforce(ctx, p.limiter, true)
}

type controlStep struct {
	capType  string
	instance string
	value    any
}

func controlSteps(req gossiprpc.Request) []controlStep {
	if req.PowerOff {
		return []controlStep{{powerCapabilityType, powerInstance, 0}}
	}
	if req.Scene != nil {
		value, _, _ := parseSceneValue(req.Scene.Instance, req.Scene.Value)
		return []controlStep{
			{powerCapabilityType, powerInstance, 1},
			{req.Scene.Type, req.Scene.Instance, value},
		}
	}
	return []controlStep{
		{powerCapabilityType, powerInstance, 1},
		{colorCapabilityType, colorInstance, req.ColorRGB},
	}
}

type goveeTarget struct {
	http    *core.HTTPClient
	headers map[string]string
	sku     string
	device  string
}

func (t goveeTarget) set(ctx context.Context, capType, instance string, value any) error {
	body, err := codec.Marshal(controlRequest{
		RequestID: uuid.NewString(),
		Payload: controlPayload{
			SKU:        t.sku,
			Device:     t.device,
			Capability: controlCapability{Type: capType, Instance: instance, Value: value},
		},
	})
	if err != nil {
		return err
	}
	var resp controlResponse
	req := core.Request{Method: http.MethodPost, Path: controlPath, Headers: t.headers, Body: body}
	if err := t.http.Do(ctx, req, &resp); err != nil {
		return err
	}
	return goveeCodeError(resp.Code, resp.Message)
}

func goveeCodeError(code int, message string) error {
	if code != 0 && code != 200 {
		return &core.UpstreamError{Status: code, Message: message}
	}
	return nil
}

func authHeader(key string) map[string]string {
	return map[string]string{apiKeyHeader: key}
}

func controlStepError(req gossiprpc.Request, err error) string {
	if req.Scene != nil && sceneKindUnsupported(err) {
		return "this light does not have that scene"
	}
	return friendlyControlError(err)
}

func friendlyControlError(err error) string {
	var ue *core.UpstreamError
	if errors.As(err, &ue) && ue.Status == 429 {
		return "too many light changes, slow down"
	}
	return "could not reach your lights"
}
