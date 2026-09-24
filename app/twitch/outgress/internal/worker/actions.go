// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"ItsBagelBot/app/twitch/outgress/internal/action"
	"ItsBagelBot/internal/domain/outgress"
)

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
	b.Action(outgress.TypeCommercial).Post("/helix/channels/commercial").As(outgress.AsBroadcaster).Run(w.processCommercial)
	b.Action(outgress.TypeClip).Post("/helix/clips").As(outgress.AsBroadcaster).Run(w.processClip)
	b.Action(outgress.TypeChannelUpdate).Internal().Run(w.processChannelUpdate)
	b.Action(outgress.TypeStreamMarker).Post("/helix/streams/markers").As(outgress.AsBroadcaster).Run(w.processMarker)
	b.Action(outgress.TypeAPI).Passthrough().Run(w.processAPI)
	b.Action(outgress.TypeEventSub).Internal().Run(w.processEventSub)
	b.Action(outgress.TypeStreamStatus).Internal().Run(w.processStreamStatus)
	b.Action(outgress.TypeRedemptionUpdate).Internal().Run(w.processRedemptionUpdate)
	return b.Build()
}
