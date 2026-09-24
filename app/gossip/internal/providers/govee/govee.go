// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package govee

import (
	"context"
	"errors"
	"net/http"
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
)

const (
	devicesTTL  = 60 * time.Second
	negativeTTL = 10 * time.Second

	httpTimeout    = 6 * time.Second
	devicesTimeout = 8 * time.Second
	controlTimeout = 12 * time.Second

	rateWindowSeconds = 60.0
	defaultRateLimit  = 8.0

	apiKeyHeader = "Govee-API-Key"

	devicesPath = "/router/api/v1/user/devices"
	controlPath = "/router/api/v1/device/control"

	powerCapabilityType = "devices.capabilities.on_off"
	powerInstance       = "powerSwitch"
	colorCapabilityType = "devices.capabilities.color_setting"
	colorInstance       = "colorRgb"
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
	b, err := core.CachedBytes(ctx, p.cache, cacheKey, nil, func(ctx context.Context) ([]byte, time.Duration, error) {
		b, ttl, _, err := core.BuildReply(ctx, devicesTTL, negativeTTL,
			func(ctx context.Context) (any, error) {
				var resp deviceListResponse
				req := core.Request{Method: http.MethodGet, Path: devicesPath, Headers: authHeader(key)}
				if err := p.http.Do(ctx, req, &resp); err != nil {
					return nil, err
				}
				if err := goveeCodeError(resp.Code, resp.Message); err != nil {
					return nil, err
				}
				return buildDevicesReply(resp), nil
			},
			func(msg string) any { return gossiprpc.GoveeDevicesReply{Error: msg} },
		)
		return b, ttl, err
	})
	if err != nil {
		log.Warn("govee devices fetch failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return gossiprpc.GoveeDevicesReply{Error: "device lookup failed"}
	}
	return codec.RawMessage(b)
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
			return gossiprpc.GoveeControlReply{Error: friendlyControlError(err)}
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
	if !req.PowerOff && !validColorRGB(req.ColorRGB) {
		return "colour out of range"
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

func friendlyControlError(err error) string {
	var ue *core.UpstreamError
	if errors.As(err, &ue) && ue.Status == 429 {
		return "too many light changes, slow down"
	}
	return "could not reach your lights"
}
