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
	// cancel (refund anyway), or leave (for a human mod). A rejection (offline,
	// bad colour, upstream failure) always refunds regardless.
	OnRedeem string `json:"onRedeem"`
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
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	m := module.NewModule(goveeModuleName, module.KindOptIn)
	m.On(redemptionAddType, goveeRedemption(d, log))
	return m.Build()
}

// goveeRedemption builds the redemption handler. It short-circuits to nil for
// anything that is not this module's configured reward, so an unconfigured or
// unrelated redemption costs one decode and nothing else.
func goveeRedemption(d engine.Deps, log *zap.Logger) module.EventHandler {
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		if d.Gateway == nil || d.Live == nil {
			return nil
		}

		var cfg goveeConfig
		_ = c.Decode(&cfg)
		if cfg.RewardID == "" || cfg.Device == "" || cfg.SKU == "" || len(c.Env.Event) == 0 {
			return nil
		}

		var ev redemptionEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.Reward.ID != cfg.RewardID || ev.BroadcasterUserID == "" {
			return nil
		}

		// Live only: a live-check error is treated as "not confirmably live" so a
		// transient projector hiccup refunds rather than driving lights off-stream.
		live, err := d.Live.IsLive(ctx, c.BroadcasterID)
		if err != nil || !live {
			if err != nil {
				log.Warn("govee: live check failed, refunding", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
			}
			goveeRefund(emit, ev, "the lights only change while live, your points were refunded")
			return nil
		}

		rgb, ok := parseColor(ev.UserInput)
		if !ok {
			goveeRefund(emit, ev, "didn't recognize that colour, your points were refunded (try a name like blue, or a hex like #00ccff)")
			return nil
		}

		var reply gatewayrpc.GoveeControlReply
		err = d.Gateway.Call(ctx, "govee", "control", gatewayrpc.Request{
			ChannelID: ev.BroadcasterUserID,
			Device:    cfg.Device,
			SKU:       cfg.SKU,
			ColorRGB:  rgb,
		}, &reply)
		if err != nil {
			goveeRefund(emit, ev, goveeFailureMessage(err))
			return nil
		}

		goveeChat(emit, ev, "set the lights to "+strings.TrimSpace(ev.UserInput)+"!")
		emitRedemptionStatus(emit, ev, goveeSuccessStatus(cfg.OnRedeem))
		return nil
	}
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
