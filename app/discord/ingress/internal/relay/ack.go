// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package relay

import (
	"context"

	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/pkg/codec"
)

// deferredResponseType is Discord's type 5, DEFERRED_CHANNEL_MESSAGE_WITH_SOURCE.
// Every interaction this bot handles -- a slash command or a button press --
// completes with a brand new (often ephemeral) message rather than an edit
// of whatever triggered it, so one defer type covers both: type 6
// (DEFERRED_UPDATE_MESSAGE) would instead put the button's OWN message
// (the ticket desk, the voice-room panel) into a loading state, which is
// wrong for a panel meant to stay clickable for the next person.
const deferredResponseType = 5

// interactionHeader is the minimal shape every interaction payload carries,
// enough to ack it without decoding anything the engine will decode itself.
type interactionHeader struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

// deferInteraction acknowledges the interaction before its 3s deadline. A
// malformed payload or a REST failure both return an error so Dispatch drops
// the event instead of publishing work for a reply that can never complete
// (a followup against an un-deferred interaction is just another failure,
// louder).
func (r *Relay) deferInteraction(ctx context.Context, raw []byte) error {
	var in interactionHeader
	if err := codec.Unmarshal(raw, &in); err != nil {
		return err
	}
	if r.REST == nil {
		return nil
	}
	return r.REST.InteractionCallback(ctx, discordapi.Callback{
		Interaction: discordapi.Interaction{ID: in.ID, Token: in.Token},
		Type:        deferredResponseType,
	})
}
