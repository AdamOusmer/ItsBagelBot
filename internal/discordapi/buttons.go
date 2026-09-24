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

func TicketDeskButtons(label string) []Button {
	if label == "" {
		label = "Open a ticket"
	}
	return []Button{{Style: ButtonPrimary, Label: label, CustomID: CustomTicketOpen}}
}

func TicketOpenButtons() []Button {
	return []Button{
		{Style: ButtonSecondary, Label: "Claim", CustomID: CustomTicketClaim},
		{Style: ButtonDanger, Label: "Close ticket", CustomID: CustomTicketClose},
	}
}

func VoiceRoomButtons() []Button {
	return []Button{
		{Style: ButtonDanger, Label: "Lock", CustomID: CustomVoiceLock},
		{Style: ButtonSuccess, Label: "Unlock", CustomID: CustomVoiceUnlock},
	}
}

func DailyClaimButtons() []Button {
	return []Button{{Style: ButtonPrimary, Label: "Claim daily", CustomID: CustomDailyClaim}}
}
