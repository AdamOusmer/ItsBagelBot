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
	"net/http"
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

	// httpTimeout bounds one Govee call. Trimmed from 10s: Govee answers a
	// healthy request in ~1-2s, and the byte-flow cache now serves the device
	// list stale-while-revalidate, so a caller almost never waits on the wire.
	httpTimeout = 6 * time.Second
	// devicesTimeout bounds the whole devices handler (key resolve + one HTTP
	// call). Kept under the dashboard's 9s RPC budget so the caller always
	// outlasts the handler instead of abandoning a fetch it is still running.
	devicesTimeout = 8 * time.Second
	// controlTimeout is looser: a redemption fires two sequential Govee calls
	// (power, then colour) plus the key resolve.
	controlTimeout = 12 * time.Second

	// Govee's per-key budget window and a conservative per-broadcaster ceiling.
	// Govee documents ~10 requests/minute for device control; control spends two
	// upstream calls (power, colour), so the ceiling is set on the redemption
	// action, not the raw HTTP call.
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
		{Name: "devices", Timeout: devicesTimeout, Handle: p.devices},
		{Name: "control", Timeout: controlTimeout, Handle: p.control},
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
				req := core.Request{Method: http.MethodGet, Path: devicesPath, Headers: authHeader(key)}
				if err := p.http.Do(ctx, req, &resp); err != nil {
					return nil, err
				}
				if err := goveeCodeError(resp.Code, resp.Message); err != nil {
					return nil, err
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
	if msg := validateControlInput(req); msg != "" {
		return gatewayrpc.GoveeControlReply{Error: msg}
	}
	broadcaster := strings.TrimSpace(req.ChannelID)

	key, msg := p.controlKey(ctx, broadcaster)
	if msg != "" {
		return gatewayrpc.GoveeControlReply{Error: msg}
	}
	if err := p.enforceRate(ctx, broadcaster); err != nil {
		return gatewayrpc.GoveeControlReply{Error: friendlyControlError(err)}
	}

	target := goveeTarget{http: p.http, headers: authHeader(key), sku: strings.TrimSpace(req.SKU), device: strings.TrimSpace(req.Device)}
	for _, step := range controlSteps(req) {
		if err := target.set(ctx, step.capType, step.instance, step.value); err != nil {
			p.log.Warn("govee control step failed", zap.String("broadcaster", broadcaster), zap.String("capability", step.capType), zap.Error(err))
			return gatewayrpc.GoveeControlReply{Error: friendlyControlError(err)}
		}
	}
	return gatewayrpc.GoveeControlReply{OK: true}
}

// validateControlInput returns a reply error message for a malformed control
// request, or "" when it is well-formed. Splitting the checks keeps any single
// condition simple.
func validateControlInput(req gatewayrpc.Request) string {
	if strings.TrimSpace(req.ChannelID) == "" {
		return "missing channel"
	}
	if strings.TrimSpace(req.Device) == "" || strings.TrimSpace(req.SKU) == "" {
		return "missing device"
	}
	// A power-off carries no colour; only validate the colour on a colour set.
	if !req.PowerOff && !validColorRGB(req.ColorRGB) {
		return "colour out of range"
	}
	return ""
}

// validColorRGB reports whether rgb is a packed 24-bit colour (0..0xFFFFFF).
func validColorRGB(rgb int) bool {
	return rgb >= 0 && rgb <= 0xFFFFFF
}

// controlKey resolves the broadcaster's key, returning ("", msg) with a
// chat-safe reason when it cannot be used.
func (p *Provider) controlKey(ctx context.Context, broadcaster string) (key, msg string) {
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

// enforceRate spends one action from the broadcaster's own key budget. One
// redemption is one action even though control costs two upstream calls.
func (p *Provider) enforceRate(ctx context.Context, broadcaster string) error {
	buckets := core.NewBuckets("ratelimit:gateway:govee:"+broadcaster, p.rateLimit, rateWindowSeconds)
	return buckets.Enforce(ctx, p.deps.Limiter, true)
}

// controlStep is one capability set in a control sequence.
type controlStep struct {
	capType  string
	instance string
	value    any
}

// controlSteps is the ordered capability sequence one redemption runs. A colour
// set powers the device on then sets the colour; a power-off is the single
// off step (Govee's power capability with value 0).
func controlSteps(req gatewayrpc.Request) []controlStep {
	if req.PowerOff {
		return []controlStep{{powerCapabilityType, powerInstance, 0}}
	}
	return []controlStep{
		{powerCapabilityType, powerInstance, 1},
		{colorCapabilityType, colorInstance, req.ColorRGB},
	}
}

// goveeTarget is one device to drive under one broadcaster's key, so setting a
// capability needs only the capability itself.
type goveeTarget struct {
	http    *core.HTTPClient
	headers map[string]string
	sku     string
	device  string
}

// set posts one Govee capability and verifies both the HTTP status (via core)
// and the API-level code in the body.
func (t goveeTarget) set(ctx context.Context, capType, instance string, value any) error {
	body, err := json.Marshal(controlRequest{
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

// goveeCodeError maps Govee's API-level status code (which can report failure
// even on an HTTP 200) to an *UpstreamError; 0 and 200 are success.
func goveeCodeError(code int, message string) error {
	if code != 0 && code != 200 {
		return &core.UpstreamError{Status: code, Message: message}
	}
	return nil
}

// authHeader is the per-request Govee key header.
func authHeader(key string) map[string]string {
	return map[string]string{apiKeyHeader: key}
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
