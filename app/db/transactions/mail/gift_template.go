// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mail

import (
	"fmt"
	"html/template"
	"strings"
)

type giftData struct {
	Gifter    string
	Message   template.HTML
	NoteLabel string
}

// giftMessageHTML escapes the buyer's note for the HTML email and keeps line
// breaks as <br>. The result is a template.HTML so the template inserts it
// verbatim — escaping must happen here, never rely on the note being safe.
func giftMessageHTML(msg string) template.HTML {
	escaped := template.HTMLEscapeString(msg)
	escaped = strings.ReplaceAll(escaped, "\n", "<br>")
	return template.HTML(escaped) //nolint:gosec // escaped just above
}

func giftHTML(giftedByLogin, personalMessage, dashboardURL string) (string, error) {

	preheader := "A month of ItsBagelBot Premium just landed on your account."
	if giftedByLogin != "" {
		preheader = fmt.Sprintf("%s sent you a month of ItsBagelBot Premium. It's live now.", giftedByLogin)
	}

	data := giftData{Gifter: giftedByLogin}
	if personalMessage != "" {
		data.Message = giftMessageHTML(personalMessage)
		data.NoteLabel = "A note for you"
		if giftedByLogin != "" {
			data.NoteLabel = giftedByLogin + " wrote"
		}
	}

	return renderPremium(premiumData{
		Title: "You've got Premium", Preheader: preheader,
		Category: "Gift", Heading: "Someone just made your day.",
		StatusLine: "Active on your account now", StatusColor: colorGreen,
		StatusDetail: "Your Premium gift was activated automatically.",
		PerkPeriod:   "Unlocked for the whole month.", ActionLabel: "See what's new",
		Footer:       "Sent by ItsBagelBot because you received a Premium gift.",
		DashboardURL: dashboardURL, Gift: &data,
	})
}

func giftText(giftedByLogin, personalMessage, dashboardURL string) string {

	who := "You've been gifted"
	if giftedByLogin != "" {
		who = giftedByLogin + " gifted you"
	}

	note := ""
	if personalMessage != "" {
		label := "A note for you"
		if giftedByLogin != "" {
			label = giftedByLogin + " wrote"
		}
		note = fmt.Sprintf("%s:\n%q\n\n", label, personalMessage)
	}

	return fmt.Sprintf(`You've got Premium

%s 1 month of ItsBagelBot Premium.
The priority lane and every premium perk are active on your account now.
%s

%sSee what's new: %s

Staying safe. %s
Discord: %s

Sent by ItsBagelBot because you received a Premium gift.
https://itsbagelbot.com
`, who, betaAccessText, note, dashboardURL, safetyNotice, discordURL)
}
