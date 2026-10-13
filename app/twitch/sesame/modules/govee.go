// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const goveeModuleName = "govee"

// Never add the API key here: this blob is projected and cached in cleartext.
type goveeConfig struct {
	RewardID     string `json:"rewardId"`
	Device       string `json:"device"`
	SKU          string `json:"sku"`
	OnRedeem     string `json:"onRedeem"`
	AllowOffline bool   `json:"allowOffline"`
	AllowOff     bool   `json:"allowOff"`
	ReplyMessage string `json:"replyMessage"`
}

type goveeConfigs struct {
	Bindings []goveeConfig `json:"bindings"`
}

func bindingsOf(c *module.Context) []goveeConfig {
	var wrap goveeConfigs
	_ = c.Decode(&wrap)
	if len(wrap.Bindings) > 0 {
		return wrap.Bindings
	}
	var single goveeConfig
	_ = c.Decode(&single)
	if goveeConfigured(single) {
		return []goveeConfig{single}
	}
	return nil
}

func Govee(d engine.Deps) module.Module {
	m := module.NewModule(goveeModuleName, module.KindOptIn)
	m.On(redemptionAddType, goveeRedemption(d))
	return m.Build()
}

func goveeRedemption(d engine.Deps) module.EventHandler {
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		if d.Gossip == nil || d.Live == nil {
			return nil
		}
		cfg, ev, ok := decodeGoveeRedemption(c)
		if !ok {
			return nil
		}
		r := goveeRun{d: d, emit: emit, ev: ev, cfg: cfg, locale: c.Locale}
		if !goveeLivePermits(ctx, d, cfg, c.BroadcasterID) {
			r.refund("govee.refund.offline")
			return nil
		}
		r.apply(ctx)
		return nil
	}
}

type goveeRun struct {
	d      engine.Deps
	emit   module.Emit
	ev     redemptionEvent
	cfg    goveeConfig
	locale string
}

func (r goveeRun) apply(ctx context.Context) {
	req, label, ok := goveeIntent(r.cfg, strings.TrimSpace(r.ev.UserInput))
	if !ok {
		r.refund("govee.refund.colour")
		return
	}
	if err := r.control(ctx, req); err != nil {
		r.refund(goveeFailureKey(err))
		return
	}
	r.chat(renderGoveeReply(r.locale, r.cfg.ReplyMessage, r.ev, label))
	emitRedemptionStatus(r.emit, r.ev, goveeSuccessStatus(r.cfg.OnRedeem))
}

func (r goveeRun) control(ctx context.Context, req gossiprpc.Request) error {
	req.ChannelID = r.ev.BroadcasterUserID
	req.Device = r.cfg.Device
	req.SKU = r.cfg.SKU
	var reply gossiprpc.GoveeControlReply
	return r.d.Gossip.Call(ctx, engine.GossipRoute{Provider: "govee", Endpoint: "control"}, req, &reply)
}

func goveeIntent(cfg goveeConfig, input string) (gossiprpc.Request, string, bool) {
	if cfg.AllowOff && isOffInput(input) {
		return gossiprpc.Request{PowerOff: true}, "off", true
	}
	rgb, ok := parseColor(input)
	if !ok {
		return gossiprpc.Request{}, "", false
	}
	return gossiprpc.Request{ColorRGB: rgb}, input, true
}

func isOffInput(input string) bool {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "off", "turn off", "lights off", "light off":
		return true
	default:
		return false
	}
}

const defaultGoveeReply = "@{govee:user} set the lights to {govee:color}!"

func renderGoveeReply(locale, text string, ev redemptionEvent, color string) string {
	if strings.TrimSpace(text) == "" {
		text = defaultGoveeReply
	}
	user := strings.TrimPrefix(displayName(ev.UserName, ev.UserLogin), "@")
	return module.KV(
		"user", user,
		"color", color,
		"input", sanitizeRewardInput(ev.UserInput),
	).WithLocale(module.Locale(locale)).WithNamespace("govee").ExpandString(text)
}

func decodeGoveeRedemption(c *module.Context) (goveeConfig, redemptionEvent, bool) {
	bindings := bindingsOf(c)
	if len(bindings) == 0 || len(c.Env.Event) == 0 {
		return goveeConfig{}, redemptionEvent{}, false
	}
	var ev redemptionEvent
	if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
		return goveeConfig{}, ev, false
	}
	if ev.BroadcasterUserID == "" {
		return goveeConfig{}, ev, false
	}
	for _, b := range bindings {
		if goveeConfigured(b) && b.RewardID == ev.Reward.ID {
			return b, ev, true
		}
	}
	return goveeConfig{}, ev, false
}

func goveeConfigured(cfg goveeConfig) bool {
	if cfg.RewardID == "" {
		return false
	}
	if cfg.Device == "" {
		return false
	}
	return cfg.SKU != ""
}

func goveeLivePermits(ctx context.Context, d engine.Deps, cfg goveeConfig, broadcasterID uint64) bool {
	if cfg.AllowOffline {
		return true
	}
	live, err := d.Live.IsLive(ctx, broadcasterID)
	if err != nil {
		if d.Log != nil {
			d.Log.Warn("govee: live check failed, refunding", module.BIDField(broadcasterID), zap.Error(err))
		}
		return false
	}
	return live
}

var goveeFailureKeys = map[string]string{
	"no Govee API key on file":          "govee.refund.setup",
	"too many light changes, slow down": "govee.refund.busy",
}

func goveeFailureKey(err error) string {
	var re bus.RPCReplyError
	if errors.As(err, &re) {
		if key, ok := goveeFailureKeys[re.Message]; ok {
			return key
		}
	}
	return "govee.refund.unreachable"
}

func (r goveeRun) refund(key string) {
	r.chat(renderGoveeReply(r.locale, i18n.T(r.locale, key), r.ev, ""))
	emitRedemptionStatus(r.emit, r.ev, outgress.RedemptionCanceled)
}

func (r goveeRun) chat(text string) {
	r.emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: r.ev.BroadcasterUserID,
		Text:          text,
	})
}

func goveeSuccessStatus(onRedeem string) string {
	switch onRedeem {
	case onRedeemCancel:
		return outgress.RedemptionCanceled
	case onRedeemLeave:
		return ""
	default:
		return outgress.RedemptionFulfilled
	}
}

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
