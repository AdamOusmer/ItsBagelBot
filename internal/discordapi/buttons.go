// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

const (
	ButtonPrimary   = 1
	ButtonSecondary = 2
	ButtonSuccess   = 3
	ButtonDanger    = 4

	CustomTicketOpen  = "bagel:ticket:open"
	CustomTicketClaim = "bagel:ticket:claim"
	CustomTicketClose = "bagel:ticket:close"
	CustomVoiceLock   = "bagel:voice:lock"
	CustomVoiceUnlock = "bagel:voice:unlock"
	CustomDailyClaim  = "bagel:crumbs:daily"
)

// TicketDeskButtons is the persistent Open ticket control on the support
// embed. The label is the streamer's (Config.TicketPanel().Button) rather
// than a constant: the panel embed is fully editable from the dashboard, and
// a button reading "Open a ticket" under a panel that says "Contact the mod
// team" is the one part of that card that would not follow the copy.
func TicketDeskButtons(label string) []Button {
	if label == "" {
		label = "Open a ticket"
	}
	return []Button{{Style: ButtonPrimary, Label: label, CustomID: CustomTicketOpen}}
}

// TicketOpenButtons are the Claim and Close controls inside a private ticket.
func TicketOpenButtons() []Button {
	return []Button{
		{Style: ButtonSecondary, Label: "Claim", CustomID: CustomTicketClaim},
		{Style: ButtonDanger, Label: "Close ticket", CustomID: CustomTicketClose},
	}
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
