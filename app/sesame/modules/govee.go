package modules

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

// goveeModuleName is the ModuleView key. The dashboard's Govee tile links to
// its own inspector page, which writes this row: the enable toggle gates the
// module and the Configs blob carries the reward binding this handler reads.
const goveeModuleName = "govee"

// goveeConfig is the module's dashboard configuration. It names the one custom
// reward that drives the lights and which device to control. The Govee API key
// is deliberately absent: it is a secret, sealed in the modules service and
// fetched (decrypted) by the gateway at call time, so it never rides this blob
// (which is projected and cached in cleartext).
type goveeConfig struct {
	// RewardID is the Twitch custom reward id whose redemptions set the lights.
	RewardID string `json:"rewardId"`
	// Device / SKU identify the light to drive (id + model), as chosen from the
	// dashboard's device picker.
	Device string `json:"device"`
	SKU    string `json:"sku"`
	// OnRedeem is the resolution on a successful colour change: fulfill (default),
	// cancel (refund anyway), or leave (for a human mod). A rejection (bad colour,
	// upstream failure, or offline while live-only) always refunds regardless.
	OnRedeem string `json:"onRedeem"`
	// AllowOffline opts out of the live-only gate so redemptions drive the lights
	// even when the stream is offline. It defaults to false (live-only enforced)
	// so the safe posture is the zero value; the dashboard only sets it true
	// behind a warning, since it lets viewers control the lights any time.
	AllowOffline bool `json:"allowOffline"`
}

// Govee turns a channel-points redemption into a smart-light colour change. It
// is a named, opt-in module (KindOptIn): off by default, configured on its own
// dashboard inspector page where the broadcaster stores their Govee API key,
// picks a device and creates the reward. It leverages the same channel-points
// plumbing as the channelpoints module (a Twitch custom reward + the
// redemption.add event) but owns its own binding, so the two never collide: a
// reward is either bound here or there, never both.
//
// On a redemption of its bound reward it enforces live-only (refunding the
// points when the stream is offline), parses the colour the viewer typed (a
// name like "blue" or a hex like "#00ccff"), drives the light through the
// gateway's govee provider, and resolves the redemption in Twitch's queue.
func Govee(d engine.Deps) module.Module {
	m := module.NewModule(goveeModuleName, module.KindOptIn)
	m.On(redemptionAddType, goveeRedemption(d))
	return m.Build()
}

// goveeRedemption builds the redemption handler. It short-circuits to nil for
// anything that is not this module's configured reward, so an unconfigured or
// unrelated redemption costs one decode and nothing else.
func goveeRedemption(d engine.Deps) module.EventHandler {
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		if d.Gateway == nil || d.Live == nil {
			return nil
		}
		cfg, ev, ok := decodeGoveeRedemption(c)
		if !ok {
			return nil
		}
		if !goveeLivePermits(ctx, d, cfg, c.BroadcasterID) {
			goveeRefund(emit, ev, "the lights only change while live, your points were refunded")
			return nil
		}
		rgb, ok := parseColor(ev.UserInput)
		if !ok {
			goveeRefund(emit, ev, "didn't recognize that colour, your points were refunded (try a name like blue, or a hex like #00ccff)")
			return nil
		}

		var reply gatewayrpc.GoveeControlReply
		if err := d.Gateway.Call(ctx, "govee", "control", gatewayrpc.Request{
			ChannelID: ev.BroadcasterUserID,
			Device:    cfg.Device,
			SKU:       cfg.SKU,
			ColorRGB:  rgb,
		}, &reply); err != nil {
			goveeRefund(emit, ev, goveeFailureMessage(err))
			return nil
		}

		goveeChat(emit, ev, "set the lights to "+strings.TrimSpace(ev.UserInput)+"!")
		emitRedemptionStatus(emit, ev, goveeSuccessStatus(cfg.OnRedeem))
		return nil
	}
}

// decodeGoveeRedemption decodes the module config and the redemption event, and
// reports ok=false for anything that is not this module's configured reward: an
// unconfigured module, a non-redemption envelope, or a different reward id. The
// checks are kept as separate single conditions for readability.
func decodeGoveeRedemption(c *module.Context) (goveeConfig, redemptionEvent, bool) {
	var cfg goveeConfig
	_ = c.Decode(&cfg)
	if !goveeConfigured(cfg) || len(c.Env.Event) == 0 {
		return cfg, redemptionEvent{}, false
	}
	var ev redemptionEvent
	if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
		return cfg, ev, false
	}
	if ev.Reward.ID != cfg.RewardID || ev.BroadcasterUserID == "" {
		return cfg, ev, false
	}
	return cfg, ev, true
}

// goveeConfigured reports whether the module has a complete binding (reward +
// device). Written as sequential checks to avoid a compound conditional.
func goveeConfigured(cfg goveeConfig) bool {
	if cfg.RewardID == "" {
		return false
	}
	if cfg.Device == "" {
		return false
	}
	return cfg.SKU != ""
}

// goveeLivePermits reports whether the redemption may drive the lights now.
// Live-only is the default, safe posture; a broadcaster can opt out
// (allowOffline, gated behind a dashboard warning) to test off-stream. When
// enforced, a live-check error counts as "not confirmably live" so a transient
// projector hiccup refunds rather than driving lights off-stream.
func goveeLivePermits(ctx context.Context, d engine.Deps, cfg goveeConfig, broadcasterID uint64) bool {
	if cfg.AllowOffline {
		return true
	}
	live, err := d.Live.IsLive(ctx, broadcasterID)
	if err != nil {
		if d.Log != nil {
			d.Log.Warn("govee: live check failed, refunding", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		}
		return false
	}
	return live
}

// goveeFailureMessage turns a gateway failure into a chat-safe reason. A
// reply-level error (the provider's own friendly text: rate limited, no key on
// file) is surfaced; anything else stays generic so an outage leaks no detail.
func goveeFailureMessage(err error) string {
	var re bus.RPCReplyError
	if errors.As(err, &re) && re.Message != "" {
		return re.Message + ", your points were refunded"
	}
	return "couldn't reach your lights, your points were refunded"
}

// goveeRefund tells the viewer why and cancels the redemption (refunding the
// points) in one place, so every rejection path stays consistent.
func goveeRefund(emit module.Emit, ev redemptionEvent, reason string) {
	goveeChat(emit, ev, reason)
	emitRedemptionStatus(emit, ev, outgress.RedemptionCanceled)
}

// goveeChat posts a reply addressed to the redeemer.
func goveeChat(emit module.Emit, ev redemptionEvent, text string) {
	user := strings.TrimPrefix(displayName(ev.UserName, ev.UserLogin), "@")
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: ev.BroadcasterUserID,
		Text:          "@" + user + " " + text,
	})
}

// goveeSuccessStatus maps the binding's success policy to a redemption status.
func goveeSuccessStatus(onRedeem string) string {
	switch onRedeem {
	case onRedeemCancel:
		return outgress.RedemptionCanceled
	case onRedeemLeave:
		return "" // leave it UNFULFILLED for a human mod
	default:
		return outgress.RedemptionFulfilled
	}
}

// emitRedemptionStatus resolves the redemption in Twitch's queue. An empty
// status (the "leave" policy) emits nothing, leaving it for a human mod.
func emitRedemptionStatus(emit module.Emit, ev redemptionEvent, status string) {
	if status == "" {
		return
	}
	emit(&module.Output{
		Type:          outgress.TypeRedemptionUpdate,
		BroadcasterID: ev.BroadcasterUserID,
		RewardID:      ev.Reward.ID,
		RedemptionID:  ev.ID,
		Status:        status,
	})
}
