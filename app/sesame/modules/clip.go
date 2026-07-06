package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// Twitch's Create Clip API accepts a duration from 5 to 60 seconds inclusive; a
// request outside that range is rejected, so the inline number is clamped into
// it here rather than sent raw.
const (
	clipMinDuration = 5
	clipMaxDuration = 60
)

// clipCooldown is the shared per-channel window on !clip so a single viewer (or
// the whole chat) cannot spam clip creation and blow the broadcaster's Helix
// budget. Twitch also rate-limits clip creation server-side; this is the polite
// first line.
const clipCooldown = 15 * time.Second

// clipModuleName is the modules-service key that stores the per-broadcaster
// on/off state for the built-in !clip command. It is deliberately NOT in the
// dashboard MODULE_CATALOG (so it never shows on the modules page); it surfaces
// on the commands page as a built-in command instead. Absent row means on.
const clipModuleName = "clip"

// Clip owns the built-in !clip command. It is a core module (always indexed, no
// per-message ModuleView fetch on the hot chat path), but the command is
// toggleable per broadcaster: clipRun reads the "clip" module toggle lazily,
// only when !clip is actually typed, so a channel that never clips pays nothing.
//
// It is live-only: Twitch's Create Clip API rejects an offline channel, so the
// command gate skips it when the broadcaster is not streaming.
//
// !clip <title> and !clip<N> <title> both work: N is an inline number the
// trigger absorbs (NumericSuffix) and sets the clip length in seconds. Twitch's
// Create Clip API takes a duration from 5 to 60 (default 30); N is clamped into
// that range and passed through outgress, and <title> becomes the clip's title.
// Both are echoed back in the chat reply outgress posts with the clip URL.
func Clip(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule("", module.KindCore)
	m.Command("clip").Everyone().LiveOnly().NumericSuffix().Cooldown(clipCooldown).Run(clipRun(d, log))
	return m.Build()
}

// clipRun creates a clip for the broadcaster when the built-in is enabled. It
// emits one TypeClip outgress action carrying the title, the requested duration,
// the clipper's login, and the broadcaster's custom reply template (if any);
// outgress creates the clip and posts the resulting public URL back to chat
// (expanding the template's {clip} token), since the Create Clip response — and
// thus the URL — is only visible to outgress, never here.
func clipRun(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		enabled, reply := clipSettings(ctx, d, c.BroadcasterID, log)
		if !enabled {
			return nil
		}
		emit(&module.Output{
			Type:          outgress.TypeClip,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          strings.TrimSpace(args), // clip title, sent to Twitch and echoed
			To:            c.Env.ChatterUserLogin,  // the clipper, named in the reply
			Duration:      clipDuration(c.Num),     // inline !clipN, clamped to Twitch's 5–60
			Template:      reply,                   // custom reply template; empty = default
		})
		return nil
	}
}

// clipDuration turns the inline numeric suffix ("30" for !clip30) into a Twitch
// Create Clip duration in seconds. An empty suffix (plain !clip) returns 0,
// which outgress omits so Twitch applies its default (30). Any typed value is
// clamped to Twitch's accepted 5–60 range so an out-of-range number (!clip3,
// !clip90) still creates a clip at the nearest legal length instead of being
// rejected. A number too large to parse (overflow) is treated as the maximum.
func clipDuration(num string) float64 {
	if num == "" {
		return 0
	}
	n, err := strconv.Atoi(num)
	if err != nil || n > clipMaxDuration {
		return clipMaxDuration
	}
	if n < clipMinDuration {
		return clipMinDuration
	}
	return float64(n)
}

// clipConfig is the built-in clip command's per-broadcaster config, stored in
// the modules-service "clip" row's config blob alongside the on/off toggle.
// Reply is the custom chat-reply template the broadcaster set on the dashboard;
// empty means outgress falls back to the default reply format. The template's
// {clip} token is expanded to the clip URL by outgress (only it knows the URL).
type clipConfig struct {
	Reply string `json:"reply"`
}

// clipSettings reads the built-in clip command's per-broadcaster state from the
// modules service: whether it is enabled and the custom reply template (if any).
// The state lives under clipModuleName; a missing row means default-on with no
// custom template (the built-in ships enabled). Read lazily here, not on the hot
// chat path. On a projection error it fails open (enabled, no template): a
// transient read blip must not silently swallow a viewer's clip.
func clipSettings(ctx context.Context, d engine.Deps, broadcasterID uint64, log *zap.Logger) (enabled bool, reply string) {
	if d.Proj == nil {
		return true, ""
	}
	views, err := d.Proj.Modules(ctx, broadcasterID)
	if err != nil {
		log.Warn("clip: module state read failed, allowing",
			zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return true, ""
	}
	for _, v := range views {
		if v.Name == clipModuleName {
			var cfg clipConfig
			if len(v.Configs) > 0 {
				_ = json.Unmarshal(v.Configs, &cfg)
			}
			return v.IsEnabled, strings.TrimSpace(cfg.Reply)
		}
	}
	return true, ""
}
