// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mail

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/i18n"
)

type GrantMessage struct {
	To             string
	ExpiresAt      time.Time
	IdempotencyKey string
	Locale         string
}

func (m *Mailer) SendGrant(ctx context.Context, msg GrantMessage) error {
	if m == nil {
		return ErrDisabled
	}
	if msg.ExpiresAt.IsZero() {
		return ErrInvalidMessage
	}
	html, err := renderPremium(grantEnvelope(msg, m.dashboardURL))
	if err != nil {
		return ErrInvalidMessage
	}
	_, err = m.send(ctx, renderedMessage{
		To: msg.To, Subject: i18n.T(msg.Locale, "mail.subject.grant"), HTML: html,
		Text: grantText(msg, m.dashboardURL), IdempotencyKey: msg.IdempotencyKey,
	})
	return err
}

func grantEnvelope(msg GrantMessage, dashboardURL string) premiumData {
	until := emailDate(msg.Locale, msg.ExpiresAt)
	chrome := mailChrome(msg.Locale)
	return premiumData{
		Title: i18n.T(msg.Locale, "mail.grant.title"), Preheader: fmt.Sprintf(i18n.T(msg.Locale, "mail.grant.preheader"), until),
		Category: i18n.T(msg.Locale, "mail.grant.category"), Heading: i18n.T(msg.Locale, "mail.grant.heading"),
		StatusLine: i18n.T(msg.Locale, "mail.gift.status"), StatusColor: colorGreen,
		StatusDetail: fmt.Sprintf(i18n.T(msg.Locale, "mail.grant.detail"), until),
		PerkPeriod:   i18n.T(msg.Locale, "mail.grant.perk"), ActionLabel: i18n.T(msg.Locale, "mail.gift.action"),
		Footer:       i18n.T(msg.Locale, "mail.grant.footer"),
		DashboardURL: dashboardURL, Gift: &giftData{}, Locale: msg.Locale,
		GiftIntro: i18n.T(msg.Locale, "mail.grant.intro"), GiftNoStrings: chrome["mail.gift.no_strings"],
		PriorityLabel: chrome["mail.priority.label"], PriorityBody: chrome["mail.priority.body"],
		PerksLabel: chrome["mail.perks.label"], BetaLabel: chrome["mail.beta.label"], BetaBody: chrome["mail.beta.body"],
		SubscriptionLabel: chrome["mail.subscription"], SafetyLabel: chrome["mail.safety.label"], SafetyBody: chrome["mail.safety.body"],
		Signature:     i18n.T(msg.Locale, "mail.signature"),
		ProductPeriod: i18n.T(msg.Locale, "mail.grant.product"),
	}
}

func grantText(msg GrantMessage, dashboardURL string) string {
	data := grantEnvelope(msg, dashboardURL)
	lines := []string{
		data.Title, "",
		data.GiftIntro + " " + data.ProductPeriod + ".",
		data.StatusDetail,
		i18n.T(msg.Locale, "mail.gift.active_body"),
		data.BetaLabel + " " + data.BetaBody, "",
		data.ActionLabel + ": " + dashboardURL, "",
		data.SafetyLabel + " " + data.SafetyBody + " itsbagelbot.com.",
		"", data.Footer, supportURL, "",
	}
	return strings.Join(lines, "\n")
}
