// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

const (
	ButtonPrimary   = 1
	ButtonSecondary = 2
	ButtonSuccess   = 3
	ButtonDanger    = 4

	CustomTicketOpen  = "bagel:ticket:open"
	CustomTicketClose = "bagel:ticket:close"
	CustomVoiceLock   = "bagel:voice:lock"
	CustomVoiceUnlock = "bagel:voice:unlock"
	CustomDailyClaim  = "bagel:crumbs:daily"
)

// TicketDeskButtons is the persistent Open ticket control on the support embed.
func TicketDeskButtons() []Button {
	return []Button{{Style: ButtonPrimary, Label: "Open a ticket", CustomID: CustomTicketOpen}}
}

// TicketCloseButtons is the Close control inside a private ticket.
func TicketCloseButtons() []Button {
	return []Button{{Style: ButtonDanger, Label: "Close ticket", CustomID: CustomTicketClose}}
}

// VoiceRoomButtons is Lock/Unlock on a join-to-create clone.
func VoiceRoomButtons() []Button {
	return []Button{
		{Style: ButtonDanger, Label: "Lock", CustomID: CustomVoiceLock},
		{Style: ButtonSuccess, Label: "Unlock", CustomID: CustomVoiceUnlock},
	}
}

// DailyClaimButtons is the Claim daily control on a rank card.
func DailyClaimButtons() []Button {
	return []Button{{Style: ButtonPrimary, Label: "Claim daily", CustomID: CustomDailyClaim}}
}
