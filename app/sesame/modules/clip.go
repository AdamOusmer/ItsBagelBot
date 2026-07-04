package modules

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
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
// trigger absorbs (NumericSuffix). Twitch's Create Clip API cannot set a clip
// title or a custom duration (clips are a fixed recent window, titled later on
// Twitch's edit page), so <title> and N are cosmetic on Twitch's side; the title
// is echoed back in the chat reply outgress posts with the clip URL.
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
// emits one TypeClip outgress action carrying the title and the clipper's login;
// outgress creates the clip and posts the resulting public URL (with the title)
// back to chat, since the Create Clip response — and thus the URL — is only
// visible to outgress, never here.
func clipRun(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if !clipEnabled(ctx, d, c.BroadcasterID, log) {
			return nil
		}
		emit(&module.Output{
			Type:          outgress.TypeClip,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          strings.TrimSpace(args), // clip title, echoed in the reply
			To:            c.Env.ChatterUserLogin,   // the clipper, named in the reply
		})
		return nil
	}
}

// clipEnabled reports whether the broadcaster has the built-in clip command on.
// The toggle lives in the modules service under clipModuleName; a missing row
// means default-on (the built-in ships enabled). Read lazily here, not on the
// hot chat path. On a projection error it fails open (allows the clip): a
// transient read blip should not silently swallow a viewer's clip.
func clipEnabled(ctx context.Context, d engine.Deps, broadcasterID uint64, log *zap.Logger) bool {
	if d.Proj == nil {
		return true
	}
	views, err := d.Proj.Modules(ctx, broadcasterID)
	if err != nil {
		log.Warn("clip: module state read failed, allowing",
			zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return true
	}
	for _, v := range views {
		if v.Name == clipModuleName {
			return v.IsEnabled
		}
	}
	return true
}
