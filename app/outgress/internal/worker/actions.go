package worker

import (
	"ItsBagelBot/app/outgress/internal/action"
	"ItsBagelBot/internal/domain/outgress"
)

// buildActions declares every message type this worker executes, in the same
// fluent builder style sesame's modules use. The Registry it produces owns the
// Helix route per type, so producers send intent ("chat", "ban", "ad", "clip")
// plus the body instead of hardcoding paths; Build panics at boot on a
// misdeclared action. Handlers capture the worker by method value, so the set
// is built once per lane worker in New.
//
//   - chat/announce/shoutout/pin: cloud-bot chat actions on the app token.
//     Twitch only awards the Chat Bot badge to Send Chat Message calls made
//     with an app access token, backed by the bot's user:bot/action grants and
//     the broadcaster's channel:bot grant.
//   - ban/timeout/unban/shield_mode/delete/warn: the bot acts as moderator
//     (/helix/moderation/* → bot user token). Timeout shares ban's endpoint;
//     the body's duration makes it a timeout.
//   - ad/commercial/clip: the broadcaster's own grant starts the ad
//     (channel:edit:commercial) or creates the clip (clips:edit).
//   - api: generic passthrough; the message must carry its own endpoint.
//   - eventsub/stream_status/redemption_update: internal jobs whose handlers
//     own their Twitch calls (typed client methods, no route resolution here).
func (w *Worker) buildActions() action.Registry {
	b := action.NewSet()
	b.Action(outgress.TypeChat).Post("/helix/chat/messages").As(outgress.AsApp).Run(w.processChat)
	b.Action(outgress.TypeAnnounce).Post("/helix/chat/announcements").As(outgress.AsApp).Run(w.processAnnounce)
	b.Action(outgress.TypeShoutout).Post("/helix/chat/shoutouts").As(outgress.AsApp).Run(w.processShoutout)
	b.Action(outgress.TypePin).Put("/helix/chat/pins").As(outgress.AsApp).Run(w.processPin)
	b.Action(outgress.TypeBan).Post("/helix/moderation/bans").As(outgress.AsBot).Run(w.processBan)
	b.Action(outgress.TypeTimeout).Post("/helix/moderation/bans").As(outgress.AsBot).Run(w.processBan)
	b.Action(outgress.TypeUnban).Delete("/helix/moderation/bans").As(outgress.AsBot).Run(w.processAPI)
	b.Action(outgress.TypeShieldMode).Put("/helix/moderation/shield_mode").As(outgress.AsBot).Run(w.processShieldMode)
	b.Action(outgress.TypeDelete).Delete("/helix/moderation/chat").As(outgress.AsBot).Run(w.processDelete)
	b.Action(outgress.TypeWarn).Post("/helix/moderation/warnings").As(outgress.AsBot).Run(w.processWarn)
	b.Action(outgress.TypeAd).Post("/helix/channels/commercial").As(outgress.AsBroadcaster).Run(w.processAPI)
	b.Action(outgress.TypeCommercial).Post("/helix/channels/commercial").As(outgress.AsBroadcaster).Run(w.processAPI)
	b.Action(outgress.TypeClip).Post("/helix/clips").As(outgress.AsBroadcaster).Run(w.processClip)
	b.Action(outgress.TypeAPI).Passthrough().Run(w.processAPI)
	b.Action(outgress.TypeEventSub).Internal().Run(w.processEventSub)
	b.Action(outgress.TypeStreamStatus).Internal().Run(w.processStreamStatus)
	b.Action(outgress.TypeRedemptionUpdate).Internal().Run(w.processRedemptionUpdate)
	return b.Build()
}
