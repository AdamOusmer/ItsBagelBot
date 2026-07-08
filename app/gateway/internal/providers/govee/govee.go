// Package govee is the gateway provider for the Govee smart-light Developer
// API (openapi.api.govee.com). Unlike the stats providers it holds no service
// API key of its own: every call authenticates with the broadcaster's own key,
// resolved just-in-time from the modules service (provider.Deps.GoveeKeys) by
// the broadcaster id the caller passes as Request.ChannelID. The gateway is the
// one door to the internet, so the plaintext key lives only inside one handler
// run and is never cached or logged.
//
// Two endpoints back the "set my lights to a colour" channel-points reward:
// "devices" lists the broadcaster's controllable lights for the dashboard's
// picker, and "control" powers a chosen device on and sets its colour.
package govee

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
	"ItsBagelBot/app/gateway/internal/provider"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	// devicesTTL caches the device list briefly: it rarely changes, but the
	// dashboard picker re-reads it on every page load, so a short window spares
	// the broadcaster's Govee quota without stranding a freshly added light.
	devicesTTL  = 60 * time.Second
	negativeTTL = 10 * time.Second

	httpTimeout    = 10 * time.Second
	handlerTimeout = 12 * time.Second

	// Govee's per-key budget window and a conservative per-broadcaster ceiling.
	// Govee documents ~10 requests/minute for device control; control spends two
	// upstream calls (power, colour), so the ceiling is set on the redemption
	// action, not the raw HTTP call.
	rateWindowSeconds = 60.0
	defaultRateLimit  = 8.0

	apiKeyHeader = "Govee-API-Key"

	powerCapabilityType = "devices.capabilities.on_off"
	powerInstance       = "powerSwitch"
	colorCapabilityType = "devices.capabilities.color_setting"
	colorInstance       = "colorRgb"
)

// Config carries the provider's environment: the Govee base URL and the
// per-broadcaster request ceiling. There is no APIKey — keys are per caller.
type Config struct {
	BaseURL   string
	RateLimit float64
}

// Provider implements provider.Provider for the Govee Developer API.
type Provider struct {
	http  *core.HTTPClient
	cache *core.Cache
	keys  provider.GoveeKeyResolver
	log   *zap.Logger

	deps      provider.Deps
	rateLimit float64
}

// New builds the govee provider. d.GoveeKeys must be non-nil (providers.All
// skips the provider otherwise, since with no key resolver it can authenticate
// nothing).
func New(cfg Config, d provider.Deps) *Provider {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://openapi.api.govee.com"
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = defaultRateLimit
	}
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Provider{
		// No baked auth header: the key rides per request (see controlHeaders).
		http:  core.NewHTTPClient(base, nil, httpTimeout),
		cache: d.Cache,
		keys:  d.GoveeKeys,
		log:   log,

		deps:      d,
		rateLimit: cfg.RateLimit,
	}
}

func (p *Provider) Name() string { return "govee" }

func (p *Provider) Endpoints() []provider.Endpoint {
	return []provider.Endpoint{
		{Name: "devices", Timeout: handlerTimeout, Handle: p.devices},
		{Name: "control", Timeout: handlerTimeout, Handle: p.control},
	}
}

// resolveKey pulls the broadcaster's decrypted key. An empty key (none on file)
// is not an error here: it is a friendly "not set up" the handlers map to a
// reply-level error.
func (p *Provider) resolveKey(ctx context.Context, broadcasterID string) (string, error) {
	if strings.TrimSpace(broadcasterID) == "" {
		return "", nil
	}
	return p.keys.Key(ctx, broadcasterID)
}

// --- devices -----------------------------------------------------------------

// deviceListResponse is the subset of Govee's GET /user/devices reply we read.
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

func (p *Provider) devices(ctx context.Context, req gatewayrpc.Request) any {
	broadcaster := strings.TrimSpace(req.ChannelID)
	if broadcaster == "" {
		return gatewayrpc.GoveeDevicesReply{Error: "missing channel"}
	}
	key, err := p.resolveKey(ctx, broadcaster)
	if err != nil {
		p.log.Warn("govee key resolve failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return gatewayrpc.GoveeDevicesReply{Error: "could not read your Govee key"}
	}
	if key == "" {
		return gatewayrpc.GoveeDevicesReply{Error: "no Govee API key on file"}
	}

	cacheKey := core.Key(p.Name(), "devices", broadcaster)
	b, err := core.CachedBytes(ctx, p.cache, cacheKey, func(ctx context.Context) ([]byte, time.Duration, error) {
		return core.BuildReply(ctx, devicesTTL, negativeTTL,
			func(ctx context.Context) (any, error) {
				var resp deviceListResponse
				if err := p.http.GetJSONWithHeaders(ctx, "/router/api/v1/user/devices", nil, map[string]string{apiKeyHeader: key}, &resp); err != nil {
					return nil, err
				}
				if resp.Code != 0 && resp.Code != 200 {
					return nil, &core.UpstreamError{Status: resp.Code, Message: resp.Message}
				}
				return buildDevicesReply(resp), nil
			},
			func(msg string) any { return gatewayrpc.GoveeDevicesReply{Error: msg} },
		)
	})
	if err != nil {
		p.log.Warn("govee devices fetch failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return gatewayrpc.GoveeDevicesReply{Error: "device lookup failed"}
	}
	return json.RawMessage(b)
}

// buildDevicesReply shapes the Govee device list into the wire reply, flagging
// which devices advertise the RGB colour capability the reward drives.
func buildDevicesReply(resp deviceListResponse) gatewayrpc.GoveeDevicesReply {
	out := gatewayrpc.GoveeDevicesReply{Devices: make([]gatewayrpc.GoveeDevice, 0, len(resp.Data))}
	for _, d := range resp.Data {
		color := false
		for _, c := range d.Capabilities {
			if c.Type == colorCapabilityType && c.Instance == colorInstance {
				color = true
				break
			}
		}
		out.Devices = append(out.Devices, gatewayrpc.GoveeDevice{
			Device: d.Device,
			SKU:    d.SKU,
			Name:   d.DeviceName,
			Color:  color,
		})
	}
	return out
}

// --- control -----------------------------------------------------------------

// controlRequest is Govee's POST /device/control body. One capability per call.
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

// controlResponse is the subset of Govee's control reply we check: an API-level
// code that can fail even on an HTTP 200.
type controlResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (p *Provider) control(ctx context.Context, req gatewayrpc.Request) any {
	broadcaster := strings.TrimSpace(req.ChannelID)
	device := strings.TrimSpace(req.Device)
	sku := strings.TrimSpace(req.SKU)
	if broadcaster == "" || device == "" || sku == "" {
		return gatewayrpc.GoveeControlReply{Error: "missing device"}
	}
	if req.ColorRGB < 0 || req.ColorRGB > 0xFFFFFF {
		return gatewayrpc.GoveeControlReply{Error: "colour out of range"}
	}

	key, err := p.resolveKey(ctx, broadcaster)
	if err != nil {
		p.log.Warn("govee key resolve failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return gatewayrpc.GoveeControlReply{Error: "could not read your Govee key"}
	}
	if key == "" {
		return gatewayrpc.GoveeControlReply{Error: "no Govee API key on file"}
	}

	// One redemption is one action against the broadcaster's own key budget,
	// even though it costs two upstream calls; enforce once, per broadcaster.
	buckets := core.NewBuckets("ratelimit:gateway:govee:"+broadcaster, p.rateLimit, rateWindowSeconds)
	if err := buckets.Enforce(ctx, p.deps.Limiter, true); err != nil {
		return gatewayrpc.GoveeControlReply{Error: friendlyControlError(err)}
	}

	headers := map[string]string{apiKeyHeader: key}
	if err := p.sendCapability(ctx, headers, sku, device, powerCapabilityType, powerInstance, 1); err != nil {
		p.log.Warn("govee power on failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return gatewayrpc.GoveeControlReply{Error: friendlyControlError(err)}
	}
	if err := p.sendCapability(ctx, headers, sku, device, colorCapabilityType, colorInstance, req.ColorRGB); err != nil {
		p.log.Warn("govee set colour failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return gatewayrpc.GoveeControlReply{Error: friendlyControlError(err)}
	}
	return gatewayrpc.GoveeControlReply{OK: true}
}

// sendCapability posts one Govee capability set (power, colour) and verifies
// both the HTTP status (via core) and the API-level code in the body.
func (p *Provider) sendCapability(ctx context.Context, headers map[string]string, sku, device, capType, instance string, value any) error {
	body, err := json.Marshal(controlRequest{
		RequestID: uuid.NewString(),
		Payload: controlPayload{
			SKU:        sku,
			Device:     device,
			Capability: controlCapability{Type: capType, Instance: instance, Value: value},
		},
	})
	if err != nil {
		return err
	}
	var resp controlResponse
	if err := p.http.PostJSON(ctx, "/router/api/v1/device/control", headers, body, &resp); err != nil {
		return err
	}
	if resp.Code != 0 && resp.Code != 200 {
		return &core.UpstreamError{Status: resp.Code, Message: resp.Message}
	}
	return nil
}

// friendlyControlError maps an upstream failure to a short chat-safe message.
// A 429 is the broadcaster hammering their own lights; anything else is a
// generic "could not reach your lights" so a Govee outage never leaks detail.
func friendlyControlError(err error) string {
	var ue *core.UpstreamError
	if errors.As(err, &ue) && ue.Status == 429 {
		return "too many light changes, slow down"
	}
	return "could not reach your lights"
}
