// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mail

import (
	"fmt"
	"html/template"
	"strings"

	"ItsBagelBot/internal/domain/i18n"
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
	return giftHTMLLocale(giftedByLogin, personalMessage, dashboardURL, "")
}

func giftHTMLLocale(giftedByLogin, personalMessage, dashboardURL, locale string) (string, error) {

	preheader := fmt.Sprintf(i18n.T(locale, "mail.gift.preheader"), i18n.T(locale, "mail.month.one"))
	if giftedByLogin != "" {
		preheader = fmt.Sprintf(i18n.T(locale, "mail.gift.preheader.by"), giftedByLogin, i18n.T(locale, "mail.month.one"))
	}

	data := giftData{Gifter: giftedByLogin}
	if personalMessage != "" {
		data.Message = giftMessageHTML(personalMessage)
		data.NoteLabel = i18n.T(locale, "mail.note")
		if giftedByLogin != "" {
			data.NoteLabel = fmt.Sprintf(i18n.T(locale, "mail.note.by"), giftedByLogin)
		}
	}

	chrome := mailChrome(locale)
	intro := chrome["mail.gift.intro"]
	if giftedByLogin == "" {
		intro = i18n.T(locale, "mail.gift.intro.anonymous")
	}
	return renderPremium(premiumData{
		Title: i18n.T(locale, "mail.gift.title"), Preheader: preheader,
		Category: i18n.T(locale, "mail.gift.category"), Heading: i18n.T(locale, "mail.gift.heading"),
		StatusLine: i18n.T(locale, "mail.gift.status"), StatusColor: colorGreen,
		StatusDetail: i18n.T(locale, "mail.gift.detail"),
		PerkPeriod:   i18n.T(locale, "mail.gift.perk"), ActionLabel: i18n.T(locale, "mail.gift.action"),
		Footer:       i18n.T(locale, "mail.gift.footer"),
		DashboardURL: dashboardURL, Gift: &data, Locale: locale,
		GiftIntro: intro, GiftNoStrings: chrome["mail.gift.no_strings"],
		PriorityLabel: chrome["mail.priority.label"], PriorityBody: chrome["mail.priority.body"],
		PerksLabel: chrome["mail.perks.label"], BetaLabel: chrome["mail.beta.label"], BetaBody: chrome["mail.beta.body"],
		SubscriptionLabel: chrome["mail.subscription"], SafetyLabel: chrome["mail.safety.label"], SafetyBody: chrome["mail.safety.body"],
		Signature:     i18n.T(locale, "mail.signature"),
		ProductPeriod: fmt.Sprintf(i18n.T(locale, "mail.product_period"), localizedPrizeMonths(locale, 1)),
	})
}

func giftText(giftedByLogin, personalMessage, dashboardURL string) string {
	return giftTextLocale(giftedByLogin, personalMessage, dashboardURL, "")
}

func giftTextLocale(giftedByLogin, personalMessage, dashboardURL, locale string) string {
	who := i18n.T(locale, "mail.gift.intro.anonymous")
	if giftedByLogin != "" {
		who = giftedByLogin + " " + i18n.T(locale, "mail.gift.intro")
	}
	lines := []string{
		i18n.T(locale, "mail.gift.title"), "",
		who + " " + fmt.Sprintf(i18n.T(locale, "mail.product_period"), localizedPrizeMonths(locale, 1)) + ".",
		i18n.T(locale, "mail.gift.active_body"),
		i18n.T(locale, "mail.beta.label") + " " + i18n.T(locale, "mail.beta.body"), "",
	}
	if personalMessage != "" {
		label := i18n.T(locale, "mail.note")
		if giftedByLogin != "" {
			label = fmt.Sprintf(i18n.T(locale, "mail.note.by"), giftedByLogin)
		}
		lines = append(lines, fmt.Sprintf("%s:\n%q", label, personalMessage), "")
	}
	lines = append(lines,
		i18n.T(locale, "mail.gift.action")+": "+dashboardURL, "",
		i18n.T(locale, "mail.safety.label")+" "+i18n.T(locale, "mail.safety.body")+" Discord.",
		"Discord: "+discordURL, "", i18n.T(locale, "mail.gift.footer"), "https://itsbagelbot.com", "",
	)
	return strings.Join(lines, "\n")
}
